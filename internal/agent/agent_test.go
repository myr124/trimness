package agent

import (
	"context"
	"errors"
	"testing"
	"trimness/internal/core"
	"trimness/internal/llm"
)

type fakeClient struct {
	replies []core.Message
	err     error
	seen    [][]core.Message // history snapshot per request
}

func (f *fakeClient) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	f.seen = append(f.seen, append([]core.Message(nil), req.Messages...))
	if f.err != nil {
		return llm.Response{}, f.err
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return llm.Response{Message: r}, nil
}

type fakeStreamer struct {
	chunks []llm.Chunk
}

func (f *fakeStreamer) Generate(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("should stream")
}

func (f *fakeStreamer) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, len(f.chunks))
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

type fakeTools struct{}

func (fakeTools) Execute(_ context.Context, c core.ToolCall) core.ToolResult {
	return core.ToolResult{Output: "ok", ToolID: c.ToolID}
}

func drain(ch <-chan Event) []Event {
	var evs []Event
	for ev := range ch {
		evs = append(evs, ev)
	}
	return evs
}

func TestRunStreamNonStreamingFallback(t *testing.T) {
	c := &fakeClient{replies: []core.Message{{Role: "assistant", Content: "hi"}}}
	a := &Agent{Client: c}
	evs := drain(a.RunStream(context.Background(), "hello"))
	if len(evs) != 2 {
		t.Fatalf("events: %#v", evs)
	}
	if m, ok := evs[0].(MessageDone); !ok || m.Msg.Content != "hi" {
		t.Fatalf("first event: %#v", evs[0])
	}
	if _, ok := evs[1].(RunDone); !ok {
		t.Fatalf("last event: %#v", evs[1])
	}
	if len(a.History) != 2 {
		t.Fatalf("history: %#v", a.History)
	}
}

func TestMultiTurnSendsFullHistory(t *testing.T) {
	c := &fakeClient{replies: []core.Message{
		{Role: "assistant", Content: "nice to meet you"},
		{Role: "assistant", Content: "Eric"},
	}}
	a := &Agent{Client: c}
	drain(a.RunStream(context.Background(), "my name is Eric"))
	drain(a.RunStream(context.Background(), "what is my name?"))
	if len(c.seen) != 2 || len(c.seen[1]) != 3 {
		t.Fatalf("second request should carry 3 messages, saw %#v", c.seen)
	}
	if c.seen[1][0].Content != "my name is Eric" {
		t.Fatalf("earlier context missing: %#v", c.seen[1])
	}
}

func TestRunStreamError(t *testing.T) {
	a := &Agent{Client: &fakeClient{err: errors.New("boom")}}
	evs := drain(a.RunStream(context.Background(), "hello"))
	if len(evs) != 1 {
		t.Fatalf("events: %#v", evs)
	}
	if e, ok := evs[0].(RunError); !ok || e.Err.Error() != "boom" {
		t.Fatalf("event: %#v", evs[0])
	}
	if len(a.History) != 1 || a.History[0].Role != "user" {
		t.Fatalf("history should keep only the user message: %#v", a.History)
	}
}

func TestStreamerForwardsDeltas(t *testing.T) {
	a := &Agent{Client: &fakeStreamer{chunks: []llm.Chunk{{Text: "he"}, {Text: "llo"}}}}
	evs := drain(a.RunStream(context.Background(), "x"))
	if len(evs) != 4 {
		t.Fatalf("events: %#v", evs)
	}
	if evs[0].(TextDelta).Text != "he" || evs[1].(TextDelta).Text != "llo" {
		t.Fatalf("deltas: %#v", evs)
	}
	if evs[2].(MessageDone).Msg.Content != "hello" {
		t.Fatalf("done: %#v", evs[2])
	}
}

func TestStreamMidErrorDropsPartial(t *testing.T) {
	a := &Agent{Client: &fakeStreamer{chunks: []llm.Chunk{{Text: "par"}, {Err: errors.New("cut")}}}}
	evs := drain(a.RunStream(context.Background(), "x"))
	if _, ok := evs[len(evs)-1].(RunError); !ok {
		t.Fatalf("expected RunError last: %#v", evs)
	}
	if len(a.History) != 1 {
		t.Fatalf("partial text leaked into history: %#v", a.History)
	}
}

func TestToolLoop(t *testing.T) {
	call := core.ToolCall{ToolName: "readfile", ToolID: "t1"}
	c := &fakeClient{replies: []core.Message{
		{Role: "assistant", Content: "reading", ToolCalls: []core.ToolCall{call}},
		{Role: "assistant", Content: "done"},
	}}
	a := &Agent{Client: c, Tools: fakeTools{}}
	evs := drain(a.RunStream(context.Background(), "go"))
	var started, finished int
	for _, ev := range evs {
		switch ev.(type) {
		case ToolStarted:
			started++
		case ToolFinished:
			finished++
		}
	}
	if started != 1 || finished != 1 {
		t.Fatalf("events: %#v", evs)
	}
	if len(a.History) != 4 || a.History[2].Role != "tool" {
		t.Fatalf("history: %#v", a.History)
	}
}

func TestSetHistoryCopies(t *testing.T) {
	src := []core.Message{{Role: "user", Content: "a"}}
	a := &Agent{}
	a.SetHistory(src)
	a.AddMessage(core.Message{Role: "assistant", Content: "b"})
	if len(src) != 1 {
		t.Fatal("SetHistory aliased caller's slice")
	}
}
