package agent

import (
	"context"
	"trimness/internal/core"
	"trimness/internal/llm"
)

type AgentFunctions interface {
	Run(prompt string)
	AddMessage(msg core.Message)
}

type Agent struct {
	Name    string         // Just a name for our agent
	History []core.Message // since we aren't running in a loop currently our history is lost every run but it will help us in the future
	Client  llm.Client
}

func (a *Agent) Run(ctx context.Context, prompt string) (string, error) {

	a.AddMessage(core.Message{Role: "user", Content: prompt})
	req := llm.Request{Messages: a.History}
	response, err := a.Client.Generate(ctx, req)

	if err != nil {
		return "", err
	}

	return response.Message.Content, nil

}

func (a *Agent) AddMessage(msg core.Message) {
	a.History = append(a.History, msg)
}
