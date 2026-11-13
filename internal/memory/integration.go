package memory

import (
	"context"
	"errors"
)

const ModeAssist = "assist"
const ModeStrict = "strict"

func IntegrationMode(value string) (string, error) {
	if value == "" {
		return ModeAssist, nil
	}
	if value != ModeAssist && value != ModeStrict {
		return "", errors.New("mode must be assist or strict")
	}
	return value, nil
}

// Assist validates actual source identity/scope, without inventing a mandatory
// per-turn read/complete checkpoint. Evidence validity is still enforced by Put.
func (s *Store) ValidateWriteProvenance(ctx context.Context, d Draft) error {
	if d.Kind == "record" || d.ContextSourceID == "" {
		return nil
	}
	e, err := userSource(ctx, s.db, d.ContextSourceID, d.Scope)
	if err != nil {
		return err
	}
	_, err = s.currentCheckpoint(ctx, e, true)
	return err
}
