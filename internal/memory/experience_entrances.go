package memory

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

func indexExperienceEntrances(ctx context.Context, tx *sql.Tx, e Entry) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM experience_entrances WHERE entry_id=?", e.ID); err != nil {
		return err
	}
	cues := entryCues(e)
	if len(cues) == 0 {
		return nil
	}
	if e.Current {
		var key string
		if e.Profile.Experience != nil {
			key = experienceKey(e.Profile.Experience)
		} else {
			key = agreementKey(e.Profile.Agreement)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM experience_keys WHERE scope=? AND root_id=?", e.Scope, e.RootID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO experience_keys VALUES(?,?,?)", e.Scope, key, e.RootID); err != nil {
			return err
		}
	}
	for _, c := range cues {
		for _, a := range c.Entrances {
			if _, err := tx.ExecContext(ctx, "INSERT INTO experience_entrances VALUES(?,?,?,?,?,?)", e.ID, c.ID, a.Kind, a.Value, humanSignalKey(normalizedResource(a.Value)), e.Scope); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) MatchExperiences(ctx context.Context, scope, clues string) ([]ExperienceMatch, error) {
	var out []ExperienceMatch
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT a.entry_id FROM experience_entrances a JOIN entries e ON e.id=a.entry_id WHERE a.scope=? AND e.current=1 AND e.needs_review=0 AND instr(?,a.match_key)>0 ORDER BY a.entry_id LIMIT 2001`, scope, humanSignalKey(normalizedResource(clues)))
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 2000 {
		return nil, errors.New("entrance scan limit; refusing incomplete candidate list")
	}
	text := newHumanSignalText(clues)
	for _, id := range ids {
		r, er := s.Read(ctx, id)
		if er != nil {
			return nil, er
		}
		e := r.Entry
		if !e.Current || e.NeedsReview || e.State.State != "active" || profileExpired(e.Profile) {
			continue
		}
		for _, c := range entryCues(e) {
			for _, a := range c.Entrances {
				matched := literalExperienceMatch(clues, a.Value)
				if a.Kind == "business_signal" {
					matched = text.matches(a.Value)
				}
				if matched {
					out = append(out, ExperienceMatch{ID: e.ID, RootID: e.RootID, CueID: c.ID, CueLabel: c.Label, CueText: c.Text, Entrance: a.Value, Trigger: a.Kind, entry: &e})
					break
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].CueID < out[j].CueID
	})
	return out, nil
}
