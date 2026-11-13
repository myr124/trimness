package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"trimness/internal/core"
)

// Tool is one capability the model can invoke. Run receives the raw JSON
// arguments the model produced and decodes them into the tool's own struct. A
// returned error becomes an error ToolResult, which the model sees and can
// react to; it never aborts the agent run.
type Tool interface {
	Name() string
	Definition() core.ToolDefinition
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

// Gated is implemented by tools that must be confirmed by the user before they
// run (anything that changes state). Confirm returns a human-readable
// description of exactly what the call would do.
type Gated interface {
	Confirm(args json.RawMessage) string
}

// decodeArgs strictly decodes a tool's JSON arguments into v: unknown fields
// and wrong types are errors, so a confused model gets a clear message back.
func decodeArgs(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

// Registry maps tool names to implementations. It satisfies agent.ToolRunner
// and agent.Approver.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(ts))}
	for _, t := range ts {
		r.tools[t.Name()] = t
	}
	return r
}

// Names lists the registered tool names, sorted.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Definitions returns what we advertise to the model, in a stable order.
func (r *Registry) Definitions() []core.ToolDefinition {
	defs := make([]core.ToolDefinition, 0, len(r.tools))
	for _, n := range r.Names() {
		defs = append(defs, r.tools[n].Definition())
	}
	return defs
}

// NeedsApproval reports whether the call must be confirmed by the user first,
// along with a description of what it would do. Unknown tools never need
// approval (Execute rejects them anyway).
func (r *Registry) NeedsApproval(call core.ToolCall) (string, bool) {
	t, ok := r.tools[call.ToolName]
	if !ok {
		return "", false
	}
	g, ok := t.(Gated)
	if !ok {
		return "", false
	}
	return g.Confirm(call.Args), true
}

// Execute runs one tool call. Unknown tools and tool failures come back as
// IsError results; the result always carries the call's ToolID.
func (r *Registry) Execute(ctx context.Context, call core.ToolCall) core.ToolResult {
	res := core.ToolResult{ToolID: call.ToolID}

	t, ok := r.tools[call.ToolName]
	if !ok {
		res.IsError = true
		res.Output = fmt.Sprintf("unknown tool %q (available: %s)", call.ToolName, strings.Join(r.Names(), ", "))
		return res
	}
	if err := ctx.Err(); err != nil {
		res.IsError = true
		res.Output = err.Error()
		return res
	}

	out, err := t.Run(ctx, call.Args)
	if err != nil {
		res.IsError = true
		res.Output = err.Error()
		return res
	}
	res.Output = out
	return res
}
