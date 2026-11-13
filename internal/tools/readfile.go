package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"trimness/internal/core"
	"unicode/utf8"
)

// maxReadBytes caps what readfile returns so one big file can't flood the
// model's context. Longer files are truncated with a note saying so.
const maxReadBytes = 64 << 10

type readArgs struct {
	Path string `json:"path"`
}

// ReadFile returns the text of a file inside Root. Paths are relative to Root;
// os.Root rejects anything that would escape it (.., absolute paths, symlinks
// pointing outside).
type ReadFile struct {
	Root *os.Root
}

func (ReadFile) Name() string { return "readfile" }

func (ReadFile) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name: "readfile",
		Description: "Read a text file from the project and return its contents. " +
			"Use a path relative to the project root. Files larger than 64 KiB are truncated " +
			"(the result says so); binary files cannot be read.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "File path relative to the project root, e.g. internal/agent/agent.go"}
			},
			"required": ["path"]
		}`),
	}
}

func (t ReadFile) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args readArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	path := args.Path

	f, err := t.Root.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a file", path)
	}

	// Read one byte past the cap so we can tell "exactly at the cap" from "over".
	data, err := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("%s looks like a binary file; refusing to read it", path)
	}

	if len(data) > maxReadBytes {
		data = data[:maxReadBytes]
		// Don't leave half a multi-byte character at the cut.
		for i := 0; i < utf8.UTFMax && len(data) > 0; i++ {
			if r, size := utf8.DecodeLastRune(data); r != utf8.RuneError || size != 1 {
				break
			}
			data = data[:len(data)-1]
		}
		return fmt.Sprintf("%s\n[truncated: showing the first %d of %d bytes]", data, len(data), info.Size()), nil
	}
	return string(data), nil
}
