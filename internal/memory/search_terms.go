package memory

import (
	"errors"
	"strings"
)

var errSearchQueryTooLong = errors.New("normalized search query exceeds 2048 bytes; refine terms or use the paginated memory directory")

// Keep all distinct literal terms; the limit bounds total input/work rather
// than requiring the host to guess an arbitrary eight-term budget.
func normalizeSearchQuery(raw string) (string, []string, error) {
	if len(raw) > 65536 {
		return "", nil, errSearchQueryTooLong
	}
	terms := []string{}
	seen := map[string]bool{}
	for _, term := range strings.Fields(raw) {
		key := strings.ToLower(term)
		if !seen[key] {
			seen[key] = true
			terms = append(terms, term)
		}
	}
	query := strings.Join(terms, " ")
	if len(query) > 2048 {
		return "", nil, errSearchQueryTooLong
	}
	return query, terms, nil
}

type KeywordSearchInfo struct {
	TermCount    int      `json:"term_count,omitempty"`
	TermsOmitted bool     `json:"terms_omitted,omitempty"`
	Status       string   `json:"status"`
	UsedTerms    []string `json:"used_terms,omitempty"`
	Query        string   `json:"query,omitempty"`
	Notice       string   `json:"notice"`
}
