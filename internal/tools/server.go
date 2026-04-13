package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

// ServerTool wraps a server-side tool schema and proxies execution
// through the gateway's /api/v1/tools/{name}/execute endpoint.
type ServerTool struct {
	schema  client.ServerToolSchema
	gateway *client.GatewayClient
}

func NewServerTool(schema client.ServerToolSchema, gateway *client.GatewayClient) *ServerTool {
	return &ServerTool{schema: schema, gateway: gateway}
}

func (t *ServerTool) Info() agent.ToolInfo {
	return agent.ToolInfo{
		Name:        t.schema.Name,
		Description: t.schema.Description,
		Parameters:  t.schema.Parameters,
	}
}

func (t *ServerTool) Run(ctx context.Context, argsJSON string) (agent.ToolResult, error) {
	var args map[string]any
	if argsJSON != "" && argsJSON != "{}" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return agent.ToolResult{
				Content: fmt.Sprintf("invalid arguments: %v", err),
				IsError: true,
			}, nil
		}
	}
	if args == nil {
		args = map[string]any{}
	}

	resp, err := t.gateway.ExecuteTool(ctx, t.schema.Name, args, "")
	if err != nil {
		msg := err.Error()
		prefix := classifyServerError(msg)
		return agent.ToolResult{
			Content: fmt.Sprintf("%sserver tool error: %v", prefix, err),
			IsError: true,
		}, nil
	}

	if resp.Error != nil && *resp.Error != "" {
		return agent.ToolResult{Content: *resp.Error, IsError: true}, nil
	}

	if !resp.Success {
		return agent.ToolResult{Content: "tool execution failed", IsError: true}, nil
	}

	// Prefer pre-formatted text from backend; fall back to raw JSON output
	if resp.Text != nil && *resp.Text != "" {
		return agent.ToolResult{Content: *resp.Text}, nil
	}
	if len(resp.Output) == 0 || string(resp.Output) == "null" {
		return agent.ToolResult{Content: "no output"}, nil
	}
	return agent.ToolResult{Content: string(resp.Output)}, nil
}

// RequiresApproval returns false — the server enforces its own access control.
func (t *ServerTool) RequiresApproval() bool { return false }

// classifyServerError returns the appropriate error prefix based on the error
// message, so the agent loop's error-handling instructions can guide the model
// to retry transient failures instead of fabricating explanations.
func classifyServerError(msg string) string {
	lower := strings.ToLower(msg)
	for _, sig := range []string{
		"returned 429", "returned 502", "returned 503", "returned 504",
		"rate limit", "timeout", "timed out", "connection refused",
		"connection reset", "eof", "unavailable",
	} {
		if strings.Contains(lower, sig) {
			return "[transient error] "
		}
	}
	if strings.Contains(lower, "returned 401") || strings.Contains(lower, "returned 403") {
		return "[permission error] "
	}
	if strings.Contains(lower, "returned 400") || strings.Contains(lower, "returned 422") {
		return "[validation error] "
	}
	return ""
}

// ToolSource implements agent.ToolSourcer for deterministic tool ordering.
func (t *ServerTool) ToolSource() agent.ToolSource { return agent.SourceGateway }
