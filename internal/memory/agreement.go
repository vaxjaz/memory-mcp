package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// A compact pointer to an existing, explicitly confirmed agreement. It is not
// manufactured failure evidence, a document manual, or new operational authority.
type Agreement struct {
	Cues                  []ExperienceCue      `json:"cues" jsonschema:"Evidence-backed applicability cues for the already-confirmed requirement."`
	ObjectIDPrefixes      []string             `json:"object_id_prefixes,omitempty" jsonschema:"Object identifier prefixes for same-input result association."`
	Requirement           string               `json:"requirement" jsonschema:"Compact already-confirmed agreement; not a manual or new rule."`
	Applicability         string               `json:"applicability" jsonschema:"Actual scope, conditions and environment."`
	Boundary              string               `json:"boundary" jsonschema:"Exceptions and unknowns; retrieval grants no permission."`
	Authority             string               `json:"authority" jsonschema:"Actual owner/document establishing this agreement."`
	ConfirmationReference string               `json:"confirmation_reference" jsonschema:"Real agreement/approval locator, not installation consent."`
	Evidence              []ExperienceEvidence `json:"evidence" jsonschema:"1-3 actual source locators; no copied source text."`
}

type AgreementWrite struct {
	Scope      string    `json:"scope"`
	Project    string    `json:"project"`
	Title      string    `json:"title"`
	Agreement  Agreement `json:"agreement"`
	PreviousID string    `json:"previous_id,omitempty" jsonschema:"Full current agreement revision ID. Send the complete revised agreement, including evidence and cue references."`
	RequestID  string    `json:"request_id,omitempty"`
}

func validatePrefixes(prefixes []string) error {
	if len(prefixes) > 6 {
		return errors.New("at most six evidence-grounded identifier prefixes")
	}
	seen := map[string]bool{}
	for _, p := range prefixes {
		if len(p) < 3 || len(p) > 40 || strings.TrimSpace(p) != p || genericEntranceValue[strings.ToLower(p)] {
			return errors.New("identifier prefixes need 3-40 specific ASCII characters")
		}
		for i, r := range p {
			if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && (r >= '0' && r <= '9' || r == '_' || r == '-')) {
				return errors.New("prefix must start with an ASCII letter; wildcards and regular expressions are not allowed")
			}
		}
		k := strings.ToLower(p)
		if seen[k] {
			return errors.New("duplicate identifier prefix")
		}
		seen[k] = true
	}
	return nil
}

func validateAgreement(a *Agreement) error {
	if a == nil {
		return errors.New("agreement required")
	}
	var validation recordValidation
	if err := validatePrefixes(a.ObjectIDPrefixes); err != nil {
		validation.add("agreement.object_id_prefixes", err.Error())
	}
	validation.shortText("agreement.requirement", a.Requirement, 240)
	validation.shortText("agreement.applicability", a.Applicability, 240)
	validation.shortText("agreement.boundary", a.Boundary, 240)
	validation.shortText("agreement.authority", a.Authority, 240)
	validation.shortText("agreement.confirmation_reference", a.ConfirmationReference, 240)
	validation.evidence("agreement", a.Evidence)
	// Agreements keep requiring cue text; only the experience side derives
	// delivered facts from the body.
	validation.cues("agreement", a.Cues, a.Evidence, true)
	b, _ := json.Marshal(visibleAgreement(a))
	if len(b) > 3000 {
		validation.add("agreement", fmt.Sprintf("body budget is 3000 UTF-8 bytes, got %d; condense prose and preserve conditions", len(b)))
	}
	return validation.err()
}

func (s *Store) RecordAgreement(ctx context.Context, in AgreementWrite) (ExperienceReceipt, error) {
	var zero ExperienceReceipt
	original, _ := json.Marshal(in)
	if err := validateAgreement(&in.Agreement); err != nil {
		return zero, err
	}
	project, err := s.experienceProject(ctx, in.Project, in.Scope)
	if err != nil {
		return zero, err
	}
	fingerprint := "agreement-v1:" + digestText(string(original))
	if in.RequestID != "" {
		var fp, id string
		er := s.db.QueryRowContext(ctx, "SELECT fingerprint,entry_id FROM write_requests WHERE scope=? AND request_id=?", in.Scope, in.RequestID).Scan(&fp, &id)
		if er == nil {
			if fp != fingerprint {
				return zero, errors.New("request_id conflict: agreement payload changed")
			}
			r, e := s.Read(ctx, id)
			if e != nil {
				return zero, e
			}
			return agreementReceipt(r.Entry, true), nil
		}
		if er != nil && !errors.Is(er, sql.ErrNoRows) {
			return zero, er
		}
	}
	in.Agreement.Evidence = append([]ExperienceEvidence(nil), in.Agreement.Evidence...)
	for i := range in.Agreement.Evidence {
		e := &in.Agreement.Evidence[i]
		if e.Path == "" {
			continue
		}
		path, er := experienceFile(project, e.Path)
		if er != nil {
			return zero, er
		}
		data, er := os.ReadFile(path)
		if er != nil {
			return zero, er
		}
		sum := sha256.Sum256(data)
		actual := hex.EncodeToString(sum[:])
		if e.SHA256 != "" && e.SHA256 != actual {
			return zero, errors.New("agreement source changed: reread it before saving")
		}
		if e.Symbol != "" && !strings.Contains(string(data), e.Symbol) {
			return zero, errors.New("agreement locator not present in current source")
		}
		e.SHA256 = actual
	}
	a := in.Agreement
	d := Draft{Kind: "record", Scope: in.Scope, Title: in.Title, Body: fmt.Sprintf("已确认约定：%s\n适用条件：%s\n边界与未知：%s\n权威来源：%s", a.Requirement, a.Applicability, a.Boundary, a.Authority), Origin: "user_supplied", SourceRef: a.ConfirmationReference, RequestID: in.RequestID, Profile: &Profile{Class: "long_term", FutureUse: a.Applicability, CounterCheck: "Pointer to an explicitly confirmed agreement.", Applicability: a.Applicability, Boundary: a.Boundary, Agreement: &a}}
	d.referenceValidated = true
	d.experienceKey = agreementKey(&a)
	d.requestFingerprint = fingerprint
	e, err := s.Put(ctx, d, in.PreviousID)
	if err != nil {
		return zero, err
	}
	return agreementReceipt(e, e.writeReused), nil
}

func agreementReceipt(e Entry, reused bool) ExperienceReceipt {
	r := experienceReceipt(e, reused)
	r.Notice = "Saved compact agreement reference, not new authority or execution permission. Confirmation and semantic applicability remain the host's responsibility."
	return r
}

// AgreementBody contains the confirmed material delivered to the host.
type AgreementBody struct {
	Requirement           string               `json:"requirement"`
	Applicability         string               `json:"applicability"`
	Boundary              string               `json:"boundary"`
	Authority             string               `json:"authority"`
	ConfirmationReference string               `json:"confirmation_reference"`
	Evidence              []ExperienceEvidence `json:"evidence"`
	RelatedFacts          []string             `json:"related_facts,omitempty"`
}

func visibleAgreement(a *Agreement) *AgreementBody {
	body := &AgreementBody{
		Requirement: a.Requirement, Applicability: a.Applicability, Boundary: a.Boundary,
		Authority: a.Authority, ConfirmationReference: a.ConfirmationReference,
		Evidence: append([]ExperienceEvidence(nil), a.Evidence...),
	}
	for i := range body.Evidence {
		body.Evidence[i].SHA256 = ""
	}
	for _, cue := range a.Cues {
		if !strings.Contains(a.Requirement+a.Applicability+a.Boundary, cue.Text) {
			body.RelatedFacts = append(body.RelatedFacts, cue.Text)
		}
	}
	return body
}

func agreementKey(a *Agreement) string {
	copyA := *a
	copyA.ObjectIDPrefixes = nil
	copyA.Cues = cueContent(a.Cues)
	copyA.Evidence = append([]ExperienceEvidence(nil), a.Evidence...)
	for i := range copyA.Evidence {
		copyA.Evidence[i].SHA256 = ""
	}
	raw, _ := json.Marshal(copyA)
	return "agreement-v2:" + digestText(string(raw))
}

func entryCues(e Entry) []ExperienceCue {
	if e.Profile != nil {
		if e.Profile.Experience != nil {
			return e.Profile.Experience.Cues
		}
		if e.Profile.Agreement != nil {
			return e.Profile.Agreement.Cues
		}
	}
	return nil
}

func entryObjectPrefixes(e Entry) []string {
	if e.Profile != nil {
		if e.Profile.Experience != nil {
			return e.Profile.Experience.ObjectIDPrefixes
		}
		if e.Profile.Agreement != nil {
			return e.Profile.Agreement.ObjectIDPrefixes
		}
	}
	return nil
}

func profileExpired(p *Profile) bool {
	if p == nil || p.ExpiresAt == "" {
		return false
	}
	t, e := time.Parse(time.RFC3339, p.ExpiresAt)
	return e != nil || time.Now().After(t)
}
