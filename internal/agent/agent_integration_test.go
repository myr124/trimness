//go:build integration

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"trimness/internal/llm"
	"trimness/internal/tools"
)

// TestGeminiToolLoop runs the real agent loop against the live Gemini API with
// the real file tools in a temp directory. It is opt-in because it needs a key
// and network access:
//
//	GEMINI_API_KEY=... go test -tags integration -run TestGeminiToolLoop -v ./internal/agent
func TestGeminiToolLoop(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-3.5-flash"
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("the magic word is pelican\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	a := &Agent{
		Client: llm.GeminiClient{API_key: key, Model: model},
		Tools:  tools.NewRegistry(tools.ReadFile{Root: root}, tools.WriteFile{Root: root}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	prompt := "Use your tools: read secret.txt, then create a file named answer.txt containing only the magic word it reveals."

	var sawRead, sawWrite, sawApproval bool
	var final string
	for ev := range a.RunStream(ctx, prompt) {
		switch e := ev.(type) {
		case ApprovalRequested:
			sawApproval = true
			t.Logf("approval requested: %s", e.Summary)
			e.Reply <- true
		case ToolStarted:
			t.Logf("tool call: %s %s (id %s)", e.Call.ToolName, e.Call.Args, e.Call.ToolID)
			sawRead = sawRead || e.Call.ToolName == "readfile"
			sawWrite = sawWrite || e.Call.ToolName == "writefile"
		case ToolFinished:
			t.Logf("tool result (error=%v): %s", e.Result.IsError, e.Result.Output)
		case MessageDone:
			final = e.Msg.Content
		case RunError:
			t.Fatalf("run failed: %v", e.Err)
		}
	}

	t.Logf("final answer: %s", final)
	if !sawRead || !sawWrite || !sawApproval {
		t.Fatalf("expected readfile, an approved writefile: read=%v write=%v approval=%v", sawRead, sawWrite, sawApproval)
	}
	b, err := os.ReadFile(filepath.Join(dir, "answer.txt"))
	if err != nil || !strings.Contains(strings.ToLower(string(b)), "pelican") {
		t.Fatalf("answer.txt: %q err=%v", b, err)
	}
}
