package llm

import (
	"context"
	"trimness/internal/core"
)

/* Type to help us structure usage info 	*/
type Usage struct {
	InputTokens  int
	OutputTokens int
}

type Request struct {
	Messages        []core.Message  // for now we're gonna pass whole agent history of messages to each request to maintain context
	ToolDefiniitons []core.ToolCall // list of tools that we are exposing to our assistant for this particular request
}

type Response struct {
	Message core.Message
	Usage   Usage // need to define this more clearly but its something we can use for observability in the future
}

type Client interface {
	Generate(
		ctx context.Context,
		req Request,
	) (Response, error)
}

type TrimnessClient struct {
}
