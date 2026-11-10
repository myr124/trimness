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

// Chunk is one incremental piece of a streamed response.
type Chunk struct {
	Text string // text delta
	Err  error  // terminal error; the stream ends after a chunk with Err set
}

// Streamer is an optional capability a Client may implement. When it does, the
// agent forwards chunks as incremental events; otherwise it falls back to
// Generate. The implementation must close the channel when the stream ends and
// stop promptly when ctx is cancelled.
type Streamer interface {
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}
