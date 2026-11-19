package memory

import (
	"strings"
)

func normalizedResource(s string) string { return strings.ToLower(strings.ReplaceAll(s, "\\", "/")) }
