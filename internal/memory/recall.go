package memory

import (
	"context"
	"path"
	"sort"
	"strings"
	"unicode"
)

// RecallContext separates the task from resources actually supplied by a host.
// A filename match locates evidence; it does not establish applicability.
type RecallContext struct {
	Query              string
	Resources, Symbols []string
}
type RecallReason struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Field string `json:"field"`
}
type RecallMatch struct {
	ID      string         `json:"id"`
	Reasons []RecallReason `json:"reasons"`
}
type RecallResult struct {
	Entries   []Entry
	Matches   []RecallMatch
	Truncated bool
	Scanned   int
}

func recallTerms(text string) map[string]int {
	out := map[string]int{}
	text = strings.ToLower(text)
	var word, han []rune
	flush := func() {
		if len(word) > 1 {
			out[string(word)] = 1
		}
		word = nil
		if len(han) > 1 {
			for i := 0; i < len(han)-1; i++ {
				out[string(han[i:i+2])] = 1
			}
		}
		han = nil
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			if len(word) > 0 {
				flush()
			}
			han = append(han, r)
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if len(han) > 0 {
				flush()
			}
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	for _, w := range []string{"一个", "这个", "我们", "你们", "可以", "需要", "进行", "相关", "一下", "现在", "怎么", "什么", "是否", "应该", "用户", "业务", "本轮", "原话", "项目", "请问", "帮我", "the", "this", "with", "please"} {
		delete(out, w)
	}
	return out
}

func normalizedResource(s string) string { return strings.ToLower(strings.ReplaceAll(s, "\\", "/")) }
func asciiID(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '$'
}
func identifierMatch(text, identifier string) bool {
	text = strings.ToLower(text)
	identifier = strings.ToLower(identifier)
	if identifier == "" {
		return false
	}
	for offset := 0; offset < len(text); {
		at := strings.Index(text[offset:], identifier)
		if at < 0 {
			return false
		}
		at += offset
		end := at + len(identifier)
		before := at == 0 || !asciiID(rune(text[at-1]))
		after := end == len(text) || !asciiID(rune(text[end]))
		if before && after {
			return true
		}
		offset = at + 1
	}
	return false
}

func canonicalIdentifier(s string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(s))
}
func normalizedIdentifierMatch(text, symbol string) string {
	target := canonicalIdentifier(symbol)
	var token []rune
	check := func() string {
		value := string(token)
		token = nil
		if value != "" && canonicalIdentifier(value) == target {
			return value
		}
		return ""
	}
	for _, r := range text {
		if asciiID(r) {
			token = append(token, r)
		} else {
			if match := check(); match != "" {
				return match
			}
		}
	}
	return check()
}
func identifierParts(text string) []string {
	// Split camelCase, PascalCase, acronyms, snake_case and dotted symbols.
	var parts []string
	var token []rune
	flush := func() {
		if len(token) > 0 {
			parts = append(parts, strings.ToLower(string(token)))
			token = nil
		}
	}
	rs := []rune(text)
	for i, r := range rs {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			flush()
			continue
		}
		if len(token) > 0 && r >= 'A' && r <= 'Z' {
			prev := token[len(token)-1]
			nextLower := i+1 < len(rs) && rs[i+1] >= 'a' && rs[i+1] <= 'z'
			if prev >= 'a' && prev <= 'z' || prev >= 'A' && prev <= 'Z' && nextLower {
				flush()
			}
		}
		token = append(token, r)
	}
	flush()
	return parts
}

var genericIdentifier = map[string]bool{"src": true, "main": true, "test": true, "tests": true, "index": true, "readme": true, "config": true, "common": true, "service": true, "controller": true, "impl": true, "base": true, "interface": true, "api": true, "java": true, "json": true, "class": true, "file": true, "name": true, "get": true, "set": true, "new": true, "com": true, "org": true, "net": true, "id": true, "ui": true, "db": true}

func meaningfulParts(symbol string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range identifierParts(symbol) {
		if len(p) >= 2 && !genericIdentifier[p] && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
func specificSymbol(symbol string) bool {
	if strings.TrimSpace(symbol) == "" || genericIdentifier[strings.ToLower(symbol)] {
		return false
	}
	if len(meaningfulParts(symbol)) > 0 {
		return true
	}
	for _, r := range symbol {
		if unicode.Is(unicode.Han, r) {
			return len([]rune(symbol)) >= 2
		}
	}
	return false
}

type recallSignal struct {
	kind, value string
	tier        int
}

func resourceSignals(c RecallContext) []recallSignal {
	out := []recallSignal{}
	seen := map[string]bool{}
	add := func(kind, value string, tier int) {
		if value == "" {
			return
		}
		key := kind + ":" + strings.ToLower(value)
		if !seen[key] {
			seen[key] = true
			out = append(out, recallSignal{kind, value, tier})
		}
	}
	for _, r := range c.Resources {
		r = strings.ReplaceAll(strings.TrimSpace(r), "\\", "/")
		if r == "" {
			continue
		}
		base := path.Base(r)
		stem := strings.TrimSuffix(base, path.Ext(base))
		if strings.Contains(r, "/") {
			add("resource_path", r, 3)
		}
		if specificSymbol(stem) {
			add("file_name", base, 3)
			add("symbol", stem, 3)
		}
		// A module-qualified Python/JS filename can still point at its core symbol.
		if at := strings.Index(stem, "."); at > 0 && specificSymbol(stem[:at]) {
			add("symbol", stem[:at], 3)
		}
	}
	for _, s := range c.Symbols {
		if specificSymbol(s) {
			add("symbol", s, 3)
			if at := strings.LastIndex(s, "."); at >= 0 && specificSymbol(s[at+1:]) {
				add("symbol", s[at+1:], 3)
			}
		}
	}
	return out
}

func (s *Store) HookRecall(ctx context.Context, scope, query string) (RecallResult, error) {
	return s.HookRecallContext(ctx, scope, RecallContext{Query: query})
}
func (s *Store) HookRecallContext(ctx context.Context, scope string, c RecallContext) (RecallResult, error) {
	terms := recallTerms(c.Query)
	signals := resourceSignals(c)
	type hint struct {
		kind, value, term string
		tier              int
	}
	hints := []hint{}
	for _, resource := range c.Resources {
		lower := strings.ToLower(resource)
		if strings.Contains(lower, "migration") || strings.Contains(lower, ".sql") {
			for _, term := range []string{"迁移", "数据库"} {
				hints = append(hints, hint{"resource_hint", path.Base(strings.ReplaceAll(resource, "\\", "/")) + " → " + term, term, 1})
			}
		}
	}
	if lower := strings.ToLower(c.Query); strings.Contains(lower, "migration") || strings.Contains(lower, ".sql") {
		for _, term := range []string{"迁移", "数据库"} {
			hints = append(hints, hint{"task_hint", "migration/.sql → " + term, term, 0})
		}
	}
	result := RecallResult{Entries: []Entry{}, Matches: []RecallMatch{}}
	if len(terms) == 0 && len(signals) == 0 && len(hints) == 0 {
		return result, nil
	}
	words := []string{}
	for w := range terms {
		words = append(words, w)
	}
	sort.Strings(words)
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+" FROM entries WHERE current=1 AND scope=? AND source_ref NOT LIKE 'hook-event:%' ORDER BY created_at DESC,id DESC LIMIT 1001", scope)
	if err != nil {
		return result, err
	}
	type ranked struct {
		e                              Entry
		tier, resourceScore, taskScore int
		reasons                        []RecallReason
	}
	found := []ranked{}
	for rows.Next() {
		e, er := scanEntry(rows)
		if er != nil {
			rows.Close()
			return result, er
		}
		result.Scanned++
		if result.Scanned > 1000 {
			result.Truncated = true
			break
		}
		candidate := ranked{e: e, reasons: []RecallReason{}}
		fields := []struct{ name, text string }{{"title", e.Title}, {"body", e.Body}, {"source_ref", e.SourceRef}}
		seen := map[string]bool{}
		add := func(kind, value, field string, tier, weight int) {
			key := kind + ":" + strings.ToLower(value) + ":" + field
			if seen[key] {
				return
			}
			seen[key] = true
			candidate.reasons = append(candidate.reasons, RecallReason{kind, value, field})
			if tier > candidate.tier {
				candidate.tier = tier
			}
			if tier > 0 {
				candidate.resourceScore += weight
			} else {
				candidate.taskScore += weight
			}
		}
		for _, sig := range signals {
			matched := false
			for _, field := range fields {
				ok := false
				if sig.kind == "resource_path" {
					ok = strings.Contains(normalizedResource(field.text), normalizedResource(sig.value))
				} else {
					ok = identifierMatch(field.text, sig.value)
				}
				if ok {
					weight := 1
					if field.name == "title" {
						weight = 4
					}
					add(sig.kind, sig.value, field.name, sig.tier, weight)
					matched = true
				} else if sig.kind == "symbol" {
					if actual := normalizedIdentifierMatch(field.text, sig.value); actual != "" {
						add("symbol_normalized", sig.value+" ≈ "+actual, field.name, 3, 1)
						matched = true
					}
				}
			}
			if !matched && sig.kind == "symbol" {
				parts := meaningfulParts(sig.value)
				if len(parts) < 2 {
					continue
				}
				for _, field := range fields {
					tokenSet := map[string]bool{}
					for _, p := range identifierParts(field.text) {
						tokenSet[p] = true
					}
					hits := []string{}
					for _, part := range parts {
						if tokenSet[part] {
							hits = append(hits, part)
						}
					}
					if len(hits) >= 2 {
						add("symbol_parts", strings.Join(hits, " + "), field.name, 2, len(hits))
					}
				}
			}
		}
		for _, hint := range hints {
			for _, field := range fields[:2] {
				if strings.Contains(field.text, hint.term) {
					add(hint.kind, hint.value, field.name, hint.tier, 1)
				}
			}
		}
		for _, w := range words {
			for _, field := range fields[:2] {
				if strings.Contains(strings.ToLower(field.text), w) {
					weight := 1
					if field.name == "title" {
						weight = 4
					}
					add("task_keyword", w, field.name, 0, weight)
				}
			}
		}
		if len(candidate.reasons) > 0 {
			found = append(found, candidate)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	// Resource matches rank ahead of arbitrarily many generic prompt matches.
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.tier != b.tier {
			return a.tier > b.tier
		}
		if a.resourceScore != b.resourceScore {
			return a.resourceScore > b.resourceScore
		}
		return a.taskScore > b.taskScore
	})
	selected := []ranked{}
	used := map[string]bool{}
	choose := func(candidate ranked) {
		if len(selected) < 3 && !used[candidate.e.ID] {
			used[candidate.e.ID] = true
			selected = append(selected, candidate)
		}
	}
	for _, candidate := range found {
		if candidate.tier > 0 && len(selected) < 2 {
			choose(candidate)
		}
	}
	topic := append([]ranked(nil), found...)
	sort.SliceStable(topic, func(i, j int) bool { return topic[i].taskScore > topic[j].taskScore })
	for _, candidate := range topic {
		if candidate.taskScore > 0 && !used[candidate.e.ID] {
			choose(candidate)
			break
		}
	}
	for _, candidate := range found {
		choose(candidate)
	}
	for _, candidate := range selected {
		read, er := s.Read(ctx, candidate.e.ID)
		if er != nil {
			return result, er
		}
		// Delivery reasons are capped; full source content remains available by ID.
		reasons := candidate.reasons
		if len(reasons) > 6 {
			reasons = reasons[:6]
		}
		result.Entries = append(result.Entries, read.Entry)
		result.Matches = append(result.Matches, RecallMatch{read.Entry.ID, reasons})
	}
	return result, nil
}
