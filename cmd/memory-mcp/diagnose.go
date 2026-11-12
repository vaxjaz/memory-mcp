package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func diagnose(args []string, version, commit string) error {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	dir := fs.String("data-dir", "", "EXPLICIT existing library directory; opened read-only without migration")
	entry := fs.String("entry-id", "", "full original/current case ID to audit exact forward links")
	scope := fs.String("scope", "", "expected scope, reported for comparison")
	ids := fs.String("probe-ids", "", "comma-separated hook/source IDs to test for existence")
	out := fs.String("out", "", "optional new JSON report file; never overwritten")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 || *dir == "" {
		return errors.New("diagnose requires explicit --data-dir; no library path is guessed")
	}
	store, err := memory.OpenReadOnly(*dir)
	if err != nil {
		return fmt.Errorf("diagnose read-only open: %w", err)
	}
	defer store.Close()
	probes := []string{}
	for _, id := range strings.Split(*ids, ",") {
		if id = strings.TrimSpace(id); id != "" {
			probes = append(probes, id)
		}
	}
	report, err := store.DiagnoseLookups(context.Background(), *scope, *entry, probes)
	if err != nil {
		return err
	}
	result := map[string]any{"version": version, "source_commit": commit, "diagnostics": report}
	if *out == "" {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	f, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(result)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Saved raw read-only diagnostics to", *out)
	return nil
}
