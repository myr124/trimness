package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"trimness/internal/agent"
	"trimness/internal/core"
)

// The registry must plug into the agent's tool and approval seams.
var (
	_ agent.ToolRunner = (*Registry)(nil)
	_ agent.Approver   = (*Registry)(nil)
)

func newTestRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return NewRegistry(ReadFile{Root: root}, WriteFile{Root: root}), dir
}

func callOf(name string, args map[string]any) core.ToolCall {
	raw, _ := json.Marshal(args)
	return core.ToolCall{ToolName: name, ToolID: "id-1", Args: raw}
}

func exec(r *Registry, name string, args map[string]any) core.ToolResult {
	return r.Execute(context.Background(), callOf(name, args))
}

func TestWriteThenRead(t *testing.T) {
	r, dir := newTestRegistry(t)

	res := exec(r, "writefile", map[string]any{"path": "notes.txt", "content": "hello\nworld"})
	if res.IsError || !strings.HasPrefix(res.Output, "created notes.txt") {
		t.Fatalf("write: %+v", res)
	}
	if res.ToolID != "id-1" {
		t.Fatalf("ToolID not echoed: %q", res.ToolID)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(b) != "hello\nworld" {
		t.Fatalf("on disk: %q", b)
	}

	res = exec(r, "readfile", map[string]any{"path": "notes.txt"})
	if res.IsError || res.Output != "hello\nworld" {
		t.Fatalf("read: %+v", res)
	}
}

func TestWriteOverwritesAndKeepsMode(t *testing.T) {
	r, dir := newTestRegistry(t)
	p := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(p, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := exec(r, "writefile", map[string]any{"path": "run.sh", "content": "new"})
	if res.IsError || !strings.HasPrefix(res.Output, "overwrote run.sh") {
		t.Fatalf("overwrite: %+v", res)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode changed to %v", info.Mode().Perm())
	}
	if b, _ := os.ReadFile(p); string(b) != "new" {
		t.Fatalf("content: %q", b)
	}
	if _, err := os.Stat(p + ".trimness-tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

func TestWriteEmptyFileAllowed(t *testing.T) {
	r, dir := newTestRegistry(t)
	if res := exec(r, "writefile", map[string]any{"path": "empty.txt", "content": ""}); res.IsError {
		t.Fatalf("%+v", res)
	}
	if info, err := os.Stat(filepath.Join(dir, "empty.txt")); err != nil || info.Size() != 0 {
		t.Fatalf("empty file not created: %v", err)
	}
}

func TestWriteCreatesParentDirs(t *testing.T) {
	r, dir := newTestRegistry(t)
	if res := exec(r, "writefile", map[string]any{"path": "a/b/c.txt", "content": "x"}); res.IsError {
		t.Fatalf("%+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a/b/c.txt")); string(b) != "x" {
		t.Fatalf("content: %q", b)
	}
}

func TestSandboxRejectsEscapes(t *testing.T) {
	r, dir := newTestRegistry(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"../secret.txt", secret, "link/secret.txt", "a/../../secret.txt"} {
		if res := exec(r, "readfile", map[string]any{"path": path}); !res.IsError || strings.Contains(res.Output, "top secret") {
			t.Errorf("readfile %q escaped: %+v", path, res)
		}
		if res := exec(r, "writefile", map[string]any{"path": path, "content": "pwned"}); !res.IsError {
			t.Errorf("writefile %q escaped: %+v", path, res)
		}
	}
	if b, _ := os.ReadFile(secret); string(b) != "top secret" {
		t.Fatalf("file outside the root was modified: %q", b)
	}
}

func TestReadErrors(t *testing.T) {
	r, dir := newTestRegistry(t)
	_ = os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin"), []byte{'a', 0, 'b'}, 0o644)

	for name, path := range map[string]string{"missing": "nope.txt", "directory": "sub", "binary": "bin"} {
		if res := exec(r, "readfile", map[string]any{"path": path}); !res.IsError {
			t.Errorf("%s: expected error, got %+v", name, res)
		}
	}
}

func TestReadTruncatesLargeFiles(t *testing.T) {
	r, dir := newTestRegistry(t)
	big := bytes.Repeat([]byte("é"), maxReadBytes) // 2 bytes per rune: the cap lands mid-character
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}

	res := exec(r, "readfile", map[string]any{"path": "big.txt"})
	if res.IsError || !strings.Contains(res.Output, "[truncated:") {
		t.Fatalf("expected truncation note: error=%v len=%d", res.IsError, len(res.Output))
	}
	body, _, _ := strings.Cut(res.Output, "\n[truncated:")
	if strings.ContainsRune(body, '�') {
		t.Fatal("truncation split a multi-byte character")
	}
}

func TestBadArguments(t *testing.T) {
	r, dir := newTestRegistry(t)
	raw := func(name, args string) core.ToolResult {
		return r.Execute(context.Background(), core.ToolCall{ToolName: name, ToolID: "x", Args: json.RawMessage(args)})
	}

	cases := map[string]core.ToolResult{
		"readfile no args":          raw("readfile", ``),
		"readfile empty object":     raw("readfile", `{}`),
		"readfile wrong type":       raw("readfile", `{"path": 5}`),
		"readfile unknown field":    raw("readfile", `{"path": "a", "mode": "x"}`),
		"readfile malformed json":   raw("readfile", `{"path":`),
		"writefile missing content": raw("writefile", `{"path": "f.txt"}`),
		"writefile missing path":    raw("writefile", `{"content": "x"}`),
		"writefile wrong type":      raw("writefile", `{"path": "f.txt", "content": 7}`),
	}
	for name, res := range cases {
		if !res.IsError || res.ToolID != "x" {
			t.Errorf("%s: expected error result, got %+v", name, res)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); !os.IsNotExist(err) {
		t.Error("a rejected write created a file")
	}
}

func TestUnknownTool(t *testing.T) {
	r, _ := newTestRegistry(t)
	res := exec(r, "deletefile", map[string]any{"path": "x"})
	if !res.IsError || !strings.Contains(res.Output, "readfile") || res.ToolID != "id-1" {
		t.Errorf("unknown tool: %+v", res)
	}
}

func TestCancelledContext(t *testing.T) {
	r, dir := newTestRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := r.Execute(ctx, callOf("writefile", map[string]any{"path": "f.txt", "content": "x"}))
	if !res.IsError {
		t.Fatal("expected error for cancelled ctx")
	}
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); !os.IsNotExist(err) {
		t.Fatal("tool ran despite cancelled ctx")
	}
}

func TestDefinitions(t *testing.T) {
	r, _ := newTestRegistry(t)
	defs := r.Definitions()
	if len(defs) != 2 || defs[0].Name != "readfile" || defs[1].Name != "writefile" {
		t.Fatalf("definitions (should be sorted by name): %+v", defs)
	}
	for _, d := range defs {
		if d.Description == "" {
			t.Errorf("%s has no description", d.Name)
		}
		var schema struct {
			Type       string         `json:"type"`
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(d.Parameters, &schema); err != nil {
			t.Fatalf("%s: parameters are not valid JSON: %v", d.Name, err)
		}
		if schema.Type != "object" || len(schema.Properties) == 0 || len(schema.Required) == 0 {
			t.Errorf("%s: schema looks wrong: %+v", d.Name, schema)
		}
		for _, req := range schema.Required {
			if _, ok := schema.Properties[req]; !ok {
				t.Errorf("%s: required field %q is not a property", d.Name, req)
			}
		}
	}
}

func TestOnlyWriteNeedsApproval(t *testing.T) {
	r, dir := newTestRegistry(t)
	_ = os.WriteFile(filepath.Join(dir, "exists.txt"), []byte("old"), 0o644)

	if _, needed := r.NeedsApproval(callOf("readfile", map[string]any{"path": "exists.txt"})); needed {
		t.Error("readfile should not need approval")
	}
	if _, needed := r.NeedsApproval(callOf("nope", nil)); needed {
		t.Error("unknown tools should not need approval")
	}

	summary, needed := r.NeedsApproval(callOf("writefile", map[string]any{"path": "new.txt", "content": "a\nb"}))
	if !needed || !strings.Contains(summary, "create new.txt") || !strings.Contains(summary, "| a") {
		t.Errorf("new file summary: needed=%v %q", needed, summary)
	}
	summary, _ = r.NeedsApproval(callOf("writefile", map[string]any{"path": "exists.txt", "content": "new"}))
	if !strings.Contains(summary, "overwrite exists.txt") {
		t.Errorf("overwrite summary: %q", summary)
	}

	long := strings.Repeat("line\n", 20)
	summary, _ = r.NeedsApproval(callOf("writefile", map[string]any{"path": "big.txt", "content": long}))
	if !strings.Contains(summary, "more lines") || strings.Count(summary, "| line") != previewLines {
		t.Errorf("long content should be previewed and cut:\n%s", summary)
	}

	summary, needed = r.NeedsApproval(core.ToolCall{ToolName: "writefile", Args: json.RawMessage(`{"path": 3}`)})
	if !needed || !strings.Contains(summary, "invalid") {
		t.Errorf("invalid args should still be gated: %v %q", needed, summary)
	}
}
