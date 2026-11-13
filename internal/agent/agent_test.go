package agent

import (
	"context"
	"errors"
	"strings"
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

func (fakeTools) Definitions() []core.ToolDefinition {
	return []core.ToolDefinition{{Name: "readfile"}}
}

// gatedTools is fakeTools plus an approval requirement for "writefile".
type gatedTools struct {
	fakeTools
	ran []string // names of tools that actually executed
}

func (g *gatedTools) Execute(ctx context.Context, c core.ToolCall) core.ToolResult {
	g.ran = append(g.ran, c.ToolName)
	return g.fakeTools.Execute(ctx, c)
}

func (g *gatedTools) NeedsApproval(c core.ToolCall) (string, bool) {
	return "write " + c.ToolName, c.ToolName == "writefile"
}

// answerApprovals consumes events like a UI would, answering every approval
// request with allow, and returns everything it saw.
func answerApprovals(ch <-chan Event, allow bool) []Event {
	var evs []Event
	for ev := range ch {
		if req, ok := ev.(ApprovalRequested); ok {
			req.Reply <- allow
		}
		evs = append(evs, ev)
	}
	return evs
}

func toolReply(calls ...core.ToolCall) core.Message {
	return core.Message{Role: "assistant", ToolCalls: calls}
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
	if h := a.History[2]; h.ToolCallID != "t1" || h.ToolName != "readfile" || h.IsError {
		t.Fatalf("tool message should carry the call's ID and name: %#v", h)
	}
}

// recordingClient remembers the tool definitions of each request.
type recordingClient struct {
	fakeClient
	tools [][]core.ToolDefinition
}

func (r *recordingClient) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	r.tools = append(r.tools, req.Tools)
	return r.fakeClient.Generate(ctx, req)
}

func TestToolDefinitionsAreSentToModel(t *testing.T) {
	c := &recordingClient{fakeClient: fakeClient{replies: []core.Message{{Role: "assistant", Content: "hi"}}}}
	a := &Agent{Client: c, Tools: fakeTools{}}
	drain(a.RunStream(context.Background(), "x"))
	if len(c.tools) != 1 || len(c.tools[0]) != 1 || c.tools[0][0].Name != "readfile" {
		t.Fatalf("definitions not sent: %#v", c.tools)
	}

	c2 := &recordingClient{fakeClient: fakeClient{replies: []core.Message{{Role: "assistant", Content: "hi"}}}}
	drain((&Agent{Client: c2}).RunStream(context.Background(), "x"))
	if len(c2.tools[0]) != 0 {
		t.Fatalf("no tools registered, none should be sent: %#v", c2.tools)
	}
}

func TestStreamedToolCallsRunTheLoop(t *testing.T) {
	call := core.ToolCall{ToolName: "readfile", ToolID: "t1"}
	// A streaming client: first turn is text + a tool call, second turn is text.
	turns := [][]llm.Chunk{
		{{Text: "reading"}, {ToolCalls: []core.ToolCall{call}}},
		{{Text: "done"}},
	}
	a := &Agent{Client: &turnStreamer{turns: turns}, Tools: fakeTools{}}
	evs := drain(a.RunStream(context.Background(), "go"))

	var started int
	for _, ev := range evs {
		if _, ok := ev.(ToolStarted); ok {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("tool should have run once: %#v", evs)
	}
	if len(a.History) != 4 || len(a.History[1].ToolCalls) != 1 || a.History[1].Content != "reading" || a.History[3].Content != "done" {
		t.Fatalf("history: %#v", a.History)
	}
}

type turnStreamer struct {
	turns [][]llm.Chunk
}

func (*turnStreamer) Generate(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("should stream")
}

func (s *turnStreamer) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	chunks := s.turns[0]
	s.turns = s.turns[1:]
	ch := make(chan llm.Chunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func TestApprovalAllowRunsTool(t *testing.T) {
	c := &fakeClient{replies: []core.Message{
		toolReply(core.ToolCall{ToolName: "writefile", ToolID: "w1"}),
		{Role: "assistant", Content: "written"},
	}}
	tools := &gatedTools{}
	a := &Agent{Client: c, Tools: tools}
	evs := answerApprovals(a.RunStream(context.Background(), "write it"), true)

	if len(tools.ran) != 1 {
		t.Fatalf("approved tool should run: %v", tools.ran)
	}
	var sawApproval bool
	for _, ev := range evs {
		if r, ok := ev.(ApprovalRequested); ok {
			sawApproval = true
			if r.Summary != "write writefile" || r.Call.ToolID != "w1" {
				t.Errorf("approval event: %#v", r)
			}
		}
	}
	if !sawApproval {
		t.Fatal("no approval was requested")
	}
	if tool := a.History[2]; tool.IsError || tool.Content != "ok" {
		t.Fatalf("tool message: %#v", tool)
	}
}

func TestApprovalDenyDoesNotRunToolAndTellsModel(t *testing.T) {
	c := &fakeClient{replies: []core.Message{
		toolReply(core.ToolCall{ToolName: "writefile", ToolID: "w1"}),
		{Role: "assistant", Content: "ok, I won't"},
	}}
	tools := &gatedTools{}
	a := &Agent{Client: c, Tools: tools}
	evs := answerApprovals(a.RunStream(context.Background(), "write it"), false)

	if len(tools.ran) != 0 {
		t.Fatalf("denied tool must not run: %v", tools.ran)
	}
	tool := a.History[2]
	if tool.Role != "tool" || !tool.IsError || tool.ToolCallID != "w1" || tool.Content == "" {
		t.Fatalf("model should be told the call was declined: %#v", tool)
	}
	// The run continues: the model gets to respond to the refusal.
	if _, ok := evs[len(evs)-1].(RunDone); !ok {
		t.Fatalf("run should finish normally after a refusal: %#v", evs[len(evs)-1])
	}
	if len(c.seen) != 2 || c.seen[1][2].Role != "tool" {
		t.Fatalf("second request should include the refusal: %#v", c.seen)
	}
}

func TestReadOnlyToolSkipsApproval(t *testing.T) {
	c := &fakeClient{replies: []core.Message{
		toolReply(core.ToolCall{ToolName: "readfile", ToolID: "r1"}),
		{Role: "assistant", Content: "done"},
	}}
	tools := &gatedTools{}
	a := &Agent{Client: c, Tools: tools}
	for _, ev := range drain(a.RunStream(context.Background(), "x")) { // nobody answers approvals here
		if _, ok := ev.(ApprovalRequested); ok {
			t.Fatal("readfile should not ask for approval")
		}
	}
	if len(tools.ran) != 1 {
		t.Fatalf("readfile should have run: %v", tools.ran)
	}
}

func TestCancelWhileAwaitingApprovalKeepsHistoryValid(t *testing.T) {
	c := &fakeClient{replies: []core.Message{
		toolReply(
			core.ToolCall{ToolName: "writefile", ToolID: "w1"},
			core.ToolCall{ToolName: "readfile", ToolID: "r1"},
		),
	}}
	tools := &gatedTools{}
	a := &Agent{Client: c, Tools: tools}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := a.RunStream(ctx, "go")
	var last Event
	for ev := range ch {
		if _, ok := ev.(ApprovalRequested); ok {
			cancel() // the user gives up instead of answering
		}
		last = ev
	}

	if e, ok := last.(RunError); !ok || !errors.Is(e.Err, context.Canceled) {
		t.Fatalf("expected a cancelled RunError last, got %#v", last)
	}
	if len(tools.ran) != 0 {
		t.Fatalf("nothing should have run: %v", tools.ran)
	}
	// user, assistant(2 calls), tool(w1), tool(r1): every call is answered, so
	// the next request to the provider is still well-formed.
	if len(a.History) != 4 || a.History[2].ToolCallID != "w1" || a.History[3].ToolCallID != "r1" {
		t.Fatalf("every call needs a result: %#v", a.History)
	}
	if !a.History[2].IsError || !a.History[3].IsError {
		t.Fatalf("unanswered calls should be marked as errors: %#v", a.History)
	}
}

func TestTurnLimitEndsWithError(t *testing.T) {
	// A model that asks for a tool forever.
	var replies []core.Message
	for i := 0; i < maxTurns+1; i++ {
		replies = append(replies, toolReply(core.ToolCall{ToolName: "readfile", ToolID: "t"}))
	}
	a := &Agent{Client: &fakeClient{replies: replies}, Tools: fakeTools{}}
	evs := drain(a.RunStream(context.Background(), "loop"))

	last, ok := evs[len(evs)-1].(RunError)
	if !ok || !strings.Contains(last.Err.Error(), "without a final answer") {
		t.Fatalf("expected a turn-limit error last, got %#v", evs[len(evs)-1])
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
