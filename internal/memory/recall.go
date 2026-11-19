package memory

import (
	"strings"
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

// MatchIdentifier uses the same complete token boundaries as literal cue matching.
func MatchIdentifier(text, identifier string) bool { return identifierMatch(text, identifier) }
