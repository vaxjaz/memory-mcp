package memory

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A narrow code locator can wake recall without a transient task phrase.
// Business names/prefixes and bare generic words remain a two-feature match.
// Provenance is admitted on write; the host still checks current applicability.
func specificEvidenceAnchor(e Entry, anchor string) bool {
	if e.Profile == nil {
		return false
	}
	var evidence []ExperienceEvidence
	if e.Profile.Experience != nil {
		evidence = e.Profile.Experience.Evidence
	}
	if e.Profile.Agreement != nil {
		evidence = e.Profile.Agreement.Evidence
	}
	value := normalizedResource(anchor)
	for _, source := range evidence {
		if source.Kind != "code" || source.Path == "" {
			continue
		}
		file := normalizedResource(source.Path)
		base := path.Base(file)
		stem := strings.TrimSuffix(base, path.Ext(base))
		if value == file && (strings.Contains(file, "/") || strings.Contains(file, ".")) {
			return true
		}
		if value == base && path.Ext(base) != "" && specificSymbol(stem) {
			return true
		}
		if value == stem && specificSymbol(stem) && len([]rune(stem)) >= 4 {
			return true
		}
		if strings.EqualFold(anchor, source.Symbol) && specificSymbol(anchor) {
			return true
		}
		if source.Symbol != "" && strings.EqualFold(anchor, stem+"."+source.Symbol) && specificSymbol(source.Symbol) {
			return true
		}
	}
	return false
}

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

func uniqueHumanSignals(signals []string) []string {
	out := make([]string, 0, len(signals))
	seen := map[string]bool{}
	for _, s := range signals {
		key := humanSignalKey(s)
		// Preserve invalid blanks for admission to reject, rather than hide damage.
		if key == "" || !seen[key] {
			out = append(out, s)
			seen[key] = true
		}
	}
	return out
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
