package memory

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/matching"
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
		index := 0
		for _, group := range c.Matches {
			var failure error
			group.VisitEntrances(func(_ *matching.Expression, value string) {
				if failure != nil {
					return
				}
				_, failure = tx.ExecContext(ctx, "INSERT INTO experience_entrances VALUES(?,?,?,?,?,?)", e.ID, c.ID, index, value, humanSignalKey(normalizedResource(value)), e.Scope)
				index++
			})
			if failure != nil {
				return failure
			}
		}
	}
	return nil
}

func (s *Store) MatchExperiences(ctx context.Context, scope string, clues ...string) ([]ExperienceMatch, error) {
	var input matching.Input
	for _, text := range clues {
		input.Texts = append(input.Texts, matching.NamedText{Field: "text", Value: text})
	}
	if len(clues) > 0 {
		input.Fields = map[string]any{"text": clues[0]}
	}
	return s.MatchToolInput(ctx, scope, &input)
}

func (s *Store) MatchToolInput(ctx context.Context, scope string, input *matching.Input) ([]ExperienceMatch, error) {
	var clues []string
	for _, text := range input.Texts {
		clues = append(clues, text.Value)
	}
	if len(clues) == 0 {
		return nil, nil
	}
	if tool, ok := input.Fields["tool"].(string); ok {
		clues = append(clues, tool)
	}
	var out []ExperienceMatch
	// Prefilter once. Only the exact check against individual fields can
	// confirm an entrance; the combined prefilter may overselect candidates.
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT a.entry_id FROM experience_entrances a JOIN entries e ON e.id=a.entry_id WHERE a.scope=? AND e.current=1 AND e.needs_review=0 AND instr(?,a.match_key)>0 ORDER BY a.entry_id LIMIT 2001`, scope, humanSignalKey(normalizedResource(strings.Join(clues, " "))))
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
	for _, id := range ids {
		r, er := s.Read(ctx, id)
		if er != nil {
			return nil, er
		}
		e := r.Entry
		if !e.Current || e.NeedsReview || e.State.State != "active" || profileExpired(e.Profile) {
			continue
		}
	cues:
		for _, c := range entryCues(e) {
			for _, group := range c.Matches {
				truth, hit, err := group.Evaluate(input, nil)
				if err != nil {
					return nil, err
				}
				if truth == matching.True && hit != nil {
					out = append(out, ExperienceMatch{ID: e.ID, RootID: e.RootID, CueID: c.ID, CueLabel: c.Label,
						CueText: c.Text, Entrance: hit.Entrance, Field: hit.Field, EntranceComparison: entranceComparison(hit.Rule), entry: &e})
					continue cues
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

type CueCheck struct {
	Outcome string           `json:"outcome"`
	CueID   string           `json:"cue_id"`
	Group   int              `json:"group"`
	Checks  []matching.Check `json:"checks"`
}

func ExplainCue(e Entry, cueID string, input *matching.Input) ([]CueCheck, error) {
	var result []CueCheck
	for _, cue := range entryCues(e) {
		if cueID != "" && cueID != cue.ID {
			continue
		}
		for index, group := range cue.Matches {
			check := CueCheck{CueID: cue.ID, Group: index + 1}
			truth, _, err := group.Evaluate(input, func(c matching.Check) { check.Checks = append(check.Checks, c) })
			if err != nil {
				return nil, err
			}
			check.Outcome = truth.String()
			result = append(result, check)
		}
	}
	return result, nil
}

func (s *Store) VisitToolFacts(ctx context.Context, scope string, visitor func(*matching.Fact)) error {
	entries, err := s.Experiences(ctx, scope)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if profileExpired(e.Profile) {
			continue
		}
		for _, cue := range entryCues(e) {
			for _, group := range cue.Matches {
				group.VisitFacts(visitor)
			}
		}
	}
	return nil
}
