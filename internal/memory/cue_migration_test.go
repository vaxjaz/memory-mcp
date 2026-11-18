package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitCueMigrationPreservesHistoryAndRequiresReviewedCues(t *testing.T) {
	s, project, in := experienceFixture(t)
	active, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t1", "e1"); err != nil {
		t.Fatal(err)
	}
	challenge := probeDelivery(t, s, project, "LYN 创建准入")
	if challenge.ID == "" {
		t.Fatal("pre-upgrade fixture did not create a challenge")
	}
	observeDelivery(t, s, "before-upgrade", nativeLookup(t, s, project, "before-upgrade", challenge.ID, 0))
	pendingInput := in
	pendingInput.Title = "old record awaiting a verified cue"
	pendingInput.Experience.Counterexample = "Isolated old cue-less case"
	pending, err := s.RecordExperience(ctx, pendingInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{active.ID, pending.ID} {
		if _, err := s.db.Exec(
			`UPDATE entry_profiles SET profile_json=json_set(profile_json,
			  '$.experience.anchors',json_array('retired_marker'),
			  '$.experience.signals',json_array('old_term'),
			  '$.experience.anchor_prefixes',json_array('WXN')) WHERE entry_id=?`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("UPDATE entry_profiles SET profile_json=json_remove(profile_json,'$.experience.cues') WHERE entry_id=?", pending.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version=5"); err != nil {
		t.Fatal(err)
	}
	dir := s.Dir
	s.Close()

	plan, err := InspectCueMigration(ctx, dir)
	if err != nil || plan.FromSchema != 5 || plan.Applied || plan.Convertible != 1 || len(plan.NeedsCues) != 1 || plan.NeedsCues[0].ID != pending.ID {
		t.Fatal(plan, err)
	}
	if opened, err := Open(dir); err == nil || !strings.Contains(err.Error(), "requires explicit cue migration") {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("normal startup did not identify migration", err)
	}
	backupDir := t.TempDir()
	got, err := ApplyCueMigration(ctx, dir, filepath.Join(backupDir, "memory.db"))
	if err != nil || !got.Applied {
		t.Fatal(got, err)
	}
	original, err := OpenReadOnly(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	var version int
	if err := original.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatal("backup did not retain previous schema", version, err)
	}
	upgraded, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	matches, err := upgraded.MatchExperiences(ctx, in.Scope, "LYN 创建准入")
	if err != nil || len(matches) != 1 || matches[0].ID != active.ID {
		t.Fatal(matches, err)
	}
	if after := probeDelivery(t, upgraded, project, "LYN 创建准入"); after.ID != "" || after.Context != "" {
		t.Fatal("existing body or cue delivery was forgotten", after)
	}
	if got, err := upgraded.MatchExperiences(ctx, in.Scope, "retired_marker old_term"); err != nil || len(got) != 0 {
		t.Fatal("legacy terms became entrances", got, err)
	}
	saved, err := upgraded.Read(ctx, active.ID)
	if err != nil || len(saved.Entry.Profile.Experience.ObjectIDPrefixes) != 1 || saved.Entry.Profile.Experience.ObjectIDPrefixes[0] != "WXN" {
		t.Fatal("object selector was lost", saved, err)
	}
	old, err := upgraded.Read(ctx, pending.ID)
	if err != nil || !old.Entry.NeedsReview || old.Entry.BodySHA256 != pending.BodySHA256 || old.Entry.RootID != pending.RootID {
		t.Fatal("pending record lost evidence or original identity", old, err)
	}
	var stale int
	if err = upgraded.db.QueryRow(`SELECT count(*) FROM entry_profiles WHERE json_type(profile_json,'$.experience.anchors') IS NOT NULL
	  OR json_type(profile_json,'$.experience.signals') IS NOT NULL
	  OR json_type(profile_json,'$.experience.anchor_prefixes') IS NOT NULL`).Scan(&stale); err != nil || stale != 0 {
		t.Fatal("profile still contains obsolete fields", stale, err)
	}
	pendingInput.PreviousID = pending.ID
	revised, err := upgraded.RecordExperience(ctx, pendingInput)
	if err != nil || revised.RootID != pending.RootID || revised.ID == pending.ID {
		t.Fatal("reviewed cue did not create a revision", revised, err)
	}
	matches, err = upgraded.MatchExperiences(ctx, in.Scope, "LYN 创建准入")
	if err != nil || len(matches) != 2 {
		t.Fatal(matches, err)
	}
}
