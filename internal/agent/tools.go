package agent

import (
	"context"

	"github.com/Kocoro-lab/shan/internal/client"
)

type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
}

type ImageBlock struct {
	MediaType string // e.g. "image/png"
	Data      string // base64-encoded
}

type ToolResult struct {
	Content string
	IsError bool
	Images  []ImageBlock
}

type Tool interface {
	Info() ToolInfo
	Run(ctx context.Context, args string) (ToolResult, error)
	RequiresApproval() bool
}

// NativeToolProvider is an optional interface for tools that use a provider's
// native tool schema (e.g., Anthropic's computer_20251124) instead of the
// standard function-calling format.
type NativeToolProvider interface {
	NativeToolDef() *client.NativeToolDef
}

// SafeChecker is an optional interface tools can implement to indicate
// certain invocations are safe and don't need approval.
type SafeChecker interface {
	IsSafeArgs(argsJSON string) bool
}

type ToolRegistry struct {
	tools map[string]Tool
	order []string
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

func (r *ToolRegistry) Register(t Tool) {
	name := t.Info().Name
	r.tools[name] = t
	r.order = append(r.order, name)
}

func (r *ToolRegistry) Clone() *ToolRegistry {
	clone := NewToolRegistry()
	for _, name := range r.order {
		tool := r.tools[name]
		clone.tools[name] = tool
		clone.order = append(clone.order, name)
	}
	return clone
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *ToolRegistry) All() []Tool {
	tools := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		tools = append(tools, r.tools[name])
	}
	return tools
}

func (r *ToolRegistry) Schemas() []client.Tool {
	schemas := make([]client.Tool, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]

		// Check for native tool definition
		if native, ok := t.(NativeToolProvider); ok {
			def := native.NativeToolDef()
			if def != nil {
				schemas = append(schemas, client.Tool{
					Type:            def.Type,
					Name:            def.Name,
					DisplayWidthPx:  def.DisplayWidthPx,
					DisplayHeightPx: def.DisplayHeightPx,
				})
				continue
			}
		}

		// Standard function tool
		info := t.Info()
		params := info.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		if info.Required != nil {
			params["required"] = info.Required
		}
		schemas = append(schemas, client.Tool{
			Type: "function",
			Function: client.FunctionDef{
				Name:        info.Name,
				Description: info.Description,
				Parameters:  params,
			},
		})
	}
	return schemas
}
