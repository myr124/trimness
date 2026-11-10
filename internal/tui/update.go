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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.KeyMsg:
		if m.mode == modeSessions {
			return m.updateSessions(msg)
		}
		return m.updateChat(msg)

	case agentEventMsg:
		return m.handleEvent(msg)

	case streamClosedMsg:
		return m.finishRun()

	case saveErrMsg:
		m.status = "save failed: " + msg.err.Error()
		return m, nil
	}

	// Spinner ticks (only keep them alive while a run is active).
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

func (m Model) updateChat(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
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
		if m.running {
			m.status = "a reply is in progress — press esc to cancel first"
			return m, nil
		}
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
		m.list = newSessionList(sums, m.w, m.h)
		m.mode = modeSessions
		return m, nil
	case "enter":
		return m.submit()
	case "pgup", "pgdown", "ctrl+u":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

func (m Model) updateSessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+o":
		m.mode = modeChat
		return m, nil
	case "ctrl+c", "ctrl+d":
		return m, tea.Quit
	case "enter":
		it, ok := m.list.SelectedItem().(sessionItem)
		if !ok {
			return m, nil
		}
		s, err := m.store.Load(it.s.ID)
		if err != nil {
			m.status = "cannot open session: " + err.Error()
			m.mode = modeChat
			return m, nil
		}
		m.loadSession(s)
		m.mode = modeChat
		return m, nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	prompt := strings.TrimSpace(m.ta.Value())
	if prompt == "" || m.running {
		return m, nil
	}
	m.ta.Reset()
	m.status = ""

	// Snapshot history + the new prompt for persistence *before* the agent
	// goroutine starts mutating its own history.
	snapshot := append(append([]core.Message(nil), m.agent.History...), core.Message{Role: "user", Content: prompt})

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	m.cancel = cancel
	ch := m.agent.RunStream(ctx, prompt)

	m.entries = append(m.entries, entry{kindUser, prompt})
	m.partial.Reset()
	m.running, m.waitingFirst, m.cancelling = true, true, false
	m.refresh(true)

	save := m.persist(snapshot) // mutates m, so evaluate before building the return
	return m, tea.Batch(save, waitForEvent(ch), m.sp.Tick)
}

func (m *Model) cancelRun() {
	if m.running && m.cancel != nil {
		m.cancel()
		m.cancelling = true
	}
}

func (m Model) handleEvent(msg agentEventMsg) (tea.Model, tea.Cmd) {
	switch ev := msg.ev.(type) {
	case agent.TextDelta:
		m.waitingFirst = false
		m.partial.WriteString(ev.Text)
	case agent.MessageDone:
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
			m.status = "cancelled"
		} else {
			m.entries = append(m.entries, entry{kindError, ev.Err.Error()})
		}
	case agent.RunDone:
	}
	m.refresh(false)
	return m, waitForEvent(msg.ch)
}

// finishRun runs once the agent has closed its channel, so reading its history
// is race-free. It persists the final state.
func (m Model) finishRun() (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.running, m.waitingFirst, m.cancelling = false, false, false
	m.partial.Reset()
	m.refresh(false)
	save := m.persist(append([]core.Message(nil), m.agent.History...))
	return m, save
}
