package tui

import (
	"context"
	"errors"
	"strings"
	"trimness/internal/agent"
	"trimness/internal/core"
	"trimness/internal/session"

	tea "github.com/charmbracelet/bubbletea"
)

// Update is the heart of Bubble Tea: it receives every message (key press,
// resize, agent event, ...) and returns the new model plus an optional command.
// It must be fast; slow work goes in commands (see waitForEvent).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Terminal resized (also sent once at startup): remember size, re-layout.
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.KeyMsg:
		// Which screen is showing decides who handles the key.
		if m.mode == modeSessions {
			return m.updateSessions(msg)
		}
		return m.updateChat(msg)

	case agentEventMsg:
		return m.handleEvent(msg)

	case streamClosedMsg:
		// The agent closed its channel: the run is completely over.
		return m.finishRun()

	case saveErrMsg:
		m.status = "save failed: " + msg.err.Error()
		return m, nil
	}

	// Anything else (e.g. spinner ticks). The spinner only keeps ticking while
	// a run is active, so it stops costing CPU once the reply is done.
	if m.running {
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	}
	if m.mode == modeSessions {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	return m, nil
}

// updateChat handles keys on the main chat screen.
func (m Model) updateChat(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// First press cancels a running reply; a second press (or pressing
		// when idle) quits.
		if m.running && !m.cancelling {
			m.cancelRun()
			return m, nil
		}
		m.cancelRun()
		return m, tea.Quit
	case "ctrl+d":
		m.cancelRun()
		return m, tea.Quit
	case "esc":
		if m.running {
			m.cancelRun()
		}
		return m, nil
	case "ctrl+n":
		// Switching while the agent is writing to history would race with it.
		if m.running {
			m.status = "a reply is in progress — press esc to cancel first"
			return m, nil
		}
		// New sessions are only written to disk after their first message.
		m.loadSession(session.New())
		m.ta.Reset()
		return m, nil
	case "ctrl+o":
		if m.running {
			m.status = "a reply is in progress — press esc to cancel first"
			return m, nil
		}
		sums, err := m.store.List()
		if err != nil {
			m.status = "cannot list sessions: " + err.Error()
			return m, nil
		}
		// Build the list fresh each time so it reflects what's on disk now.
		m.list = newSessionList(sums, m.w, m.h)
		m.mode = modeSessions
		return m, nil
	case "enter":
		return m.submit()
	case "pgup", "pgdown", "ctrl+u":
		// Scroll keys go to the transcript viewport, not the text input.
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	// Everything else is ordinary typing: hand it to the text input.
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

// updateSessions handles keys while the session picker is open.
func (m Model) updateSessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+o":
		m.mode = modeChat
		return m, nil
	case "ctrl+c", "ctrl+d":
		return m, tea.Quit
	case "enter":
		it, ok := m.list.SelectedItem().(sessionItem)
		if !ok { // empty list
			return m, nil
		}
		s, err := m.store.Load(it.s.ID)
		if err != nil {
			m.status = "cannot open session: " + err.Error()
			m.mode = modeChat
			return m, nil
		}
		// Replaces agent history and transcript with the chosen session's.
		m.loadSession(s)
		m.mode = modeChat
		return m, nil
	}
	// Up/down/filter keys are handled by the list component itself.
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// submit sends the typed prompt to the agent and starts listening for events.
func (m Model) submit() (tea.Model, tea.Cmd) {
	prompt := strings.TrimSpace(m.ta.Value())
	// Ignore empty input, and ignore Enter while a reply is already running.
	if prompt == "" || m.running {
		return m, nil
	}
	m.ta.Reset()
	m.status = ""

	// Snapshot history + the new prompt for persistence *before* the agent
	// goroutine starts mutating its own history (avoids a data race).
	snapshot := append(append([]core.Message(nil), m.agent.History...), core.Message{Role: "user", Content: prompt})

	// Per-run timeout; cancel is stored so esc/ctrl+c can stop the run.
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	m.cancel = cancel
	ch := m.agent.RunStream(ctx, prompt) // returns immediately; work happens in a goroutine

	// Show the user's message right away, before any reply arrives.
	m.entries = append(m.entries, entry{kindUser, prompt})
	m.partial.Reset()
	m.running, m.waitingFirst, m.cancelling = true, true, false
	m.refresh(true)

	save := m.persist(snapshot) // mutates m, so evaluate before building the return
	// Batch = run these commands concurrently:
	//   save the prompt to disk, start waiting for the first agent event,
	//   and start the spinner animation.
	return m, tea.Batch(save, waitForEvent(ch), m.sp.Tick)
}

// cancelRun asks the in-flight run to stop. The run itself ends a moment later,
// when the agent notices ctx is cancelled and closes its channel.
func (m *Model) cancelRun() {
	if m.running && m.cancel != nil {
		m.cancel()
		m.cancelling = true
	}
}

// handleEvent updates the UI for one event from the agent.
func (m Model) handleEvent(msg agentEventMsg) (tea.Model, tea.Cmd) {
	switch ev := msg.ev.(type) {
	case agent.TextDelta:
		// A streamed chunk: grow the in-progress reply and stop the spinner.
		m.waitingFirst = false
		m.partial.WriteString(ev.Text)
	case agent.MessageDone:
		// The reply is complete: replace the live buffer with a final entry.
		m.waitingFirst = false
		m.partial.Reset()
		if strings.TrimSpace(ev.Msg.Content) != "" {
			m.entries = append(m.entries, entry{kindAssistant, ev.Msg.Content})
		}
	case agent.ToolStarted:
		m.waitingFirst = false
		m.entries = append(m.entries, entry{kindToolCall, formatCall(ev.Call)})
	case agent.ToolFinished:
		m.entries = append(m.entries, entry{kindToolResult, ev.Result.Output})
	case agent.RunError:
		m.waitingFirst = false
		m.partial.Reset() // partial text is never kept
		if errors.Is(ev.Err, context.Canceled) {
			// A user-requested cancel isn't an error worth a red message.
			m.status = "cancelled"
		} else {
			m.entries = append(m.entries, entry{kindError, ev.Err.Error()})
		}
	case agent.RunDone:
		// Nothing to draw; the channel closing triggers finishRun.
	}
	m.refresh(false)
	// Re-arm the pump: ask for the NEXT event. This chain is what keeps
	// events flowing until the channel closes.
	return m, waitForEvent(msg.ch)
}

// finishRun runs once the agent has closed its channel, so reading its history
// is race-free. It persists the final state.
func (m Model) finishRun() (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel() // release the timeout's resources even on normal completion
		m.cancel = nil
	}
	m.running, m.waitingFirst, m.cancelling = false, false, false
	m.partial.Reset()
	m.refresh(false)
	save := m.persist(append([]core.Message(nil), m.agent.History...))
	return m, save
}
