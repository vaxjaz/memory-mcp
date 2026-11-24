package hooks

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// layoutWidth is the line length the rendered file stays within where it can.
// A value that fits on one line is written on one line, so tool-probes.json
// reads as a list of rules instead of one element per line.
const layoutWidth = 150

// jsonValue is JSON held in memory without losing the order of object keys.
type jsonValue struct {
	kind  byte // 's' scalar, 'o' object, 'a' array
	raw   json.RawMessage
	keys  []string
	items []*jsonValue
}

func parseJSON(raw []byte) (*jsonValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return readJSONValue(decoder)
}

func readJSONValue(decoder *json.Decoder) (*jsonValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'), json.Delim('['):
		value := &jsonValue{kind: 'a'}
		if token == json.Delim('{') {
			value.kind = 'o'
		}
		for decoder.More() {
			key := ""
			if value.kind == 'o' {
				name, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key = name.(string)
			}
			item, err := readJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			value.keys = append(value.keys, key)
			value.items = append(value.items, item)
		}
		_, err = decoder.Token()
		return value, err
	default:
		return &jsonValue{kind: 's', raw: compactJSON(token)}, nil
	}
}

// compactJSON encodes on one line, without the HTML escaping of `<`, `>` and
// `&`, so a path or a tool name is written as it was read.
func compactJSON(value any) []byte {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return []byte("null")
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
}

func (v *jsonValue) line() string {
	var out strings.Builder
	v.writeInline(&out)
	return out.String()
}

func (v *jsonValue) writeInline(out *strings.Builder) {
	switch v.kind {
	case 's':
		out.Write(v.raw)
	case 'o':
		out.WriteString("{")
		for i, key := range v.keys {
			if i > 0 {
				out.WriteString(", ")
			}
			out.Write(compactJSON(key))
			out.WriteString(": ")
			v.items[i].writeInline(out)
		}
		out.WriteString("}")
	case 'a':
		out.WriteString("[")
		for i, item := range v.items {
			if i > 0 {
				out.WriteString(", ")
			}
			item.writeInline(out)
		}
		out.WriteString("]")
	}
}

// write puts the value at the given column of its line. A value that fits
// stays on the line; a longer one breaks at its braces, and a long list of
// scalars is filled several per line.
func (v *jsonValue) write(out *strings.Builder, indent string, column int) {
	if line := v.line(); column+len(line) <= layoutWidth {
		out.WriteString(line)
		return
	}
	inner := indent + "  "
	switch v.kind {
	case 'o':
		out.WriteString("{\n")
		for i, key := range v.keys {
			name := string(compactJSON(key))
			out.WriteString(inner + name + ": ")
			v.items[i].write(out, inner, len(inner)+len(name)+2)
			writeSeparator(out, i, len(v.keys))
		}
		out.WriteString(indent + "}")
	case 'a':
		out.WriteString("[\n")
		if v.scalarsOnly() {
			line := inner
			for i, item := range v.items {
				text := string(item.raw)
				if i > 0 && len(line)+len(text)+2 > layoutWidth {
					out.WriteString(line + ",\n")
					line = inner + text
					continue
				}
				if i > 0 {
					line += ", "
				}
				line += text
			}
			out.WriteString(line + "\n")
		} else {
			for i, item := range v.items {
				out.WriteString(inner)
				item.write(out, inner, len(inner))
				writeSeparator(out, i, len(v.items))
			}
		}
		out.WriteString(indent + "]")
	default:
		out.Write(v.raw)
	}
}

// writeSeparator ends the line of item i and joins it to the next with a comma.
func writeSeparator(out *strings.Builder, i, count int) {
	if i < count-1 {
		out.WriteString(",")
	}
	out.WriteString("\n")
}

func (v *jsonValue) scalarsOnly() bool {
	for _, item := range v.items {
		if item.kind != 's' {
			return false
		}
	}
	return true
}

// renderProbeConfig writes "tools" first and every other top-level key after
// it with its content unchanged, using LF line endings.
func renderProbeConfig(tools []probeRule, rest map[string]json.RawMessage) []byte {
	document := &jsonValue{kind: 'o'}
	keys := make([]string, 0, len(rest)+1)
	for key := range rest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := map[string][]byte{"tools": compactJSON(tools)}
	for key, value := range rest {
		values[key] = value
	}
	for _, key := range append([]string{"tools"}, keys...) {
		value, err := parseJSON(values[key])
		if err != nil {
			// The caller validated the file; should anything still not parse,
			// it is carried over byte for byte rather than dropped.
			value = &jsonValue{kind: 's', raw: values[key]}
		}
		document.keys = append(document.keys, key)
		document.items = append(document.items, value)
	}
	var out strings.Builder
	out.WriteString("{\n")
	for i, key := range document.keys {
		name := string(compactJSON(key))
		out.WriteString("  " + name + ": ")
		document.items[i].write(&out, "  ", 2+len(name)+2)
		writeSeparator(&out, i, len(document.keys))
	}
	out.WriteString("}\n")
	return []byte(out.String())
}
