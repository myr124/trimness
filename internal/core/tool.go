package core

import "encoding/json"

// ToolCall is one invocation the model asked for.
type ToolCall struct {
	ToolName string `json:"tool_name"` // Name of the tool (readfile, writefile)
	ToolID   string `json:"tool_id"`   // identifies this call; the matching ToolResult and "tool" message carry the same ID
	// Args is the JSON object the model produced. Each tool decodes it into its
	// own typed struct, so the framework never needs to look inside.
	Args json.RawMessage `json:"args,omitempty"`
	// ThoughtSignature is opaque provider data attached to a call (Gemini
	// requires it to be echoed back verbatim on later requests).
	ThoughtSignature []byte `json:"thought_signature,omitempty"`
}

type ToolResult struct {
	Output  string `json:"output"`   // not necessarily the output but rather the result of tool call (success or failure, what changed)
	IsError bool   `json:"is_error"` // boolean instead of error type since we are going to marshall this and boolean would be easier for that
	ToolID  string `json:"tool_id"`  // matches the one from ToolCall
}

// ToolDefinition is what we advertise to the model: what the tool is called,
// what it is for, and the shape of its arguments. It is provider-neutral; each
// llm client translates it into its own SDK type.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema (type: object) describing the arguments
}
