package core

// A message type that can be used to take prompts and structure them
type Message struct {
	Role       string     `json:"role"`                   // Who the message belongs to: Assistant, User, System
	Content    string     `json:"content"`                // Actual text content of prompt
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // Slice of all tool calls, mostly going to be used the agent where we pass it a registry of tools and it picks which it needs
	ToolCallID string     `json:"tool_call_id,omitempty"` // Unique identifier for tool call
}
