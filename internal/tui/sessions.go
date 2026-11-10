package tui

import (
	"fmt"
	"trimness/internal/session"

	"github.com/charmbracelet/bubbles/list"
)

// sessionItem adapts session.Summary to list.Item for the default delegate.
type sessionItem struct{ s session.Summary }

func (i sessionItem) Title() string { return i.s.Title }
func (i sessionItem) Description() string {
	return fmt.Sprintf("%s · %s", i.s.UpdatedAt.Format("2006-01-02 15:04"), i.s.ID[:min(8, len(i.s.ID))])
}
func (i sessionItem) FilterValue() string { return i.s.Title }

func newSessionList(sums []session.Summary, w, h int) list.Model {
	items := make([]list.Item, len(sums))
	for i, s := range sums {
		items[i] = sessionItem{s}
	}
	l := list.New(items, list.NewDefaultDelegate(), w, h)
	l.Title = "Sessions  (enter: open · esc: back)"
	l.SetShowHelp(false)
	return l
}
