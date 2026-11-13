package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"trimness/internal/core"
)

type writeArgs struct {
	Path string `json:"path"`
	// A pointer so a missing "content" is an error but an empty string (an
	// intentionally empty file) is allowed.
	Content *string `json:"content"`
}

// previewLines is how many lines of the new content Confirm shows.
const previewLines = 6

// WriteFile creates or overwrites a file inside Root with the given content.
//
// Missing parent directories are created. The write goes to a temp file that
// is then renamed into place, so a failure partway through never leaves a
// half-written target. An existing file keeps its permissions.
//
// WriteFile is Gated: the agent asks the user before running it.
type WriteFile struct {
	Root *os.Root
}

func (WriteFile) Name() string { return "writefile" }

func (WriteFile) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name: "writefile",
		Description: "Create a file or completely overwrite an existing one with the given content. " +
			"Use a path relative to the project root; missing directories are created. " +
			"This replaces the whole file, so read it first if you only mean to change part of it. " +
			"The user is asked to approve each write and may refuse.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "File path relative to the project root, e.g. notes/todo.md"},
				"content": {"type": "string", "description": "The complete new contents of the file"}
			},
			"required": ["path", "content"]
		}`),
	}
}

// Confirm describes the write for the approval prompt: what, where, whether it
// replaces something, and the start of the new content.
func (t WriteFile) Confirm(raw json.RawMessage) string {
	var args writeArgs
	if err := decodeArgs(raw, &args); err != nil || args.Path == "" || args.Content == nil {
		return "writefile with invalid arguments (it will be rejected)"
	}

	action := "create"
	if info, err := t.Root.Stat(args.Path); err == nil && !info.IsDir() {
		action = "overwrite"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s (%d bytes)", action, args.Path, len(*args.Content))

	lines := strings.Split(strings.TrimRight(*args.Content, "\n"), "\n")
	if *args.Content != "" {
		shown := lines
		if len(shown) > previewLines {
			shown = shown[:previewLines]
		}
		for _, l := range shown {
			sb.WriteString("\n  | " + l)
		}
		if len(lines) > previewLines {
			fmt.Fprintf(&sb, "\n  | … (%d more lines)", len(lines)-previewLines)
		}
	}
	return sb.String()
}

func (t WriteFile) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args writeArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if args.Content == nil {
		return "", fmt.Errorf("content is required (use an empty string for an empty file)")
	}
	path, content := args.Path, *args.Content

	perm := fs.FileMode(0o644)
	verb := "created"
	switch info, err := t.Root.Stat(path); {
	case err == nil:
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory, not a file", path)
		}
		perm = info.Mode().Perm()
		verb = "overwrote"
	case !errors.Is(err, fs.ErrNotExist):
		return "", err
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := t.Root.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}

	tmp := path + ".trimness-tmp"
	if err := t.Root.WriteFile(tmp, []byte(content), perm); err != nil {
		return "", err
	}
	if err := t.Root.Rename(tmp, path); err != nil {
		_ = t.Root.Remove(tmp)
		return "", err
	}
	return fmt.Sprintf("%s %s (%d bytes)", verb, path, len(content)), nil
}
