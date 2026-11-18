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
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Experience is an observed failure pattern, never a prescribed implementation.
// Evidence stores locators and concise observations, not chat transcripts/code.
type Experience struct {
	Cues             []ExperienceCue      `json:"cues" jsonschema:"Evidenced stable associations; provide with the synchronous write. New facts require a revision. Entrances are aliases, not separate risks."`
	ObjectIDPrefixes []string             `json:"object_id_prefixes,omitempty" jsonschema:"Prefixes of object identifiers for same-input business result association only; no effect on direct matching or search."`
	Pitfall          string               `json:"pitfall" jsonschema:"1-240 Unicode characters. The fallible inference that this incident challenged."`
	Counterexample   string               `json:"counterexample" jsonschema:"1-240 Unicode characters. Summarize the observation that challenges the inference and separate it from the explanation."`
	Impact           string               `json:"impact" jsonschema:"1-240 Unicode characters. Concrete possible bug, affected behavior or wrong decision if this condition is missed."`
	Applicability    string               `json:"applicability" jsonschema:"1-240 Unicode characters. Conditions in which this failure pattern is worth investigating."`
	Boundary         string               `json:"boundary" jsonschema:"1-240 Unicode characters. Exceptions, different environments and unknowns."`
	Evidence         []ExperienceEvidence `json:"evidence" jsonschema:"1-3 actual evidence locators; no entire conversations or source files"`
}

type ExperienceEvidence struct {
	Kind        string `json:"kind" jsonschema:"code/runtime/business; host verifies truth."`
	Reference   string `json:"reference" jsonschema:"1-240 Unicode characters. Actual commit, trace or confirmed-agreement locator. Cue references copy this exact value."`
	Observation string `json:"observation" jsonschema:"1-180 Unicode characters. Compact verified observation; no raw source or assumed cause."`
	Path        string `json:"path,omitempty" jsonschema:"Existing file within bound workspace; required for code."`
	Symbol      string `json:"symbol,omitempty" jsonschema:"Literal locator, not semantic proof."`
	SHA256      string `json:"sha256,omitempty" jsonschema:"Optional expected SHA256; rejects local source drift."`
}

type ExperienceWrite struct {
	Scope      string     `json:"scope"`
	Project    string     `json:"project" jsonschema:"Actual bound project directory; not inferred from a model name or another machine"`
	Title      string     `json:"title"`
	Experience Experience `json:"experience"`
	PreviousID string     `json:"previous_id,omitempty" jsonschema:"Full current case revision ID for a material correction. Send the complete revised experience, including evidence and cue references. Omit to create/deduplicate."`
	RequestID  string     `json:"request_id,omitempty" jsonschema:"Reuse only when retrying the exact same submitted write"`
}

type ExperienceReceipt struct {
	Indexed    bool   `json:"indexed"`
	ID         string `json:"id"`
	RootID     string `json:"root_id"`
	Reused     bool   `json:"reused"`
	BodySHA256 string `json:"body_sha256"`
	Notice     string `json:"notice"`
}

type EvidenceLocation struct {
	Reference      string `json:"reference"`
	Path           string `json:"path,omitempty"`
	Symbol         string `json:"symbol,omitempty"`
	Lines          []int  `json:"lines,omitempty"`
	CurrentSHA256  string `json:"current_sha256,omitempty"`
	ContentChanged bool   `json:"content_changed,omitempty"`
	Status         string `json:"status"`
}

type ExperienceItem struct {
	BodySHA256     string             `json:"body_sha256"`
	FeatureSHA256  string             `json:"feature_sha256,omitempty"`
	ID             string             `json:"id"`
	RootID         string             `json:"root_id"`
	Title          string             `json:"title"`
	Experience     *ExperienceBody    `json:"experience"`
	Agreement      *AgreementBody     `json:"agreement,omitempty"`
	SourceKind     string             `json:"source_kind,omitempty"`
	Excerpt        string             `json:"excerpt,omitempty"`
	NeedsReview    bool               `json:"needs_review,omitempty"`
	MatchedQueries []int              `json:"matched_queries,omitempty"`
	MatchedTerms   []string           `json:"matched_terms,omitempty"`
	Locations      []EvidenceLocation `json:"locations,omitempty"`
}

var genericEntranceValue = map[string]bool{
	"代码": true, "检查": true, "方案": true, "需求": true, "项目": true,
	"service": true, "controller": true, "main": true, "test": true, "code": true,
}

func validateExperience(x *Experience) error {
	if x == nil {
		return errors.New("experience required")
	}
	var validation recordValidation
	if err := validatePrefixes(x.ObjectIDPrefixes); err != nil {
		validation.add("experience.object_id_prefixes", err.Error())
	}
	validation.shortText("experience.pitfall", x.Pitfall, 240)
	validation.shortText("experience.counterexample", x.Counterexample, 240)
	validation.shortText("experience.impact", x.Impact, 240)
	validation.shortText("experience.applicability", x.Applicability, 240)
	validation.shortText("experience.boundary", x.Boundary, 240)
	validation.evidence("experience", x.Evidence)
	validation.cues("experience", x.Cues, x.Evidence)
	body, _ := json.Marshal(visibleExperience(x))
	if len(body) > 3000 {
		validation.add("experience", fmt.Sprintf("body budget is 3000 UTF-8 bytes, got %d; condense prose and preserve conditions", len(body)))
	}
	return validation.err()
}

// Resolve symlinks before reading; memory content never authorizes external reads.
func experienceFile(project, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") && filepath.Separator != '\\' {
		return "", errors.New("code path must be project-relative")
	}
	root, err := filepath.EvalSymlinks(project)
	if err != nil {
		return "", err
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("code evidence escapes bound project")
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == ".git" || part == ".aws" || part == ".ssh" || part == ".env" {
			return "", errors.New("credential/VCS paths are not code evidence")
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {
		return "", errors.New("code evidence must be a regular file of at most 2 MiB")
	}
	return target, nil
}

func (s *Store) experienceProject(ctx context.Context, project, scope string) (string, error) {
	if err := validScope(scope); err != nil {
		return "", err
	}
	if scope == "global" {
		return "", errors.New("experience trial requires explicit project scope")
	}
	abs, err := filepath.Abs(project)
	if err != nil || project == "" {
		return "", errors.New("actual bound project directory required")
	}
	bound, err := s.HookResolve(ctx, abs)
	if err != nil || bound != scope {
		return "", errors.New("project/scope binding mismatch")
	}
	return abs, nil
}

func experienceKey(x *Experience) string {
	// Content-equivalent writes do not create revisions merely for changed line
	// positions or code hashes. The original observed evidence remains historical.
	copyX := *x
	copyX.ObjectIDPrefixes = nil
	copyX.Cues = cueContent(x.Cues)
	for i := range copyX.Cues {
		for j, ref := range copyX.Cues[i].EvidenceRefs {
			for _, e := range x.Evidence {
				if e.Kind == "code" && e.Reference == ref {
					copyX.Cues[i].EvidenceRefs[j] = "code:" + e.Path + ":" + e.Symbol
				}
			}
		}
		sort.Strings(copyX.Cues[i].EvidenceRefs)
	}

	copyX.Evidence = append([]ExperienceEvidence(nil), x.Evidence...)
	for i := range copyX.Evidence {
		copyX.Evidence[i].SHA256 = ""
		if copyX.Evidence[i].Kind == "code" {
			copyX.Evidence[i].Reference = ""
		}
	}
	b, _ := json.Marshal(copyX)
	return digestText(string(b))
}

func (s *Store) RecordExperience(ctx context.Context, in ExperienceWrite) (ExperienceReceipt, error) {
	var result ExperienceReceipt
	raw, _ := json.Marshal(in)
	if err := validateExperience(&in.Experience); err != nil {
		return result, err
	}
	project, err := s.experienceProject(ctx, in.Project, in.Scope)
	if err != nil {
		return result, err
	}
	requestFingerprint := "experience-v1:" + digestText(string(raw))
	// Hash enrichment must not mutate the caller's slice or change a retry's
	// original payload (in-process hosts/tests may reuse that exact object).
	in.Experience.Evidence = append([]ExperienceEvidence(nil), in.Experience.Evidence...)
	if in.RequestID != "" {
		var fp, id string
		err = s.db.QueryRowContext(ctx, "SELECT fingerprint,entry_id FROM write_requests WHERE scope=? AND request_id=?", in.Scope, in.RequestID).Scan(&fp, &id)
		if err == nil {
			if fp != requestFingerprint {
				return result, errors.New("request_id conflict: experience payload changed")
			}
			old, e := s.Read(ctx, id)
			if e != nil {
				return result, e
			}
			return experienceReceipt(old.Entry, true), nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
	}
	for i := range in.Experience.Evidence {
		e := &in.Experience.Evidence[i]
		if e.Kind != "code" && e.Path == "" {
			continue
		}
		path, er := experienceFile(project, e.Path)
		if er != nil {
			return result, fmt.Errorf("evidence[%d]: %w", i, er)
		}
		body, er := os.ReadFile(path)
		if er != nil {
			return result, er
		}
		hash := sha256.Sum256(body)
		actual := hex.EncodeToString(hash[:])
		if e.SHA256 != "" && e.SHA256 != actual {
			return result, errors.New("code evidence changed: recheck it before recording")
		}
		if e.Symbol != "" && !strings.Contains(string(body), e.Symbol) {
			return result, fmt.Errorf("code symbol %q not found in supplied file", e.Symbol)
		}
		e.SHA256 = actual
	}
	x := &in.Experience
	body := fmt.Sprintf("易错推断：%s\n关键反例：%s\nBug影响：%s\n适用条件：%s\n边界与未知：%s",
		x.Pitfall, x.Counterexample, x.Impact, x.Applicability, x.Boundary)
	d := Draft{Kind: "case", Scope: in.Scope, Title: in.Title, Body: body, Origin: "model_inference", RequestID: in.RequestID,
		SourceRef: x.Evidence[0].Reference, Profile: &Profile{Class: "long_term", FutureUse: x.Applicability, CounterCheck: "Recorded counterexample and concrete impact; ordinary recoverable facts are not admitted.", Applicability: x.Applicability, Boundary: x.Boundary, Experience: x}}
	d.experienceValidated = true
	d.experienceKey = experienceKey(x)
	d.requestFingerprint = requestFingerprint
	e, err := s.Put(ctx, d, in.PreviousID)
	if err != nil {
		return result, err
	}
	return experienceReceipt(e, e.writeReused), nil
}

func experienceReceipt(e Entry, reused bool) ExperienceReceipt {
	return ExperienceReceipt{ID: e.ID, RootID: e.RootID, Reused: reused, BodySHA256: e.BodySHA256, Indexed: true,
		Notice: "Saved failure features, not an instruction or prescribed destination. Code hashes/locators check mechanics; semantic evidence remains the host's responsibility."}
}

// Load only explicit compact experiences. Legacy prose is still searchable but
// is never silently promoted into automatic intervention rules.
func (s *Store) Experiences(ctx context.Context, scope string) ([]Entry, error) {
	query := "SELECT " + columns + " FROM entries JOIN entry_profiles p ON p.entry_id=entries.id WHERE current=1 AND needs_review=0 AND scope=? AND (json_array_length(p.profile_json,'$.experience.cues')>0 OR json_array_length(p.profile_json,'$.agreement.cues')>0) ORDER BY created_at DESC,id DESC LIMIT 2001"
	rows, err := s.db.QueryContext(ctx, query, scope)
	if err != nil {
		return nil, err
	}
	var candidates []Entry
	for rows.Next() {
		e, er := scanEntry(rows)
		if er != nil {
			rows.Close()
			return nil, er
		}
		candidates = append(candidates, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(candidates) > 2000 {
		return nil, errors.New("experience scan limit reached; no incomplete intervention index is used")
	}
	var out []Entry
	for _, e := range candidates {
		if err = loadGovernance(ctx, s.db, &e); err != nil {
			return nil, err
		}
		if e.NeedsReview || e.State.State != "active" || e.Profile == nil || (e.Profile.Experience == nil && e.Profile.Agreement == nil) {
			continue
		}
		if e.Profile.ExpiresAt != "" {
			expiry, er := time.Parse(time.RFC3339, e.Profile.ExpiresAt)
			if er != nil || time.Now().After(expiry) {
				continue
			}
		}
		if e.Profile.Experience != nil {
			checked := *e.Profile.Experience
			if err = validateExperience(&checked); err != nil {
				continue
			}
		}
		if e.Profile.Agreement != nil {
			checked := *e.Profile.Agreement
			if err = validateAgreement(&checked); err != nil {
				continue
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func LocateExperience(ctx context.Context, project string, x *Experience) []EvidenceLocation {
	var out []EvidenceLocation
	for _, e := range x.Evidence {
		if ctx.Err() != nil {
			break
		}
		item := EvidenceLocation{Reference: e.Reference, Path: e.Path, Symbol: e.Symbol, Status: "external_reference_not_revalidated"}
		if e.Kind != "code" && e.Path == "" {
			out = append(out, item)
			continue
		}
		path, err := experienceFile(project, e.Path)
		if err != nil {
			item.Status = "unresolved"
			out = append(out, item)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			item.Status = "unresolved"
			out = append(out, item)
			continue
		}
		hash := sha256.Sum256(data)
		item.CurrentSHA256 = hex.EncodeToString(hash[:])
		item.ContentChanged = e.SHA256 != "" && item.CurrentSHA256 != e.SHA256
		item.Status = "file_located"
		if e.Symbol != "" {
			for n, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, e.Symbol) {
					item.Lines = append(item.Lines, n+1)
					if len(item.Lines) == 4 {
						break
					}
				}
			}
			if len(item.Lines) == 0 {
				item.Status = "symbol_unresolved"
			} else {
				item.Status = "literal_locations_not_semantic_verification"
			}
		}
		out = append(out, item)
	}
	return out
}
