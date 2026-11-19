package memory

import (
	"strings"
	"unicode"
)

// Human phrases ignore case and Unicode whitespace, not punctuation or meaning.
// Abbreviations remain explicitly scoped values in the record's signals list.
func humanSignalKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		if !unicode.IsSpace(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// Full-text search can use either the original words or their whitespace-free
// signal form. It does not invent synonyms or expand abbreviations itself.
func recallTermOptions(term string) [][]string {
	var options [][]string
	if tokens := recallTokens(term); len(tokens) > 0 {
		options = append(options, tokens)
	}
	compact := humanSignalKey(term)
	if compact != strings.ToLower(term) {
		if tokens := recallTokens(compact); len(tokens) > 0 {
			options = append(options, tokens)
		}
	}
	return options
}
