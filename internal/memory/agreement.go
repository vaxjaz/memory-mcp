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
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

// A compact pointer to an existing, explicitly confirmed agreement. It is not
// manufactured failure evidence, a document manual, or new operational authority.
type Agreement struct {
	Anchors               []string             `json:"anchors" jsonschema:"1-6 actual identifiers, symbols or paths."`
	AnchorPrefixes        []string             `json:"anchor_prefixes,omitempty" jsonschema:"Confirmed ASCII identifier prefixes, not guesses or regexes."`
	Signals               []string             `json:"signals" jsonschema:"1-6 independent observable conditions."`
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
	PreviousID string    `json:"previous_id,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
}

func validatePrefixes(prefixes []string) error {
	if len(prefixes) > 6 {
		return errors.New("at most six evidence-grounded identifier prefixes")
	}
	seen := map[string]bool{}
	for _, p := range prefixes {
		if len(p) < 3 || len(p) > 40 || strings.TrimSpace(p) != p || genericExperienceAnchor[strings.ToLower(p)] {
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
	if err := validatePrefixes(a.AnchorPrefixes); err != nil {
		return err
	}
	for _, terms := range [][]string{a.Anchors, a.Signals} {
		if len(terms) < 1 || len(terms) > 6 {
			return errors.New("agreement anchors/signals need 1-6 specific grounded features")
		}
		seen := map[string]bool{}
		for _, v := range terms {
			k := strings.ToLower(strings.TrimSpace(v))
			if k == "" || utf8.RuneCountInString(v) < 2 || utf8.RuneCountInString(v) > 80 || genericExperienceAnchor[k] || seen[k] {
				return errors.New("invalid, generic or duplicate agreement feature")
			}
			if err := textguard.Check("agreement.feature", v, false); err != nil {
				return err
			}
			seen[k] = true
		}
	}
	for name, v := range map[string]string{"requirement": a.Requirement, "applicability": a.Applicability, "boundary": a.Boundary, "authority": a.Authority, "confirmation_reference": a.ConfirmationReference} {
		if strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > 240 {
			return fmt.Errorf("agreement.%s needs 1-240 characters", name)
		}
		if err := textguard.Check(name, v, false); err != nil {
			return err
		}
	}
	if len(a.Evidence) < 1 || len(a.Evidence) > 3 {
		return errors.New("agreement needs 1-3 actual evidence locators")
	}
	// Reuse evidence admission without asserting a historical failure occurred.
	probe := Experience{Anchors: a.Anchors, Signals: a.Signals, Pitfall: "validation", Counterexample: "validation", Impact: "validation", Applicability: a.Applicability, Boundary: a.Boundary, Evidence: a.Evidence}
	if err := validateExperience(&probe); err != nil {
		return err
	}
	b, _ := json.Marshal(a)
	if len(b) > 3000 {
		return errors.New("agreement reference exceeds 3000 bytes; preserve conditions rather than copying source text")
	}
	return nil
}

func (s *Store) RecordAgreement(ctx context.Context, in AgreementWrite) (ExperienceReceipt, error) {
	var zero ExperienceReceipt
	if err := validateAgreement(&in.Agreement); err != nil {
		return zero, err
	}
	project, err := s.experienceProject(ctx, in.Project, in.Scope)
	if err != nil {
		return zero, err
	}
	original, _ := json.Marshal(in)
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
	keys := a
	keys.Anchors = append([]string(nil), a.Anchors...)
	keys.Signals = append([]string(nil), a.Signals...)
	keys.AnchorPrefixes = append([]string(nil), a.AnchorPrefixes...)
	keys.Evidence = append([]ExperienceEvidence(nil), a.Evidence...)
	for i := range keys.Evidence {
		if keys.Evidence[i].Path != "" {
			keys.Evidence[i].SHA256 = ""
		}
	}
	sort.Strings(keys.Anchors)
	sort.Strings(keys.Signals)
	sort.Strings(keys.AnchorPrefixes)
	b, _ := json.Marshal(keys)
	d := Draft{Kind: "record", Scope: in.Scope, Title: in.Title, Body: fmt.Sprintf("识别特征：%s；%s\n已确认约定：%s\n适用条件：%s\n边界与未知：%s\n权威来源：%s", strings.Join(a.Anchors, ", "), strings.Join(a.Signals, ", "), a.Requirement, a.Applicability, a.Boundary, a.Authority), Origin: "user_supplied", SourceRef: a.ConfirmationReference, RequestID: in.RequestID, Profile: &Profile{Class: "long_term", FutureUse: a.Applicability, CounterCheck: "Pointer to an explicitly confirmed agreement; not inferred from an agent or a correction alone.", Applicability: a.Applicability, Boundary: a.Boundary, Agreement: &a}}
	d.referenceValidated = true
	d.experienceKey = "agreement-v1:" + digestText(string(b))
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

func recognitionFeatures(e Entry) (anchors, signals, prefixes []string) {
	if e.Profile == nil {
		return
	}
	if x := e.Profile.Experience; x != nil {
		return x.Anchors, x.Signals, x.AnchorPrefixes
	}
	if x := e.Profile.Agreement; x != nil {
		return x.Anchors, x.Signals, x.AnchorPrefixes
	}
	return
}

func profileExpired(p *Profile) bool {
	if p == nil || p.ExpiresAt == "" {
		return false
	}
	t, e := time.Parse(time.RFC3339, p.ExpiresAt)
	return e != nil || time.Now().After(t)
}
