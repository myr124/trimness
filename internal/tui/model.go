// Package tui is the Bubble Tea front end. It holds no business logic: it
// drives agent.RunStream and renders the events it emits.
package tui

import (
	"context"
	"strings"
	"time"
	"trimness/internal/agent"
	"trimness/internal/core"
	"trimness/internal/session"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// runTimeout bounds a single agent run (the whole prompt -> reply cycle).
const runTimeout = 60 * time.Second

const inputHeight = 3

type mode int

const (
	modeChat mode = iota
	modeSessions
)

type Model struct {
	agent *agent.Agent
	store session.Store
	sess  session.Session

	mode    mode
	entries []entry
	partial strings.Builder // in-flight assistant text (streamed deltas)

	running      bool
	waitingFirst bool // spinner shown until first delta / message
	cancelling   bool
	cancel       context.CancelFunc
	status       string // transient status-line message

	vp   viewport.Model
	ta   textarea.Model
	sp   spinner.Model
	list list.Model

	w, h int
}

// New builds a Model around an agent and store, resuming sess. The agent's
// history is replaced with the session's messages.
func New(a *agent.Agent, store session.Store, sess session.Session) Model {
	ta := textarea.New()
	ta.Placeholder = "Send a message…  (enter: send · alt+enter: newline)"
	ta.ShowLineNumbers = false
	ta.SetHeight(inputHeight)
	ta.KeyMap.InsertNewline.SetKeys("alt+enter", "ctrl+j")
	ta.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))

	m := Model{
		agent: a,
		store: store,
		sess:  sess,
		vp:    viewport.New(0, 0),
		ta:    ta,
		sp:    sp,
	}
	m.loadSession(sess)
	return m
}

func (m *Model) loadSession(s session.Session) {
	m.sess = s
	m.agent.SetHistory(s.Messages)
	m.entries = entriesFromMessages(s.Messages)
	m.partial.Reset()
	m.status = ""
	m.refresh(true)
}

func (m Model) Init() tea.Cmd { return textarea.Blink }

// layout resizes children to the window.
func (m *Model) layout() {
	m.ta.SetWidth(m.w)
	m.vp.Width = m.w
	m.vp.Height = max(m.h-inputHeight-2, 1) // status line + rule
	if m.mode == modeSessions {             // the list is zero-valued (and unsafe to resize) until first opened
		m.list.SetSize(m.w, m.h)
	}
	m.refresh(false)
}

// refresh re-renders the transcript. It keeps the view pinned to the bottom if
// it already was there (or when force is set), so scrolling up isn't hijacked.
func (m *Model) refresh(force bool) {
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(renderTranscript(m.entries, m.partial.String(), m.vp.Width))
	if force || atBottom {
		m.vp.GotoBottom()
	}
}

// waitForEvent blocks on the agent channel off the UI goroutine.
func waitForEvent(ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{ch: ch}
		}
		return agentEventMsg{ev: ev, ch: ch}
	}
}

// persist snapshots the given messages into the session and saves atomically.
func (m *Model) persist(msgs []core.Message) tea.Cmd {
	m.sess.Messages = msgs
	m.sess.DeriveTitle()
	m.sess.UpdatedAt = time.Now()
	s := m.sess
	store := m.store
	return func() tea.Msg {
		if err := store.Save(s); err != nil {
			return saveErrMsg{err: err}
		}
		return nil
	}
}
