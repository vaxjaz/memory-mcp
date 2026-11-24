package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/matching"
	"github.com/vaxjaz/memory-mcp/internal/memory"
)

type probeObservation struct {
	Name       string            `json:"name"`
	Key        []string          `json:"key"`
	Fields     map[string]string `json:"fields"`
	Lifetime   string            `json:"lifetime,omitempty"`
	TTLSeconds int               `json:"ttl_seconds,omitempty"`
}

func (o probeObservation) validate() error {
	if !matching.FieldName.MatchString(o.Name) || len(o.Key) < 1 || len(o.Key) > 4 || len(o.Fields) < 1 || len(o.Fields) > 8 {
		return errors.New("observation requires a name, 1-4 correlation paths, and 1-8 result fields")
	}
	for _, key := range o.Key {
		source, path, ok := strings.Cut(key, ".")
		if !ok || (source != "input" && source != "result") || !probeObjectPath.MatchString(path) {
			return fmt.Errorf("observation %q key paths use input.<field> or result.<field>", o.Name)
		}
	}
	for alias, path := range o.Fields {
		if !matching.FieldName.MatchString(alias) || !probeObjectPath.MatchString(path) {
			return fmt.Errorf("observation %q has an invalid result field mapping", o.Name)
		}
	}
	if (o.Lifetime != "" && o.Lifetime != "input" && o.Lifetime != "session") || o.TTLSeconds < 0 || o.TTLSeconds > 600 {
		return fmt.Errorf("observation %q requires input/session lifetime and 0-600 ttl_seconds", o.Name)
	}
	return nil
}

func (c probeConfig) observation(scope, name string) (probeObservation, bool) {
	for _, rule := range c.Projects[scope] {
		if !rule.active() {
			continue
		}
		for _, o := range rule.Observe {
			if o.Name == name {
				return o, true
			}
		}
	}
	return probeObservation{}, false
}

func observationKey(paths []string, value func(string) any) string {
	values := make([]any, 0, len(paths))
	for _, path := range paths {
		raw := value(path)
		switch v := raw.(type) {
		case string:
			if strings.TrimSpace(v) == "" || len(v) > 256 {
				return ""
			}
		case float64:
			if math.Trunc(v) != v || math.Abs(v) > 9007199254740991 {
				return ""
			}
		case bool:
		default:
			return ""
		}
		values = append(values, raw)
	}
	bytes, _ := json.Marshal(values)
	return string(bytes)
}

func observationDefinition(o probeObservation, expr *matching.Expression) string {
	return probeDefinition(struct {
		Observation probeObservation
		Match       *matching.Expression
	}{o, expr})
}
func probeDefinition(v any) string {
	data, _ := json.Marshal(v)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func probeResult(e Event) map[string]any {
	if e.Name == "PostToolUseFailure" || e.IsError || e.Error != nil {
		return nil
	}
	result, _ := e.Response.(map[string]any)
	if result == nil || result["isError"] == true {
		return nil
	}
	if structured, found := result["structuredContent"]; found {
		object, _ := structured.(map[string]any)
		return object
	}
	if content, ok := result["content"].([]any); ok {
		if len(content) != 1 {
			return nil
		}
		block, _ := content[0].(map[string]any)
		text, _ := block["text"].(string)
		if block["type"] != "text" || len(text) > 32768 {
			return nil
		}
		var object map[string]any
		if err := json.Unmarshal([]byte(text), &object); err != nil {
			return nil
		}
		return object
	}
	return result
}

func (r Runtime) probeContext(ctx context.Context, e Event, scope string) (memory.ProbeContext, error) {
	project, err := r.Store.ExperienceProjectBinding(ctx, e.CWD, scope)
	return memory.ProbeContext{Host: r.host(), Scope: scope, Session: e.Session, Project: project, Turn: e.TurnID}, err
}

func (r Runtime) startProbeFacts(ctx context.Context, e Event, scope string, sources []probeObservation) error {
	if e.ToolUseID == "" || len(e.ToolUseID) > 512 {
		return r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_call_unknown", e.Tool)
	}
	bound, err := r.probeContext(ctx, e, scope)
	if err != nil {
		return err
	}
	pending, err := r.Store.BeginProbeCall(ctx, bound, e.Tool, e.ToolUseID, probeDefinition(sources))
	if err == nil && !pending {
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_call_unknown", e.Tool)
	}
	return err
}

func (r Runtime) attachFacts(ctx context.Context, e Event, scope string, config probeConfig, input *matching.Input) {
	if e.Session == "" {
		return
	}
	var bound memory.ProbeContext
	input.Fact = func(f *matching.Fact) (matching.Truth, error) {
		source, ok := config.observation(scope, f.Name)
		if !ok || len(source.Key) != len(f.Key) {
			return matching.Unknown, nil
		}
		key := observationKey(f.Key, func(field string) any { return input.Fields[field] })
		if key == "" {
			return matching.Unknown, nil
		}
		if bound.Project == "" {
			var err error
			bound, err = r.probeContext(ctx, e, scope)
			if err != nil {
				return matching.Unknown, err
			}
		}
		value, err := r.Store.ReadProbeFact(ctx, bound, observationDefinition(source, f.Match), key)
		return matching.Truth(value), err
	}
}

func (r Runtime) finishProbeFacts(ctx context.Context, e Event, scope string, sources []probeObservation) error {
	if len(sources) == 0 {
		return nil
	}
	if e.ToolUseID == "" || len(e.ToolUseID) > 512 {
		return r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_result_unbound", e.Tool)
	}
	bound, err := r.probeContext(ctx, e, scope)
	if err != nil {
		return err
	}
	result := probeResult(e)
	type observed struct {
		source probeObservation
		key    string
		input  matching.Input
	}
	mapped := map[string]observed{}
	for _, source := range sources {
		key := observationKey(source.Key, func(path string) any {
			side, path, _ := strings.Cut(path, ".")
			if side == "input" {
				return probeValue(e.Input, path)
			}
			return probeValue(result, path)
		})
		if key == "" {
			continue
		}
		input := matching.Input{Fields: map[string]any{}}
		for alias, path := range source.Fields {
			v := probeValue(result, path)
			switch value := v.(type) {
			case string:
				if len(value) > customProbeTextLimit {
					continue
				}
				input.Texts = append(input.Texts, matching.NamedText{Field: alias, Value: value})
			case bool, float64:
			default:
				continue
			}
			input.Fields[alias] = v
		}
		mapped[source.Name] = observed{source, key, input}
	}

	// Result comparisons are read from current cues. The registration only
	// exposes result fields and stores no input or result value.
	var facts []memory.ProbeFact
	var compareErr error
	seen := map[string]bool{}
	if len(mapped) > 0 {
		err = r.Store.VisitToolFacts(ctx, scope, func(f *matching.Fact) {
			value, ok := mapped[f.Name]
			if !ok || len(f.Key) != len(value.source.Key) || compareErr != nil {
				return
			}
			definition := observationDefinition(value.source, f.Match)
			if seen[definition] {
				return
			}
			if len(facts) >= 128 {
				compareErr = errors.New("too many result comparisons for one tool observation")
				return
			}
			seen[definition] = true
			truth := matching.Unknown
			if result != nil {
				truth, _, compareErr = f.Match.Evaluate(&value.input, nil)
				if compareErr != nil {
					return
				}
			}
			ttl := value.source.TTLSeconds
			if ttl == 0 {
				ttl = 120
			}
			facts = append(facts, memory.ProbeFact{Definition: definition, Key: value.key, Value: int(truth), TTL: time.Duration(ttl) * time.Second, Session: value.source.Lifetime == "session"})
		})
		if err != nil {
			return err
		}
		if compareErr != nil {
			return compareErr
		}
	}
	saved, err := r.Store.CompleteProbeCall(ctx, bound, e.Tool, e.ToolUseID, probeDefinition(sources), facts)
	if err != nil {
		return err
	}
	switch {
	case !saved:
		return r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_result_unbound", e.Tool)
	case len(mapped) == 0:
		return r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_key_unknown", e.Tool)
	default:
		return r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_result_observed", e.Tool)
	}
}
