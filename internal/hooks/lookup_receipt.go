package hooks

import (
	"encoding/json"
	"errors"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

// Parse only the documented MCP call-result structure. Never search business
// text, nested arbitrary JSON, logs or UI notifications for a receipt string.
func lookupResult(e Event) (memory.ExperienceLookupResult, error) {
	var out memory.ExperienceLookupResult
	if e.IsError || e.Error != nil {
		return out, errors.New("native lookup transport failed")
	}
	raw, err := json.Marshal(e.Response)
	if err != nil {
		return out, err
	}
	var result struct {
		IsError bool `json:"isError"`
		Error   any  `json:"error"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return out, err
	}
	if result.IsError || result.Error != nil || len(result.Content) != 1 || result.Content[0].Type != "text" {
		return out, errors.New("unsuccessful or unsupported MCP lookup result")
	}
	if err = json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		return out, err
	}
	if out.ReceiptID == "" || out.ContentRole != "evidence_candidates" || out.Items == nil {
		return out, errors.New("lookup omitted complete receipt envelope")
	}
	return out, nil
}
