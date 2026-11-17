package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

// An observed compaction invalidates bookkeeping; it does not claim that the
// host exposes the actual model context or that all pruning emits this event.
func (s *Store) ExperienceAfterCompact(ctx context.Context, host, scope, session string) error {
	generation := experienceNonce()
	if generation == "" {
		return errors.New("cannot reset compacted input")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM business_observations WHERE host=? AND scope=? AND session=?", host, scope, session); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE experience_epochs SET generation=? WHERE host=? AND scope=? AND session=?", generation, host, scope, session); err != nil {
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
	matches, err := s.MatchExperiences(ctx, scope, clues)
	if err != nil {
		return err
	}
	kept := []ExperienceMatch{}
	for _, m := range matches {
		e, er := s.Read(ctx, m.ID)
		if er != nil {
			return er
		}
		_, _, prefixes := recognitionFeatures(e.Entry)
		m.Objects = observedObjects(clues, prefixes)
		if len(m.Objects) > 0 {
			kept = append(kept, m)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	b, _ := json.Marshal(kept)
	_, err = s.db.ExecContext(ctx, `INSERT INTO business_observations VALUES(?,?,?,?,?) ON CONFLICT(host,scope,session) DO UPDATE SET generation=excluded.generation,matches_json=excluded.matches_json`, host, scope, session, generation, string(b))
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
		if !e.Entry.Current || e.Entry.NeedsReview || e.Entry.State.State != "active" || e.Entry.Profile == nil || profileExpired(e.Entry.Profile) {
			continue
		}
		_, _, prefixes := recognitionFeatures(e.Entry)
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
