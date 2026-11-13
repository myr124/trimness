package core

// A message type that can be used to take prompts and structure them
type Message struct {
	Role       string     `json:"role"`                   // Who the message belongs to: Assistant, User, System
	Content    string     `json:"content"`                // Actual text content of prompt
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // Slice of all tool calls, mostly going to be used the agent where we pass it a registry of tools and it picks which it needs
	ToolCallID string     `json:"tool_call_id,omitempty"` // Unique identifier for tool call (set on "tool" messages)
	ToolName   string     `json:"tool_name,omitempty"`    // Name of the tool a "tool" message answers; some providers need it to pair results with calls
	IsError    bool       `json:"is_error,omitempty"`     // true on a "tool" message when the tool failed or was refused
}
