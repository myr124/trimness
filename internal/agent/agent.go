package agent

import "trimness/internal/core"

type AgentFunctions interface {
	Run(prompt string)
	AddMessage(msg core.Message)
}

type Agent struct {
	Name    string         // Just a name for our agent
	History []core.Message // since we aren't running in a loop currently our history is lost every run but it will help us in the future

}

func (a *Agent) Run(prompt string) {
	// call llm client?
	m := core.Message{Role: "user", Content: prompt}
	a.AddMessage(m)
}

func (a *Agent) AddMessage(msg core.Message) {
	a.History = append(a.History, msg)
}
