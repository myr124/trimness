package core

type ToolCall struct {
	ToolName string   `json:"tool_name"` // Name of the tool (readfile, writefile)
	ToolID   string   `json:"tool_id"`   // unique identifier for multiple tool calls for the same tool, we also have one in message so need to check if this is redudant
	Args     []string `json:"args"`      // arguments that need to be passed into tool calls (files, text, images)
}

type ToolResult struct {
	Output  string `json:"output"`   // not necessarily the output but rather the result of tool call (success or failure, what changed)
	IsError bool   `json:"is_error"` // boolean instead of error type since we are going to marshall this and boolean would be easier for that
	ToolID  string `json:"tool_id"`  // matches the one from ToolCall
}
