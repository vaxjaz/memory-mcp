package memory

import (
	"database/sql"
	"fmt"
)

// Keep ErrNoRows discoverable by internal callers without exposing an
// unactionable SQL message to the host. Missing context is not a disk-write error.
type lookupFailure struct{ code, field, id, scope, recovery string }

func (e *lookupFailure) Error() string {
	return fmt.Sprintf("%s: %s=%q, scope=%q. %s", e.code, e.field, e.id, e.scope, e.recovery)
}
func (e *lookupFailure) Unwrap() error { return sql.ErrNoRows }
func missingLookup(code, field, id, scope, recovery string) error {
	return &lookupFailure{code, field, id, scope, recovery}
}
