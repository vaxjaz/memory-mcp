package memory

import (
	"encoding/json"
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
	if stale.Status != "stale_comparison" || stale.Indexed || stale.ComparisonToken == pending.ComparisonToken {
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
	receipt, err := s.commitExperience(ctx, in, "", &in.Experience, "create")
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
