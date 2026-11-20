package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/matching"
)

type MigrationEntry struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
}

type MigrationReport struct {
	FromSchema int `json:"from_schema"`
	// LegacyQualifierEntries are current records whose necessary qualifiers may
	// live only in the legacy narrative fields. They stay readable with their
	// full stored shape; slim them only after verifying the original evidence.
	LegacyQualifierEntries []MigrationEntry `json:"current_entries_with_legacy_narrative_qualifiers"`
	// CueFactsBeyondBody are current records whose cue text carries facts not
	// contained in the body core; a new-form revision must not drop them.
	CueFactsBeyondBody []MigrationEntry `json:"current_entries_with_cue_facts_beyond_body"`
	// MissingEvidence lists structured records without locators; verify these
	// manually before relying on them.
	MissingEvidence   []MigrationEntry `json:"structured_entries_missing_evidence"`
	TargetSchema      int              `json:"target_schema"`
	Applied           bool             `json:"applied"`
	Backup            string           `json:"backup,omitempty"`
	Convertible       int              `json:"structured_entries_with_cues"`
	NeedsCues         []MigrationEntry `json:"current_entries_needing_cues"`
}

func InspectCueMigration(ctx context.Context, dir string) (MigrationReport, error) {
	report := MigrationReport{TargetSchema: SchemaVersion, NeedsCues: []MigrationEntry{},
		LegacyQualifierEntries: []MigrationEntry{}, CueFactsBeyondBody: []MigrationEntry{}, MissingEvidence: []MigrationEntry{}}
	store, err := OpenReadOnly(dir)
	if err != nil {
		return report, err
	}
	defer store.Close()
	if report.FromSchema, err = store.LibrarySchema(ctx); err != nil {
		return report, err
	}
	if report.FromSchema > SchemaVersion {
		return report, fmt.Errorf("database schema %d exceeds supported schema %d", report.FromSchema, SchemaVersion)
	}
	var exists int
	if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='entry_profiles'").Scan(&exists); err != nil || exists == 0 {
		return report, err
	}
	rows, err := store.db.QueryContext(ctx, `
SELECT e.id,e.scope,e.title,CASE WHEN json_type(p.profile_json,'$.experience')='object' THEN 'experience' ELSE 'agreement' END,
       e.current,coalesce(json_array_length(p.profile_json,'$.experience.cues'),json_array_length(p.profile_json,'$.agreement.cues'),0),
       coalesce(p.profile_json,'')
FROM entry_profiles p JOIN entries e ON e.id=p.entry_id
WHERE json_type(p.profile_json,'$.experience')='object' OR json_type(p.profile_json,'$.agreement')='object'
ORDER BY e.scope,e.id`)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry MigrationEntry
		var current bool
		var cueCount int
		var profileJSON string
		if err := rows.Scan(&entry.ID, &entry.Scope, &entry.Title, &entry.Kind, &current, &cueCount, &profileJSON); err != nil {
			return report, err
		}
		if cueCount > 0 {
			report.Convertible++
		} else if current {
			report.NeedsCues = append(report.NeedsCues, entry)
		}
		if !current {
			continue
		}
		var profile Profile
		if err := json.Unmarshal([]byte(profileJSON), &profile); err != nil {
			continue
		}
		if profile.Experience == nil && profile.Agreement == nil {
			continue
		}
		var evidence []ExperienceEvidence
		var cues []ExperienceCue
		core := ""
		if x := profile.Experience; x != nil {
			evidence, cues = x.Evidence, x.Cues
			core = x.Pitfall + x.Counterexample + x.Fact + x.Impact + x.Applicability + x.Boundary
			if strings.TrimSpace(x.Impact) != "" || strings.TrimSpace(x.Applicability) != "" || strings.TrimSpace(x.Boundary) != "" {
				report.LegacyQualifierEntries = append(report.LegacyQualifierEntries, entry)
			}
		} else if a := profile.Agreement; a != nil {
			evidence, cues = a.Evidence, a.Cues
			core = a.Requirement + a.Applicability + a.Boundary
		}
		if len(evidence) == 0 {
			report.MissingEvidence = append(report.MissingEvidence, entry)
		}
		for _, cue := range cues {
			if strings.TrimSpace(cue.Text) != "" && !strings.Contains(core, cue.Text) {
				report.CueFactsBeyondBody = append(report.CueFactsBeyondBody, entry)
				break
			}
		}
	}
	return report, rows.Err()
}

func ApplyCueMigration(ctx context.Context, dir, backup string) (MigrationReport, error) {
	report, err := InspectCueMigration(ctx, dir)
	if err != nil || report.FromSchema == SchemaVersion {
		return report, err
	}
	if backup == "" {
		return report, errors.New("migration requires --backup FILE pointing to a new SQLite backup")
	}
	backup, err = filepath.Abs(backup)
	if err != nil {
		return report, err
	}
	store, err := openStore(dir, 10000, backup)
	if err != nil {
		return report, err
	}
	if err := store.Close(); err != nil {
		return report, err
	}
	report.Applied = true
	report.Backup = backup
	return report, nil
}

// Upgrade stored shapes once. Cues are evidence-backed, so old recognition
// terms do not become active entrances by mechanical conversion.
func migrateCueProfiles(tx *sql.Tx, from int) error {
	for _, key := range []string{"experience", "agreement"} {
		prefix := "$." + key
		old, next := prefix+".anchor_prefixes", prefix+".object_id_prefixes"
		if _, err := tx.Exec(`
UPDATE entry_profiles SET profile_json=json_remove(
  CASE WHEN json_type(profile_json,?)='array' AND json_type(profile_json,?) IS NULL
       THEN json_set(profile_json,?,json_extract(profile_json,?)) ELSE profile_json END, ?,?,?)
WHERE json_type(profile_json,?)='object'`, old, next, next, old, prefix+".anchors", prefix+".signals", old, prefix); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
UPDATE entries SET needs_review=1 WHERE current=1 AND id IN(
  SELECT entry_id FROM entry_profiles
  WHERE (json_type(profile_json,'$.experience')='object' AND coalesce(json_array_length(profile_json,'$.experience.cues'),0)=0)
     OR (json_type(profile_json,'$.agreement')='object' AND coalesce(json_array_length(profile_json,'$.agreement.cues'),0)=0))`); err != nil {
		return err
	}
	if from == 5 {
		for _, query := range []string{
			"DELETE FROM business_observations",
			"DELETE FROM experience_challenges WHERE completed=0",
		} {
			if _, err := tx.Exec(query); err != nil {
				return err
			}
		}
	}
	return nil
}

func migrateGroupedCues(tx *sql.Tx) error {
	// This table is derived. migrateRecall rebuilds the new group-based index
	// in the same transaction as the profile and schema changes.
	if _, err := tx.Exec("DROP TABLE IF EXISTS experience_entrances"); err != nil {
		return err
	}
	cursor := ""
	for {
		rows, err := tx.Query(`
SELECT entry_id,profile_json FROM entry_profiles WHERE entry_id>? AND
(json_type(profile_json,'$.experience.cues')='array' OR json_type(profile_json,'$.agreement.cues')='array')
ORDER BY entry_id LIMIT 128`, cursor)
		if err != nil {
			return err
		}
		var batch [][2]string
		for rows.Next() {
			var row [2]string
			if err = rows.Scan(&row[0], &row[1]); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(batch) == 0 {
			return err
		}
		for _, row := range batch {
			cursor = row[0]
			raw, changed, err := groupProfileCues([]byte(row[1]))
			if err != nil {
				return fmt.Errorf("migrate cue entrances for %s: %w", row[0], err)
			}
			if changed {
				if _, err = tx.Exec("UPDATE entry_profiles SET profile_json=? WHERE entry_id=?", string(raw), row[0]); err != nil {
					return err
				}
			}
		}
	}
}

func groupProfileCues(raw []byte) ([]byte, bool, error) {
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(raw, &profile); err != nil {
		return nil, false, err
	}
	changed := false
	for _, field := range []string{"experience", "agreement"} {
		if len(profile[field]) == 0 {
			continue
		}
		var owner map[string]json.RawMessage
		if err := json.Unmarshal(profile[field], &owner); err != nil {
			return nil, false, err
		}
		if len(owner["cues"]) == 0 {
			continue
		}
		var cues []map[string]json.RawMessage
		if err := json.Unmarshal(owner["cues"], &cues); err != nil {
			return nil, false, err
		}
		converted := false
		for _, cue := range cues {
			old, exists := cue["entrances"]
			if !exists {
				continue
			}
			if _, exists := cue["matches"]; exists {
				return nil, false, errors.New("cue contains both entrance schemas")
			}
			var entries []struct {
				Kind  string `json:"kind"`
				Value string `json:"value"`
			}
			if err := json.Unmarshal(old, &entries); err != nil {
				return nil, false, err
			}
			groups := make([]CueMatch, 0, len(entries))
			for _, entrance := range entries {
				rule := comparisonForOldEntrance(entrance.Kind, entrance.Value)
				// Keep order, so the reported matching alias stays the same.
				last := len(groups) - 1
				if last >= 0 && groups[last].Rule == rule {
					if !slices.Contains(groups[last].Entrances, entrance.Value) {
						groups[last].Entrances = append(groups[last].Entrances, entrance.Value)
					}
				} else {
					groups = append(groups, CueMatch{Rule: rule, Entrances: []string{entrance.Value}})
				}
			}
			cue["matches"], _ = json.Marshal(groups)
			delete(cue, "entrances")
			converted = true
		}
		if converted {
			owner["cues"], _ = json.Marshal(cues)
			profile[field], _ = json.Marshal(owner)
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(profile)
	return out, true, err
}

func comparisonForOldEntrance(kind, value string) matching.Rule {
	if kind == "business_signal" {
		return matching.Rule{Op: "token", Normalize: "whitespace"}
	}
	if strings.ContainsAny(value, "/\\") {
		return matching.Rule{Op: "contains", Normalize: "path"}
	}
	for _, char := range value {
		if char > 127 {
			return matching.Rule{Op: "contains"}
		}
	}
	return matching.Rule{Op: "token"}
}
