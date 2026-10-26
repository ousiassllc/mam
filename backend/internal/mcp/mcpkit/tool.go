package mcpkit

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Handler runs one tool. In and Out are structs: their json / jsonschema tags become the input and output
// schema of the tool, and the arguments are validated against the input schema before the handler runs.
// Out MUST be a struct (a list goes into a field of it). Return a service error as it is: the wrapper
// maps it with MapError.
type Handler[In, Out any] func(ctx context.Context, in In) (Out, error)

// Option adjusts the annotations of a tool.
type Option func(*mcp.ToolAnnotations)

// Idempotent marks a Write tool whose repeat with the same input changes nothing more.
func Idempotent() Option { return func(a *mcp.ToolAnnotations) { a.IdempotentHint = true } }

// OpenWorld marks a tool that reaches an external service (a model API) by itself.
func OpenWorld() Option {
	return func(a *mcp.ToolAnnotations) { a.OpenWorldHint = boolPtr(true) }
}

func boolPtr(b bool) *bool { return &b }

// Read registers a tool that only reads (readOnlyHint).
func Read[In, Out any](r *Registrar, name, description string, h Handler[In, Out], opts ...Option) {
	add(r, name, description, &mcp.ToolAnnotations{ReadOnlyHint: true}, h, opts)
}

// Write registers a tool that changes data without deleting or overwriting what a person made
// (destructiveHint false).
func Write[In, Out any](r *Registrar, name, description string, h Handler[In, Out], opts ...Option) {
	add(r, name, description, &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)}, h, opts)
}

// Destroy registers a tool that deletes or overwrites data (destructiveHint true). It asks for no
// confirmation: the screen does not either, or asks in the browser, which an agent cannot answer.
func Destroy[In, Out any](r *Registrar, name, description string, h Handler[In, Out], opts ...Option) {
	add(r, name, description, &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)}, h, opts)
}

func add[In, Out any](r *Registrar, name, description string, a *mcp.ToolAnnotations, h Handler[In, Out], opts []Option) {
	r.claim(name, description)
	a.OpenWorldHint = boolPtr(false) // the MCP default is true; a tool says so with OpenWorld
	for _, o := range opts {
		o(a)
	}
	mcp.AddTool(r.server, &mcp.Tool{Name: name, Description: description, Annotations: a},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			out, err := h(ctx, in)
			if err != nil {
				if Internal(err) {
					r.Logger.Error("mcp tool failed", "tool", name, "err", err)
				}
				var zero Out
				return nil, zero, MapError(err)
			}
			return nil, out, nil
		})
}
