package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

var probeRuleID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,39}$`)
var probeToken = regexp.MustCompile(`^[A-Za-z0-9_$]+$`)

type probeCondition struct {
	All   []*probeCondition `json:"all,omitempty"`
	Any   []*probeCondition `json:"any,omitempty"`
	Not   *probeCondition   `json:"not,omitempty"`
	Field string            `json:"field,omitempty"`
	Op    string            `json:"op,omitempty"`
	Value any               `json:"value,omitempty"`
	Fact  string            `json:"fact,omitempty"`
	Key   []string          `json:"key,omitempty"`
}

func probeFieldAllowed(path string, observation bool) bool {
	source, path, ok := strings.Cut(path, ".")
	return ok && (source == "input" || observation && source == "result") && probeFieldPath.MatchString(path)
}

func validProbeKey(key []string, observation bool) bool {
	if len(key) == 0 || len(key) > 4 {
		return false
	}
	for _, field := range key {
		if !probeFieldAllowed(field, observation) {
			return false
		}
	}
	return true
}

func (c *probeCondition) validate(observation bool, facts []probeFactRule, depth int) error {
	if c == nil {
		if depth > 0 {
			return errors.New("null condition")
		}
		return nil
	}
	if depth > 5 {
		return errors.New("when has more than six levels")
	}
	forms := 0
	for _, present := range []bool{c.All != nil, c.Any != nil, c.Not != nil, c.Field != "", c.Fact != ""} {
		if present {
			forms++
		}
	}
	if forms != 1 || (c.Field == "" && (c.Op != "" || c.Value != nil)) || (c.Fact == "" && c.Key != nil) {
		return errors.New("expected exactly one of all, any, not, field or fact")
	}
	if c.Field != "" {
		if !probeFieldAllowed(c.Field, observation) {
			return fmt.Errorf("invalid field %q for this event", c.Field)
		}
		switch c.Op {
		case "exists":
			if c.Value != nil {
				return errors.New("exists takes no value")
			}
		case "eq":
			switch value := c.Value.(type) {
			case string:
				if len(value) > 256 {
					return errors.New("eq text value exceeds 256 bytes")
				}
			case bool:
			case float64:
				if math.Abs(value) > 9007199254740991 {
					return errors.New("eq number exceeds the safe JSON integer range; use a string")
				}
			default:
				return errors.New("eq requires a string, boolean or number")
			}
		case "prefix", "suffix", "token":
			value, ok := c.Value.(string)
			if !ok || value == "" || len(value) > 256 || c.Op == "token" && !probeToken.MatchString(value) {
				return fmt.Errorf("%s requires a nonempty text value (token uses ASCII identifier characters)", c.Op)
			}
		default:
			return fmt.Errorf("unknown operator %q", c.Op)
		}
		return nil
	}
	if c.Fact != "" {
		if observation || !validProbeKey(c.Key, false) {
			return errors.New("fact conditions require input correlation fields in a pre-tool rule")
		}
		for _, rule := range facts {
			if rule.Name == c.Fact && len(rule.Key) == len(c.Key) {
				return nil
			}
		}
		return fmt.Errorf("fact %q is undefined in this project or its key length differs", c.Fact)
	}
	if c.Not != nil {
		return c.Not.validate(observation, facts, depth+1)
	}
	children := c.All
	if c.Any != nil {
		children = c.Any
	}
	if len(children) == 0 || len(children) > 8 {
		return errors.New("all/any requires 1-8 conditions")
	}
	for _, child := range children {
		if err := child.validate(observation, facts, depth+1); err != nil {
			return err
		}
	}
	return nil
}

type probeTruth int

const (
	probeUnknown probeTruth = iota - 1
	probeFalse
	probeTrue
)

func probeBool(v bool) probeTruth {
	if v {
		return probeTrue
	}
	return probeFalse
}

type probeValues struct {
	input, result map[string]any
	fact          func(name, key string) (probeTruth, error)
}

func (v probeValues) field(field string) any {
	source, field, _ := strings.Cut(field, ".")
	if source == "result" {
		return probeValue(v.result, field)
	}
	return probeValue(v.input, field)
}

func (v probeValues) key(fields []string) string {
	values := make([]any, 0, len(fields))
	for _, field := range fields {
		value := v.field(field)
		switch item := value.(type) {
		case string:
			if strings.TrimSpace(item) == "" || len(item) > 256 {
				return ""
			}
		case float64:
			if math.Trunc(item) != item || math.Abs(item) > 9007199254740991 {
				return ""
			}
		case bool:
		default:
			return ""
		}
		values = append(values, value)
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}

func (c *probeCondition) evaluate(v probeValues) (probeTruth, error) {
	switch {
	case c == nil:
		return probeTrue, nil
	case c.Not != nil:
		result, err := c.Not.evaluate(v)
		if err != nil || result == probeUnknown {
			return result, err
		}
		return probeBool(result == probeFalse), nil
	case c.Fact != "":
		key := v.key(c.Key)
		if key == "" || v.fact == nil {
			return probeUnknown, nil
		}
		return v.fact(c.Fact, key)
	case c.Field != "":
		value := v.field(c.Field)
		if c.Op == "exists" {
			return probeBool(value != nil), nil
		}
		switch expected := c.Value.(type) {
		case string:
			actual, ok := value.(string)
			if !ok || len(actual) > 32768 {
				return probeUnknown, nil
			}
			switch c.Op {
			case "prefix":
				return probeBool(strings.HasPrefix(actual, expected)), nil
			case "suffix":
				return probeBool(strings.HasSuffix(actual, expected)), nil
			case "token":
				return probeBool(memory.MatchIdentifier(actual, expected)), nil
			default:
				return probeBool(actual == expected), nil
			}
		case bool:
			if actual, ok := value.(bool); ok {
				return probeBool(actual == expected), nil
			}
		case float64:
			if actual, ok := value.(float64); ok {
				return probeBool(actual == expected), nil
			}
		}
		return probeUnknown, nil
	default:
		children, stop, fallback := c.All, probeFalse, probeTrue
		if c.Any != nil {
			children, stop, fallback = c.Any, probeTrue, probeFalse
		}
		for _, child := range children {
			result, err := child.evaluate(v)
			if err != nil || result == stop {
				return result, err
			}
			if result == probeUnknown {
				fallback = probeUnknown
			}
		}
		return fallback, nil
	}
}
