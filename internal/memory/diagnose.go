package memory

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
)

// Unlike normal Open, this never creates a database, migrates metadata, enables
// WAL or seeds a missing singleton. Diagnostics must not mask the state examined.
func OpenReadOnly(dir string) (*Store, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(absolute, "memory.db")
	if _, err = os.Stat(path); err != nil {
		return nil, err
	}
	// Use SQLite's file:<filename> URI form; /C:/... is not a Windows path
	// in the Go VFS. Percent-escape the filename, including literal ?/#.
	uri := "file:" + url.PathEscape(filepath.ToSlash(path)) + "?mode=ro"
	db, err := driver.Open(uri, func(c *sqlite3.Conn) error { return c.Exec("PRAGMA query_only=ON; PRAGMA busy_timeout=10000;") })
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, Dir: absolute, readOnly: true}, nil
}

type ProbeEntry struct {
	ID             string       `json:"id"`
	IDLength       int          `json:"id_length"`
	Format32Hex    bool         `json:"format_32_hex"`
	Exists         bool         `json:"exists"`
	Scope          string       `json:"scope,omitempty"`
	Kind           string       `json:"kind,omitempty"`
	Host           string       `json:"host,omitempty"`
	Session        string       `json:"session,omitempty"`
	SourceRef      string       `json:"source_ref,omitempty"`
	RootID         string       `json:"root_id,omitempty"`
	Current        bool         `json:"current"`
	NeedsReview    bool         `json:"needs_review"`
	Management     ManagedState `json:"management"`
	BodySHA256     string       `json:"body_sha256,omitempty"`
	HookTurnExists bool         `json:"hook_turn_exists"`
	ContextState   string       `json:"context_state"`
	CaptureLogged  bool         `json:"capture_logged_in_this_library"`
}
type AuditLink struct {
	Target   ProbeEntry `json:"target"`
	Relation string     `json:"relation"`
}
type LinkDiagnostics struct {
	DataDir           string       `json:"data_dir"`
	ReadOnly          bool         `json:"read_only"`
	Scope             string       `json:"requested_scope,omitempty"`
	SchemaVersion     int          `json:"schema_version"`
	Entry             *ProbeEntry  `json:"entry,omitempty"`
	ForwardLinks      []AuditLink  `json:"forward_links"`
	MissingTargets    int          `json:"missing_targets"`
	Truncated         bool         `json:"truncated"`
	ProbeSources      []ProbeEntry `json:"probe_sources"`
	WriteClockPresent bool         `json:"write_clock_present"`
	Notice            string       `json:"notice"`
}

func tablePresent(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n)
	return n > 0, err
}
func probeEntry(ctx context.Context, tx *sql.Tx, id string) (ProbeEntry, error) {
	_, decodeErr := hex.DecodeString(id)
	p := ProbeEntry{ID: id, IDLength: len(id), Format32Hex: len(id) == 32 && decodeErr == nil, ContextState: "not_prepared"}
	if present, er := tablePresent(ctx, tx, "hook_runs"); er != nil {
		return p, er
	} else if present {
		var n int
		if er = tx.QueryRowContext(ctx, "SELECT count(*) FROM hook_runs WHERE detail=? AND result='input_saved'", id).Scan(&n); er != nil {
			return p, er
		}
		p.CaptureLogged = n > 0
	}
	e, err := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	p.Exists = true
	p.Scope = e.Scope
	p.Kind = e.Kind
	p.Host = e.Host
	p.Session = e.Session
	p.SourceRef = e.SourceRef
	p.RootID = e.RootID
	p.Current = e.Current
	p.NeedsReview = e.NeedsReview
	p.BodySHA256 = e.BodySHA256
	if err = loadGovernance(ctx, tx, &e); err != nil {
		return p, err
	}
	p.Management = e.State
	if present, er := tablePresent(ctx, tx, "hook_turns"); er != nil {
		return p, er
	} else if present {
		var n int
		if er = tx.QueryRowContext(ctx, "SELECT count(*) FROM hook_turns WHERE source_id=?", id).Scan(&n); er != nil {
			return p, er
		}
		p.HookTurnExists = n > 0
	}
	if present, er := tablePresent(ctx, tx, "context_checks"); er != nil {
		return p, er
	} else if present {
		er = tx.QueryRowContext(ctx, "SELECT status FROM context_checks WHERE source_id=?", id).Scan(&p.ContextState)
		if er != nil && !errors.Is(er, sql.ErrNoRows) {
			return p, er
		}
	}
	return p, nil
}
func (s *Store) DiagnoseLookups(ctx context.Context, scope, entryID string, probes []string) (LinkDiagnostics, error) {
	d := LinkDiagnostics{DataDir: s.Dir, ReadOnly: true, Scope: scope, ForwardLinks: []AuditLink{}, ProbeSources: []ProbeEntry{}, Notice: "Metadata only: no message bodies or repair. Forward links and memory_read.referenced_by are queries over the SAME links table, not independent indexes. IDs below are exact stored strings; do not fuzzy-match, guess replacements or rewrite this report through a model. Compare original tool JSON and pre-cleanup backups before attributing corruption or lost writes."}
	if len(probes) > 64 {
		return d, errors.New("at most 64 probe IDs")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return d, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&d.SchemaVersion); err != nil {
		return d, err
	}
	if present, er := tablePresent(ctx, tx, "operation_clock"); er != nil {
		return d, er
	} else if present {
		var n int
		if er = tx.QueryRowContext(ctx, "SELECT count(*) FROM operation_clock WHERE id=1").Scan(&n); er != nil {
			return d, er
		}
		d.WriteClockPresent = n > 0
	}
	if entryID != "" {
		p, er := probeEntry(ctx, tx, entryID)
		if er != nil {
			return d, er
		}
		d.Entry = &p
		rows, er := tx.QueryContext(ctx, "SELECT target_id,relation FROM links WHERE entry_id=? ORDER BY target_id,relation LIMIT 1001", entryID)
		if er != nil {
			return d, er
		}
		links := []Link{}
		for rows.Next() {
			var l Link
			if er = rows.Scan(&l.TargetID, &l.Relation); er != nil {
				rows.Close()
				return d, er
			}
			links = append(links, l)
		}
		er = rows.Err()
		rows.Close()
		if er != nil {
			return d, er
		}
		if len(links) > 1000 {
			d.Truncated = true
			links = links[:1000]
		}
		for _, l := range links {
			target, er := probeEntry(ctx, tx, l.TargetID)
			if er != nil {
				return d, er
			}
			if !target.Exists {
				d.MissingTargets++
			}
			d.ForwardLinks = append(d.ForwardLinks, AuditLink{target, l.Relation})
		}
	}
	for _, id := range probes {
		p, er := probeEntry(ctx, tx, id)
		if er != nil {
			return d, er
		}
		d.ProbeSources = append(d.ProbeSources, p)
	}
	return d, tx.Commit()
}
