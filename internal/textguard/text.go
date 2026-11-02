// Package textguard prevents JSON decoders from silently repairing evidence.
package textguard

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

type Issue struct {
	Field      string `json:"field"`
	Kind       string `json:"kind"`
	ByteOffset int    `json:"byte_offset"`
}

// Inspect reports the first issue per field, without returning its private text.
func Inspect(field, value string) *Issue {
	if !utf8.ValidString(value) {
		for offset := 0; offset < len(value); {
			r, n := utf8.DecodeRuneInString(value[offset:])
			if r == utf8.RuneError && n == 1 {
				return &Issue{field, "invalid_utf8", offset}
			}
			offset += n
		}
	}
	if offset := strings.IndexRune(value, utf8.RuneError); offset >= 0 {
		return &Issue{field, "replacement_character", offset}
	}
	return nil
}

func Check(field, value string, allowReplacement bool) error {
	issue := Inspect(field, value)
	if issue == nil {
		return nil
	}
	if issue.Kind == "replacement_character" && allowReplacement {
		return nil
	}
	return fmt.Errorf("text integrity error in %s: %s at UTF-8 byte %d; no write performed. Resend this field from the original source. Do not guess or silently remove damaged characters. Set allow_replacement_characters only when the original intentionally contains U+FFFD", issue.Field, issue.Kind, issue.ByteOffset)
}

func hex4(raw []byte) (uint16, bool) {
	if len(raw) < 4 {
		return 0, false
	}
	var n uint16
	for _, b := range raw[:4] {
		n <<= 4
		switch {
		case b >= '0' && b <= '9':
			n += uint16(b - '0')
		case b >= 'a' && b <= 'f':
			n += uint16(b - 'a' + 10)
		case b >= 'A' && b <= 'F':
			n += uint16(b - 'A' + 10)
		default:
			return 0, false
		}
	}
	return n, true
}

// JSON checks bytes and UTF-16 escapes BEFORE encoding/json or the MCP SDK can
// normalize them. Literal/escaped U+FFFD stays intact for field-level handling.
// Ordinary JSON syntax is still validated by the protocol decoder.
func JSON(raw []byte) error {
	if !utf8.Valid(raw) {
		for offset := 0; offset < len(raw); {
			r, n := utf8.DecodeRune(raw[offset:])
			if r == utf8.RuneError && n == 1 {
				return fmt.Errorf("invalid_utf8 at JSON byte %d", offset)
			}
			offset += n
		}
	}
	inString := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || raw[i] != '\\' {
			continue
		}
		if i+1 >= len(raw) {
			return fmt.Errorf("incomplete JSON escape at byte %d", i)
		}
		if raw[i+1] != 'u' {
			i++
			continue
		}
		if i+6 > len(raw) {
			return fmt.Errorf("incomplete Unicode escape at JSON byte %d", i)
		}
		v, ok := hex4(raw[i+2 : i+6])
		if !ok {
			return fmt.Errorf("invalid Unicode escape at JSON byte %d", i)
		}
		switch {
		case v >= 0xD800 && v <= 0xDBFF:
			if i+12 > len(raw) || !bytes.Equal(raw[i+6:i+8], []byte{'\\', 'u'}) {
				return fmt.Errorf("unpaired UTF-16 high surrogate at JSON byte %d", i)
			}
			low, ok := hex4(raw[i+8 : i+12])
			if !ok || low < 0xDC00 || low > 0xDFFF {
				return fmt.Errorf("unpaired UTF-16 high surrogate at JSON byte %d", i)
			}
			i += 11
		case v >= 0xDC00 && v <= 0xDFFF:
			return fmt.Errorf("unpaired UTF-16 low surrogate at JSON byte %d", i)
		default:
			i += 5
		}
	}
	return nil
}
