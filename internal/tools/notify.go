package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
)

// NotifyHandler delivers a notify tool call through an attached daemon client
// (typically the Desktop app) instead of shelling out to osascript. It returns
// true when the notification was delivered — in which case the tool skips the
// osascript fallback — and false when no client is attached, which tells the
// tool to fall back to osascript for headless mode.
type NotifyHandler func(title, body string, sound bool) bool

type notifyHandlerKey struct{}

// WithNotifyHandler returns a context carrying a NotifyHandler. The daemon
// runner attaches one per run so that notify tool calls from scheduled or
// interactive agents can be routed through the Desktop's UNUserNotificationCenter
// with correct app attribution and click-through.
func WithNotifyHandler(ctx context.Context, h NotifyHandler) context.Context {
	if h == nil {
		return ctx
	}
	return context.WithValue(ctx, notifyHandlerKey{}, h)
}

// NotifyHandlerFrom returns the NotifyHandler from ctx, or nil if none is set.
func NotifyHandlerFrom(ctx context.Context) NotifyHandler {
	h, _ := ctx.Value(notifyHandlerKey{}).(NotifyHandler)
	return h
}

type NotifyTool struct{}

type notifyArgs struct {
	Title   string `json:"title"`
	Body    string `json:"body,omitempty"`
	Message string `json:"message,omitempty"` // alias for body
	Sound   bool   `json:"sound,omitempty"`
}

func (t *NotifyTool) Info() agent.ToolInfo {
	return agent.ToolInfo{
		Name:        "notify",
		Description: "Send a macOS desktop notification using osascript.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string", "description": "Notification title"},
				"body":    map[string]any{"type": "string", "description": "Notification body text (alias: message)"},
			"message": map[string]any{"type": "string", "description": "Alias for body"},
				"sound": map[string]any{"type": "boolean", "description": "Play notification sound (default: false)"},
			},
		},
		Required: []string{"title"},
	}
}

func (t *NotifyTool) Run(ctx context.Context, argsJSON string) (agent.ToolResult, error) {
	var args notifyArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	body := args.Body
	if body == "" {
		body = args.Message
	}

	// Prefer the Desktop route when a handler is attached. The handler returns
	// true when a Desktop client is actually subscribed; if it returns false,
	// the daemon is headless and we fall through to the osascript path so the
	// banner still shows (attributed to Script Editor, which is expected in
	// headless mode since there's no app bundle to attribute it to).
	if h := NotifyHandlerFrom(ctx); h != nil {
		if h(args.Title, body, args.Sound) {
			return agent.ToolResult{Content: "notification sent"}, nil
		}
	}

	script := buildNotifyScript(args.Title, body, args.Sound)

	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("notification error: %v\n%s", err, string(output)), IsError: true}, nil
	}

	return agent.ToolResult{Content: "notification sent"}, nil
}

func buildNotifyScript(title, body string, sound bool) string {
	title = escapeAppleScript(title)
	body = escapeAppleScript(body)

	script := fmt.Sprintf(`display notification "%s" with title "%s"`, body, title)
	if sound {
		script += ` sound name "default"`
	}
	return script
}

func (t *NotifyTool) RequiresApproval() bool { return true }

func (t *NotifyTool) IsReadOnlyCall(string) bool { return false }
