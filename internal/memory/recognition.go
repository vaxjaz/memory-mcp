package memory

import (
	"strings"
	"unicode"
	"unicode/utf8"
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

type humanSignalText struct {
	original, compact string
	starts, ends      []int
}

func newHumanSignalText(text string) humanSignalText {
	h := humanSignalText{original: text}
	var b strings.Builder
	for offset, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		lower := unicode.ToLower(r)
		b.WriteRune(lower)
		_, width := utf8.DecodeRuneInString(text[offset:])
		for i := 0; i < utf8.RuneLen(lower); i++ {
			h.starts = append(h.starts, offset)
			h.ends = append(h.ends, offset+width)
		}
	}
	h.compact = b.String()
	return h
}

func (h humanSignalText) matches(signal string) bool {
	target := humanSignalKey(signal)
	if target == "" {
		return false
	}
	for offset := 0; offset < len(h.compact); {
		at := strings.Index(h.compact[offset:], target)
		if at < 0 {
			return false
		}
		at += offset
		start, end := h.starts[at], h.ends[at+len(target)-1]
		// Retain ORIGINAL ASCII word boundaries: "use P R D" matches, while
		// "notprd" or "prd_backup" does not acquire the shorter abbreviation.
		before := !asciiID(rune(target[0])) || start == 0 || !asciiID(rune(h.original[start-1]))
		after := !asciiID(rune(target[len(target)-1])) || end == len(h.original) || !asciiID(rune(h.original[end]))
		if before && after {
			return true
		}
		offset = at + 1
	}
	return false
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
