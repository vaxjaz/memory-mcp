package server

import (
	"fmt"
	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func toolError(operation string, store *memory.Store, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s (data_dir=%q): %w", operation, store.Dir, err)
}
