package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"trimness/internal/core"

	"github.com/charmbracelet/lipgloss"
)

type entryKind int

const (
	kindUser entryKind = iota
	kindAssistant
	kindToolCall
	kindToolResult
	kindApproval // a pending request to let the agent run a tool
	kindError
)

// entry is one rendered item in the transcript.
type entry struct {
	kind entryKind
	text string
}

// entriesFromMessages rebuilds a transcript from persisted messages.
func entriesFromMessages(msgs []core.Message) []entry {
	var out []entry
	for _, m := range msgs {
		switch m.Role {
		case "user":
			out = append(out, entry{kindUser, m.Content})
		case "assistant":
			if strings.TrimSpace(m.Content) != "" {
				out = append(out, entry{kindAssistant, m.Content})
			}
			for _, c := range m.ToolCalls {
				out = append(out, entry{kindToolCall, formatCall(c)})
			}
		case "tool":
			out = append(out, entry{kindToolResult, resultText(m.Content, m.IsError)})
		}
	}
	return out
}

// maxCallArgsLen caps how much of a call's JSON arguments is shown inline
// (a writefile call carries its whole file content).
const maxCallArgsLen = 120

func formatCall(c core.ToolCall) string {
	args := string(c.Args)
	var buf bytes.Buffer
	if json.Compact(&buf, c.Args) == nil {
		args = buf.String()
	}
	if r := []rune(args); len(r) > maxCallArgsLen {
		args = string(r[:maxCallArgsLen]) + "…"
	}
	return fmt.Sprintf("⚙ %s(%s)", c.ToolName, args)
}

// resultText is the transcript text for a tool result; failures are labelled.
func resultText(output string, isErr bool) string {
	if isErr {
		return "error: " + output
	}
	return output
}

// renderEntry renders one entry wrapped to width.
func renderEntry(e entry, width int) string {
	if width < 10 {
		width = 10
	}
	wrap := lipgloss.NewStyle().Width(width)
	switch e.kind {
	case kindUser:
		return userLabel.Render("you ›") + "\n" + wrap.Render(e.text)
	case kindAssistant:
		return assistantLabel.Render("trimness ›") + "\n" + wrap.Render(e.text)
	case kindToolCall:
		return toolStyle.Width(width).Render(e.text)
	case kindToolResult:
		return toolStyle.Width(width).Render("↳ " + truncateLines(e.text, 8))
	case kindApproval:
		return approvalStyle.Width(width).Render("⚠ " + e.text)
	default:
		return errorStyle.Width(width).Render("✗ " + e.text)
	}
}

// renderTranscript renders all entries plus any in-flight assistant text.
func renderTranscript(entries []entry, partial string, width int) string {
	parts := make([]string, 0, len(entries)+1)
	for _, e := range entries {
		parts = append(parts, renderEntry(e, width))
	}
	if partial != "" {
		parts = append(parts, renderEntry(entry{kindAssistant, partial}, width))
	}
	return strings.Join(parts, "\n\n")
}

func truncateLines(s string, max int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= max {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:max], "\n") + fmt.Sprintf("\n… (%d more lines)", len(lines)-max)
}
