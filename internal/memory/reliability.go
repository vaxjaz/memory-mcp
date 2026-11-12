package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vaxjaz/memory-mcp/internal/textguard"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func digestText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func withIntegrity(e Entry) Entry {
	e.BodyBytes = len(e.Body)
	e.BodySHA256 = digestText(e.Body)
	e.TextIssues = nil
	for _, field := range []struct{ name, value string }{{"title", e.Title}, {"body", e.Body}, {"source_ref", e.SourceRef}} {
		if issue := textguard.Inspect(field.name, field.value); issue != nil {
			e.TextIssues = append(e.TextIssues, *issue)
		}
	}
	return e
}

func writeFingerprint(d Draft, previousID string) string {
	d.RequestID = ""
	d.ResponseMode = ""
	d.Links = append([]Link(nil), d.Links...)
	sort.Slice(d.Links, func(i, j int) bool {
		if d.Links[i].TargetID == d.Links[j].TargetID {
			return d.Links[i].Relation < d.Links[j].Relation
		}
		return d.Links[i].TargetID < d.Links[j].TargetID
	})
	data, _ := json.Marshal(struct {
		Draft      Draft
		PreviousID string
	}{d, previousID})
	return digestText(string(data))
}

func loadLinks(ctx context.Context, tx *sql.Tx, id string) ([]Link, error) {
	rows, err := tx.QueryContext(ctx, "SELECT target_id,relation FROM links WHERE entry_id=? ORDER BY target_id,relation", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := []Link{}
	for rows.Next() {
		var l Link
		if err = rows.Scan(&l.TargetID, &l.Relation); err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

type pageCursor struct {
	Version int    `json:"v"`
	Filter  string `json:"filter"`
	ID      string `json:"id"`
	Sort    string `json:"sort"`
}

func encodeCursor(c pageCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeCursor(value, filter string) (pageCursor, error) {
	var c pageCursor
	if len(value) > 2048 {
		return c, errors.New("invalid pagination cursor")
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return c, errors.New("invalid pagination cursor")
	}
	if err = json.Unmarshal(b, &c); err != nil || c.Version != 1 || c.Filter != filter || len(c.ID) != 32 || c.Sort == "" {
		return c, errors.New("invalid cursor or changed query; start a fresh search/read")
	}
	return c, nil
}

func searchFingerprint(in SearchInput) string {
	b, _ := json.Marshal(struct {
		Scope, Query, Kind       string
		Global, Events, Inactive bool
		NeedsReview              *bool
	}{in.Scope, strings.Join(strings.Fields(in.Query), " "), in.Kind, in.IncludeGlobal, in.IncludeEvents, in.IncludeInactive, in.NeedsReview})
	return digestText(string(b))
}

// Backup creates a standalone, transactionally consistent SQLite snapshot, including
// WAL data and retry keys. It never overwrites an existing destination.
func (s *Store) Backup(ctx context.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(abs); err == nil {
		return errors.New("backup destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(abs), ".memory-mcp-backup-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err = temp.Close(); err != nil {
		return err
	}
	if _, err = s.db.ExecContext(ctx, "VACUUM main INTO ?", tempPath); err != nil {
		return fmt.Errorf("create backup snapshot: %w", err)
	}
	f, err := os.OpenFile(tempPath, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Same-directory hard link is an atomic, non-overwriting publication.
	if err = os.Link(tempPath, abs); err != nil {
		return fmt.Errorf("publish backup without overwriting: %w", err)
	}
	return nil
}

type Stats struct {
	Entries     int
	Current     int
	NeedsReview int
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var out Stats
	err := s.db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(current),0),coalesce(sum(current=1 AND needs_review=1),0) FROM entries`).Scan(&out.Entries, &out.Current, &out.NeedsReview)
	return out, err
}
