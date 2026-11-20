package memory

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

type recordValidation struct {
	issues  []string
	omitted int
}

func (v *recordValidation) add(field, message string) {
	if len(v.issues) == 12 {
		v.omitted++
		return
	}
	v.issues = append(v.issues, field+": "+message)
}

func (v *recordValidation) err() error {
	if len(v.issues) == 0 {
		return nil
	}
	more := ""
	if v.omitted > 0 {
		more = fmt.Sprintf("\n%d additional issues omitted", v.omitted)
	}
	return fmt.Errorf("invalid record (%d issues, no write performed):\n- %s%s", len(v.issues)+v.omitted, strings.Join(v.issues, "\n- "), more)
}

func (v *recordValidation) count(field string, got, min, max int, unit string) {
	if got < min || got > max {
		v.add(field, fmt.Sprintf("needs %d-%d %s, got %d", min, max, unit, got))
	}
}

func (v *recordValidation) checkText(field, value string) {
	if issue := textguard.Inspect(field, value); issue != nil {
		v.add(field, fmt.Sprintf("%s at UTF-8 byte %d; resend text from the original source", issue.Kind, issue.ByteOffset))
	}
}

func (v *recordValidation) shortText(field, value string, max int) {
	length := utf8.RuneCountInString(value)
	if strings.TrimSpace(value) == "" {
		length = 0
	}
	v.count(field, length, 1, max, "Unicode characters")
	v.checkText(field, value)
}

// optionalText validates a legacy qualifier only when submitted; blank is valid.
func (v *recordValidation) optionalText(field, value string, max int) {
	if strings.TrimSpace(value) == "" {
		return
	}
	v.shortText(field, value, max)
}

func (v *recordValidation) evidence(root string, evidence []ExperienceEvidence) {
	field := root + ".evidence"
	v.count(field, len(evidence), 1, 3, "actual evidence locators")
	for i, e := range evidence {
		row := fmt.Sprintf("%s[%d]", field, i)
		if e.Kind != "code" && e.Kind != "runtime" && e.Kind != "business" {
			v.add(row+".kind", "must be code, runtime or business")
		}
		v.shortText(row+".reference", e.Reference, 240)
		v.shortText(row+".observation", e.Observation, 180)
		if e.Kind == "code" && e.Path == "" {
			v.add(row+".path", "code evidence requires a project-relative path")
		}
		v.count(row+".path", len(e.Path), 0, 512, "bytes")
		v.count(row+".symbol", len(e.Symbol), 0, 160, "bytes")
		v.checkText(row+".path", e.Path)
		v.checkText(row+".symbol", e.Symbol)
	}
}

func (v *recordValidation) experienceAdmission(admission *ExperienceAdmission, evidence []ExperienceEvidence) {
	if admission == nil {
		v.add("admission", "required for an experience write; state the future decision and cite supporting evidence. Skip writing for a task-only correction.")
		return
	}
	v.shortText("admission.future_decision", admission.FutureDecision, 240)
	v.shortText("admission.evidence_ref", admission.EvidenceRef, 240)
	if strings.TrimSpace(admission.EvidenceRef) == "" {
		return
	}
	for _, item := range evidence {
		if item.Reference == admission.EvidenceRef {
			return
		}
	}
	v.add("admission.evidence_ref", "does not match any experience.evidence[].reference; copy the exact supporting reference from this submission")
}
