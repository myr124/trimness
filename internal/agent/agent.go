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

// maxTurns bounds the model<->tool loop once tools exist.
const maxTurns = 10

type Agent struct {
	Name    string         // Just a name for our agent
	History []core.Message // single source of truth for the conversation; replaced wholesale on session switch
	Client  llm.Client
	Tools   ToolRunner // optional
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
func (a *Agent) SetHistory(msgs []core.Message) {
	a.History = append([]core.Message(nil), msgs...)
}

// RunStream appends the prompt to history and runs the model, emitting events
// on the returned channel, which is closed when the run ends. Callers must not
// touch a.History until the channel is closed.
//
// On error or cancellation the user message stays in history (so context isn't
// lost) but no partial assistant message is ever added.
func (a *Agent) RunStream(ctx context.Context, prompt string) <-chan Event {
	out := make(chan Event, 16)
	a.AddMessage(core.Message{Role: "user", Content: prompt})

	send := func(ev Event) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	go func() {
		defer close(out)
		fail := func(err error) {
			// Errors must reach the consumer even if ctx is cancelled.
			select {
			case out <- RunError{Err: err}:
			default:
			}
		}

		for turn := 0; turn < maxTurns; turn++ {
			msg, err := a.generate(ctx, send)
			if err != nil {
				fail(err)
				return
			}
			a.AddMessage(msg)
			if !send(MessageDone{Msg: msg}) {
				fail(ctx.Err())
				return
			}

			// Tool seam: with no runner (or no calls) the turn ends here.
			if a.Tools == nil || len(msg.ToolCalls) == 0 {
				send(RunDone{})
				return
			}
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
		}
		send(RunDone{})
	}()

	return out
}

// generate performs one model call, streaming if the client supports it.
func (a *Agent) generate(ctx context.Context, send func(Event) bool) (core.Message, error) {
	req := llm.Request{Messages: a.History}

	if s, ok := a.Client.(llm.Streamer); ok {
		chunks, err := s.Stream(ctx, req)
		if err != nil {
			return core.Message{}, err
		}
		var sb strings.Builder
		for {
			select {
			case <-ctx.Done():
				return core.Message{}, ctx.Err()
			case c, ok := <-chunks:
				if !ok {
					return core.Message{Role: "assistant", Content: sb.String()}, nil
				}
				if c.Err != nil {
					return core.Message{}, c.Err
				}
				sb.WriteString(c.Text)
				if !send(TextDelta{Text: c.Text}) {
					return core.Message{}, ctx.Err()
				}
			}
		}
	}

	resp, err := a.Client.Generate(ctx, req)
	if err != nil {
		return core.Message{}, err
	}
	return resp.Message, nil
}
