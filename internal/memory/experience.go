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
	"strconv"
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
	Resolution *WriteResolution     `json:"resolution,omitempty" jsonschema:"Second call only: the judged relationship to the compared candidates, with the token from the needs_comparison response. Never invent a token."`
	Cursor     string               `json:"cursor,omitempty" jsonschema:"next_cursor from a needs_comparison response to read the next candidate page for this unchanged draft."`
}

// WriteResolution is the agent's judgment of how a new observation relates to
// the returned candidates. The MCP re-checks the candidate set and target in
// the write path; a changed world invalidates the token instead of committing.
type WriteResolution struct {
	Action          string `json:"action" jsonschema:"reuse | supplement | revise | create | defer. reuse: same claim, same scope, evidence already covered. supplement: same claim plus a genuinely independent new source. revise: new evidence overturns the old record. create: an independently standing fact, even when sharing a cue or snapshot. defer: distinguishing evidence is missing."`
	TargetID        string `json:"target_id,omitempty" jsonschema:"Current version ID of the compared candidate. Required for reuse/supplement/revise; for create it only marks the candidate distinguished, never a previous_id."`
	Difference      string `json:"difference,omitempty" jsonschema:"The minimal cited difference between this observation and the candidate."`
	EvidenceRef     string `json:"evidence_ref,omitempty" jsonschema:"Evidence reference from this submission supporting the claimed relationship."`
	ComparisonToken string `json:"comparison_token,omitempty" jsonschema:"Opaque token returned by this draft's needs_comparison response."`
}

type ExperienceReceipt struct {
	Indexed         bool             `json:"indexed"`
	ID              string           `json:"id"`
	RootID          string           `json:"root_id"`
	Reused          bool             `json:"reused"`
	BodySHA256      string           `json:"body_sha256"`
	TriggerPreview  TriggerPreview   `json:"trigger_preview"`
	Notice          string           `json:"notice"`
	Status          string           `json:"status,omitempty"`
	Action          string           `json:"action,omitempty"`
	ComparisonToken string           `json:"comparison_token,omitempty"`
	Candidates      []WriteCandidate `json:"candidates,omitempty"`
	HasMore         bool             `json:"has_more,omitempty"`
	NextCursor      string           `json:"next_cursor,omitempty"`
	// ResolutionTemplate is pre-filled in every needs_comparison response so
	// completing the second phase requires no prior knowledge of the protocol.
	ResolutionTemplate *WriteResolution `json:"resolution_template,omitempty"`
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

// experienceWriteBoundary enforces protocol-12 write rules that stored history
// must still satisfy on read: new experiences do not author cue text, and
// fact-form records carry their qualifiers inside the fact sentence. Cue IDs in
// inherited carry historical text from the previous revision, not authored
// content.
func (validation *recordValidation) experienceWriteBoundary(x *Experience, inherited map[string]bool) {
	for i, c := range x.Cues {
		if strings.TrimSpace(c.Text) != "" && !inherited[c.ID] {
			validation.add(fmt.Sprintf("experience.cues[%d].text", i), "new experience writes do not author cue text; move the fact or counterexample into the body and resubmit without cue text")
		}
	}
	if strings.TrimSpace(x.Fact) == "" {
		return
	}
	for _, column := range []struct{ field, value string }{{"impact", x.Impact}, {"applicability", x.Applicability}, {"boundary", x.Boundary}} {
		if strings.TrimSpace(column.value) != "" {
			validation.add("experience."+column.field, "fact-form records keep object/environment/time qualifiers inside the fact sentence; merge this qualifier into experience.fact and resubmit")
		}
	}
}

// dev3EnrichedFingerprint rebuilds, read-only, the fingerprint dev.3 computed
// for a revision: it enriched cue text from the previous revision before
// marshalling. A committed dev.3 receipt only matches under that computation;
// the rebuild uses the request's own previous_id and changes nothing on disk.
// Any other shape (or a missing history) yields a value that matches nothing.
func (s *Store) dev3EnrichedFingerprint(ctx context.Context, in ExperienceWrite) string {
	if in.PreviousID == "" {
		return ""
	}
	prev, err := s.Read(ctx, in.PreviousID)
	if err != nil || prev.Entry.Profile == nil || prev.Entry.Profile.Experience == nil {
		return ""
	}
	legacy := in
	legacy.Admission = nil
	legacy.Experience.Cues = append([]ExperienceCue(nil), in.Experience.Cues...)
	texts := map[string]string{}
	for _, cue := range prev.Entry.Profile.Experience.Cues {
		if strings.TrimSpace(cue.Text) != "" {
			texts[cue.ID] = cue.Text
		}
	}
	for i := range legacy.Experience.Cues {
		if strings.TrimSpace(legacy.Experience.Cues[i].Text) == "" && texts[legacy.Experience.Cues[i].ID] != "" {
			legacy.Experience.Cues[i].Text = texts[legacy.Experience.Cues[i].ID]
		}
	}
	raw, _ := json.Marshal(legacy)
	return "experience-v1:" + digestText(string(raw))
}

// bodyIdentityKey is the content identity with cue texts AND entrance aliases
// removed: body, qualifiers and evidence identity only. Equal keys mean the
// submission is a pure alias update of the previous revision.
func bodyIdentityKey(x *Experience) string {
	copyX := *x
	copyX.Cues = append([]ExperienceCue(nil), x.Cues...)
	for i := range copyX.Cues {
		copyX.Cues[i].Text = ""
	}
	return experienceKey(&copyX)
}

// inheritCueText restores unchanged cue text from the previous revision of the
// same cue ID, but ONLY for a pure alias update: when body, qualifiers or
// supporting evidence materially changed, the superseded text is not carried
// into the new version and reminders must come from the new body. Authoring or
// rewriting cue text is still rejected at the boundary.
func inheritCueText(previous, submitted *Experience) map[string]bool {
	if previous == nil || submitted == nil || bodyIdentityKey(previous) != bodyIdentityKey(submitted) {
		return nil
	}
	texts := map[string]string{}
	for _, cue := range previous.Cues {
		if strings.TrimSpace(cue.Text) != "" {
			texts[cue.ID] = cue.Text
		}
	}
	inherited := map[string]bool{}
	for i := range submitted.Cues {
		id := submitted.Cues[i].ID
		if strings.TrimSpace(submitted.Cues[i].Text) == "" && texts[id] != "" {
			submitted.Cues[i].Text = texts[id]
			inherited[id] = true
		}
	}
	return inherited
}

// comparisonNotice is self-contained: an agent that has never read the guide
// must be able to complete the two-phase write from this response alone.
const comparisonNotice = "同项目存在可比旧经验，本次调用零写入。请比较：假设旧经验原句在它注明的对象、时间和环境内完全成立，本次证据还增加了哪一项可核查的事实？若只是转述→reuse；多了独立来源→supplement；证据推翻旧原句→revise；两条可同时成立且各自影响不同判断→create；区分证据缺失→defer。判定后：用同一草案和同一 request_id 重新调用本工具，并在 resolution 字段填入 resolution_template（把 action 填为上述五者之一、target_id 换成你选中的候选当前 ID、difference 写引用了旧原句与本次证据的最小差异）；token 与 evidence_ref 已预填。模板和候选见下。"

// writeComparisonAbort rolls a create back inside Put's transaction when the
// final in-transaction candidate recheck finds a comparable record.
type writeComparisonAbort struct {
	candidates []WriteCandidate
}

func (a *writeComparisonAbort) Error() string {
	return "comparison candidates appeared before commit"
}

func (s *Store) RecordExperience(ctx context.Context, in ExperienceWrite) (ExperienceReceipt, error) {
	var result ExperienceReceipt
	// A replayed committed request must satisfy the binding that governs this
	// directory right now; it predates newer validation, not the binding.
	if in.RequestID != "" {
		if _, err := s.experienceProject(ctx, in.Project, in.Scope); err != nil {
			return result, err
		}
	}
	// The request fingerprint stays over the payload exactly as submitted
	// (admission, resolution and paging cursor excluded), so both phases of one
	// comparison and retries across binary upgrades share it.
	resolution := in.Resolution
	admission := in.Admission
	pageCursor := in.Cursor
	in.Resolution = nil
	in.Admission = nil
	in.Cursor = ""
	raw, _ := json.Marshal(in)
	requestFingerprint := "experience-v1:" + digestText(string(raw))
	if in.RequestID != "" {
		var fp, id string
		err := s.db.QueryRowContext(ctx, "SELECT fingerprint,entry_id FROM write_requests WHERE scope=? AND request_id=?", in.Scope, in.RequestID).Scan(&fp, &id)
		if err == nil {
			if fp != requestFingerprint && fp != s.dev3EnrichedFingerprint(ctx, in) {
				return result, errors.New("request_id conflict: experience payload changed")
			}
			// Replay the committed ACTION and target, not whatever this retry
			// carries; a different action or target under the same ID conflicts.
			var action, targetID string
			outcomeErr := s.db.QueryRowContext(ctx, "SELECT action,target_id FROM experience_request_outcomes WHERE scope=? AND request_id=?", in.Scope, in.RequestID).Scan(&action, &targetID)
			if outcomeErr == nil && resolution != nil && (resolution.Action != action || resolution.TargetID != targetID) {
				return result, fmt.Errorf("request_id conflict: this request already committed action=%s target=%s", action, targetID)
			}
			old, e := s.Read(ctx, id)
			if e != nil {
				return result, e
			}
			receipt := experienceReceipt(old.Entry, true)
			if outcomeErr == nil {
				receipt.Action = action
			} else if resolution != nil {
				receipt.Action = resolution.Action
			}
			return receipt, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
	}
	// A pure alias update inherits the unchanged historical cue text so it
	// keeps the same version, delivery receipts and stored facts.
	var inherited map[string]bool
	if in.PreviousID != "" {
		if prev, e := s.Read(ctx, in.PreviousID); e == nil && prev.Entry.Scope == in.Scope && prev.Entry.Profile != nil && prev.Entry.Profile.Experience != nil {
			inherited = inheritCueText(prev.Entry.Profile.Experience, &in.Experience)
		}
	}
	var validation recordValidation
	validation.experienceAdmission(admission, in.Experience.Evidence)
	validation.experience(&in.Experience)
	validation.experienceWriteBoundary(&in.Experience, inherited)
	validation.writeResolution(resolution, in.Experience.Evidence)
	if err := validation.err(); err != nil {
		return result, err
	}
	project, err := s.experienceProject(ctx, in.Project, in.Scope)
	if err != nil {
		return result, err
	}
	// Hash enrichment must not mutate the caller's slice or change a retry's
	// original payload (in-process hosts/tests may reuse that exact object).
	in.Experience.Evidence = append([]ExperienceEvidence(nil), in.Experience.Evidence...)
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
	// W(x) write judgment. Explicit revisions keep the direct path; exact
	// structural content reuses without a comparison; everything else searches
	// same-project candidates inside the write path — a hit returns the old and
	// new facts with zero writes until the agent resolves the relationship.
	if in.PreviousID == "" && resolution == nil {
		var root string
		err := s.db.QueryRowContext(ctx, "SELECT root_id FROM experience_keys WHERE scope=? AND fingerprint=?", in.Scope, experienceKey(x)).Scan(&root)
		switch {
		case err == nil:
			// Exact structural reuse: fall through to Put's dedup path.
		case errors.Is(err, sql.ErrNoRows):
			candidates, er := s.writeCandidates(ctx, s.db, in.Scope, x)
			if er != nil {
				return result, er
			}
			if len(candidates) > 0 {
				return comparisonPage(in, requestFingerprint, candidates, pageCursor, admissionEvidenceRef(admission)), nil
			}
			// No candidate outside; re-check inside the write transaction so a
			// concurrent writer turns this create into a comparison.
			return s.commitNewRoot(ctx, in, requestFingerprint, x, "", "", admissionEvidenceRef(admission))
		default:
			return result, err
		}
		return s.commitExperience(ctx, in, requestFingerprint, x, "create", "", nil)
	}
	if resolution != nil {
		return s.resolveWriteComparison(ctx, in, resolution, requestFingerprint, x, admissionEvidenceRef(admission))
	}
	return s.commitExperience(ctx, in, requestFingerprint, x, "revise", in.PreviousID, nil)
}

// comparisonPage slices the ranked candidate list for one response and keeps a
// cursor so records beyond the first page stay reachable for this draft. The
// prefilled resolution_template makes the second phase completable from this
// response alone: token, evidence and a default target are server-known; only
// the action and the cited difference remain for the agent's judgment.
func comparisonPage(in ExperienceWrite, fingerprint string, candidates []WriteCandidate, cursor, draftEvidenceRef string) ExperienceReceipt {
	token := comparisonToken(in.Scope, fingerprint, candidateIDs(candidates))
	start := 0
	if cursor != "" {
		if page, err := decodeCursor(cursor, token); err == nil {
			start, _ = strconv.Atoi(page.Sort)
		}
	}
	if start < 0 || start >= len(candidates) {
		start = 0
	}
	end := start + writeCandidateLimit
	if end > len(candidates) {
		end = len(candidates)
	}
	receipt := ExperienceReceipt{Indexed: false, Status: "needs_comparison", ComparisonToken: token, Candidates: candidates[start:end], Notice: comparisonNotice,
		ResolutionTemplate: &WriteResolution{ComparisonToken: token, EvidenceRef: draftEvidenceRef, TargetID: candidates[start].ID}}
	if end < len(candidates) {
		receipt.HasMore = true
		receipt.NextCursor = encodeCursor(pageCursor{Version: 1, Filter: token, Sort: strconv.Itoa(end), ID: digestText(token)[:32]})
	}
	return receipt
}

func candidateIDs(candidates []WriteCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	return ids
}

// commitNewRoot commits a first-seen observation and re-checks candidates
// inside Put's write transaction; a concurrent comparable write aborts into a
// comparison instead of a second root.
func (s *Store) commitNewRoot(ctx context.Context, in ExperienceWrite, requestFingerprint string, x *Experience, distinguished, expectedToken, draftEvidenceRef string) (ExperienceReceipt, error) {
	recheck := func(ctx context.Context, tx *sql.Tx) error {
		found, err := s.writeCandidates(ctx, tx, in.Scope, x)
		if err != nil {
			return err
		}
		if expectedToken != "" && comparisonToken(in.Scope, requestFingerprint, candidateIDs(found)) == expectedToken {
			return nil
		}
		if len(found) == 0 {
			return nil
		}
		return &writeComparisonAbort{candidates: found}
	}
	receipt, err := s.commitExperience(ctx, in, requestFingerprint, x, "create", distinguished, recheck)
	var abort *writeComparisonAbort
	if errors.As(err, &abort) {
		return comparisonPage(in, requestFingerprint, abort.candidates, "", draftEvidenceRef), nil
	}
	return receipt, err
}

// commitExperience stores the validated draft and records the committed
// action/target so replays return the original outcome. action reports the
// semantic outcome: create (new root), revise (explicit or resolved revision),
// reuse (exact structural content) or supplement (evidence-only revision).
func (s *Store) commitExperience(ctx context.Context, in ExperienceWrite, requestFingerprint string, x *Experience, action, targetID string, preCommit func(context.Context, *sql.Tx) error) (ExperienceReceipt, error) {
	var result ExperienceReceipt
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
	d.preCommit = preCommit
	e, err := s.Put(ctx, d, in.PreviousID)
	if err != nil {
		return result, err
	}
	result = experienceReceipt(e, e.writeReused)
	if e.writeReused {
		result.Action = "reuse"
	} else {
		result.Action = action
	}
	if in.RequestID != "" {
		if _, err = s.db.ExecContext(ctx, "INSERT OR IGNORE INTO experience_request_outcomes(scope,request_id,action,target_id)VALUES(?,?,?,?)", in.Scope, in.RequestID, result.Action, targetID); err != nil {
			return result, err
		}
	}
	return result, nil
}

// resolveWriteComparison executes the agent's judged relationship between the
// draft and the compared candidates. The candidate set is re-derived here; a
// changed set (or draft) invalidates the token and returns the fresh
// candidates instead of committing anything. reuse/supplement/revise may only
// target a candidate THIS comparison actually showed, still current.
func (s *Store) resolveWriteComparison(ctx context.Context, in ExperienceWrite, resolution *WriteResolution, requestFingerprint string, x *Experience, draftEvidenceRef string) (ExperienceReceipt, error) {
	var result ExperienceReceipt
	candidates, err := s.writeCandidates(ctx, s.db, in.Scope, x)
	if err != nil {
		return result, err
	}
	ids := candidateIDs(candidates)
	if resolution.Action == "defer" {
		return ExperienceReceipt{Indexed: false, Status: "deferred", Notice: "Nothing was merged or changed. Continue the current task; submit again when distinguishing evidence is available."}, nil
	}
	if resolution.ComparisonToken != comparisonToken(in.Scope, requestFingerprint, ids) {
		return comparisonPage(in, requestFingerprint, candidates, "", draftEvidenceRef), nil
	}
	if resolution.Action != "create" && !containsString(ids, resolution.TargetID) {
		return result, missingLookup("comparison_target_not_shown", "resolution.target_id", resolution.TargetID, in.Scope, "This target was not among the candidates shown for this comparison. Pick a listed candidate current ID or re-read the fresh list; nothing was written.")
	}
	switch resolution.Action {
	case "reuse":
		target, e := s.Read(ctx, resolution.TargetID)
		if e != nil || target.Entry.Scope != in.Scope || !target.Entry.Current || target.Entry.Profile == nil || target.Entry.Profile.Experience == nil {
			return result, missingLookup("comparison_target_not_current", "resolution.target_id", resolution.TargetID, in.Scope, "The compared candidate is not a current experience in this scope; re-read the fresh candidate list.")
		}
		if in.RequestID != "" {
			if _, e = s.db.ExecContext(ctx, "INSERT OR IGNORE INTO write_requests(scope,request_id,fingerprint,entry_id)VALUES(?,?,?,?)", in.Scope, in.RequestID, requestFingerprint, target.Entry.ID); e != nil {
				return result, e
			}
			if _, e = s.db.ExecContext(ctx, "INSERT OR IGNORE INTO experience_request_outcomes(scope,request_id,action,target_id)VALUES(?,?,?,?)", in.Scope, in.RequestID, "reuse", target.Entry.ID); e != nil {
				return result, e
			}
		}
		receipt := experienceReceipt(target.Entry, true)
		receipt.Action = "reuse"
		return receipt, nil
	case "create":
		// target_id only marks a distinguished candidate; never a previous_id.
		// The final candidate re-check runs inside the write transaction.
		return s.commitNewRoot(ctx, in, requestFingerprint, x, resolution.TargetID, resolution.ComparisonToken, draftEvidenceRef)
	case "revise":
		target, e := s.Read(ctx, resolution.TargetID)
		if e != nil || target.Entry.Scope != in.Scope || !target.Entry.Current || target.Entry.Profile == nil || target.Entry.Profile.Experience == nil {
			return result, missingLookup("comparison_target_not_current", "resolution.target_id", resolution.TargetID, in.Scope, "The compared candidate is not a current experience in this scope; re-read the fresh candidate list.")
		}
		revision := in
		revision.PreviousID = resolution.TargetID
		return s.commitExperience(ctx, revision, requestFingerprint, x, "revise", resolution.TargetID, nil)
	case "supplement":
		target, e := s.Read(ctx, resolution.TargetID)
		if e != nil || target.Entry.Scope != in.Scope || !target.Entry.Current || target.Entry.Profile == nil || target.Entry.Profile.Experience == nil {
			return result, missingLookup("comparison_target_not_current", "resolution.target_id", resolution.TargetID, in.Scope, "The compared candidate is not a current experience in this scope; re-read the fresh candidate list.")
		}
		old := target.Entry.Profile.Experience
		merged := *old
		merged.Evidence = append([]ExperienceEvidence(nil), old.Evidence...)
		merged.Cues = append([]ExperienceCue(nil), old.Cues...)
		for _, candidate := range x.Evidence {
			duplicate := false
			for _, kept := range merged.Evidence {
				if kept.Reference == candidate.Reference && kept.Path == candidate.Path && kept.Symbol == candidate.Symbol {
					duplicate = true
					break
				}
			}
			if !duplicate {
				merged.Evidence = append(merged.Evidence, candidate)
			}
		}
		if len(merged.Evidence) > 3 {
			return result, errors.New("supplement would exceed the three-evidence budget; submit a revise choosing which references this version keeps")
		}
		revision := in
		revision.Experience = merged
		revision.PreviousID = resolution.TargetID
		return s.commitExperience(ctx, revision, requestFingerprint, &merged, "supplement", resolution.TargetID, nil)
	}
	return result, errors.New("resolution.action must be reuse, supplement, revise, create or defer")
}

// admissionEvidenceRef extracts the draft's already-validated admission
// evidence reference for template prefilling; empty when absent.
func admissionEvidenceRef(admission *ExperienceAdmission) string {
	if admission == nil {
		return ""
	}
	return admission.EvidenceRef
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// writeResolution validates the resolution itself: committing actions need a
// nonblank minimal difference and an evidence reference that belongs to this
// submission, so unverifiable judgments cannot store outcomes.
func (v *recordValidation) writeResolution(resolution *WriteResolution, evidence []ExperienceEvidence) {
	if resolution == nil {
		return
	}
	switch resolution.Action {
	case "reuse", "supplement", "revise":
		if strings.TrimSpace(resolution.TargetID) == "" {
			v.add("resolution.target_id", "required for "+resolution.Action)
		}
		fallthrough
	case "create":
		v.shortText("resolution.difference", resolution.Difference, 240)
		if strings.TrimSpace(resolution.EvidenceRef) != "" {
			found := false
			for _, item := range evidence {
				if item.Reference == resolution.EvidenceRef {
					found = true
					break
				}
			}
			if !found {
				v.add("resolution.evidence_ref", "does not match any experience.evidence[].reference in this submission; cite the evidence supporting the claimed relationship")
			}
		} else {
			v.add("resolution.evidence_ref", "required for "+resolution.Action)
		}
	case "defer":
	default:
		v.add("resolution.action", "must be reuse, supplement, revise, create or defer")
	}
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

// dbQuerier is satisfied by both the pool and an open write transaction, so
// the W(x) candidate search can re-run inside Put's transaction.
type dbQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Load only explicit compact experiences. Legacy prose is still searchable but
// is never silently promoted into automatic intervention rules.
func (s *Store) Experiences(ctx context.Context, scope string) ([]Entry, error) {
	return scanExperiences(ctx, s.db, scope)
}

func scanExperiences(ctx context.Context, q dbQuerier, scope string) ([]Entry, error) {
	query := "SELECT " + columns + " FROM entries JOIN entry_profiles p ON p.entry_id=entries.id WHERE current=1 AND needs_review=0 AND scope=? AND (json_array_length(p.profile_json,'$.experience.cues')>0 OR json_array_length(p.profile_json,'$.agreement.cues')>0) ORDER BY created_at DESC,id DESC LIMIT 2001"
	rows, err := q.QueryContext(ctx, query, scope)
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
		if err = loadGovernance(ctx, q, &e); err != nil {
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
