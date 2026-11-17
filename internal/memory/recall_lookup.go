package memory

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

func identifierPrefixMatch(text, prefix string) bool {
	for _, token := range strings.FieldsFunc(text, func(r rune) bool {
		return !(r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'))
	}) {
		if len(token) > len(prefix) && strings.EqualFold(token[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

func (s *Store) LookupExperience(ctx context.Context, in ExperienceLookup) (ExperienceLookupResult, error) {
	out := ExperienceLookupResult{ContentRole: "evidence_candidates", Items: []ExperienceItem{}, ReceiptID: experienceNonce(), Notice: "Historical evidence; judge current conditions and exceptions. A receipt records response generation until a native successful result is observed. No hit proves no absence of risk."}
	for name, value := range map[string]string{"task": in.Task, "questions": in.Questions} {
		if strings.TrimSpace(value) == "" || len(value) > 4000 {
			return out, errors.New(name + " needs 1-4000 bytes from the current task")
		}
		if err := textguard.Check(name, value, false); err != nil {
			return out, err
		}
	}
	queries, err := normalizeRecallQueries(in)
	if err != nil {
		return out, err
	}
	out.QueryCount = len(queries)
	if in.Limit < 0 || in.Limit > 3 {
		return out, errors.New("limit must be 1-3, or omitted")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 2
	}
	var host, session, generation, project string
	var matches []ExperienceMatch
	active := false
	attached := false
	if in.ChallengeID != "" {
		var scope, data, current string
		var completed bool
		if err = s.db.QueryRowContext(ctx, "SELECT host,scope,session,generation,project,ids_json,completed FROM experience_challenges WHERE id=?", in.ChallengeID).Scan(&host, &scope, &session, &generation, &project, &data, &completed); err != nil {
			return out, errors.New("unknown challenge: verify service and data directory")
		}
		if scope != in.Scope {
			return out, errors.New("challenge scope mismatch")
		}
		if in.Project != "" {
			p, er := s.experienceProject(ctx, in.Project, in.Scope)
			binding, _ := s.ExperienceProjectBinding(ctx, p, in.Scope)
			if er != nil || binding != project {
				return out, errors.New("challenge project mismatch")
			}
		}
		if err = json.Unmarshal([]byte(data), &matches); err != nil {
			return out, err
		}
		er := s.db.QueryRowContext(ctx, "SELECT generation FROM experience_epochs WHERE host=? AND scope=? AND session=?", host, scope, session).Scan(&current)
		attached = er == nil && current == generation
		active = attached && !completed
		if !attached {
			out.ChallengeOutcome = "stale_context_not_released"
		} else if completed {
			out.ChallengeOutcome = "response_generated"
		}
	}
	if project == "" {
		project = in.Project
	}
	project, err = s.experienceProject(ctx, project, in.Scope)
	if err != nil {
		return out, err
	}
	project, err = s.ExperienceProjectBinding(ctx, project, in.Scope)
	if err != nil {
		return out, err
	}
	ranked, err := s.rankedRecall(ctx, in.Scope, queries)
	if err != nil {
		return out, err
	}
	required := map[string]bool{}
	if attached {
		for _, m := range matches {
			required[m.RootID] = true
		}
		entries, er := s.Experiences(ctx, in.Scope)
		if er != nil {
			return out, er
		}
		for _, e := range entries {
			exact := false
			for _, m := range matches {
				if m.ID == e.ID {
					exact = true
				}
			}
			if !required[e.RootID] || !exact {
				continue
			}
			found := false
			for i := range ranked {
				if ranked[i].Entry.RootID == e.RootID {
					ranked[i].Forced = true
					found = true
					break
				}
			}
			if !found {
				ranked = append(ranked, recallRank{Entry: e, Forced: true})
			}
		}
		for _, m := range matches {
			status := "unavailable_or_revised"
			for _, r := range ranked {
				if r.Entry.ID == m.ID {
					status = "candidate"
					break
				}
			}
			if status != "candidate" {
				_, err = s.db.ExecContext(ctx, "UPDATE experience_challenges SET faulted=1 WHERE id=?", in.ChallengeID)
				if err != nil {
					return out, err
				}
				out.CandidateStates = append(out.CandidateStates, CandidateState{m.ID, status})
			}
		}
		sortRecall(ranked)
		if active {
			out.ChallengeOutcome = "query_pending"
		}

	}
	// A continuation belongs to this exact ranked result snapshot. A revision or
	// changed query cannot silently skip or repeat evidence on the next page.
	snapshot := []string{in.Scope, project, in.ChallengeID}
	qb, _ := json.Marshal(queries)
	snapshot = append(snapshot, string(qb))
	for _, r := range ranked {
		snapshot = append(snapshot, r.Entry.ID, r.Entry.BodySHA256, strconv.FormatBool(r.Entry.NeedsReview))
	}
	filter := digestText(strings.Join(snapshot, "\n"))
	start := 0
	if in.Cursor != "" {
		c, er := decodeCursor(in.Cursor, filter)
		if er != nil {
			return out, er
		}
		start, er = strconv.Atoi(c.Sort)
		if er != nil || start < 1 || start > len(ranked) || digestText(ranked[start-1].Entry.ID)[:32] != c.ID {
			return out, errors.New("invalid recall cursor")
		}
	}
	next := start
	for next < len(ranked) && len(out.Items) < limit {
		r := ranked[next]
		e := r.Entry
		item := ExperienceItem{ID: e.ID, RootID: e.RootID, Title: e.Title, BodySHA256: e.BodySHA256, SourceKind: "legacy_evidence", NeedsReview: e.NeedsReview, MatchedQueries: r.Queries, MatchedTerms: r.Terms}
		if e.Profile != nil && e.Profile.Experience != nil {
			item.SourceKind = "experience"
			item.Experience = visibleExperience(e.Profile.Experience)
			features, _ := json.Marshal(item.Experience)
			item.FeatureSHA256 = digestText(string(features))
			item.Locations = LocateExperience(ctx, project, e.Profile.Experience)
		} else if e.Profile != nil && e.Profile.Agreement != nil {
			item.SourceKind = "agreement_reference"
			item.Agreement = e.Profile.Agreement
			features, _ := json.Marshal(item.Agreement)
			item.FeatureSHA256 = digestText(string(features))
			item.Locations = LocateExperience(ctx, project, &Experience{Evidence: item.Agreement.Evidence})
		} else {
			text := []rune(e.Body)
			if len(text) > 360 {
				text = text[:360]
				item.Excerpt = string(text) + " [navigation excerpt; memory_read for full evidence]"
			} else {
				item.Excerpt = e.Body
			}
		}
		if err = ctx.Err(); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
		// Reserve the actual cursor and final challenge outcome, not guessed token
		// counts. Essential evidence/conditions are never sliced to fit the budget.
		out.HasMore = next+1 < len(ranked)
		if out.HasMore {
			out.NextCursor = encodeCursor(pageCursor{Version: 1, Filter: filter, Sort: strconv.Itoa(next + 1), ID: digestText(e.ID)[:32]})
		} else {
			out.NextCursor = ""
		}
		if active {
			out.ChallengeOutcome = "response_generated"
		}
		body, _ := json.Marshal(out)
		if len(body) > ExperienceLookupBudget {
			out.Items = out.Items[:len(out.Items)-1]
			break
		}
		next++
	}
	if next < len(ranked) {
		out.HasMore = true
		if next == start && len(out.Items) == 0 {
			return out, errors.New("complete result exceeds 8 KiB; compact the stored record without dropping conditions")
		}
		out.NextCursor = encodeCursor(pageCursor{Version: 1, Filter: filter, Sort: strconv.Itoa(next), ID: digestText(ranked[next-1].Entry.ID)[:32]})
	} else {
		out.HasMore = false
		out.NextCursor = ""
	}
	if m, er := s.Maintenance(ctx, in.Scope); er == nil {
		out.MaintenanceDue = m.Due
	}
	body, _ := json.Marshal(out)
	if len(body) > ExperienceLookupBudget {
		return out, errors.New("response envelope exceeds 8 KiB")
	}
	if in.ChallengeID != "" && attached {
		out.ChallengeOutcome = "response_generated"
	}
	if _, err = s.generateExperienceReceipt(ctx, in.Scope, project, in.ChallengeID, out); err != nil {
		return out, err
	}

	return out, nil
}
