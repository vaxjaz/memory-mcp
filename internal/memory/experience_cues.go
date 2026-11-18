package memory

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Cues are evidenced associations with the SAME historical counterexample.
// Entrances are aliases, not independent risks or model-authored read receipts.
type ExperienceCue struct {
	ID           string        `json:"cue_id" jsonschema:"Stable within the record. Start with a lowercase ASCII letter; use up to 48 lowercase letters, digits or underscores in total."`
	Label        string        `json:"label" jsonschema:"Compact association label, at most 24 Unicode characters."`
	Text         string        `json:"text" jsonschema:"A complete evidence-backed reminder stating the applicable fact, at most 160 Unicode characters. Only facts already in this version's body; new risks require a material revision or new experience."`
	EvidenceRefs []string      `json:"evidence_refs" jsonschema:"1-3 exact evidence[].reference values from this submitted body. When removing evidence, update all cues that cite it."`
	Entrances    []CueEntrance `json:"entrances" jsonschema:"1-12 directly evidenced code symbols, files, configuration keys or business actions. Never expand a dependency graph."`
}

type CueEntrance struct {
	Kind  string `json:"kind" jsonschema:"code_symbol, file, config or business_signal"`
	Value string `json:"value" jsonschema:"Literal specific observable entrance, not a regex or parent directory."`
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
		v.count(row+".entrances", len(c.Entrances), 1, 12, "evidenced entrances")
		for j, a := range c.Entrances {
			entrance := fmt.Sprintf("%s.entrances[%d]", row, j)
			if a.Kind != "code_symbol" && a.Kind != "file" && a.Kind != "config" && a.Kind != "business_signal" {
				v.add(entrance+".kind", "must be code_symbol, file, config or business_signal")
			}
			v.count(entrance+".value", utf8.RuneCountInString(a.Value), 2, 160, "Unicode characters")
			if genericEntranceValue[strings.ToLower(a.Value)] {
				v.add(entrance+".value", "must identify a specific entrance")
			}
			if a.Kind == "file" && (!strings.Contains(a.Value, ".") || strings.HasSuffix(a.Value, "/")) {
				v.add(entrance+".value", "must name a file")
			}
			v.checkText(entrance+".value", a.Value)
			key := a.Kind + ":" + humanSignalKey(a.Value)
			if entrances[key] {
				v.add(entrance+".value", "duplicates another entrance in this record")
			}
			entrances[key] = true
		}
	}
}

// Content identity intentionally excludes entrance aliases, code hashes and
// line/commit relocation. Cue facts/references remain material content.
func cueContent(cues []ExperienceCue) []ExperienceCue {
	out := append([]ExperienceCue(nil), cues...)
	for i := range out {
		out[i].Entrances = nil
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
