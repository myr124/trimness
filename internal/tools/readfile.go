package tools

import (
	"trimness/internal/core"
	"uuid"
)

// we could have a general interface for tools but for now i am gonna make a basic readfile function that returns a ToolResult

func readfile() core.ToolResult {

	// tool id and result generation
	idv4 := uuid.NewV4()
	t := core.ToolResult{Output: "successful read operation", IsError: false, ToolID: "readfile" + idv4.String()}

	// actual tool functionality (tbd)

	// success condition nothing went wrong
	return t

}
