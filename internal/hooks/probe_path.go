package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Instruction and skill files describe how the agent should work and often
// name the project's identifiers. Reading one is not a moment to wake a cue.
var instructionFiles = map[string]bool{"agents.md": true, "claude.md": true, "gemini.md": true, "skill.md": true}
var instructionDirs = map[string]bool{".agents": true, ".claude": true, ".codex": true, ".cursor": true, ".qoder": true}

// instructionPath reports whether a path is an instruction or skill file. It
// compares path segments, so the answer does not depend on whether the path is
// written absolute or relative.
func instructionPath(value string) bool {
	for _, segment := range strings.Split(strings.ToLower(strings.ReplaceAll(value, "\\", "/")), "/") {
		if instructionFiles[segment] || instructionDirs[segment] {
			return true
		}
	}
	return false
}

// instructionReference reports whether a command text names such a file.
func instructionReference(command string) bool {
	for _, token := range strings.FieldsFunc(command, func(r rune) bool {
		return strings.ContainsRune(" \t\n\r\"'`=;|&<>()[]{}", r)
	}) {
		if instructionPath(strings.TrimPrefix(token, "./")) {
			return true
		}
	}
	return false
}

var environmentReference = regexp.MustCompile(`\$\{?[A-Za-z_][A-Za-z0-9_]*\}?|%[A-Za-z_][A-Za-z0-9_]*%`)

// unexpandedPath reports a home or environment reference that the client did
// not expand. Its real location is unknown, so it cannot lie inside the project.
func unexpandedPath(value string) bool {
	return value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) || environmentReference.MatchString(value)
}

// pathVerdict reports why a path is not confirmed to lie inside the project
// root, or "" when it is. The path is followed one component at a time from
// the root: a symbolic link or junction on the way is replaced by its target,
// which must itself lie inside the project. Components that do not exist yet
// cannot hide a link, so the walk stops at the first one.
func pathVerdict(root, cwd, value string) string {
	if unexpandedPath(value) {
		return "path_unresolved"
	}
	path := filepath.FromSlash(value)
	if !filepath.IsAbs(path) {
		if filepath.VolumeName(path) != "" {
			return "path_unresolved"
		}
		path = filepath.Join(cwd, path)
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if outside(rel, err) {
		return "path_outside_project"
	}
	resolved, links := ".", 0
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "." {
			continue
		}
		next := filepath.Join(resolved, component)
		info, err := os.Lstat(filepath.Join(root, next))
		switch {
		case err == nil && info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0:
			resolved = next
		case err == nil:
			// Windows reports a junction as irregular; Readlink names the
			// target of either kind.
			links++
			if links > maxPathLinks {
				return "path_unresolved"
			}
			target, err := os.Readlink(filepath.Join(root, next))
			if err != nil {
				return "path_unresolved"
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(filepath.Join(root, next)), target)
			}
			if target, err = filepath.Rel(root, filepath.Clean(target)); outside(target, err) {
				return "path_outside_project"
			}
			resolved = target
		case errors.Is(err, os.ErrNotExist):
			return ""
		default:
			return "path_unresolved"
		}
	}
	return ""
}

// maxPathLinks bounds the links one path may pass through, so a link loop
// ends as unresolved instead of never.
const maxPathLinks = 32

func outside(rel string, err error) bool {
	return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}
