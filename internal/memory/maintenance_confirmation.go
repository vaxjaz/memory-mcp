package memory

import (
	"context"
	"errors"
	"github.com/vaxjaz/memory-mcp/internal/textguard"
	"strings"
)

const MaintenanceSourcePrefix = "maintenance-event:"

func (s *Store) ConfirmMaintenance(ctx context.Context, scope, planID, purpose, quote, requestID string) (string, error) {
	if purpose != "apply" && purpose != "undo" && purpose != "configure" {
		return "", errors.New("confirmation purpose must be apply/undo/configure")
	}
	if strings.TrimSpace(quote) == "" || len(quote) > 1024 || requestID == "" {
		return "", errors.New("a compact actual user approval excerpt and stable request_id are required")
	}
	if err := textguard.Check("approval_quote", quote, false); err != nil {
		return "", err
	}
	if purpose != "configure" {
		p, err := s.CleanupPlan(ctx, planID)
		if err != nil {
			return "", err
		}
		if p.Scope != scope {
			return "", errors.New("confirmation scope mismatch")
		}
		want := "preview"
		if purpose == "undo" {
			want = "applied"
		}
		if p.Status != want {
			return "", errors.New("confirmation requires the current displayed plan")
		}
	} else if planID != "" {
		return "", errors.New("configuration confirmation is separate from a cleanup plan")
	}
	e, err := s.Put(ctx, Draft{Kind: "record", Scope: scope, Title: "Maintenance-only user confirmation", Body: quote, Origin: "user_supplied", SourceRef: MaintenanceSourcePrefix + purpose + ":" + planID, RequestID: "maintenance:" + requestID}, "")
	return e.ID, err
}
