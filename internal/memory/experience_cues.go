package memory

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

"github.com/vaxjaz/memory-mcp/internal/matching"
)

// Cues are evidenced associations with the SAME historical counterexample.
// Entrances are aliases, not independent risks or model-authored read receipts.
type ExperienceCue struct {
	ID           string        `json:"cue_id" jsonschema:"Stable within the record. Start with a lowercase ASCII letter; use up to 48 lowercase letters, digits or underscores in total."`
	Label        string        `json:"label" jsonschema:"Compact association label, at most 24 Unicode characters."`
	Text         string        `json:"text" jsonschema:"A complete evidence-backed reminder stating the applicable fact, at most 160 Unicode characters. Only facts already in this version's body; new risks require a material revision or new experience."`
	EvidenceRefs []string      `json:"evidence_refs" jsonschema:"1-3 exact evidence[].reference values from this submitted body. When removing evidence, update all cues that cite it."`
 Matches []CueMatch `json:"matches" jsonschema:"Matching groups with a total of 1-12 directly evidenced entrance strings."`
}

type CueMatch struct {
    matching.Rule
    Entrances []string `json:"entrances" jsonschema:"Original literal strings sharing this comparison rule. Preserve the original case."`
}

type EntranceComparison struct {
    Op string `json:"op"`
    CaseSensitive bool `json:"case_sensitive"`
    Normalize string `json:"normalize,omitempty"`
}

func (m CueMatch) comparison() EntranceComparison {
    return EntranceComparison{Op:m.Op, CaseSensitive:m.Sensitive(false), Normalize:m.Normalize}
}

var cueIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func (v *recordValidation) cues(root string, cues []ExperienceCue, evidence []ExperienceEvidence) {
	field := root + ".cues"
	v.count(field, len(cues), 1, 12, "evidenced cues")
	refs := map[string]bool{}
	for _, e := range evidence {
		refs[e.Reference] = true
	}
	ids, entrances := map[string]bool{}, map[string]bool{}
	for i, c := range cues {
		row := fmt.Sprintf("%s[%d]", field, i)
		if !cueIDPattern.MatchString(c.ID) || ids[c.ID] {
			v.add(row+".cue_id", "must be unique and match [a-z][a-z0-9_]{0,47}")
		}
		ids[c.ID] = true
		v.shortText(row+".label", c.Label, 24)
		v.shortText(row+".text", c.Text, 160)
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
            v.count(path+".entrances", len(group.Entrances), 1, 12, "evidenced entrances")
            total += len(group.Entrances)
            if group.Op == "exists" {
                v.add(path+".op", "cue matches require a text comparison")
                continue
            }
            for k, value := range group.Entrances {
                entrance := fmt.Sprintf("%s.entrances[%d]", path, k)
                v.count(entrance, utf8.RuneCountInString(value), 2, 160, "Unicode characters")
                if genericEntranceValue[strings.ToLower(value)] || strings.TrimSpace(value)=="" {
                    v.add(entrance, "must identify a specific entrance")
                }
                if err := group.Validate(value, 640); err != nil {
                    v.add(entrance, err.Error())
                }
                v.checkText(entrance, value)
                key := fmt.Sprintf("%s:%s:%t:%s", group.Op, group.Normalize, group.Sensitive(false), group.NormalizeText(value,false))
                if entrances[key] {
                    v.add(entrance, "duplicates another entrance with the same comparison")
                }
                entrances[key] = true
            }
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

// ExperienceBody is deliberately separate from the persisted recognition model.
// Facts behind every cue are present on first delivery; only the actual trigger
// cues are consumed by the native receipt.
type ExperienceBody struct {
	Pitfall        string               `json:"pitfall"`
	Counterexample string               `json:"counterexample"`
	Impact         string               `json:"impact"`
	Applicability  string               `json:"applicability"`
	Boundary       string               `json:"boundary"`
	RelatedFacts   []string             `json:"related_facts,omitempty"`
	Evidence       []ExperienceEvidence `json:"evidence"`
}

func visibleExperience(x *Experience) *ExperienceBody {
	out := &ExperienceBody{Pitfall: x.Pitfall, Counterexample: x.Counterexample, Impact: x.Impact, Applicability: x.Applicability, Boundary: x.Boundary, Evidence: append([]ExperienceEvidence(nil), x.Evidence...)}
	core := x.Pitfall + x.Counterexample + x.Impact + x.Applicability + x.Boundary
	for _, c := range x.Cues {
		if !strings.Contains(core, c.Text) {
			out.RelatedFacts = append(out.RelatedFacts, c.Text)
		}
	}
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
