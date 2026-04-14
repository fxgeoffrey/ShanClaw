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

	// Convert server-reported usage (xAI Grok tokens for x_search, SerpAPI
	// queries for web_search, etc.) into an agent-level ToolUsage. Populated
	// on the ToolResult so the audit logger can attribute cost per call; also
	// emitted via context so the per-run usage accumulator picks it up.
	// Server populates resp.Usage when the underlying provider returns billing
	// info; older servers leave it nil and this is a no-op.
	var toolUsage *agent.ToolUsage
	if resp.Usage != nil {
		u := resp.Usage
		// The gateway currently returns a flat `tokens` count (synthetic for
		// SERP tools, real input+output sum for x_search). If explicit
		// input/output breakdowns are present, prefer them; else collapse
		// `tokens` into TotalTokens so the accumulator still sees the volume.
		totalTokens := u.TotalTokens
		if totalTokens == 0 {
			totalTokens = u.Tokens
		}
		if totalTokens == 0 {
			totalTokens = u.InputTokens + u.OutputTokens
		}
		model := u.Model
		if model == "" {
			model = u.CostModel
		}
		toolUsage = &agent.ToolUsage{
			Provider:     u.Provider,
			Model:        model,
			InputTokens:  u.InputTokens,
			OutputTokens: u.OutputTokens,
			TotalTokens:  totalTokens,
			CostUSD:      u.CostUSD,
			Units:        u.Units,
			UnitType:     u.UnitType,
		}
		agent.EmitUsage(ctx, agent.TurnUsage{
			InputTokens:  u.InputTokens,
			OutputTokens: u.OutputTokens,
			TotalTokens:  totalTokens,
			CostUSD:      u.CostUSD,
			// Gateway tool calls are not LLM calls from the driving model's
			// perspective — leave LLMCalls=0 so session LLMCalls stays clean.
			Model: model,
		})
	}

	if resp.Error != nil && *resp.Error != "" {
		return agent.ToolResult{Content: *resp.Error, IsError: true, Usage: toolUsage}, nil
	}

	if !resp.Success {
		return agent.ToolResult{Content: "tool execution failed", IsError: true, Usage: toolUsage}, nil
	}

	// Prefer pre-formatted text from backend; fall back to raw JSON output
	if resp.Text != nil && *resp.Text != "" {
		return agent.ToolResult{Content: *resp.Text, Usage: toolUsage}, nil
	}
	if len(resp.Output) == 0 || string(resp.Output) == "null" {
		return agent.ToolResult{Content: "no output", Usage: toolUsage}, nil
	}
	return agent.ToolResult{Content: string(resp.Output), Usage: toolUsage}, nil
}

// RequiresApproval returns false — the server enforces its own access control.
func (t *ServerTool) RequiresApproval() bool { return false }

// classifyServerError returns the appropriate error prefix based on the error
// message, so the agent loop's error-handling instructions can guide the model
// to retry transient failures instead of fabricating explanations.
//
// Status-code markers (returned NNN) are checked before free-text transient
// keywords so that a 4xx response body mentioning "timeout" (e.g. validation
// "timeout must be <= 30") is not mis-tagged as transient and retried.
func classifyServerError(msg string) string {
	lower := strings.ToLower(msg)
	// Status-code classification first — the HTTP status is authoritative.
	if strings.Contains(lower, "returned 401") || strings.Contains(lower, "returned 403") {
		return "[permission error] "
	}
	if strings.Contains(lower, "returned 400") || strings.Contains(lower, "returned 422") {
		return "[validation error] "
	}
	if strings.Contains(lower, "returned 429") ||
		strings.Contains(lower, "returned 502") ||
		strings.Contains(lower, "returned 503") ||
		strings.Contains(lower, "returned 504") {
		return "[transient error] "
	}
	// Keyword fallback for network-layer failures that have no HTTP status
	// (connection refused/reset, DNS, timeouts before the server responded).
	for _, sig := range []string{
		"rate limit", "timeout", "timed out", "connection refused",
		"connection reset", "eof", "unavailable",
	} {
		if strings.Contains(lower, sig) {
			return "[transient error] "
		}
	}
	return ""
}

// ToolSource implements agent.ToolSourcer for deterministic tool ordering.
func (t *ServerTool) ToolSource() agent.ToolSource { return agent.SourceGateway }
