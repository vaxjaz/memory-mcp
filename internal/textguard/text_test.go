package textguard

import "testing"

func TestStrictJSONPreservesUnicodeAndRejectsLossyDecode(t *testing.T) {
	for _, good := range []string{`{"body":"关键信息、助手、同级——、须追到、用户未表态😀"}`, `{"body":"\uD83D\uDE00"}`, `{"body":"\\ud800"}`, `{"body":"\ufffd"}`} {
		if err := JSON([]byte(good)); err != nil {
			t.Fatalf("valid input rejected: %s: %v", good, err)
		}
	}
	bad := [][]byte{[]byte(`{"body":"\ud800"}`), []byte(`{"body":"\udc00"}`), []byte(`{"body":"\ud800\u0041"}`), append([]byte(`{"body":"`), append([]byte{0xe4, 0xbf}, []byte(`"}`)...)...)}
	for _, raw := range bad {
		if err := JSON(raw); err == nil {
			t.Fatalf("lossy JSON accepted: %q", raw)
		}
	}
}

func TestReplacementRequiresExplicitIntentAndCannotAllowInvalidBytes(t *testing.T) {
	for _, value := range []string{"关键信���", "��手", "同级���—", "须��到", "用��未表态"} {
		if err := Check("body", value, false); err == nil {
			t.Fatal("suspected corruption accepted", value)
		}
		if err := Check("body", value, true); err != nil {
			t.Fatal("intentional replacement symbol rejected", err)
		}
	}
	if err := Check("note", string([]byte{0xe4, 0xbf}), true); err == nil {
		t.Fatal("opt-in allowed invalid UTF-8")
	}
	if issue := Inspect("note", "😀助手�"); issue == nil || issue.ByteOffset != 10 {
		t.Fatal("wrong byte position", issue)
	}
}
