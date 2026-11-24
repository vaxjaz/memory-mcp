package memory

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/vaxjaz/memory-mcp/internal/matching"
)

// Cues are evidenced associations with the SAME historical body.
// Entrances are aliases, not independent risks or model-authored read receipts.
// Experience-side cue text is no longer authored; delivered facts come from
// the body. Legacy records keep their stored text and it keeps being honored.
type ExperienceCue struct {
	ID           string     `json:"cue_id" jsonschema:"Stable within the record. Start with a lowercase ASCII letter; use up to 48 lowercase letters, digits or underscores in total."`
	Label        string     `json:"label" jsonschema:"Compact association label, at most 24 Unicode characters."`
	Text         string     `json:"text,omitempty" jsonschema:"Experiences: leave it empty, delivered facts come from the body; submitted text is rejected. Agreements: required, the confirmed sentence this cue quotes, at most 160 Unicode characters."`
	EvidenceRefs []string   `json:"evidence_refs" jsonschema:"1-3 exact evidence[].reference values from this submitted body. When removing evidence, update all cues that cite it."`
	Matches      []CueMatch `json:"matches" jsonschema:"Matching groups with a total of 1-12 directly evidenced entrance strings."`
}

type CueMatch = matching.Expression

type EntranceComparison struct {
	Op            string `json:"op"`
	CaseSensitive bool   `json:"case_sensitive"`
	Normalize     string `json:"normalize,omitempty"`
}

func entranceComparison(rule matching.Rule) EntranceComparison {
	return EntranceComparison{Op: rule.Op, CaseSensitive: rule.Sensitive(false), Normalize: rule.Normalize}
}

var cueIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func (v *recordValidation) cues(root string, cues []ExperienceCue, evidence []ExperienceEvidence, requireText bool) {
	field := root + ".cues"
	v.count(field, len(cues), 1, 12, "evidenced cues")
	refs := map[string]bool{}
	for _, e := range evidence {
		refs[e.Reference] = true
	}
	ids := map[string]bool{}
	for i, c := range cues {
		row := fmt.Sprintf("%s[%d]", field, i)
		if !cueIDPattern.MatchString(c.ID) || ids[c.ID] {
			v.add(row+".cue_id", "must be unique and match [a-z][a-z0-9_]{0,47}")
		}
		ids[c.ID] = true
		v.shortText(row+".label", c.Label, 24)
		if requireText || strings.TrimSpace(c.Text) != "" {
			v.shortText(row+".text", c.Text, 160)
		}
		v.count(row+".evidence_refs", len(c.EvidenceRefs), 1, 3, "existing evidence references")
		for j, ref := range c.EvidenceRefs {
			if !refs[ref] {
				v.add(fmt.Sprintf("%s.evidence_refs[%d]", row, j), "does not match any "+root+".evidence[].reference; copy the exact value or revise the cue to use supporting evidence")
			}
		}
		v.count(row+".matches", len(c.Matches), 1, 12, "matching groups")
		total := 0
		for j, group := range c.Matches {
			path := fmt.Sprintf("%s.matches[%d]", row, j)
			if err := group.Validate(0, true); err != nil {
				v.add(path, err.Error())
				continue
			}
			if !group.RequiresEntrance() {
				v.add(path, "every alternative needs a positive entrance string")
			}
			seen := map[*matching.Expression]map[string]bool{}
			group.VisitEntrances(func(rule *matching.Expression, value string) {
				total++
				v.count(path+".entrances", utf8.RuneCountInString(value), 2, 160, "Unicode characters")
				if genericEntranceValue[strings.ToLower(value)] || strings.TrimSpace(value) == "" {
					v.add(path+".entrances", "must identify a specific entrance")
				}
				v.checkText(path+".entrances", value)
				if seen[rule] == nil {
					seen[rule] = map[string]bool{}
				}
				key := rule.NormalizeText(value, false)
				if seen[rule][key] {
					v.add(path+".entrances", "duplicates an entrance in this comparison")
				}
				seen[rule][key] = true
			})
		}
		v.count(row+".matches", total, 1, 12, "total evidenced entrances")
	}
}

// Content identity intentionally excludes entrance aliases, code hashes and
// line/commit relocation. Cue facts/references remain material content.
func cueContent(cues []ExperienceCue) []ExperienceCue {
	out := append([]ExperienceCue(nil), cues...)
	for i := range out {
		out[i].Matches = nil
		out[i].EvidenceRefs = append([]string(nil), out[i].EvidenceRefs...)
		sort.Strings(out[i].EvidenceRefs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// experienceForm reports the stored body form. Legacy five-field records are
// counterexample form; fact is the new sourced-business-fact form.
func experienceForm(x *Experience) string {
	if strings.TrimSpace(x.Fact) != "" {
		return "fact"
	}
	return "counterexample"
}

// coreFact is the short fact or counterexample of this version's body. Cue
// reminders quote it instead of an experience-side cue text; qualifiers that
// matter to it are written inside the text itself.
func coreFact(x *Experience) string {
	if strings.TrimSpace(x.Fact) != "" {
		return x.Fact
	}
	return x.Counterexample
}

// ExperienceBody is deliberately separate from the persisted recognition model.
// Delivery quotes THIS version's body only: first lookup and reminders share
// one path, and legacy cue text never re-enters context through the projection.
// Cue-only material keeps its stored identity, stays readable by entry ID and
// is listed by the migration inventory pending evidence confirmation.
// Legacy qualifier fields are always preserved so no stored record is
// displayed through a lossy projection.
type ExperienceBody struct {
	Form           string               `json:"form"`
	Pitfall        string               `json:"pitfall,omitempty"`
	Counterexample string               `json:"counterexample,omitempty"`
	Fact           string               `json:"fact,omitempty"`
	Impact         string               `json:"impact,omitempty"`
	Applicability  string               `json:"applicability,omitempty"`
	Boundary       string               `json:"boundary,omitempty"`
	Evidence       []ExperienceEvidence `json:"evidence"`
}

func visibleExperience(x *Experience) *ExperienceBody {
	out := &ExperienceBody{Form: experienceForm(x), Pitfall: x.Pitfall, Counterexample: x.Counterexample, Fact: x.Fact, Impact: x.Impact, Applicability: x.Applicability, Boundary: x.Boundary, Evidence: append([]ExperienceEvidence(nil), x.Evidence...)}
	for i := range out.Evidence {
		out.Evidence[i].SHA256 = ""
	}
	return out
}

// Alias updates retain the original historical evidence identity. Relocating a
// code reference must not leave a cue pointing outside its preserved evidence.
func recognitionUpdate(old, submitted *Experience) *Experience {
	x := *old
	x.ObjectIDPrefixes = submitted.ObjectIDPrefixes
	x.Cues = append([]ExperienceCue(nil), submitted.Cues...)
	for i := range x.Cues {
		x.Cues[i].EvidenceRefs = append([]string(nil), x.Cues[i].EvidenceRefs...)
		for j, ref := range x.Cues[i].EvidenceRefs {
			for _, e := range submitted.Evidence {
				if e.Kind != "code" || e.Reference != ref {
					continue
				}
				for _, historical := range old.Evidence {
					if historical.Kind == "code" && historical.Path == e.Path && historical.Symbol == e.Symbol {
						x.Cues[i].EvidenceRefs[j] = historical.Reference
						break
					}
				}
			}
		}
	}
	return &x
}
