package llm

import (
	"context"
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

func (gc GeminiClient) Generate(
	ctx context.Context,
	req Request,
) (Response, error) {

	// Send our request through either library or https request

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  gc.API_key,
		Backend: genai.BackendGeminiAPI,
	})

	parts := []*genai.Part{
		{Text: req.Messages[0].Content},
	}

	r, err := client.Models.GenerateContent(ctx, gc.Model, []*genai.Content{{Parts: parts}}, nil)

	if err != nil {
		return Response{}, err
	}

	response := Response{Message: core.Message{Role: "assistant", Content: r.Text()}}

	return response, err
}
