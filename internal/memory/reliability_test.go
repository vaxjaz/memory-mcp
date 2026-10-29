package memory

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRetryAfterLostResponseAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := recordDraft()
	d.Body = "原文\r\n\"引用\" 😀 e\u0301\n  保留空格  \n"
	d.RequestID = "write-001"
	first := put(t, s, d, "")
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	retry := put(t, s, d, "")
	if retry.ID != first.ID || retry.Body != d.Body {
		t.Fatal("retry duplicated or changed source")
	}
	sum := sha256.Sum256([]byte(d.Body))
	if retry.BodySHA256 != hex.EncodeToString(sum[:]) || retry.BodyBytes != len(d.Body) {
		t.Fatal("integrity metadata mismatch")
	}
	changed := d
	changed.Body += "different"
	if _, err = s.Put(ctx, changed, ""); err == nil {
		t.Fatal("request ID reused with changed payload")
	}
	corrected := d
	corrected.RequestID = "revision-001"
	corrected.Body = "纠正原文\r\n"
	v2 := put(t, s, corrected, first.ID)
	if got := put(t, s, corrected, first.ID); got.ID != v2.ID {
		t.Fatal("revision retry created another revision")
	}
	// A retry returns the original receipt even after a subsequent correction.
	if got := put(t, s, d, ""); got.ID != first.ID || got.Current {
		t.Fatal("retry did not identify original historical write")
	}
	if _, err = s.Put(ctx, corrected, v2.ID); err == nil {
		t.Fatal("same key accepted for different previous_id")
	}
	other := d
	other.Scope = "project:other"
	put(t, s, other, "")
	noKey := recordDraft()
	a := put(t, s, noKey, "")
	b := put(t, s, noKey, "")
	if a.ID == b.ID {
		t.Fatal("independent identical statements merged")
	}
	export, err := s.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(export.Entries) != 5 {
		t.Fatalf("unexpected entries: %d", len(export.Entries))
	}
	bad := d
	bad.Body = string([]byte{0xff})
	bad.RequestID = "invalid-utf8"
	if _, err = s.Put(ctx, bad, ""); err == nil {
		t.Fatal("invalid text silently repaired")
	}
}

func TestSearchPaginationDoesNotHideOlderMatches(t *testing.T) {
	s := testStore(t)
	want := map[string]bool{}
	for i := 0; i < 67; i++ {
		d := recordDraft()
		d.Title = fmt.Sprintf("原文 %d", i)
		e := put(t, s, d, "")
		want[e.ID] = true
	}
	other := recordDraft()
	other.Scope = "project:other"
	put(t, s, other, "")
	global := recordDraft()
	global.Scope = "global"
	put(t, s, global, "")
	// Ties must still paginate deterministically, without offset-based omissions.
	if _, err := s.db.Exec("UPDATE entries SET created_at=? WHERE scope=?", "2026-10-29T00:00:00Z", "project:demo"); err != nil {
		t.Fatal(err)
	}
	in := SearchInput{Scope: "project:demo", Query: "原文", Limit: 7}
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		page, err := s.SearchPage(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range page.Results {
			if !want[hit.ID] || seen[hit.ID] {
				t.Fatal("scope leak or duplicate", hit)
			}
			seen[hit.ID] = true
		}
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Fatal("cursor on final page")
			}
			break
		}
		if page.NextCursor == "" {
			t.Fatal("missing continuation cursor")
		}
		if i == 0 {
			changed := in
			changed.Scope = "project:other"
			changed.Cursor = page.NextCursor
			if _, err = s.SearchPage(ctx, changed); err == nil {
				t.Fatal("cursor reused across scopes")
			}
		}
		in.Cursor = page.NextCursor
	}
	if len(seen) != len(want) {
		t.Fatalf("read %d of %d matches", len(seen), len(want))
	}
}

func TestIncomingReferencesHaveContinuation(t *testing.T) {
	s := testStore(t)
	source := put(t, s, recordDraft(), "")
	for i := 0; i < 107; i++ {
		d := recordDraft()
		d.Kind = "case"
		d.Links = []Link{{source.ID, "context"}}
		if i%10 == 0 {
			d.Links = append(d.Links, Link{source.ID, "contradicts"})
		}
		put(t, s, d, "")
	}
	first, err := s.Read(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !first.ReferencesHasMore || len(first.ReferencedBy) != 100 {
		t.Fatal("incoming references silently truncated")
	}
	second, err := s.ReadPage(ctx, source.ID, first.ReferencesNextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if second.ReferencesHasMore || len(second.ReferencedBy) != 18 {
		t.Fatal("incorrect final reference page", len(second.ReferencedBy))
	}
	seen := map[string]bool{}
	for _, r := range append(first.ReferencedBy, second.ReferencedBy...) {
		key := r.EntryID + ":" + r.Relation
		if seen[key] {
			t.Fatal("reference repeated")
		}
		seen[key] = true
	}
	other := put(t, s, recordDraft(), "")
	if _, err = s.ReadPage(ctx, other.ID, first.ReferencesNextCursor); err == nil {
		t.Fatal("reference cursor accepted for another entry")
	}
}

func TestSettingsSurvivePhysicalConnectionReplacement(t *testing.T) {
	s := testStore(t)
	s.db.SetMaxIdleConns(0)
	for _, pragma := range []struct {
		name string
		want int
	}{{"foreign_keys", 1}, {"synchronous", 2}, {"busy_timeout", 10000}} {
		var value int
		if err := s.db.QueryRow("PRAGMA " + pragma.name).Scan(&value); err != nil {
			t.Fatal(err)
		}
		if value != pragma.want {
			t.Fatalf("%s=%d", pragma.name, value)
		}
	}
	d := recordDraft()
	d.RequestID = "connection-retry"
	e := put(t, s, d, "")
	if _, err := s.db.Exec("DELETE FROM entries WHERE id=?", e.ID); err != nil {
		t.Fatal(err)
	}
	var keys int
	if err := s.db.QueryRow("SELECT count(*) FROM write_requests").Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if keys != 0 {
		t.Fatal("foreign keys disabled after reconnect")
	}
}

func TestVersionOneMigrationAndConsistentBackup(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := recordDraft()
	d.RequestID = "original"
	e := put(t, s, d, "")
	h := put(t, s, hypothesis(e), "")
	// Version 1 has exactly the same entries/links schema, without write_requests.
	if _, err = s.db.Exec("DROP TABLE write_requests; PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Read(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Entry.Links[0].TargetID != e.ID {
		t.Fatal("migration changed provenance")
	}
	original, err := s.Read(ctx, e.ID)
	if err != nil || original.Entry.Body != d.Body {
		t.Fatal("migration changed source", err)
	}
	retryDraft := recordDraft()
	retryDraft.RequestID = "after-migration"
	keyed := put(t, s, retryDraft, "")
	backup := filepath.Join(t.TempDir(), "snapshot.db")
	if err = s.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err = s.Backup(ctx, backup); err == nil {
		t.Fatal("backup overwrote existing snapshot")
	}
	put(t, s, recordDraft(), "") // Must not appear in the earlier snapshot.
	restored := t.TempDir()
	b, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(restored, "memory.db"), b, 0600); err != nil {
		t.Fatal(err)
	}
	copy, err := Open(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	var integrity string
	if err = copy.db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal("invalid backup", integrity, err)
	}
	snapshot, err := copy.Export(ctx)
	if err != nil || len(snapshot.Entries) != 3 {
		t.Fatal("backup snapshot inconsistent", len(snapshot.Entries), err)
	}
	if got := put(t, copy, retryDraft, ""); got.ID != keyed.ID {
		t.Fatal("backup lost retry keys")
	}
}

func TestBackupDuringUncommittedWriter(t *testing.T) {
	s := testStore(t)
	e := put(t, s, recordDraft(), "")
	writer, err := Open(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	tx, err := writer.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE entries SET body=? WHERE id=?", "uncommitted", e.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out := filepath.Join(t.TempDir(), "online.db")
	if err = s.Backup(ctx, out); err != nil {
		t.Fatal(err)
	}
	restore := t.TempDir()
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(restore, "memory.db"), data, 0600); err != nil {
		t.Fatal(err)
	}
	copy, err := Open(restore)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	r, err := copy.Read(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Entry.Body != e.Body {
		t.Fatal("backup contained uncommitted data")
	}
}

// Invoked only by the subprocess tests below. Parent kill bypasses all Close calls.
func TestMemoryProcessHelper(t *testing.T) {
	mode := os.Getenv("MEMORY_MCP_TEST_HELPER")
	if mode == "" {
		return
	}
	s, err := Open(os.Getenv("MEMORY_MCP_TEST_DIR"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	d := recordDraft()
	if mode == "crash" {
		d.RequestID = "acknowledged-before-kill"
		e, err := s.Put(ctx, d, "")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, e.ID)
		for {
			time.Sleep(time.Hour)
		}
	}
	for i := 0; i < 15; i++ {
		d.RequestID = mode + ":" + strconv.Itoa(i)
		if _, err = s.Put(ctx, d, ""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if err = s.Close(); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func helperCommand(ctx context.Context, dir, mode string) *exec.Cmd {
	c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMemoryProcessHelper$")
	c.Env = append(os.Environ(), "MEMORY_MCP_TEST_HELPER="+mode, "MEMORY_MCP_TEST_DIR="+dir)
	return c
}

func TestAcknowledgedWriteSurvivesKilledProcess(t *testing.T) {
	deadline, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	dir := t.TempDir()
	cmd := helperCommand(deadline, dir, "crash")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	line := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			line <- scanner.Text()
		} else {
			line <- ""
		}
	}()
	var id string
	select {
	case id = <-line:
	case <-deadline.Done():
		t.Fatal("helper did not acknowledge write")
	}
	if len(id) != 32 {
		t.Fatal("invalid acknowledgment", id)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Read(ctx, id)
	if err != nil {
		t.Fatal("committed write lost after process kill", err)
	}
	if r.Entry.Body != recordDraft().Body {
		t.Fatal("body changed")
	}
	d := recordDraft()
	d.RequestID = "acknowledged-before-kill"
	if got := put(t, s, d, ""); got.ID != id {
		t.Fatal("retry duplicated after crash")
	}
}

func TestIndependentProcessesWriteAndRestart(t *testing.T) {
	independentProcesses(t, false)
}

func TestIndependentProcessesRetrySameWrites(t *testing.T) {
	independentProcesses(t, true)
}

func independentProcesses(t *testing.T, sameRequests bool) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	commands := []*exec.Cmd{}
	logs := []*bytes.Buffer{}
	for i := 0; i < 4; i++ {
		mode := fmt.Sprintf("worker-%d", i)
		if sameRequests {
			mode = "shared-retry-batch"
		}
		c := helperCommand(deadline, dir, mode)
		b := new(bytes.Buffer)
		c.Stdout = b
		c.Stderr = b
		commands = append(commands, c)
		logs = append(logs, b)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Process.Kill(); c.Wait() })
	}
	for i, c := range commands {
		if err := c.Wait(); err != nil {
			t.Fatal("worker failed", err, logs[i].String())
		}
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snapshot, err := s.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := 60
	if sameRequests {
		want = 15
	}
	if len(snapshot.Entries) != want {
		t.Fatal("concurrent writes lost", len(snapshot.Entries))
	}
	for _, e := range snapshot.Entries {
		if e.Body != recordDraft().Body || !strings.HasPrefix(e.Scope, "project:") {
			t.Fatal("record corrupted")
		}
	}
	var integrity string
	if err = s.db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal(integrity, err)
	}
}

func TestFailedWriteDoesNotConsumeRequestID(t *testing.T) {
	s := testStore(t)
	d := recordDraft()
	d.RequestID = "retry-after-failure"
	d.Links = []Link{{"missing", "context"}}
	if _, err := s.Put(ctx, d, ""); err == nil {
		t.Fatal("invalid source was accepted")
	}
	stats, err := s.Stats(ctx)
	if err != nil || stats.Entries != 0 {
		t.Fatal("failed write partially committed", err)
	}
	d.Links = nil
	first := put(t, s, d, "")
	if retry := put(t, s, d, ""); retry.ID != first.ID {
		t.Fatal("failed attempt consumed retry ID")
	}
}
