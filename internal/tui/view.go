package tui

import (
	"fmt"
	"strings"
)

func (m Model) View() string {
	if m.w == 0 {
		return "loading…"
	}
	if m.mode == modeSessions {
		return m.list.View()
	}
	rule := ruleStyle.Render(strings.Repeat("─", m.w))
	return m.vp.View() + "\n" + rule + "\n" + m.statusLine() + "\n" + m.ta.View()
}

func (m Model) statusLine() string {
	if m.pending != nil {
		return approvalStyle.PaddingLeft(0).Render("⚠ allow this action?  [y] yes · [n] no · ctrl+c cancel the run")
	}
	var left string
	switch {
	case m.running && m.cancelling:
		left = "cancelling…"
	case m.running && m.waitingFirst:
		left = m.sp.View() + " thinking…"
	case m.running:
		left = m.sp.View() + " responding…"
	case m.status != "":
		left = m.status
	default:
		left = fmt.Sprintf("session %s", m.sess.Title)
	}
	hints := "esc cancel · pgup/pgdn scroll · ctrl+n new · ctrl+o sessions · ctrl+c quit"
	return statusStyle.Render(left + "  |  " + hints)
}
