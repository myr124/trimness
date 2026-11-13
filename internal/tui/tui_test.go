package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"trimness/internal/agent"
	"trimness/internal/core"
	"trimness/internal/llm"
	"trimness/internal/session"

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
