// Package matching compares selected values. Callers own extraction and delivery.
package matching

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Truth int

const (
	Unknown Truth = iota - 1
	False
	True
)

func Bool(value bool) Truth {
	if value {
		return True
	}
	return False
}

type Rule struct {
	Op            string `json:"op,omitempty" jsonschema:"eq, prefix, suffix, contains, token or exists (field conditions only)."`
	CaseSensitive *bool  `json:"case_sensitive,omitempty" jsonschema:"Optional case policy for string values."`
	Normalize     string `json:"normalize,omitempty" jsonschema:"Optional path (unify slashes) or whitespace (ignore Unicode whitespace)."`
}

func (r Rule) Sensitive(defaultCase bool) bool {
	if r.CaseSensitive != nil {
		return *r.CaseSensitive
	}
	return defaultCase
}

func (r Rule) Validate(expected any, maxBytes int) error {
	if r.Normalize != "" && r.Normalize != "path" && r.Normalize != "whitespace" {
		return errors.New("normalize must be path or whitespace")
	}
	switch r.Op {
	case "exists":
		if expected != nil || r.CaseSensitive != nil || r.Normalize != "" {
			return errors.New("exists takes no value or string options")
		}
		return nil
	case "eq":
		switch value := expected.(type) {
		case bool, float64:
			if r.CaseSensitive != nil || r.Normalize != "" {
				return errors.New("case_sensitive and normalize require a string value")
			}
			if n, ok := value.(float64); ok && (math.IsNaN(n) || math.Abs(n) > 9007199254740991) {
				return errors.New("eq number exceeds the safe JSON integer range; use a string")
			}
			return nil
		}
	case "prefix", "suffix", "contains", "token":
	default:
		return fmt.Errorf("unknown operator %q", r.Op)
	}
	value, ok := expected.(string)
	if !ok || len(value) > maxBytes || (r.Op != "eq" && transform(value, r.Normalize, true) == "") {
		return fmt.Errorf("%s requires a valid text value of at most %d bytes", r.Op, maxBytes)
	}
	return nil
}

func transform(value, normalization string, sensitive bool) string {
	switch normalization {
	case "path":
		value = strings.ReplaceAll(value, "\\", "/")
	case "whitespace":
		value = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, value)
	}
	if !sensitive {
		value = strings.ToLower(value)
	}
	return value
}

func (r Rule) NormalizeText(value string, defaultCase bool) string {
	return transform(value, r.Normalize, r.Sensitive(defaultCase))
}

// Text caches normalized views when entrances share the same input field.
type Text struct {
	raw   string
	forms map[textPolicy]textView
}

type textPolicy struct {
	normalize           string
	sensitive, boundary bool
}

type textView struct {
	value        string
	starts, ends []int
}

func NewText(raw string) *Text { return &Text{raw: raw} }

func (t *Text) view(p textPolicy) textView {
	if prepared, ok := t.forms[p]; ok {
		return prepared
	}
	var out textView
	if p.normalize == "whitespace" && p.boundary {
		// Token boundaries refer to the original field before removing spaces.
		var b strings.Builder
		for start, r := range t.raw {
			if unicode.IsSpace(r) {
				continue
			}
			_, width := utf8.DecodeRuneInString(t.raw[start:])
			if !p.sensitive {
				r = unicode.ToLower(r)
			}
			b.WriteRune(r)
			for i := 0; i < utf8.RuneLen(r); i++ {
				out.starts = append(out.starts, start)
				out.ends = append(out.ends, start+width)
			}
		}
		out.value = b.String()
	} else {
		out.value = transform(t.raw, p.normalize, p.sensitive)
	}
	if t.forms == nil {
		t.forms = make(map[textPolicy]textView)
	}
	t.forms[p] = out
	return out
}

func (r Rule) MatchText(actual *Text, expected string, defaultCase bool) bool {
	p := textPolicy{normalize: r.Normalize, sensitive: r.Sensitive(defaultCase), boundary: r.Op == "token" && r.Normalize == "whitespace"}
	view := actual.view(p)
	needle := transform(expected, p.normalize, p.sensitive)
	switch r.Op {
	case "eq":
		return view.value == needle
	case "prefix":
		return needle != "" && strings.HasPrefix(view.value, needle)
	case "suffix":
		return needle != "" && strings.HasSuffix(view.value, needle)
	case "contains":
		return needle != "" && strings.Contains(view.value, needle)
	case "token":
		return containsToken(actual.raw, view, needle)
	default:
		return false
	}
}

func identifierByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '$'
}

func containsToken(raw string, view textView, needle string) bool {
	if needle == "" {
		return false
	}
	for offset := 0; offset < len(view.value); {
		at := strings.Index(view.value[offset:], needle)
		if at < 0 {
			break
		}
		at += offset
		start, end, text := at, at+len(needle), view.value
		before, after := true, true
		if view.starts != nil {
			start, end, text = view.starts[start], view.ends[end-1], raw
			before, after = identifierByte(needle[0]), identifierByte(needle[len(needle)-1])
		}
		if (!before || start == 0 || !identifierByte(text[start-1])) &&
			(!after || end == len(text) || !identifierByte(text[end])) {
			return true
		}
		offset = at + 1
	}
	return false
}

func (r Rule) Compare(actual, expected any, defaultCase bool) Truth {
	if r.Op == "exists" {
		return Bool(actual != nil)
	}
	switch v := expected.(type) {
	case string:
		if s, ok := actual.(string); ok {
			return Bool(r.MatchText(NewText(s), v, defaultCase))
		}
	case bool:
		if b, ok := actual.(bool); ok && r.Op == "eq" {
			return Bool(b == v)
		}
	case float64:
		if n, ok := actual.(float64); ok && r.Op == "eq" {
			return Bool(n == v)
		}
	}
	return Unknown
}
