package memory

import (
	"context"
	"sort"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/matching"
)

// WriteCandidate is one older experience worth comparing a new observation
// against, found inside the write path. The query is read-only: it creates no
// receipt, consumes no cue and writes nothing. A miss here never proves the
// library lacks the fact; lexical retrieval can miss rephrasings without
// shared evidence, objects or entrances.
type WriteCandidate struct {
	ID         string          `json:"id"`
	RootID     string          `json:"root_id"`
	Experience *ExperienceBody `json:"experience"`
	MatchedOn  []string        `json:"matched_on"`
}

const writeCandidateLimit = 3

// comparisonToken binds a candidate set to one draft fingerprint and scope.
// It is opaque, carries no business identity, and is re-derived at resolution
// time; a changed candidate set invalidates it.
func comparisonToken(scope, fingerprint string, ids []string) string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return digestText(scope + "\x1f" + fingerprint + "\x1f" + strings.Join(sorted, "\x1f"))
}

func evidenceLocators(x *Experience) map[string]bool {
	out := map[string]bool{}
	for _, e := range x.Evidence {
		if e.Kind == "code" {
			if e.Path != "" || e.Symbol != "" {
				out["code\x1f"+e.Path+"\x1f"+e.Symbol] = true
			}
			continue
		}
		if e.Reference != "" {
			out["ref\x1f"+e.Reference] = true
		}
	}
	return out
}

func entranceValues(x *Experience) map[string]bool {
	out := map[string]bool{}
	for _, cue := range x.Cues {
		for _, group := range cue.Matches {
			group.VisitEntrances(func(_ *matching.Expression, value string) {
				if v := strings.TrimSpace(value); v != "" {
					out[strings.ToLower(v)] = true
				}
			})
		}
	}
	return out
}

// bodyOverlapThreshold is the share of distinct words two bodies must have
// in common before a write is compared with a stored record.
const bodyOverlapThreshold = 0.5

// bodyTokens are the distinct indexable words of an experience's own claim.
func bodyTokens(x *Experience) map[string]bool {
	out := map[string]bool{}
	for _, token := range recallTokens(x.Fact + x.Pitfall + x.Counterexample) {
		out[token] = true
	}
	return out
}

// bodyOverlap is the share of distinct words shared by two bodies:
// intersection over union, 0 when either body has no indexable word.
func bodyOverlap(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for token := range a {
		if b[token] {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}

// writeCandidates searches current active records in this scope for entries
// worth checking against a new observation. A record is worth the check when
// it cites the same evidence locator, wakes from the same entrance, or states
// a body that overlaps the draft by at least half. A few shared words are not
// a reason: narrative words such as how a fact was verified recur across a
// whole project and would ask for a judgment on nearly every write. Evidence
// and object double hits rank first; ordering is stable by entry ID. The FULL ranked list is
// returned; callers page it with writeCandidateLimit per response and keep a
// cursor so a hidden true duplicate stays reachable. The query runs read-only
// against either the pool or an open write transaction.
func (s *Store) writeCandidates(ctx context.Context, q dbQuerier, scope string, draft *Experience) ([]WriteCandidate, error) {
	if draft == nil {
		return nil, nil
	}
	entries, err := scanExperiences(ctx, q, scope)
	if err != nil {
		return nil, err
	}
	draftEvidence := evidenceLocators(draft)
	draftEntrances := entranceValues(draft)
	draftBody := bodyTokens(draft)
	var out []WriteCandidate
	for _, e := range entries {
		if e.Profile == nil || e.Profile.Experience == nil {
			continue
		}
		other := e.Profile.Experience
		matched := map[string]bool{}
		for locator := range evidenceLocators(other) {
			if draftEvidence[locator] {
				matched["evidence_ref"] = true
				break
			}
		}
		for value := range entranceValues(other) {
			if draftEntrances[value] {
				matched["entrance"] = true
				break
			}
		}
		if overlap := bodyOverlap(draftBody, bodyTokens(other)); overlap >= bodyOverlapThreshold {
			matched["body_overlap"] = true
		}
		if !matched["evidence_ref"] && !matched["entrance"] && !matched["body_overlap"] {
			continue
		}
		reasons := make([]string, 0, 3)
		for _, reason := range []string{"evidence_ref", "entrance", "body_overlap"} {
			if matched[reason] {
				reasons = append(reasons, reason)
			}
		}
		out = append(out, WriteCandidate{ID: e.ID, RootID: e.RootID, Experience: visibleExperience(other), MatchedOn: reasons})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].MatchedOn) != len(out[j].MatchedOn) {
			return len(out[i].MatchedOn) > len(out[j].MatchedOn)
		}
		// Evidence overlap outranks entrances, which outrank pure term hits.
		rank := func(c WriteCandidate) int {
			for _, reason := range c.MatchedOn {
				if reason == "evidence_ref" {
					return 0
				}
			}
			if len(c.MatchedOn) == 0 {
				return 2
			}
			return 1
		}
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
