package llm

import (
	"context"
	"fmt"
	"trimness/internal/core"

	"google.golang.org/genai"
)

/*
Components of our gemini provider

- Plugs into Client
- Takes a request, and api key from our env
- Sends an http request to an api

*/

// EXPLORE WAYS TO CONSOLIDATE (I think some of this logic is repetitive, for example we have client types and interfaces but so does gemini and probably openai)

type GeminiClient struct {
	API_key string
	Model   string
}

// toContents converts our message history into Gemini contents, mapping
// "assistant" to Gemini's "model" role. Tool messages are not mapped yet.
func toContents(msgs []core.Message) []*genai.Content {
	contents := make([]*genai.Content, 0, len(msgs))
	for _, m := range msgs {
		role := genai.RoleUser
		if m.Role == "assistant" {
			role = genai.RoleModel
		}
		contents = append(contents, genai.NewContentFromText(m.Content, genai.Role(role)))
	}
	return contents
}

func (gc GeminiClient) Generate(
	ctx context.Context,
	req Request,
) (Response, error) {

	if len(req.Messages) == 0 {
		return Response{}, fmt.Errorf("gemini: request has no messages")
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  gc.API_key,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return Response{}, err
	}

	r, err := client.Models.GenerateContent(ctx, gc.Model, toContents(req.Messages), nil)
	if err != nil {
		return Response{}, err
	}

	return Response{Message: core.Message{Role: "assistant", Content: r.Text()}}, nil
}
