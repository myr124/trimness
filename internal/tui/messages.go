package tui

import "trimness/internal/agent"

// agentEventMsg wraps one agent event together with the channel it came from,
// so Update can re-arm waitForEvent for the next one.
type agentEventMsg struct {
	ev agent.Event
	ch <-chan agent.Event
}

// streamClosedMsg means the agent closed its event channel: the run is over.
type streamClosedMsg struct {
	ch <-chan agent.Event
}

// saveErrMsg reports a failed session save.
type saveErrMsg struct{ err error }
