package llm

import "context"

/*
Components of our gemini provider

- Plugs into Client
- Takes a request, and api key from our env
- Sends an http request to an api

*/

type GeminiClient struct {
	api_key string
	Request Request
}

func (gc GeminiClient) Generate(
	ctx context.Context,
	req Request,
) (Response, error) {


	// Send our request through either library or https request



	return (Response, error)
}
