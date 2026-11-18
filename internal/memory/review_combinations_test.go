package memory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewSchema5ExtensionPreservesExistingDelivery(t *testing.T) {
	s, p, in := deliveryFixture(t)
	c := probeDelivery(t, s, p, "WorkOrder.remark")
	o := nativeLookup(t, s, p, "schema5-body", c.ID, 0)
	observeDelivery(t, s, "schema5-body", o)
	export, err := s.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(export.Entries)
	if _, err = s.db.Exec("DROP TABLE experience_input_events"); err != nil {
		t.Fatal(err)
	}
	dir := s.Dir
	s.Close()
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(after.Entries)
	if string(before) != string(encoded) {
		t.Fatal("schema-5 extension changed history")
	}
	if err = reopened.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t2", "new-event"); err != nil {
		t.Fatal(err)
	}
	if c := probeDelivery(t, reopened, p, "WorkOrder.remark"); c.ID != "" || c.Context != "" {
		t.Fatal("schema-5 extension reset delivery", c)
	}
	var count int
	if err = reopened.db.QueryRow("SELECT count(*) FROM experience_input_events").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestReviewInputReplayPreservesCurrentObservationAndDelivery(t *testing.T) {
	for _, nativeTurn := range []bool{false, true} {
		t.Run(map[bool]string{false: "event-only", true: "native-turn"}[nativeTurn], func(t *testing.T) {
			s, p, in := deliveryFixture(t)
			in.Experience.AnchorPrefixes = []string{"WXN"}
			if _, err := s.RecordExperience(ctx, in); err != nil {
				t.Fatal(err)
			}
			first := probeDelivery(t, s, p, "WorkOrder.remark")
			page := nativeLookup(t, s, p, "replay-body", first.ID, 0)
			observeDelivery(t, s, "replay-body", page)
			consumer, generation, err := s.deliveryContext(ctx, "codex", in.Scope, "consumer", p)
			if err != nil {
				t.Fatal(err)
			}
			start := func(event string) {
				turn := ""
				if nativeTurn {
					turn = "turn-" + event
				}
				if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", turn, event); err != nil {
					t.Fatal(err)
				}
			}
			turn := func(event string) string {
				if nativeTurn {
					return "turn-" + event
				}
				return ""
			}
			observe := func(event, object string) {
				if err := s.ObserveBusiness(ctx, "codex", in.Scope, "consumer", turn(event), object+" 生成回馈日志"); err != nil {
					t.Fatal(err)
				}
			}
			check := func(event, object string, want int) {
				t.Helper()
				m, err := s.ObservedBusinessMatches(ctx, "codex", in.Scope, "consumer", turn(event), object)
				if err != nil || len(m) != want {
					t.Fatalf("%s/%s: got %v, %v", event, object, m, err)
				}
			}
			start("a")
			observe("a", "WXN-001")
			start("a")
			check("a", "WXN-001", 1)
			start("b")
			check("b", "WXN-001", 0)
			observe("b", "WXN-002")
			start("a") // Late replay cannot replace b or delete its observation.
			check("b", "WXN-002", 1)
			check("b", "WXN-001", 0)
			after, afterGeneration, err := s.deliveryContext(ctx, "codex", in.Scope, "consumer", p)
			if err != nil || consumer != after || generation != afterGeneration {
				t.Fatal("input reset body context", err)
			}
			if c := probeDelivery(t, s, p, "WorkOrder.remark"); c.ID != "" || c.Context != "" {
				t.Fatal("input replay reset A", c)
			}
			var stored string
			if err := s.db.QueryRow("SELECT matches_json FROM business_observations").Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(stored, "WXN-002") || strings.Contains(stored, in.Experience.Cues[1].Text) {
				t.Fatal("observation persisted raw object or cue body", stored)
			}
		})
	}
}

func TestReviewUnknownInputIdentityExpiresOnlyTemporaryObservation(t *testing.T) {
	s, p, in := deliveryFixture(t)
	in.Experience.AnchorPrefixes = []string{"WXN"}
	if _, err := s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	c := probeDelivery(t, s, p, "WorkOrder.remark")
	o := nativeLookup(t, s, p, "unknown-body", c.ID, 0)
	observeDelivery(t, s, "unknown-body", o)
	for i := 0; i < 2; i++ {
		if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "", ""); err != nil {
			t.Fatal(err)
		}
		m, err := s.ObservedBusinessMatches(ctx, "codex", in.Scope, "consumer", "", "WXN-001")
		if err != nil || len(m) != 0 {
			t.Fatal("unidentifiable new input reused observation", m, err)
		}
		if c := probeDelivery(t, s, p, "WorkOrder.remark"); c.ID != "" || c.Context != "" {
			t.Fatal("unknown identity reset body/cue", c)
		}
		if err := s.ObserveBusiness(ctx, "codex", in.Scope, "consumer", "", "WXN-001 生成回馈日志"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReviewObservedCueDoesNotSurviveMaterialRevision(t *testing.T) {
	s, _, in := deliveryFixture(t)
	in.Experience.AnchorPrefixes = []string{"WXN"}
	saved, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveBusiness(ctx, "codex", in.Scope, "consumer", "t1", "WXN-001 生成回馈日志"); err != nil {
		t.Fatal(err)
	}
	in.PreviousID = saved.ID
	in.Experience.Boundary = "合成修订：本地参数改为独立事务，需要新证据"
	if _, err = s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	m, err := s.ObservedBusinessMatches(ctx, "codex", in.Scope, "consumer", "t1", "WXN-001")
	if err != nil || len(m) != 0 {
		t.Fatal("old observation credited newer cue version", m, err)
	}
}

func TestReviewAmbiguousFreeTextCannotAssociateMultipleObjects(t *testing.T) {
	s, _, in := deliveryFixture(t)
	in.Experience.AnchorPrefixes = []string{"WXN"}
	if _, err := s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.ObserveBusiness(ctx, "codex", in.Scope, "consumer", "t1", "WXN-001 生成回馈日志；WXN-002 仅普通查看"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"WXN-001", "WXN-002"} {
		m, err := s.ObservedBusinessMatches(ctx, "codex", in.Scope, "consumer", "t1", id)
		if err != nil || len(m) != 0 {
			t.Fatal("ambiguous object association", id, m, err)
		}
	}
}
