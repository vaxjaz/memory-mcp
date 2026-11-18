package memory

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

// Cues are evidenced associations with the SAME historical counterexample.
// Entrances are aliases, not independent risks or model-authored read receipts.
type ExperienceCue struct {
	ID           string        `json:"cue_id" jsonschema:"Stable within this experience; multiple entrance aliases share one cue."`
	Label        string        `json:"label" jsonschema:"Compact association label, at most 24 Unicode characters."`
	Text         string        `json:"text" jsonschema:"A complete evidence-backed reminder stating the applicable fact, at most 160 Unicode characters. Only facts already in this version's body; new risks require a material revision or new experience."`
	EvidenceRefs []string      `json:"evidence_refs" jsonschema:"Actual references from this experience's evidence, not invented identifiers."`
	Entrances    []CueEntrance `json:"entrances" jsonschema:"1-12 directly evidenced code symbols, files, configuration keys or business actions. Never expand a dependency graph."`
}

type CueEntrance struct {
	Kind  string `json:"kind" jsonschema:"code_symbol, file, config or business_signal"`
	Value string `json:"value" jsonschema:"Literal specific observable entrance, not a regex or parent directory."`
}

var cueIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func validateCues(cues []ExperienceCue, evidence []ExperienceEvidence) error {
	if len(cues) == 0 || len(cues) > 12 {
		return errors.New("provide 1-12 evidenced cues")
	}
	refs := map[string]bool{}
	for _, e := range evidence {
		refs[e.Reference] = true
	}
	ids, entrances := map[string]bool{}, map[string]bool{}
	for _, c := range cues {
		if !cueIDPattern.MatchString(c.ID) || ids[c.ID] {
			return errors.New("invalid or duplicate stable cue_id")
		}
		ids[c.ID] = true
		for name, v := range map[string]string{"label": c.Label, "text": c.Text} {
			max := 160
			if name == "label" {
				max = 24
			}
			if strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > max {
				return fmt.Errorf("cue %s needs 1-%d Unicode characters; no truncation", name, max)
			}
			if err := textguard.Check("cue."+name, v, false); err != nil {
				return err
			}
		}
		if len(c.EvidenceRefs) < 1 || len(c.EvidenceRefs) > 3 {
			return errors.New("cue needs 1-3 existing evidence references")
		}
		for _, ref := range c.EvidenceRefs {
			if !refs[ref] {
				return errors.New("cue references evidence absent from this body")
			}
		}
		if len(c.Entrances) < 1 || len(c.Entrances) > 12 {
			return errors.New("cue needs 1-12 evidenced entrances")
		}
		for _, a := range c.Entrances {
			if a.Kind != "code_symbol" && a.Kind != "file" && a.Kind != "config" && a.Kind != "business_signal" {
				return errors.New("unknown entrance kind")
			}
			if utf8.RuneCountInString(a.Value) < 2 || utf8.RuneCountInString(a.Value) > 160 || genericEntranceValue[strings.ToLower(a.Value)] {
				return errors.New("entrance must be specific and compact")
			}
			if a.Kind == "file" && (!strings.Contains(a.Value, ".") || strings.HasSuffix(a.Value, "/")) {
				return errors.New("file entrance must name a file, not a directory")
			}
			if err := textguard.Check("entrance", a.Value, false); err != nil {
				return err
			}
			key := a.Kind + ":" + humanSignalKey(a.Value)
			if entrances[key] {
				return errors.New("one entrance cannot manufacture multiple cues")
			}
			entrances[key] = true
		}
	}
	return nil
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
