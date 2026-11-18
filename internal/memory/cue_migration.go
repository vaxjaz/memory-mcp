package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
)

type MigrationEntry struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
}

type MigrationReport struct {
	FromSchema   int              `json:"from_schema"`
	TargetSchema int              `json:"target_schema"`
	Applied      bool             `json:"applied"`
	Backup       string           `json:"backup,omitempty"`
	Convertible  int              `json:"structured_entries_with_cues"`
	NeedsCues    []MigrationEntry `json:"current_entries_needing_cues"`
}

func InspectCueMigration(ctx context.Context, dir string) (MigrationReport, error) {
	report := MigrationReport{TargetSchema: SchemaVersion, NeedsCues: []MigrationEntry{}}
	store, err := OpenReadOnly(dir)
	if err != nil {
		return report, err
	}
	defer store.Close()
	if err := store.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&report.FromSchema); err != nil {
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
       e.current,coalesce(json_array_length(p.profile_json,'$.experience.cues'),json_array_length(p.profile_json,'$.agreement.cues'),0)
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
		var cues int
		if err := rows.Scan(&entry.ID, &entry.Scope, &entry.Title, &entry.Kind, &current, &cues); err != nil {
			return report, err
		}
		if cues > 0 {
			report.Convertible++
		} else if current {
			report.NeedsCues = append(report.NeedsCues, entry)
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
