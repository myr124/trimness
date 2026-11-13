package llm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"trimness/internal/core"

	"google.golang.org/genai"
)

/*
Components of our gemini provider

- Plugs into Client (and Streamer)
- Takes a request, and api key from our env
- Translates our provider-neutral types (messages, tool calls, tool
  definitions) to and from the genai SDK's types
*/

// EXPLORE WAYS TO CONSOLIDATE (I think some of this logic is repetitive, for example we have client types and interfaces but so does gemini and probably openai)

type GeminiClient struct {
	API_key string
	Model   string
}

// generatedIDPrefix marks tool-call IDs we made up ourselves. Gemini often does
// not send call IDs, but the rest of the system relies on every call having one.
// IDs with this prefix are never sent back to Gemini, which doesn't know them.
const generatedIDPrefix = "trimness-call-"

func newCallID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read does not fail on supported platforms
	return generatedIDPrefix + hex.EncodeToString(b)
}

// providerID returns id if Gemini issued it, or "" if we generated it.
func providerID(id string) string {
	if strings.HasPrefix(id, generatedIDPrefix) {
		return ""
	}
	return id
}

// toContents converts our message history into Gemini contents.
//
//   - "assistant" becomes the "model" role, with its text and any tool calls as parts.
//   - Consecutive "tool" messages become ONE user-role content holding one
//     functionResponse part each, in order. Gemini requires that every
//     functionCall is answered by a functionResponse in the next turn.
func toContents(msgs []core.Message) []*genai.Content {
	contents := make([]*genai.Content, 0, len(msgs))
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		switch m.Role {
		case "tool":
			var parts []*genai.Part
			for ; i < len(msgs) && msgs[i].Role == "tool"; i++ {
				parts = append(parts, functionResponsePart(msgs[i]))
			}
			i-- // the loop's i++ moves to the first message after the group
			contents = append(contents, &genai.Content{Role: genai.RoleUser, Parts: parts})

		case "assistant":
			var parts []*genai.Part
			if m.Content != "" {
				parts = append(parts, &genai.Part{Text: m.Content})
			}
			for _, c := range m.ToolCalls {
				parts = append(parts, functionCallPart(c))
			}
			if len(parts) == 0 {
				continue // Gemini rejects contents with no parts
			}
			contents = append(contents, &genai.Content{Role: genai.RoleModel, Parts: parts})

		default:
			contents = append(contents, genai.NewContentFromText(m.Content, genai.RoleUser))
		}
	}
	return contents
}

func functionCallPart(c core.ToolCall) *genai.Part {
	args := map[string]any{}
	if len(c.Args) > 0 {
		_ = json.Unmarshal(c.Args, &args) // the args came from the model as JSON, so this holds
	}
	return &genai.Part{
		FunctionCall:     &genai.FunctionCall{ID: providerID(c.ToolID), Name: c.ToolName, Args: args},
		ThoughtSignature: c.ThoughtSignature,
	}
}

func functionResponsePart(m core.Message) *genai.Part {
	key := "output"
	if m.IsError {
		key = "error"
	}
	return &genai.Part{FunctionResponse: &genai.FunctionResponse{
		ID:       providerID(m.ToolCallID),
		Name:     m.ToolName,
		Response: map[string]any{key: m.Content},
	}}
}

// toConfig builds the request config. With no tools it returns nil, which keeps
// the plain-chat request exactly as it was.
func toConfig(tools []core.ToolDefinition) (*genai.GenerateContentConfig, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, d := range tools {
		fd := &genai.FunctionDeclaration{Name: d.Name, Description: d.Description}
		if len(d.Parameters) > 0 {
			var schema map[string]any
			if err := json.Unmarshal(d.Parameters, &schema); err != nil {
				return nil, fmt.Errorf("gemini: tool %q has an invalid parameter schema: %w", d.Name, err)
			}
			fd.ParametersJsonSchema = schema
		}
		decls = append(decls, fd)
	}
	return &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: decls}}}, nil
}

// parseResponse pulls the visible text and any tool calls out of a response.
// It walks the parts itself rather than using r.Text(), which logs a warning to
// stderr whenever a response contains a function call (that would scribble
// over the TUI).
func parseResponse(r *genai.GenerateContentResponse) (string, []core.ToolCall) {
	if r == nil || len(r.Candidates) == 0 || r.Candidates[0].Content == nil {
		return "", nil
	}
	var sb strings.Builder
	var calls []core.ToolCall
	for _, p := range r.Candidates[0].Content.Parts {
		switch {
		case p.FunctionCall != nil:
			fc := p.FunctionCall
			args := json.RawMessage("{}")
			if len(fc.Args) > 0 {
				if b, err := json.Marshal(fc.Args); err == nil {
					args = b
				}
			}
			id := fc.ID
			if id == "" {
				id = newCallID()
			}
			calls = append(calls, core.ToolCall{
				ToolName:         fc.Name,
				ToolID:           id,
				Args:             args,
				ThoughtSignature: p.ThoughtSignature,
			})
		case p.Text != "" && !p.Thought:
			sb.WriteString(p.Text)
		}
	}
	return sb.String(), calls
}

// prepare validates the request and builds the SDK client and config shared by
// Generate and Stream.
func (gc GeminiClient) prepare(ctx context.Context, req Request) (*genai.Client, *genai.GenerateContentConfig, error) {
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("gemini: request has no messages")
	}
	cfg, err := toConfig(req.Tools)
	if err != nil {
		return nil, nil, err
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  gc.API_key,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, nil, err
	}
	return client, cfg, nil
}

func (gc GeminiClient) Generate(
	ctx context.Context,
	req Request,
) (Response, error) {
	client, cfg, err := gc.prepare(ctx, req)
	if err != nil {
		return Response{}, err
	}

	r, err := client.Models.GenerateContent(ctx, gc.Model, toContents(req.Messages), cfg)
	if err != nil {
		return Response{}, err
	}

	text, calls := parseResponse(r)
	return Response{Message: core.Message{Role: "assistant", Content: text, ToolCalls: calls}}, nil
}

// Compile-time check that GeminiClient supports streaming.
var _ Streamer = GeminiClient{}

// Stream sends the request and returns a channel of chunks (text deltas and
// tool calls). The channel is closed when the stream ends. A mid-stream failure
// arrives as a final Chunk with Err set; failures before streaming starts are
// returned directly.
func (gc GeminiClient) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	client, cfg, err := gc.prepare(ctx, req)
	if err != nil {
		return nil, err
	}

	out := make(chan Chunk)

	go func() {
		defer close(out) // closed channel = "stream finished" for the agent

		// emit gives up if ctx is cancelled so we never block on a reader that left.
		emit := func(c Chunk) bool {
			select {
			case out <- c:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for r, err := range client.Models.GenerateContentStream(ctx, gc.Model, toContents(req.Messages), cfg) {
			if err != nil {
				emit(Chunk{Err: err})
				return
			}
			text, calls := parseResponse(r)
			if text != "" && !emit(Chunk{Text: text}) {
				return
			}
			if len(calls) > 0 && !emit(Chunk{ToolCalls: calls}) {
				return
			}
		}
	}()

	return out, nil
}
