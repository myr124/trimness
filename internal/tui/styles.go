package tui

import "github.com/charmbracelet/lipgloss"

var (
	userLabel      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	assistantLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	toolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).PaddingLeft(2)
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	statusStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	ruleStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)
