package matching

import (
	"errors"
	"fmt"
	"regexp"
)

var FieldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,39}$`)

// Expression has one form: all, any, not, a fact, or a value comparison.
// Entrance strings are alternatives within one leaf. Only these furnish
// indexable cue entrances.
type Expression struct {
	Rule
	Field     string        `json:"field,omitempty" jsonschema:"Named input field exported by the tool. Omit to scan the available text fields."`
	Entrances []string      `json:"entrances,omitempty" jsonschema:"Original literal entrance strings, any one of which can match this comparison."`
	Value     any           `json:"value,omitempty" jsonschema:"One literal predicate value. Use entrances for observable cue keywords."`
	All       []*Expression `json:"all,omitempty"`
	Any       []*Expression `json:"any,omitempty"`
	Not       *Expression   `json:"not,omitempty"`
	Fact      *Fact         `json:"fact,omitempty"`
}

type Fact struct {
	Name  string      `json:"name"`
	Key   []string    `json:"key"`
	Match *Expression `json:"match"`
}

func (e *Expression) Validate(depth int, allowFacts bool) error {
	if e == nil || depth > 5 {
		return errors.New("match must be non-null and at most six levels deep")
	}
	leaf := e.Field != "" || e.Op != "" || e.Value != nil || e.Entrances != nil || e.CaseSensitive != nil || e.Normalize != ""
	forms := 0
	for _, set := range []bool{leaf, e.All != nil, e.Any != nil, e.Not != nil, e.Fact != nil} {
		if set {
			forms++
		}
	}
	if forms != 1 {
		return errors.New("choose one match form: comparison, all, any, not or fact")
	}
	if leaf {
		if e.Field != "" && !FieldName.MatchString(e.Field) {
			return errors.New("field must be a named tool value")
		}
		if e.Entrances != nil {
			if len(e.Entrances) == 0 || len(e.Entrances) > 12 || e.Value != nil || e.Op == "exists" {
				return errors.New("use 1-12 literal entrances with a text operator")
			}
			for i, s := range e.Entrances {
				if err := e.Rule.Validate(s, 640); err != nil {
					return fmt.Errorf("entrances[%d]: %w", i, err)
				}
			}
			return nil
		}
		if e.Field == "" {
			return errors.New("a value predicate requires a named field")
		}
		return e.Rule.Validate(e.Value, 256)
	}
	if e.Fact != nil {
		if !allowFacts || !FieldName.MatchString(e.Fact.Name) || len(e.Fact.Key) < 1 || len(e.Fact.Key) > 4 {
			return errors.New("fact requires a registered name and 1-4 named correlation fields")
		}
		for _, field := range e.Fact.Key {
			if !FieldName.MatchString(field) {
				return errors.New("fact keys must use named tool fields")
			}
		}
		return e.Fact.Match.Validate(depth+1, false)
	}
	if e.Not != nil {
		return e.Not.Validate(depth+1, allowFacts)
	}
	children := e.All
	if e.Any != nil {
		children = e.Any
	}
	if len(children) < 1 || len(children) > 8 {
		return errors.New("all/any requires 1-8 children")
	}
	for i, child := range children {
		if err := child.Validate(depth+1, allowFacts); err != nil {
			return fmt.Errorf("condition %d: %w", i+1, err)
		}
	}
	return nil
}

// Every successful alternative must have an observable positive entrance.
func (e *Expression) RequiresEntrance() bool {
	if e == nil || e.Not != nil || e.Fact != nil {
		return false
	}
	if e.All != nil {
		for _, c := range e.All {
			if c.RequiresEntrance() {
				return true
			}
		}
		return false
	}
	if e.Any != nil {
		for _, c := range e.Any {
			if !c.RequiresEntrance() {
				return false
			}
		}
		return len(e.Any) > 0
	}
	return len(e.Entrances) > 0
}

func (e *Expression) VisitEntrances(yield func(*Expression, string)) {
	if e == nil || e.Not != nil || e.Fact != nil {
		return
	}
	for _, child := range e.All {
		child.VisitEntrances(yield)
	}
	for _, child := range e.Any {
		child.VisitEntrances(yield)
	}
	for _, value := range e.Entrances {
		yield(e, value)
	}
}

func (e *Expression) VisitFacts(yield func(*Fact)) {
	if e == nil {
		return
	}
	if e.Fact != nil {
		yield(e.Fact)
	}
	e.Not.VisitFacts(yield)
	for _, child := range e.All {
		child.VisitFacts(yield)
	}
	for _, child := range e.Any {
		child.VisitFacts(yield)
	}
}

type NamedText struct {
	Field string
	Value string
}

type Input struct {
	Fields   map[string]any
	Texts    []NamedText
	Fact     func(*Fact) (Truth, error)
	prepared map[string]*Text
}

func (i *Input) text(value string) *Text {
	if t := i.prepared[value]; t != nil {
		return t
	}
	t := NewText(value)
	if i.prepared == nil {
		i.prepared = map[string]*Text{}
	}
	i.prepared[value] = t
	return t
}

type Hit struct {
	Field    string
	Entrance string
	Rule     Rule
}

type Check struct {
	Value         any    `json:"value,omitempty"`
	CaseSensitive *bool  `json:"case_sensitive,omitempty"`
	Normalize     string `json:"normalize,omitempty"`

	Field     string   `json:"field,omitempty"`
	Fact      string   `json:"fact,omitempty"`
	Op        string   `json:"op,omitempty"`
	Entrances []string `json:"entrances,omitempty"`
	Outcome   string   `json:"outcome"`
}

func (e *Expression) Evaluate(i *Input, report func(Check)) (Truth, *Hit, error) {
	if e.Fact != nil {
		truth := Unknown
		var err error
		if i.Fact != nil {
			truth, err = i.Fact(e.Fact)
		}
		if report != nil {
			report(Check{Fact: e.Fact.Name, Outcome: truth.String()})
		}
		return truth, nil, err
	}
	if e.Not != nil {
		truth, _, err := e.Not.Evaluate(i, report)
		if err != nil || truth == Unknown {
			return truth, nil, err
		}
		return Bool(truth == False), nil, nil
	}
	if e.All != nil || e.Any != nil {
		children, stop, fallback := e.All, False, True
		if e.Any != nil {
			children, stop, fallback = e.Any, True, False
		}
		var witness *Hit
		for _, child := range children {
			value, hit, err := child.Evaluate(i, report)
			if err != nil || value == stop {
				return value, hit, err
			}
			if witness == nil && hit != nil {
				witness = hit
			}
			if value == Unknown {
				fallback = Unknown
			}
		}
		return fallback, witness, nil
	}
	truth, hit := e.compare(i)
	if report != nil {
		report(Check{Field: e.Field, Op: e.Op, Entrances: e.Entrances, Value: e.Value, CaseSensitive: e.CaseSensitive, Normalize: e.Normalize, Outcome: truth.String()})
	}
	return truth, hit, nil
}

func (e *Expression) compare(i *Input) (Truth, *Hit) {
	if len(e.Entrances) == 0 {
		return e.Rule.Compare(i.Fields[e.Field], e.Value, false), nil
	}
	selected := i.Texts
	if e.Field != "" {
		text, ok := i.Fields[e.Field].(string)
		if !ok {
			return Unknown, nil
		}
		selected = []NamedText{{Field: e.Field, Value: text}}
	}
	for _, value := range e.Entrances {
		for _, text := range selected {
			if e.Rule.MatchText(i.text(text.Value), value, false) {
				return True, &Hit{Field: text.Field, Entrance: value, Rule: e.Rule}
			}
		}
	}
	return False, nil
}

func (t Truth) String() string {
	switch t {
	case True:
		return "matched"
	case False:
		return "not_matched"
	default:
		return "unknown"
	}
}
