package agent

import "trimness/internal/core"

// Event is emitted by Agent.RunStream. The TUI (or any other front end)
// consumes these; the agent knows nothing about the UI.
type Event interface{ isEvent() }

// TextDelta is a streamed chunk of assistant text.
type TextDelta struct{ Text string }

// MessageDone carries the complete assistant message for the turn.
type MessageDone struct{ Msg core.Message }

// ToolStarted / ToolFinished surround a tool execution.
type ToolStarted struct{ Call core.ToolCall }
type ToolFinished struct{ Result core.ToolResult }

// RunError ends the run unsuccessfully. History is left consistent: any
// partial assistant text is discarded.
type RunError struct{ Err error }

// RunDone is always the last event unless RunError was sent.
type RunDone struct{}

func (TextDelta) isEvent()    {}
func (MessageDone) isEvent()  {}
func (ToolStarted) isEvent()  {}
func (ToolFinished) isEvent() {}
func (RunError) isEvent()     {}
func (RunDone) isEvent()      {}
