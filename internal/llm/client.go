package llm

import (
	"context"
	"trimness/internal/core"
)

type Usage struct {
	TokenUsage int
}

type Request struct {
	Message  core.Message
	ToolCall core.ToolCall
}

type Response struct {
	Message core.Message
	Usage   Usage
}

type Client interface {
	Generate(
		ctx context.Context,
		req Request,
	)
}
