package memory

import (
	"strings"
	"unicode"
)

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
