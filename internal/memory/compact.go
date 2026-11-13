package memory

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const CompactContextBudget = 8 * 1024

func ValidateResponseMode(mode string) error {
	if mode != "" && mode != "compact" && mode != "full" {
		return errors.New("response_mode must be compact or full")
	}
	return nil
}

// Previews are disposable navigation text. Never apply this to stored evidence,
// full reads, IDs, hashes, source references or completed decisions in SQLite.
func preview(s string, budget int) string {
	if len(s) <= budget {
		return s
	}
	n := budget - len("…")
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func matchPreview(s string, terms []string, budget int) string {
	if len(s) <= budget {
		return s
	}
	lower := strings.ToLower(s)
	for _, term := range terms {
		if index := strings.Index(lower, strings.ToLower(term)); index >= 0 {
			// Case folding may change UTF-8 byte widths; map through rune count.
			start := utf8.RuneCountInString(lower[:index]) - 15
			if start > 0 {
				return "…" + preview(string([]rune(s)[start:]), budget-len("…"))
			}
			break
		}
	}
	return preview(s, budget)
}

func compactState(s ManagedState) ManagedState {
	return ManagedState{State: s.State, Version: s.Version, ReplacementRootID: s.ReplacementRootID}
}

func CompactEntry(e Entry, mode string) Entry {
	if mode != "compact" {
		return e
	}
	n := len(e.Links)
	return Entry{ResponseMode: mode, ID: e.ID, RootID: e.RootID, PreviousID: e.PreviousID,
		Kind: e.Kind, Scope: e.Scope, Current: e.Current, NeedsReview: e.NeedsReview,
		BodyBytes: e.BodyBytes, BodySHA256: e.BodySHA256, State: compactState(e.State),
		CheckedContextSourceID: e.CheckedContextSourceID, RecordedSourceID: e.RecordedSourceID, LinkCount: &n, TextIssues: e.TextIssues}
}

func CompactReview(t HookTurn, mode string) HookTurn {
	if mode != "compact" {
		return t
	}
	var receipt struct {
		CaseIDs       []string `json:"case_ids"`
		ReusedCaseIDs []string `json:"reused_case_ids"`
	}
	_ = json.Unmarshal([]byte(t.Note), &receipt)
	return HookTurn{ResponseMode: mode, SourceID: t.SourceID, Scope: t.Scope, Status: t.Status,
		CaseIDs: receipt.CaseIDs, ReusedCaseIDs: receipt.ReusedCaseIDs,
		AnswerID: t.AnswerID, Attempts: t.Attempts, ReviewNoteBytes: t.ReviewNoteBytes,
		ReviewNoteSHA256: t.ReviewNoteSHA256, TextIssues: t.TextIssues}
}

func compactContext(r *ContextResult, in ContextInput) error {
	query, terms, _ := normalizeSearchQuery(strings.Join(in.Keywords, " "))
	r.ResponseMode = "compact"
	compactCandidate(r.MemoryReview)
	for i := range r.PendingCandidates {
		compactCandidate(&r.PendingCandidates[i])
	}
	r.Task, r.Questions, r.Decision = "", "", ""
	r.Notice = "Navigation previews only. Read selected FULL entries with this source, then complete before acting. Follow both cursors; no literal hit is not proof of no applicable memory."
	switch r.Status {
	case "prepared":
		r.NextAction = "read_selected_full_entries_then_complete"
	case "complete":
		r.NextAction = "ready_subject_to_current_versions"
	default:
		r.NextAction = "prepare_current_task"
	}
	if r.Status == "pending_candidates" {
		r.NextAction = "inspect_only_no_automatic_replay"
	}
	seen := map[string]bool{}
	for i := range r.Matches {
		m := &r.Matches[i]
		m.Title = preview(m.Title, 120)
		m.Excerpt = matchPreview(m.Excerpt, terms, 240)
		seen[m.ID] = true
	}
	directory := make([]InventoryItem, 0, len(r.Directory))
	for _, item := range r.Directory {
		// A candidate already in matches need not be echoed in the directory.
		// Its original inventory position is still covered by the page cursor.
		if seen[item.ID] {
			continue
		}
		item.Title = preview(item.Title, 120)
		item.Summary = preview(item.Summary, 180)
		if item.Profile != nil {
			item.Applicability = preview(item.Profile.Applicability, 90)
			item.Boundary = preview(item.Profile.Boundary, 90)
		}
		item.Profile = nil
		item.State = compactState(item.State)
		directory = append(directory, item)
	}
	r.Directory = directory
	if r.KeywordSearch != nil {
		r.KeywordSearch.TermCount = len(r.KeywordSearch.UsedTerms)
		r.KeywordSearch.Query = "" // already supplied by the caller
		r.KeywordSearch.Notice = preview(r.KeywordSearch.Notice, 450)
		terms, err := json.Marshal(r.KeywordSearch.UsedTerms)
		if err != nil {
			return err
		}
		if len(terms) > 1024 {
			r.KeywordSearch.UsedTerms = nil
			r.KeywordSearch.TermsOmitted = true
			r.KeywordSearch.Notice += " Term echo omitted for response size; the search still used all supplied terms."
		}
	}
	for {
		body, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if len(body) <= CompactContextBudget {
			return nil
		}
		// Keep the last-returned item as the continuation anchor. Never use the
		// original fetched-page cursor after dropping unseen candidates.
		if len(r.PendingCandidates) > 1 {
			r.PendingCandidates = r.PendingCandidates[:len(r.PendingCandidates)-1]
			last := r.PendingCandidates[len(r.PendingCandidates)-1]
			r.NextCursor = encodeCursor(pageCursor{Version: 1, Filter: digestText("pending-memory:" + in.Scope), ID: last.SourceID, Sort: "source_id"})
		} else if len(r.Directory) > 1 {
			r.Directory = r.Directory[:len(r.Directory)-1]
			last := r.Directory[len(r.Directory)-1]
			r.NextCursor = encodeCursor(pageCursor{Version: 1, Filter: searchFingerprint(SearchInput{Scope: in.Scope}), ID: last.ID, Sort: last.CreatedAt})
		} else if len(r.Matches) > 1 {
			r.Matches = r.Matches[:len(r.Matches)-1]
			last := r.Matches[len(r.Matches)-1]
			r.MatchNextCursor = encodeCursor(pageCursor{Version: 1, Filter: searchFingerprint(SearchInput{Scope: in.Scope, Query: query}), ID: last.ID, Sort: last.CreatedAt})
		} else {
			return errors.New("compact context metadata exceeds response budget; inspect the actual source/scope metadata")
		}
	}
}
