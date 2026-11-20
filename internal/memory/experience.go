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

	"github.com/vaxjaz/memory-mcp/internal/matching"
)

// Experience is an observed failure pattern or a sourced business fact, never
// a prescribed implementation. The body has two mutually exclusive forms:
// counterexample (pitfall+counterexample) or fact. Legacy submissions that also
// fill impact/applicability/boundary stay valid; nothing is dropped.
// Evidence stores locators and concise observations, not chat transcripts/code.
type Experience struct {
	Cues             []ExperienceCue      `json:"cues" jsonschema:"Evidenced stable associations; provide with the synchronous write. New facts require a revision. Entrances are aliases, not separate risks."`
	ObjectIDPrefixes []string             `json:"object_id_prefixes,omitempty" jsonschema:"Prefixes of object identifiers for same-input business result association only; no effect on direct matching or search."`
	Pitfall          string               `json:"pitfall,omitempty" jsonschema:"Counterexample form (mutually exclusive with fact). 1-240 Unicode characters. The fallible inference that this incident challenged."`
	Counterexample   string               `json:"counterexample,omitempty" jsonschema:"Counterexample form (mutually exclusive with fact). 1-240 Unicode characters. The observation that overturned that inference, with the object/environment qualifiers it actually observed."`
	Fact             string               `json:"fact,omitempty" jsonschema:"Fact form (mutually exclusive with pitfall/counterexample). 1-240 Unicode characters. One sourced business fact that changes a specific judgment, keeping the object, environment or time qualifiers it was actually observed under."`
	Impact           string               `json:"impact,omitempty" jsonschema:"Optional legacy qualifier. Prefer writing the qualifiers into the fact or counterexample itself."`
	Applicability    string               `json:"applicability,omitempty" jsonschema:"Optional legacy qualifier. Prefer writing the conditions into the fact or counterexample itself."`
	Boundary         string               `json:"boundary,omitempty" jsonschema:"Optional legacy qualifier. Prefer writing exceptions and unknowns into the fact or counterexample itself."`
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

// ExperienceAdmission is checked at the write boundary and discarded after validation.
type ExperienceAdmission struct {
	FutureDecision string `json:"future_decision" jsonschema:"1-240 Unicode characters. Name the specific decision this experience will change when a similar situation recurs."`
	EvidenceRef    string `json:"evidence_ref" jsonschema:"Exact experience.evidence[].reference supporting that change in judgment."`
}

type ExperienceWrite struct {
	Admission  *ExperienceAdmission `json:"admission,omitempty" jsonschema:"Required for new writes and previous_id revisions. Correct task-specific misunderstandings without calling this tool. Explain a reusable judgment backed by a submitted evidence reference."`
	Scope      string               `json:"scope"`
	Project    string               `json:"project" jsonschema:"Actual bound project directory; not inferred from a model name or another machine"`
	Title      string               `json:"title"`
	Experience Experience           `json:"experience"`
	PreviousID string               `json:"previous_id,omitempty" jsonschema:"Full current case revision ID for a material correction. Send the complete revised experience, including evidence and cue references. Omit to create/deduplicate."`
	RequestID  string               `json:"request_id,omitempty" jsonschema:"Reuse only when retrying the same record payload"`
}

type ExperienceReceipt struct {
	Indexed        bool           `json:"indexed"`
	ID             string         `json:"id"`
	RootID         string         `json:"root_id"`
	Reused         bool           `json:"reused"`
	BodySHA256     string         `json:"body_sha256"`
	TriggerPreview TriggerPreview `json:"trigger_preview"`
	Notice         string         `json:"notice"`
}

// TriggerPreview describes the saved revision, independently of host dispatch.
type TriggerPreview struct {
	Scope         string            `json:"scope"`
	RecordStatus  string            `json:"record_status"`
	WakeupEnabled *bool             `json:"wakeup_enabled,omitempty"`
	EntranceCount int               `json:"entrance_count"`
	Entrances     []PreviewEntrance `json:"entrances,omitempty"`
}

type PreviewEntrance struct {
	Conditional bool   `json:"conditional,omitempty"`
	CueID       string `json:"cue_id"`
	Value       string `json:"value"`
	Field       string `json:"field,omitempty"`
	EntranceComparison
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
	var validation recordValidation
	validation.experience(x)
	return validation.err()
}

func (validation *recordValidation) experience(x *Experience) {
	if x == nil {
		validation.add("experience", "required")
		return
	}
	if err := validatePrefixes(x.ObjectIDPrefixes); err != nil {
		validation.add("experience.object_id_prefixes", err.Error())
	}
	// Two mutually exclusive body forms. Legacy submissions keep all five
	// narrative fields; qualifiers useful to the counterexample are written
	// into the fact or counterexample text itself.
	hasFact := strings.TrimSpace(x.Fact) != ""
	hasCounterexample := strings.TrimSpace(x.Pitfall) != "" || strings.TrimSpace(x.Counterexample) != ""
	switch {
	case hasFact && hasCounterexample:
		validation.add("experience", "fact and pitfall/counterexample are mutually exclusive body forms; submit one per record")
	case hasFact:
		validation.shortText("experience.fact", x.Fact, 240)
	case hasCounterexample:
		validation.shortText("experience.pitfall", x.Pitfall, 240)
		validation.shortText("experience.counterexample", x.Counterexample, 240)
	default:
		validation.add("experience", "requires one body form: fact, or pitfall with counterexample")
	}
	validation.optionalText("experience.impact", x.Impact, 240)
	validation.optionalText("experience.applicability", x.Applicability, 240)
	validation.optionalText("experience.boundary", x.Boundary, 240)
	validation.evidence("experience", x.Evidence)
	validation.cues("experience", x.Cues, x.Evidence, false)
	body, _ := json.Marshal(visibleExperience(x))
	if len(body) > 3000 {
		validation.add("experience", fmt.Sprintf("body budget is 3000 UTF-8 bytes, got %d; condense prose and preserve conditions", len(body)))
	}
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
	var validation recordValidation
	validation.experienceAdmission(in.Admission, in.Experience.Evidence)
	validation.experience(&in.Experience)
	if err := validation.err(); err != nil {
		return result, err
	}
	// Keep retries over the material payload stable across protocol upgrades.
	in.Admission = nil
	raw, _ := json.Marshal(in)
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
	var body string
	if experienceForm(x) == "fact" {
		body = "业务事实：" + x.Fact
	} else {
		body = fmt.Sprintf("易错推断：%s\n关键反例：%s", x.Pitfall, x.Counterexample)
		for _, line := range []struct{ label, value string }{{"Bug影响", x.Impact}, {"适用条件", x.Applicability}, {"边界与未知", x.Boundary}} {
			if strings.TrimSpace(line.value) != "" {
				body += "\n" + line.label + "：" + line.value
			}
		}
	}
	futureUse := x.Applicability
	if strings.TrimSpace(futureUse) == "" {
		futureUse = coreFact(x)
	}
	d := Draft{Kind: "case", Scope: in.Scope, Title: in.Title, Body: body, Origin: "model_inference", RequestID: in.RequestID,
		SourceRef: x.Evidence[0].Reference, Profile: &Profile{Class: "long_term", FutureUse: futureUse, CounterCheck: "Recorded counterexample or sourced business fact; ordinary recoverable facts are not admitted.", Applicability: x.Applicability, Boundary: x.Boundary, Experience: x}}
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
		TriggerPreview: PreviewTriggers(e),
		Notice:         "Saved failure features, not an instruction or prescribed destination. Code hashes/locators check mechanics; semantic evidence remains the host's responsibility."}
}

func PreviewTriggers(e Entry) TriggerPreview {
	out := TriggerPreview{Scope: e.Scope}
	switch {
	case !e.Current:
		out.RecordStatus = "superseded"
	case e.NeedsReview:
		out.RecordStatus = "needs_review"
	case e.State.State != "active":
		out.RecordStatus = "inactive"
	case profileExpired(e.Profile):
		out.RecordStatus = "expired"
	default:
		out.RecordStatus = "no_entrances"
		const maxPreviewEntrances = 4
		for _, cue := range entryCues(e) {
			for _, group := range cue.Matches {
				group.VisitEntrances(func(leaf *matching.Expression, value string) {
					out.EntranceCount++
					if len(out.Entrances) < maxPreviewEntrances {
						out.Entrances = append(out.Entrances, PreviewEntrance{CueID: cue.ID, Value: value, Field: leaf.Field, Conditional: group.When != nil || group.All != nil || group.Any != nil, EntranceComparison: entranceComparison(leaf.Rule)})
					}
				})
			}
		}
		if out.EntranceCount > 0 {
			out.RecordStatus = "eligible"
		}
	}
	return out
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
