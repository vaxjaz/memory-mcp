package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode"
)

// An observed compaction invalidates bookkeeping; it does not claim that the
// host exposes the actual model context or that all pruning emits this event.
func (s *Store) ExperienceAfterCompact(ctx context.Context, host, scope, session string) error {
	return s.ExperienceContextReset(ctx, host, scope, session, "")
}

// A historic result cannot satisfy a condition on a later call's named
// fields or facts. Only plain text entrances can use this result association.
func plainObservationEntrance(cue ExperienceCue, match ExperienceMatch) bool {
	for _, group := range cue.Matches {
		if group.When == nil && group.Field == "" && group.Op != "" && slices.Contains(group.Entrances, match.Entrance) &&
			entranceComparison(group.Rule) == match.EntranceComparison {
			return true
		}
	}
	return false
}
func (s *Store) ExperienceContextReset(ctx context.Context, host, scope, session, eventID string) error {
	generation := experienceNonce()
	if generation == "" {
		return errors.New("cannot create context epoch")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if eventID != "" {
		result, e := tx.ExecContext(ctx, "INSERT OR IGNORE INTO experience_context_events VALUES(?,?,?,?)", host, scope, session, eventID)
		if e != nil {
			return e
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return tx.Commit()
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM business_observations WHERE host=? AND scope=? AND session=?", host, scope, session); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO experience_epochs VALUES(?,?,?,?,?) ON CONFLICT(host,scope,session)DO UPDATE SET generation=excluded.generation", host, scope, session, generation, ""); err != nil {
		return err
	}
	return tx.Commit()
}

// Only hashes of identifiers selected by an explicitly registered prefix are
// retained. No prompt, tool result or transcript body is persisted.
func observedObjects(clues string, prefixes []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(clues), func(r rune) bool {
		return !(r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'))
	}) {
		for _, prefix := range prefixes {
			if len(word) > len(prefix) && strings.HasPrefix(word, strings.ToLower(prefix)) {
				h := digestText(word)
				if !seen[h] {
					seen[h] = true
					out = append(out, h)
				}
				if len(out) == 4 {
					return out
				}
				break
			}
		}
	}
	return out
}

func (s *Store) ObserveBusiness(ctx context.Context, host, scope, session, nativeTurn, clues string) error {
	return s.ObserveBusinessRecords(ctx, host, scope, session, nativeTurn, []string{clues})
}

// Each record must contain its own action and a single unambiguous object for
// this experience. Never combine an action in one result row with another row's
// identifier. The persisted cache contains only indexed IDs and object hashes.
func (s *Store) ObserveBusinessRecords(ctx context.Context, host, scope, session, nativeTurn string, records []string) error {
	var generation, current string
	err := s.db.QueryRowContext(ctx, "SELECT generation,native_turn FROM experience_epochs WHERE host=? AND scope=? AND session=?", host, scope, session).Scan(&generation, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if nativeTurn != "" && nativeTurn != current {
		return nil
	}
	kept := []ExperienceMatch{}
	positions := map[[2]string]int{}
	for _, clues := range records {
		matches, err := s.MatchExperiences(ctx, scope, clues)
		if err != nil {
			return err
		}
		for _, m := range matches {
			e, er := s.Read(ctx, m.ID)
			if er != nil {
				return er
			}
			if !slices.ContainsFunc(entryCues(e.Entry), func(cue ExperienceCue) bool {
				return cue.ID == m.CueID && plainObservationEntrance(cue, m)
			}) {
				continue
			}
			prefixes := entryObjectPrefixes(e.Entry)
			m.Objects = observedObjects(clues, prefixes)
			if len(m.Objects) != 1 {
				continue
			}
			key := [2]string{m.ID, m.CueID}
			if at, exists := positions[key]; exists {
				found := false
				for _, object := range kept[at].Objects {
					if object == m.Objects[0] {
						found = true
						break
					}
				}
				if !found {
					kept[at].Objects = append(kept[at].Objects, m.Objects[0])
				}
			} else {
				positions[key] = len(kept)
				kept = append(kept, m)
			}
		}
	}
	if len(kept) == 0 {
		return nil
	}
	b, _ := json.Marshal(kept)
	// The input may change while matching. A compare-and-write prevents an old
	// in-flight observation from being inserted after the new input cleared it.
	_, err = s.db.ExecContext(ctx, `INSERT INTO business_observations SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM experience_epochs WHERE host=? AND scope=? AND session=? AND generation=? AND native_turn=?) ON CONFLICT(host,scope,session) DO UPDATE SET generation=excluded.generation,matches_json=excluded.matches_json`, host, scope, session, generation, string(b), host, scope, session, generation, current)
	return err
}

func (s *Store) ObservedBusinessMatches(ctx context.Context, host, scope, session, nativeTurn, clues string) ([]ExperienceMatch, error) {
	var b, generation, current string
	err := s.db.QueryRowContext(ctx, `SELECT o.matches_json,e.generation,e.native_turn FROM business_observations o JOIN experience_epochs e ON e.host=o.host AND e.scope=o.scope AND e.session=o.session AND e.generation=o.generation WHERE o.host=? AND o.scope=? AND o.session=?`, host, scope, session).Scan(&b, &generation, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if nativeTurn != "" && nativeTurn != current {
		return nil, nil
	}
	var matches, out []ExperienceMatch
	if err = json.Unmarshal([]byte(b), &matches); err != nil {
		return nil, err
	}
	for _, m := range matches {
		e, er := s.Read(ctx, m.ID)
		if er != nil {
			continue
		}
		if e.Entry.Scope != scope || e.Entry.RootID != m.RootID || !e.Entry.Current || e.Entry.NeedsReview || e.Entry.State.State != "active" || e.Entry.Profile == nil || profileExpired(e.Entry.Profile) {
			continue
		}
		// Cue material is deliberately absent from the serialized observation.
		// Restore it from this exact still-current version, never a title, stale
		// cached sentence or a newer revision with a superficially equal cue ID.
		found := false
		for _, cue := range entryCues(e.Entry) {
			if cue.ID == m.CueID && plainObservationEntrance(cue, m) {
				m.CueLabel = cue.Label
				m.CueText = cue.Text
				found = true
				break
			}
		}
		if !found {
			continue
		}
		m.entry = &e.Entry
		prefixes := entryObjectPrefixes(e.Entry)
		objects := observedObjects(clues, prefixes)
		same := false
		for _, a := range m.Objects {
			for _, b := range objects {
				if a == b {
					same = true
				}
			}
		}
		if same {
			out = append(out, m)
		}
	}
	return out, nil
}
