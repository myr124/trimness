package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"trimness/internal/agent"
	"trimness/internal/core"
	"trimness/internal/llm"
	"trimness/internal/session"
	"trimness/internal/tools"

	tea "github.com/charmbracelet/bubbletea"
)

type scriptedClient struct {
	replies []string
	err     error
	seen    [][]core.Message
}

func (c *scriptedClient) Generate(_ context.Context, req llm.Request) (llm.Response, error) {
	c.seen = append(c.seen, append([]core.Message(nil), req.Messages...))
	if c.err != nil {
		return llm.Response{}, c.err
	}
	r := c.replies[0]
	c.replies = c.replies[1:]
	return llm.Response{Message: core.Message{Role: "assistant", Content: r}}, nil
}

type streamClient struct{ chunks []string }

func (c *streamClient) Generate(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unused")
}
func (c *streamClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, len(c.chunks))
	for _, s := range c.chunks {
		ch <- llm.Chunk{Text: s}
	}
	close(ch)
	return ch, nil
}

func newModel(t *testing.T, c llm.Client) (Model, session.FileStore) {
	t.Helper()
	store := session.FileStore{Dir: t.TempDir()}
	m := New(&agent.Agent{Client: c}, store, session.New())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return updated.(Model), store
}

// pump runs a cmd and feeds its resulting messages back into Update until the
// run completes. Batched/spinner commands are skipped; only agent/save
// messages matter here.
func pump(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	var queue []tea.Cmd
	queue = append(queue, cmd)
	for steps := 0; len(queue) > 0 && steps < 200; steps++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		switch v := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, v...)
			continue
		case agentEventMsg, streamClosedMsg, saveErrMsg:
		default:
			continue // spinner ticks, nil, etc.
		}
		var next tea.Cmd
		nm, next := m.Update(msg)
		m = nm.(Model)
		queue = append(queue, next)
	}
	return m
}

func send(t *testing.T, m Model, text string) Model {
	t.Helper()
	m.ta.SetValue(text)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return pump(t, nm.(Model), cmd)
}

func TestMultiTurnAndPersistence(t *testing.T) {
	c := &scriptedClient{replies: []string{"hello Eric", "your name is Eric"}}
	m, store := newModel(t, c)

	m = send(t, m, "my name is Eric")
	m = send(t, m, "what is my name?")

	if len(c.seen[1]) != 3 {
		t.Fatalf("second request must include earlier turns, got %d msgs", len(c.seen[1]))
	}
	out := renderTranscript(m.entries, "", 80)
	for _, want := range []string{"you ›", "trimness ›", "your name is Eric"} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript missing %q:\n%s", want, out)
		}
	}
	if m.running {
		t.Fatal("run should have finished")
	}

	// "Restart": a fresh model resumes the saved session.
	sums, _ := store.List()
	if len(sums) != 1 {
		t.Fatalf("want 1 saved session, got %d", len(sums))
	}
	saved, err := store.Load(sums[0].ID)
	if err != nil || len(saved.Messages) != 4 {
		t.Fatalf("saved: %+v err=%v", saved, err)
	}
	m2 := New(&agent.Agent{Client: &scriptedClient{}}, store, saved)
	if len(m2.entries) != 4 {
		t.Fatalf("restored entries: %d", len(m2.entries))
	}
}

func TestStreamingAppearsIncrementally(t *testing.T) {
	m, _ := newModel(t, &streamClient{chunks: []string{"Hel", "lo"}})
	m.ta.SetValue("hi")
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)

	// Drive events one at a time and watch the partial text grow.
	var batch tea.BatchMsg = cmd().(tea.BatchMsg)
	var wait tea.Cmd
	for _, c := range batch {
		if msg, ok := c().(agentEventMsg); ok {
			nm, wait = m.Update(msg)
			m = nm.(Model)
			break
		}
	}
	if m.partial.String() != "Hel" {
		t.Fatalf("after first delta partial=%q", m.partial.String())
	}
	if m.waitingFirst {
		t.Fatal("spinner should stop at first delta")
	}
	nm, _ = m.Update(wait().(agentEventMsg))
	m = nm.(Model)
	if m.partial.String() != "Hello" {
		t.Fatalf("after second delta partial=%q", m.partial.String())
	}
}

func TestErrorDoesNotCrashOrCorrupt(t *testing.T) {
	m, store := newModel(t, &scriptedClient{err: errors.New("api down")})
	m = send(t, m, "hello")

	if m.running {
		t.Fatal("still running after error")
	}
	if !strings.Contains(renderTranscript(m.entries, "", 80), "api down") {
		t.Fatal("error not displayed")
	}
	sums, _ := store.List()
	saved, err := store.Load(sums[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Messages) != 1 || saved.Messages[0].Role != "user" {
		t.Fatalf("history should hold only the user message: %+v", saved.Messages)
	}

	// The chat is still usable afterwards.
	m.agent.Client = &scriptedClient{replies: []string{"back"}}
	m = send(t, m, "again")
	if !strings.Contains(renderTranscript(m.entries, "", 80), "back") {
		t.Fatal("chat did not recover after error")
	}
}

func TestNewAndSwitchSession(t *testing.T) {
	c := &scriptedClient{replies: []string{"one", "two"}}
	m, store := newModel(t, c)
	m = send(t, m, "first")
	firstID := m.sess.ID

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = nm.(Model)
	if m.sess.ID == firstID || len(m.entries) != 0 || len(m.agent.History) != 0 {
		t.Fatal("ctrl+n should start a clean session")
	}
	m = send(t, m, "second")

	if sums, _ := store.List(); len(sums) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(sums))
	}

	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = nm.(Model)
	if m.mode != modeSessions {
		t.Fatal("picker did not open")
	}
	// Select the other (older) session: move down then enter.
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if m.mode != modeChat || m.sess.ID != firstID {
		t.Fatalf("expected to switch back to first session, got %s", m.sess.ID)
	}
	if len(m.agent.History) != 2 {
		t.Fatalf("agent history not swapped: %d", len(m.agent.History))
	}
}

func TestMouseWheelScrollsTranscript(t *testing.T) {
	m, _ := newModel(t, &scriptedClient{})
	for i := 0; i < 60; i++ {
		m.entries = append(m.entries, entry{kindAssistant, fmt.Sprintf("line %d", i)})
	}
	m.refresh(true)
	if !m.vp.AtBottom() {
		t.Fatal("should start pinned to the bottom")
	}

	nm, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, Type: tea.MouseWheelUp})
	m = nm.(Model)
	if m.vp.AtBottom() {
		t.Fatal("wheel up should scroll away from the bottom")
	}

	// New content arriving must not yank the view back down while scrolled up.
	offset := m.vp.YOffset
	m.entries = append(m.entries, entry{kindAssistant, "new"})
	m.refresh(false)
	if m.vp.YOffset != offset {
		t.Fatalf("scroll position moved from %d to %d", offset, m.vp.YOffset)
	}

	nm, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, Type: tea.MouseWheelDown})
	m = nm.(Model)
	if m.vp.YOffset <= offset {
		t.Fatal("wheel down should scroll toward the bottom")
	}
}

// toolClient replays scripted assistant messages (including tool calls).
type toolClient struct {
	msgs []core.Message
	seen [][]core.Message
}

func (c *toolClient) Generate(_ context.Context, req llm.Request) (llm.Response, error) {
	c.seen = append(c.seen, append([]core.Message(nil), req.Messages...))
	r := c.msgs[0]
	c.msgs = c.msgs[1:]
	return llm.Response{Message: r}, nil
}

// pumpUntilApproval is pump, except it stops as soon as the model is waiting
// on a y/n (the agent is blocked then, so running the next waitForEvent would
// hang). It returns the command that resumes event delivery after the answer.
func pumpUntilApproval(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 200; steps++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		switch v := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, v...)
			continue
		case agentEventMsg, streamClosedMsg, saveErrMsg:
		default:
			continue
		}
		nm, next := m.Update(msg)
		m = nm.(Model)
		if m.pending != nil {
			return m, next
		}
		queue = append(queue, next)
	}
	t.Fatal("never reached an approval prompt")
	return m, nil
}

func key(r string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)} }

// startWrite sends a prompt to a model that wants to write hello.txt, and
// returns the model paused at the approval prompt.
func startWrite(t *testing.T) (Model, tea.Cmd, string, *toolClient) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })

	c := &toolClient{msgs: []core.Message{
		{Role: "assistant", ToolCalls: []core.ToolCall{{
			ToolName: "writefile",
			ToolID:   "w1",
			Args:     json.RawMessage(`{"path":"hello.txt","content":"hi there"}`),
		}}},
		{Role: "assistant", Content: "all done"},
	}}
	m, _ := newModel(t, c)
	m.agent.Tools = tools.NewRegistry(tools.ReadFile{Root: root}, tools.WriteFile{Root: root})

	m.ta.SetValue("make hello.txt")
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, held := pumpUntilApproval(t, nm.(Model), cmd)
	return m, held, dir, c
}

func TestWriteFileWaitsForApproval(t *testing.T) {
	m, _, dir, _ := startWrite(t)

	if m.pending == nil || !m.running {
		t.Fatal("run should be paused on an approval prompt")
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatal("file was written before the user approved")
	}
	if out := renderTranscript(m.entries, "", 80); !strings.Contains(out, "create hello.txt") || !strings.Contains(out, "hi there") {
		t.Fatalf("prompt should show what would be written:\n%s", out)
	}
	if !strings.Contains(m.View(), "allow this action?") {
		t.Fatal("status line should ask for a decision")
	}

	// Typing and Enter must not answer the prompt or reach the text box.
	for _, k := range []tea.KeyMsg{key("x"), key("hello"), {Type: tea.KeyEnter}} {
		nm, _ := m.Update(k)
		m = nm.(Model)
	}
	if m.pending == nil || m.ta.Value() != "" {
		t.Fatalf("stray keys leaked: pending=%v input=%q", m.pending != nil, m.ta.Value())
	}
}

func TestApproveWritesFile(t *testing.T) {
	m, held, dir, c := startWrite(t)

	nm, _ := m.Update(key("y"))
	m = pump(t, nm.(Model), held)

	if m.pending != nil || m.running {
		t.Fatal("run should have finished")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hello.txt")); string(b) != "hi there" {
		t.Fatalf("file content: %q", b)
	}
	out := renderTranscript(m.entries, "", 80)
	for _, want := range []string{"created hello.txt", "all done"} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript missing %q:\n%s", want, out)
		}
	}
	// The model saw the tool result on its follow-up request.
	last := c.seen[1][len(c.seen[1])-1]
	if last.Role != "tool" || last.ToolName != "writefile" || last.IsError {
		t.Fatalf("follow-up request: %#v", last)
	}
}

func TestDenyDoesNotWriteAndModelIsTold(t *testing.T) {
	m, held, dir, c := startWrite(t)

	nm, _ := m.Update(key("n"))
	m = pump(t, nm.(Model), held)

	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatal("file was written despite 'n'")
	}
	if m.running || m.pending != nil {
		t.Fatal("run should have finished")
	}
	if out := renderTranscript(m.entries, "", 80); !strings.Contains(out, "declined") {
		t.Fatalf("transcript should show the refusal:\n%s", out)
	}
	if last := c.seen[1][len(c.seen[1])-1]; last.Role != "tool" || !last.IsError {
		t.Fatalf("model should be told the call was refused: %#v", last)
	}
}

func TestCancelWhileAwaitingApprovalLeavesValidHistory(t *testing.T) {
	m, held, dir, _ := startWrite(t)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = pump(t, nm.(Model), held)

	if m.running || m.pending != nil {
		t.Fatal("ctrl+c should end the run")
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatal("file was written after cancelling")
	}
	if m.status != "cancelled" {
		t.Fatalf("status: %q", m.status)
	}
	// Every tool call must have an answer, or the next request would be malformed.
	h := m.agent.History
	if len(h) != 3 || h[1].Role != "assistant" || h[2].Role != "tool" || h[2].ToolCallID != "w1" {
		t.Fatalf("history: %#v", h)
	}
}
