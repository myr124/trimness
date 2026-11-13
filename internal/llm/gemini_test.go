package llm

import (
	"encoding/json"
	"strings"
	"testing"
	"trimness/internal/core"

	"google.golang.org/genai"
)

func TestToContentsToolRoundTrip(t *testing.T) {
	msgs := []core.Message{
		{Role: "user", Content: "read two files"},
		{Role: "assistant", Content: "on it", ToolCalls: []core.ToolCall{
			{ToolName: "readfile", ToolID: "srv-1", Args: json.RawMessage(`{"path":"a.go"}`), ThoughtSignature: []byte("sig")},
			{ToolName: "readfile", ToolID: generatedIDPrefix + "abc", Args: json.RawMessage(`{"path":"b.go"}`)},
		}},
		{Role: "tool", Content: "package a", ToolCallID: "srv-1", ToolName: "readfile"},
		{Role: "tool", Content: "no such file", ToolCallID: generatedIDPrefix + "abc", ToolName: "readfile", IsError: true},
		{Role: "assistant", Content: "done"},
	}
	cs := toContents(msgs)

	if len(cs) != 4 {
		t.Fatalf("want 4 contents (user, model, ONE grouped tool turn, model), got %d", len(cs))
	}
	if cs[0].Role != genai.RoleUser || cs[1].Role != genai.RoleModel || cs[2].Role != genai.RoleUser || cs[3].Role != genai.RoleModel {
		t.Fatalf("roles: %s %s %s %s", cs[0].Role, cs[1].Role, cs[2].Role, cs[3].Role)
	}

	// The model turn: text plus both calls, signature kept, our own ID withheld.
	model := cs[1].Parts
	if len(model) != 3 || model[0].Text != "on it" {
		t.Fatalf("model parts: %+v", model)
	}
	fc1, fc2 := model[1].FunctionCall, model[2].FunctionCall
	if fc1.Name != "readfile" || fc1.ID != "srv-1" || fc1.Args["path"] != "a.go" || string(model[1].ThoughtSignature) != "sig" {
		t.Errorf("first call: %+v sig=%q", fc1, model[1].ThoughtSignature)
	}
	if fc2.ID != "" {
		t.Errorf("generated ID leaked to Gemini: %q", fc2.ID)
	}

	// The tool turn: both responses in one content, in call order.
	resp := cs[2].Parts
	if len(resp) != 2 {
		t.Fatalf("tool parts: %+v", resp)
	}
	r1, r2 := resp[0].FunctionResponse, resp[1].FunctionResponse
	if r1.Name != "readfile" || r1.ID != "srv-1" || r1.Response["output"] != "package a" {
		t.Errorf("first response: %+v", r1)
	}
	if r2.ID != "" || r2.Response["error"] != "no such file" || r2.Response["output"] != nil {
		t.Errorf("error response should use the error key and no generated ID: %+v", r2)
	}
}

func TestToContentsPlainChatUnchanged(t *testing.T) {
	cs := toContents([]core.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "assistant"}, // empty reply with no calls: must be dropped, not sent as an empty content
	})
	if len(cs) != 2 || cs[0].Parts[0].Text != "hi" || cs[1].Role != genai.RoleModel {
		t.Fatalf("contents: %+v", cs)
	}
}

func TestParseResponse(t *testing.T) {
	resp := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{
		{Text: "private reasoning", Thought: true},
		{Text: "Let me look. "},
		{FunctionCall: &genai.FunctionCall{ID: "srv-9", Name: "readfile", Args: map[string]any{"path": "x.go"}}, ThoughtSignature: []byte("s1")},
		{FunctionCall: &genai.FunctionCall{Name: "writefile", Args: map[string]any{"path": "y", "content": "z"}}},
		{FunctionCall: &genai.FunctionCall{Name: "noargs"}},
	}}}}}

	text, calls := parseResponse(resp)
	if text != "Let me look. " {
		t.Errorf("text should skip thoughts: %q", text)
	}
	if len(calls) != 3 {
		t.Fatalf("calls: %+v", calls)
	}
	if calls[0].ToolID != "srv-9" || string(calls[0].ThoughtSignature) != "s1" {
		t.Errorf("provider ID/signature not preserved: %+v", calls[0])
	}
	if !strings.HasPrefix(calls[1].ToolID, generatedIDPrefix) || calls[1].ToolID == calls[2].ToolID {
		t.Errorf("missing IDs should be generated and unique: %q %q", calls[1].ToolID, calls[2].ToolID)
	}
	var args map[string]any
	if err := json.Unmarshal(calls[1].Args, &args); err != nil || args["content"] != "z" {
		t.Errorf("args: %s err=%v", calls[1].Args, err)
	}
	if string(calls[2].Args) != "{}" {
		t.Errorf("a call with no args should have an empty object, got %s", calls[2].Args)
	}

	if text, calls := parseResponse(&genai.GenerateContentResponse{}); text != "" || calls != nil {
		t.Error("empty response should parse to nothing")
	}
	if text, calls := parseResponse(nil); text != "" || calls != nil {
		t.Error("nil response should parse to nothing")
	}
}

func TestToConfig(t *testing.T) {
	if cfg, err := toConfig(nil); cfg != nil || err != nil {
		t.Fatalf("no tools should mean no config: %v %v", cfg, err)
	}

	cfg, err := toConfig([]core.ToolDefinition{{
		Name:        "readfile",
		Description: "reads",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	fd := cfg.Tools[0].FunctionDeclarations[0]
	schema, ok := fd.ParametersJsonSchema.(map[string]any)
	if fd.Name != "readfile" || fd.Description != "reads" || !ok || schema["type"] != "object" {
		t.Fatalf("declaration: %+v", fd)
	}

	if _, err := toConfig([]core.ToolDefinition{{Name: "bad", Parameters: json.RawMessage(`{nope`)}}); err == nil {
		t.Fatal("an invalid schema should be an error")
	}
}
