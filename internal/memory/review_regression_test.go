package memory

import (
	"strings"
	"testing"
)

// Synthetic review fixtures; every database is created by t.TempDir().
func TestReviewObservedBusinessNewCueRetainsMaterial(t *testing.T) {
	s, p, in := deliveryFixture(t)
	in.Experience.ObjectIDPrefixes = []string{"WXN"}
	if _, err := s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	first := probeDelivery(t, s, p, "WorkOrder.remark")
	page := nativeLookup(t, s, p, "review-body", first.ID, 0)
	observeDelivery(t, s, "review-body", page)
	if err := s.ObserveBusiness(ctx, "codex", in.Scope, "consumer", "t1", "WXN-001 生成回馈日志"); err != nil {
		t.Fatal(err)
	}
	matches, err := s.ObservedBusinessMatches(ctx, "codex", in.Scope, "consumer", "t1", "WXN-001")
	if err != nil || len(matches) != 1 || matches[0].CueID != "log_input" {
		t.Fatalf("expected observed B: %#v, %v", matches, err)
	}
	result, err := s.ChallengeExperience(ctx, "codex", in.Scope, "consumer", "t1", p, matches)
	if err != nil || result.ID != "" || !strings.Contains(result.Context, in.Experience.Cues[1].Text) {
		t.Fatalf("new observed B must deliver its material after A: %#v, %v", result, err)
	}
}

func TestReviewBusinessObservationExpiresWithoutNativeTurnID(t *testing.T) {
	s, _, in := deliveryFixture(t)
	in.Experience.ObjectIDPrefixes = []string{"WXN"}
	if _, err := s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.ExperienceStartTurn(ctx, "zcode", in.Scope, "no-native-turn", "", "input-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.ObserveBusiness(ctx, "zcode", in.Scope, "no-native-turn", "", "WXN-001 生成回馈日志"); err != nil {
		t.Fatal(err)
	}
	before, err := s.ObservedBusinessMatches(ctx, "zcode", in.Scope, "no-native-turn", "", "WXN-001")
	if err != nil || len(before) != 1 {
		t.Fatalf("fixture observation missing: %#v %v", before, err)
	}
	if err := s.ExperienceStartTurn(ctx, "zcode", in.Scope, "no-native-turn", "", "input-b"); err != nil {
		t.Fatal(err)
	}
	after, err := s.ObservedBusinessMatches(ctx, "zcode", in.Scope, "no-native-turn", "", "WXN-001")
	if err != nil || len(after) != 0 {
		t.Fatalf("previous input's business association leaked: %#v %v", after, err)
	}
}
