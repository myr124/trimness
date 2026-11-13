package agent

import (
	"context"
	"strings"
	"trimness/internal/core"
	"trimness/internal/llm"
)

type AgentFunctions interface {
	Run(prompt string)
	AddMessage(msg core.Message)
}

// ToolRunner executes tool calls. It is nil until the tool milestones land;
// the loop in RunStream is already shaped to use it.
type ToolRunner interface {
	Execute(ctx context.Context, call core.ToolCall) core.ToolResult
}

// maxTurns bounds the model<->tool loop once tools exist, so a model that
// keeps asking for tools can't loop forever.
const maxTurns = 10

type Agent struct {
	Name    string         // Just a name for our agent
	History []core.Message // single source of truth for the conversation; replaced wholesale on session switch
	Client  llm.Client
	Tools   ToolRunner // optional: nil means "no tools available"
}

func (a *Agent) Run(ctx context.Context, prompt string) (string, error) {

	a.AddMessage(core.Message{Role: "user", Content: prompt})
	req := llm.Request{Messages: a.History}
	response, err := a.Client.Generate(ctx, req)

	if err != nil {
		return "", err
	}

	a.AddMessage(response.Message)

	return response.Message.Content, nil

}

func (a *Agent) AddMessage(msg core.Message) {
	a.History = append(a.History, msg)
}

// SetHistory replaces the conversation, e.g. when loading or switching sessions.
// It copies the slice so later appends to a.History can't change the caller's data.
func (a *Agent) SetHistory(msgs []core.Message) {
	a.History = append([]core.Message(nil), msgs...)
}

// RunStream appends the prompt to history and runs the model, emitting events
// on the returned channel, which is closed when the run ends. Callers must not
// touch a.History until the channel is closed (the goroutine below writes to it).
//
// On error or cancellation the user message stays in history (so context isn't
// lost) but no partial assistant message is ever added.
func (a *Agent) RunStream(ctx context.Context, prompt string) <-chan Event {
	// Buffered so the goroutine can run slightly ahead of the UI without blocking.
	out := make(chan Event, 16)

	// The prompt is added synchronously, BEFORE the goroutine starts, so the
	// caller can safely snapshot history right after this function returns.
	a.AddMessage(core.Message{Role: "user", Content: prompt})

	// send delivers an event, but gives up if the run was cancelled, so we never
	// block forever on a consumer that has stopped listening.
	send := func(ev Event) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	go func() {
		// Closing the channel is the "run is over" signal the TUI waits for.
		defer close(out)

		// fail reports an error. It uses a non-blocking send (default case)
		// because ctx may already be cancelled and nobody may be reading.
		fail := func(err error) {
			select {
			case out <- RunError{Err: err}:
			default:
			}
		}

		// One iteration = one model call. Without tools we leave after the first.
		for turn := 0; turn < maxTurns; turn++ {
			msg, err := a.generate(ctx, send)
			if err != nil {
				// Nothing was added to history, so no half-finished reply is kept.
				fail(err)
				return
			}
			a.AddMessage(msg)
			if !send(MessageDone{Msg: msg}) {
				fail(ctx.Err())
				return
			}

			// Tool seam: with no runner (or no tool calls requested) the turn ends here.
			if a.Tools == nil || len(msg.ToolCalls) == 0 {
				send(RunDone{})
				return
			}

			// The model asked for tools: run each, report it, and record the
			// result as a "tool" message so the NEXT model call can see it.
			for _, call := range msg.ToolCalls {
				if !send(ToolStarted{Call: call}) {
					fail(ctx.Err())
					return
				}
				res := a.Tools.Execute(ctx, call)
				if !send(ToolFinished{Result: res}) {
					fail(ctx.Err())
					return
				}
				a.AddMessage(core.Message{Role: "tool", Content: res.Output, ToolCallID: res.ToolID})
			}
			// loop again: the model now gets the tool results and continues
		}
		send(RunDone{})
	}()

	return out
}

// generate performs one model call, streaming if the client supports it.
// It returns the complete assistant message; deltas are emitted along the way.
func (a *Agent) generate(ctx context.Context, send func(Event) bool) (core.Message, error) {
	req := llm.Request{Messages: a.History}

	// Type assertion: "does this client ALSO have a Stream method?"
	if s, ok := a.Client.(llm.Streamer); ok {
		chunks, err := s.Stream(ctx, req)
		if err != nil {
			return core.Message{}, err
		}
		// Accumulate the chunks so we can build the full message at the end.
		var sb strings.Builder
		for {
			select {
			case <-ctx.Done(): // cancelled or timed out: discard whatever we have
				return core.Message{}, ctx.Err()
			case c, ok := <-chunks:
				if !ok { // channel closed = stream finished normally
					return core.Message{Role: "assistant", Content: sb.String()}, nil
				}
				if c.Err != nil { // stream broke mid-way: drop the partial text
					return core.Message{}, c.Err
				}
				sb.WriteString(c.Text)
				// Forward the chunk to the UI so it appears immediately.
				if !send(TextDelta{Text: c.Text}) {
					return core.Message{}, ctx.Err()
				}
			}
		}
	}

	// Fallback: no streaming support, so wait for the whole reply at once.
	resp, err := a.Client.Generate(ctx, req)
	if err != nil {
		return core.Message{}, err
	}
	return resp.Message, nil
}
