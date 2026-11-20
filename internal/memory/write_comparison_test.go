package memory

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/matching"
)

// W(x) write-judgment contract: candidate search inside the write path, the
// two-phase resolution protocol and the paired acceptance samples (handoff
// batch two). Business claims are synthetic.

func comparisonFixture(t *testing.T) (*Store, string, ExperienceWrite) {
	t.Helper()
	s := testStore(t)
	project := t.TempDir()
	if err := s.HookBind(ctx, project, "project:demo"); err != nil {
		t.Fatal(err)
	}
	in := ExperienceWrite{Scope: "project:demo", Project: project, Title: "Settlement configuration", RequestID: "seed",
		Admission: &ExperienceAdmission{FutureDecision: "Decide if one code fits both targets.", EvidenceRef: "fixture:config-snapshot"},
		Experience: Experience{Fact: "On 19 November item A had different settlement codes in TEST and PROD.",
			Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:config-snapshot", Observation: "Synthetic snapshot for item A."}},
			Cues: []ExperienceCue{
				{ID: "settle", Label: "settle", EvidenceRefs: []string{"fixture:config-snapshot"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"settlementCode"}}}},
				{ID: "offline", Label: "offline", EvidenceRefs: []string{"fixture:config-snapshot"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"offlineSettlement"}}}},
			}}}
	return s, project, in
}

func rootCount(t *testing.T, s *Store, scope string) (int, int) {
	t.Helper()
	var roots, total int
	rows, err := s.db.QueryContext(ctx, "SELECT count(DISTINCT root_id), count(*) FROM entries WHERE kind='case' AND scope=?", scope)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		if err = rows.Scan(&roots, &total); err != nil {
			t.Fatal(err)
		}
	}
	return roots, total
}

func TestWriteComparisonExactReuseBypassesComparison(t *testing.T) {
	s, _, in := comparisonFixture(t)
	first, err := s.RecordExperience(ctx, in)
	if err != nil || !first.Indexed || first.Action != "create" {
		t.Fatal(first, err)
	}
	retry, err := s.RecordExperience(ctx, in)
	if err != nil || !retry.Reused || retry.ID != first.ID {
		t.Fatal("same request_id retry did not return its receipt", retry, err)
	}
	other := in
	other.RequestID = "other"
	exact, err := s.RecordExperience(ctx, other)
	if err != nil || !exact.Reused || exact.ID != first.ID {
		t.Fatal("exact structural duplicate was not reused silently", exact, err)
	}
	if roots, total := rootCount(t, s, in.Scope); roots != 1 || total != 1 {
		t.Fatal("replay duplicated rows", roots, total)
	}
	// Explicit previous_id revisions commit directly without comparison.
	revision := in
	revision.RequestID = "explicit-revision"
	revision.PreviousID = first.ID
	revision.Experience.Fact = "On 19 November item A had different settlement codes in TEST and PREPROD."
	revision.Experience.Evidence = []ExperienceEvidence{{Kind: "business", Reference: "fixture:config-snapshot", Observation: "Corrected synthetic snapshot."}}
	fixed, err := s.RecordExperience(ctx, revision)
	if err != nil || !fixed.Indexed || fixed.Action != "revise" || fixed.ID == first.ID || fixed.RootID != first.RootID {
		t.Fatal("explicit revision bypass failed", fixed, err)
	}
}

func TestWriteComparisonNeedsComparisonThenReuse(t *testing.T) {
	s, _, in := comparisonFixture(t)
	seed, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	paraphrase := in
	paraphrase.RequestID = "comparison"
	paraphrase.Experience.Fact = "For item A on 19 November, the PROD and TEST settlement codes did not match."
	pending, err := s.RecordExperience(ctx, paraphrase)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != "needs_comparison" || pending.Indexed || pending.ComparisonToken == "" || len(pending.Candidates) != 1 || pending.Candidates[0].ID != seed.ID {
		t.Fatal("candidate phase did not return the known record", pending)
	}
	for _, reason := range pending.Candidates[0].MatchedOn {
		if reason == "evidence_ref" || reason == "entrance" {
			t.Log("matched on", reason)
		}
	}
	if pending.Candidates[0].Experience == nil || !strings.Contains(pending.Candidates[0].Experience.Fact, "different settlement codes") {
		t.Fatal("candidate body missing", pending.Candidates[0])
	}
	if roots, total := rootCount(t, s, in.Scope); roots != 1 || total != 1 {
		t.Fatal("candidate phase wrote rows", roots, total)
	}
	resolved, err := s.RecordExperience(ctx, withResolution(paraphrase, "reuse", seed.ID, pending.ComparisonToken))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Action != "reuse" || resolved.ID != seed.ID || resolved.RootID != seed.RootID || !resolved.Indexed {
		t.Fatal("reuse did not return the current ID", resolved)
	}
	if roots, total := rootCount(t, s, in.Scope); roots != 1 || total != 1 {
		t.Fatal("reuse created rows", roots, total)
	}
	// Response-loss retry of the ORIGINAL first call returns the resolved receipt.
	replayed, err := s.RecordExperience(ctx, paraphrase)
	if err != nil || replayed.ID != seed.ID || !replayed.Reused {
		t.Fatal("original draft retry lost the resolved outcome", replayed, err)
	}
}

func TestWriteComparisonIndependentFactCreatesSecondRoot(t *testing.T) {
	s, _, in := comparisonFixture(t)
	seed, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	disabled := in
	disabled.RequestID = "independent"
	disabled.Experience.Fact = "On 19 November item A was disabled in TEST."
	pending, err := s.RecordExperience(ctx, disabled)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != "needs_comparison" {
		t.Fatal("independent observation skipped comparison", pending)
	}
	resolved, err := s.RecordExperience(ctx, withResolution(disabled, "create", seed.ID, pending.ComparisonToken))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Action != "create" || resolved.RootID == seed.RootID {
		t.Fatal("independent fact was merged or linked", resolved)
	}
	if roots, total := rootCount(t, s, in.Scope); roots != 2 || total != 2 {
		t.Fatal("independent fact did not create a second root", roots, total)
	}
}

func TestWriteComparisonSupplementAppendsIndependentSource(t *testing.T) {
	s, _, in := comparisonFixture(t)
	seed, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	corroborate := in
	corroborate.RequestID = "corroborate"
	corroborate.Experience.Evidence = []ExperienceEvidence{{Kind: "business", Reference: "fixture:independent-ledger", Observation: "Independent dated ledger for item A."}}
	for i := range corroborate.Experience.Cues {
		corroborate.Experience.Cues[i].EvidenceRefs = []string{"fixture:independent-ledger"}
	}
	corroborate.Admission.EvidenceRef = "fixture:independent-ledger"
	pending, err := s.RecordExperience(ctx, corroborate)
	if err != nil || pending.Status != "needs_comparison" {
		t.Fatal("corroboration skipped comparison", pending, err)
	}
	resolved, err := s.RecordExperience(ctx, withResolution(corroborate, "supplement", seed.ID, pending.ComparisonToken))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Action != "supplement" || resolved.RootID != seed.RootID || resolved.ID == seed.ID || !resolved.Indexed {
		t.Fatal("supplement lineage wrong", resolved)
	}
	stored, err := s.Read(ctx, resolved.ID)
	if err != nil {
		t.Fatal(err)
	}
	supplemented := stored.Entry.Profile.Experience
	if supplemented.Fact != in.Experience.Fact || len(supplemented.Evidence) != 2 {
		refs := make([]string, 0, len(supplemented.Evidence))
		for _, e := range supplemented.Evidence {
			refs = append(refs, e.Reference)
		}
		t.Fatal("supplement changed the claim or lost a source", supplemented.Fact, refs)
	}
	// Repeated submission of the same new source creates no further versions.
	again := in
	again.RequestID = "corroborate-2"
	again.Experience = corroborate.Experience
	again.Admission = corroborate.Admission
	repeat, err := s.RecordExperience(ctx, again)
	if err != nil {
		t.Fatal(err)
	}
	if repeat.Status == "needs_comparison" {
		repeat, err = s.RecordExperience(ctx, withResolution(again, "supplement", resolved.ID, repeat.ComparisonToken))
		if err != nil {
			t.Fatal(err)
		}
	}
	if roots, total := rootCount(t, s, in.Scope); roots != 1 || total != 2 {
		t.Fatal("repeated corroboration created extra versions", roots, total)
	}
}

func TestWriteComparisonDeferWritesNothing(t *testing.T) {
	s, _, in := comparisonFixture(t)
	if _, err := s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	incoming := in
	incoming.RequestID = "uncertain"
	incoming.Experience.Fact = "For item A on 19 November, the settlement codes were reported differently."
	pending, err := s.RecordExperience(ctx, incoming)
	if err != nil || pending.Status != "needs_comparison" {
		t.Fatal(pending, err)
	}
	deferred, err := s.RecordExperience(ctx, withResolution(incoming, "defer", "", pending.ComparisonToken))
	if err != nil || deferred.Status != "deferred" || deferred.Indexed {
		t.Fatal("defer committed content", deferred, err)
	}
	if roots, total := rootCount(t, s, in.Scope); roots != 1 || total != 1 {
		t.Fatal("defer changed the library", roots, total)
	}
}

func TestWriteComparisonStaleTokenReturnsFreshCandidates(t *testing.T) {
	s, _, in := comparisonFixture(t)
	seed, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	incoming := in
	incoming.RequestID = "stale"
	incoming.Experience.Fact = "For item A on 19 November, the PROD and TEST settlement codes did not match."
	pending, err := s.RecordExperience(ctx, incoming)
	if err != nil || pending.Status != "needs_comparison" {
		t.Fatal(pending, err)
	}
	// The world moves: an explicit revision supersedes the candidate.
	revision := in
	revision.RequestID = "world-moved"
	revision.PreviousID = seed.ID
	revision.Experience.Fact = "On 20 November item A had equal settlement codes in TEST and PROD."
	revision.Experience.Evidence = []ExperienceEvidence{{Kind: "business", Reference: "fixture:config-snapshot", Observation: "Next-day snapshot."}}
	moved, err := s.RecordExperience(ctx, revision)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := s.RecordExperience(ctx, withResolution(incoming, "reuse", seed.ID, pending.ComparisonToken))
	if err != nil {
		t.Fatal(err)
	}
	// The expired token returns the fresh candidate list under a new token
	// instead of committing the stale judgment.
	if stale.Status != "needs_comparison" || stale.Indexed || stale.ComparisonToken == pending.ComparisonToken {
		t.Fatal("stale token committed or echoed", stale)
	}
	found := false
	for _, c := range stale.Candidates {
		if c.ID == moved.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("fresh candidate missing from stale response", stale.Candidates)
	}
	if roots, total := rootCount(t, s, in.Scope); total != 2 {
		t.Fatal("stale resolution wrote rows", roots, total)
	}
}

func withResolution(in ExperienceWrite, action, target, token string) ExperienceWrite {
	in.Resolution = &WriteResolution{Action: action, TargetID: target, Difference: "synthetic", EvidenceRef: in.Admission.EvidenceRef, ComparisonToken: token}
	return in
}

// seedExperience commits a validated write through the storage path directly.
// Delivery, recall and migration mechanics tests seed additional records this
// way: their subject is not the write judgment, which has dedicated tests.
func seedExperience(t *testing.T, s *Store, in ExperienceWrite) Entry {
	t.Helper()
	receipt, err := s.commitExperience(ctx, in, "", &in.Experience, "create", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.Read(ctx, receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	return entry.Entry
}

func TestWriteComparisonCrossScopeIsolation(t *testing.T) {
	s, _, in := comparisonFixture(t)
	if _, err := s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	// A different scope shares nothing: no candidates, direct create.
	other := t.TempDir()
	if err := s.HookBind(ctx, other, "project:other"); err != nil {
		t.Fatal(err)
	}
	foreign := in
	foreign.Scope = "project:other"
	foreign.Project = other
	foreign.RequestID = "foreign"
	out, err := s.RecordExperience(ctx, foreign)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(out)
	if out.Status == "needs_comparison" || strings.Contains(string(encoded), "cross") {
		t.Fatal("cross-scope candidate leaked", out.Status)
	}
	var rootsOther int
	if err = s.db.QueryRowContext(ctx, "SELECT count(DISTINCT root_id) FROM entries WHERE scope='project:other'").Scan(&rootsOther); err != nil || rootsOther != 1 {
		t.Fatal("foreign write did not land in its scope", rootsOther, err)
	}
}

func TestWriteComparisonRejectsOffListTarget(t *testing.T) {
	s, _, in := comparisonFixture(t)
	if _, err := s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	// An unrelated warehouse record in the same scope.
	warehouse := in
	warehouse.RequestID = "warehouse"
	warehouse.Title = "Warehouse hours"
	warehouse.Admission = &ExperienceAdmission{FutureDecision: "Check closing hours.", EvidenceRef: "fixture:warehouse"}
	warehouse.Experience.Fact = "Warehouse shutters close nightly."
	warehouse.Experience.Evidence = []ExperienceEvidence{{Kind: "business", Reference: "fixture:warehouse", Observation: "Synthetic."}}
	warehouse.Experience.Cues = append([]ExperienceCue(nil), in.Experience.Cues...)
	for i := range warehouse.Experience.Cues {
		warehouse.Experience.Cues[i].EvidenceRefs = []string{"fixture:warehouse"}
		warehouse.Experience.Cues[i].Matches = []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{fmt.Sprintf("otherLookup%d", i)}}}
	}
	warehouse.Admission.EvidenceRef = "fixture:warehouse"
	other, err := s.RecordExperience(ctx, warehouse)
	if err != nil {
		t.Fatal(err)
	}
	// The settlement comparison shows only the settlement record; a revision
	// naming the warehouse ID must be rejected with zero writes.
	incoming := in
	incoming.RequestID = "misdirected"
	incoming.Experience.Fact = "Different codes were observed for settlement in the two environments."
	pending, err := s.RecordExperience(ctx, incoming)
	if err != nil || pending.Status != "needs_comparison" {
		t.Fatal(pending, err)
	}
	resolution := withResolution(incoming, "revise", other.ID, pending.ComparisonToken)
	rejected, err := s.RecordExperience(ctx, resolution)
	if err == nil || rejected.Indexed {
		t.Fatal("off-list revision committed", rejected, err)
	}
	kept, err := s.Read(ctx, other.ID)
	if err != nil || !kept.Entry.Current {
		t.Fatal("warehouse record was superseded by an off-list revision", err)
	}
	if roots, total := rootCount(t, s, in.Scope); roots != 2 || total != 2 {
		t.Fatal("off-list attempt changed the library", roots, total)
	}
}

func TestWriteComparisonConcurrentParaphrasesSingleRoot(t *testing.T) {
	s, _, in := comparisonFixture(t)
	left := in
	left.RequestID = "left"
	left.Experience.Fact = "In TEST and PROD the settlement codes differ."
	right := in
	right.RequestID = "right"
	right.Experience.Fact = "The PROD and TEST settlement codes do not match."
	barrier := make(chan struct{})
	done := make(chan struct{})
	results := make([]ExperienceReceipt, 2)
	errs := make([]error, 2)
	for i, draft := range []ExperienceWrite{left, right} {
		go func(i int, draft ExperienceWrite) {
			defer func() { done <- struct{}{} }()
			<-barrier
			results[i], errs[i] = s.RecordExperience(ctx, draft)
		}(i, draft)
	}
	close(barrier)
	<-done
	<-done
	for i := range errs {
		if errs[i] != nil {
			t.Fatal(i, errs[i])
		}
	}
	roots, total := rootCount(t, s, in.Scope)
	if roots != 1 || total != 1 {
		for i, r := range results {
			t.Logf("result[%d]: status=%q action=%q indexed=%v err=%v", i, r.Status, r.Action, r.Indexed, errs[i])
		}
		t.Fatal("concurrent paraphrases created multiple roots", roots, total, results[0].Action, results[1].Action)
	}
	if results[0].Status != "needs_comparison" && results[1].Status != "needs_comparison" {
		t.Fatal("loser of the race did not receive a comparison", results[0], results[1])
	}
}

func TestWriteComparisonReplaysCommittedAction(t *testing.T) {
	s, _, in := comparisonFixture(t)
	seed, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	incoming := in
	incoming.RequestID = "replay-action"
	incoming.Experience.Fact = "TEST uses a different settlement code than PROD."
	pending, err := s.RecordExperience(ctx, incoming)
	if err != nil || pending.Status != "needs_comparison" {
		t.Fatal(pending, err)
	}
	first, err := s.RecordExperience(ctx, withResolution(incoming, "reuse", seed.ID, pending.ComparisonToken))
	if err != nil || first.Action != "reuse" {
		t.Fatal(first, err)
	}
	// A plain replay of the ORIGINAL draft returns the committed action.
	replay, err := s.RecordExperience(ctx, incoming)
	if err != nil || replay.Action != "reuse" || replay.ID != seed.ID {
		t.Fatal("replay lost the committed action", replay, err)
	}
	// The same request_id cannot re-decide to create.
	changed, err := s.RecordExperience(ctx, withResolution(incoming, "create", seed.ID, pending.ComparisonToken))
	if err == nil || changed.Indexed {
		t.Fatal("committed action was re-decided", changed, err)
	}
}

func TestWriteComparisonRejectsUnsubstantiatedResolutions(t *testing.T) {
	s, _, in := comparisonFixture(t)
	seed, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for name, override := range map[string]func(*WriteResolution){
		"missing difference": func(r *WriteResolution) { r.Difference = "  " },
		"foreign evidence":   func(r *WriteResolution) { r.EvidenceRef = "absent:unprovided" },
		"missing evidence":   func(r *WriteResolution) { r.EvidenceRef = "" },
	} {
		incoming := in
		incoming.RequestID = "unsubstantiated-" + name
		incoming.Experience.Fact = "TEST uses a different settlement code than PROD."
		pending, err := s.RecordExperience(ctx, incoming)
		if err != nil || pending.Status != "needs_comparison" {
			t.Fatal(pending, err)
		}
		resolution := withResolution(incoming, "reuse", seed.ID, pending.ComparisonToken)
		override(resolution.Resolution)
		rejected, err := s.RecordExperience(ctx, resolution)
		if err == nil || rejected.Indexed {
			t.Fatal(name, "accepted an unsubstantiated decision", rejected, err)
		}
	}
}

func TestWriteComparisonPagingReachesHiddenCandidate(t *testing.T) {
	s, _, in := comparisonFixture(t)
	facts := []string{
		"Collection closes at noon.",
		"Only cash is accepted.",
		"Managers sign twice.",
		"Cancellations require approval.",
	}
	ids := make([]string, 0, len(facts))
	for i, fact := range facts {
		draft := in
		draft.RequestID = fmt.Sprintf("seed-%d", i)
		draft.Experience.Fact = fact
		out, err := s.RecordExperience(ctx, draft)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status == "needs_comparison" {
			out, err = s.RecordExperience(ctx, withResolution(draft, "create", out.Candidates[0].ID, out.ComparisonToken))
			if err != nil {
				t.Fatal(err)
			}
		}
		ids = append(ids, out.ID)
	}
	hidden := ids[len(ids)-1]
	// A paraphrase of the hidden record shares evidence and entrances; the
	// first page shows at most three candidates, the cursor must reach it.
	incoming := in
	incoming.RequestID = "page-me"
	incoming.Experience.Fact = "Withdrawal needs consent."
	first, err := s.RecordExperience(ctx, incoming)
	if err != nil || first.Status != "needs_comparison" {
		t.Fatal(first, err)
	}
	seen := map[string]bool{}
	for _, c := range first.Candidates {
		seen[c.ID] = true
	}
	cursor := first.NextCursor
	for !seen[hidden] && cursor != "" {
		paged := incoming
		paged.Cursor = cursor
		page, err := s.RecordExperience(ctx, paged)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range page.Candidates {
			seen[c.ID] = true
		}
		cursor = page.NextCursor
	}
	if !seen[hidden] {
		t.Fatal("hidden true duplicate unreachable through candidate pages", seen)
	}
	if roots, total := rootCount(t, s, in.Scope); roots != len(facts) || total != len(facts) {
		t.Fatal("paging changed the library", roots, total)
	}
}

func TestWriteComparisonTemplateCompletesNaiveAgent(t *testing.T) {
	s, _, in := comparisonFixture(t)
	seed, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// A host that has read NOTHING but this response must be able to finish.
	incoming := in
	incoming.RequestID = "naive"
	incoming.Experience.Fact = "For item A on 19 November, the PROD and TEST settlement codes did not match."
	pending, err := s.RecordExperience(ctx, incoming)
	if err != nil || pending.Status != "needs_comparison" {
		t.Fatal(pending, err)
	}
	tmpl := pending.ResolutionTemplate
	if tmpl == nil {
		t.Fatal("resolution_template missing from needs_comparison response")
	}
	if tmpl.ComparisonToken != pending.ComparisonToken || tmpl.EvidenceRef == "" || tmpl.TargetID != pending.Candidates[0].ID {
		t.Fatal("template not prefilled with server-known values", tmpl)
	}
	if tmpl.Action != "" || tmpl.Difference != "" {
		t.Fatal("judgment fields must stay empty for the agent", tmpl)
	}
	// Naive completion: copy the template verbatim, fill ONLY the two judgment fields.
	resolution := *tmpl
	resolution.Action = "reuse"
	resolution.Difference = "同一快照同一结算项，仅换表述。"
	done, err := s.RecordExperience(ctx, withRawResolution(incoming, &resolution))
	if err != nil || done.Action != "reuse" || done.ID != seed.ID {
		t.Fatal("naive completion failed", done, err)
	}
	if roots, total := rootCount(t, s, in.Scope); roots != 1 || total != 1 {
		t.Fatal("naive reuse changed the library", roots, total)
	}
	// The create path from a bare template too.
	other := in
	other.RequestID = "naive-create"
	other.Experience.Fact = "On 19 November item A was disabled in TEST."
	pending2, err := s.RecordExperience(ctx, other)
	if err != nil || pending2.Status != "needs_comparison" {
		t.Fatal(pending2, err)
	}
	resolution2 := *pending2.ResolutionTemplate
	resolution2.Action = "create"
	resolution2.Difference = "停用状态独立于编码差异，二者可同时成立。"
	done2, err := s.RecordExperience(ctx, withRawResolution(other, &resolution2))
	if err != nil || done2.Action != "create" || done2.RootID == seed.RootID {
		t.Fatal("naive create failed", done2, err)
	}
	// Stale tokens re-emit a fresh prefilled template instead of a dead end.
	staleIncoming := in
	staleIncoming.RequestID = "naive-stale-check"
	staleIncoming.Admission = &ExperienceAdmission{FutureDecision: in.Admission.FutureDecision, EvidenceRef: "fixture:config-snapshot"}
	staleIncoming.Experience.Cues = append([]ExperienceCue(nil), in.Experience.Cues...)
	for i := range staleIncoming.Experience.Cues {
		staleIncoming.Experience.Cues[i].EvidenceRefs = []string{"fixture:config-snapshot"}
	}
	staleIncoming.Experience.Fact = "For item A on 19 November, the PROD and TEST settlement codes did not match."
	pendingStale, err := s.RecordExperience(ctx, staleIncoming)
	if err != nil || pendingStale.Status != "needs_comparison" {
		t.Fatal(pendingStale, err)
	}
	dead := *pendingStale.ResolutionTemplate
	dead.Action = "reuse"
	dead.Difference = "基于过期候选集的判断。"
	// The world moves AFTER the comparison opened: a revision supersedes the
	// shown candidate set's anchor.
	moved := in
	moved.RequestID = "naive-stale"
	moved.PreviousID = seed.ID
	moved.Experience.Fact = "On 20 November item A had equal settlement codes in TEST and PROD."
	moved.Experience.Evidence = []ExperienceEvidence{{Kind: "business", Reference: "fixture:next-day", Observation: "Next-day snapshot."}}
	moved.Admission = &ExperienceAdmission{FutureDecision: "按次日快照判断。", EvidenceRef: "fixture:next-day"}
	moved.Experience.Cues = append([]ExperienceCue(nil), in.Experience.Cues...)
	for i := range moved.Experience.Cues {
		moved.Experience.Cues[i].EvidenceRefs = []string{"fixture:next-day"}
	}
	if _, err = s.RecordExperience(ctx, moved); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.RecordExperience(ctx, withRawResolution(staleIncoming, &dead))
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Status != "needs_comparison" || fresh.ResolutionTemplate == nil || fresh.ResolutionTemplate.ComparisonToken == pendingStale.ComparisonToken {
		t.Fatal("stale path did not re-emit a fresh prefilled template", fresh.Status)
	}
}

func withRawResolution(in ExperienceWrite, resolution *WriteResolution) ExperienceWrite {
	in.Resolution = resolution
	return in
}
