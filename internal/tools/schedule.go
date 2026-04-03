package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
	"github.com/Kocoro-lab/ShanClaw/internal/schedule"
)

type ScheduleTool struct {
	manager *schedule.Manager
	action  string
}

func NewScheduleTools(mgr *schedule.Manager) []agent.Tool {
	return []agent.Tool{
		&ScheduleTool{manager: mgr, action: "create"},
		&ScheduleTool{manager: mgr, action: "list"},
		&ScheduleTool{manager: mgr, action: "update"},
		&ScheduleTool{manager: mgr, action: "remove"},
	}
}

func (t *ScheduleTool) Info() agent.ToolInfo {
	switch t.action {
	case "create":
		return agent.ToolInfo{
			Name:        "schedule_create",
			Description: "Create a scheduled task that runs a shan agent on a cron schedule. Supports full cron syntax (ranges, steps, lists). Each run saves its result as a session (searchable via session_search).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent":  map[string]any{"type": "string", "description": "Agent name (from ~/.shannon/agents/). Empty for default agent."},
					"cron":   map[string]any{"type": "string", "description": "5-field cron expression (minute hour day month weekday). Supports */5, 1-5, 1,3,5."},
					"prompt": map[string]any{"type": "string", "description": "The prompt to send to the agent on each run."},
				},
			},
			Required: []string{"cron", "prompt"},
		}
	case "list":
		return agent.ToolInfo{
			Name:        "schedule_list",
			Description: "List all locally scheduled tasks with their status.",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		}
	case "update":
		return agent.ToolInfo{
			Name:        "schedule_update",
			Description: "Update an existing scheduled task.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":      map[string]any{"type": "string", "description": "Schedule ID"},
					"cron":    map[string]any{"type": "string", "description": "New cron expression"},
					"prompt":  map[string]any{"type": "string", "description": "New prompt"},
					"enabled": map[string]any{"type": "boolean", "description": "Enable or disable"},
				},
			},
			Required: []string{"id"},
		}
	case "remove":
		return agent.ToolInfo{
			Name:        "schedule_remove",
			Description: "Remove a scheduled task.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Schedule ID to remove"},
				},
			},
			Required: []string{"id"},
		}
	}
	return agent.ToolInfo{}
}

func (t *ScheduleTool) Run(ctx context.Context, argsJSON string) (agent.ToolResult, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return agent.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
	}
	switch t.action {
	case "create":
		agentName, _ := args["agent"].(string)
		cron, _ := args["cron"].(string)
		prompt, _ := args["prompt"].(string)
		if cron == "" || prompt == "" {
			return agent.ToolResult{Content: "cron and prompt are required", IsError: true}, nil
		}
		id, err := t.manager.Create(agentName, cron, prompt)
		if err != nil {
			return agent.ToolResult{Content: err.Error(), IsError: true}, nil
		}
		// 默认捕获并保存当前对话上下文，触发时 agent 可理解任务背景
		if ctxMsgs := extractConversationContext(ctx); len(ctxMsgs) > 0 {
			if saveErr := t.manager.SaveContext(id, ctxMsgs); saveErr != nil {
				return agent.ToolResult{Content: fmt.Sprintf("Schedule created: %s (warning: failed to save context: %v)", id, saveErr)}, nil
			}
		}
		return agent.ToolResult{Content: fmt.Sprintf("Schedule created: %s", id)}, nil
	case "list":
		list, err := t.manager.List()
		if err != nil {
			return agent.ToolResult{Content: err.Error(), IsError: true}, nil
		}
		if len(list) == 0 {
			return agent.ToolResult{Content: "No scheduled tasks."}, nil
		}
		var sb strings.Builder
		for _, s := range list {
			agentDisplay := s.Agent
			if agentDisplay == "" {
				agentDisplay = "(default)"
			}
			ctxTag := ""
			if t.manager.HasContext(s.ID) {
				ctxTag = " [ctx]"
			}
			fmt.Fprintf(&sb, "%s | agent=%s | cron=%s | enabled=%v | sync=%s | %s%s\n",
				s.ID, agentDisplay, s.Cron, s.Enabled, s.SyncStatus, s.Prompt, ctxTag)
		}
		return agent.ToolResult{Content: sb.String()}, nil
	case "update":
		id, _ := args["id"].(string)
		if id == "" {
			return agent.ToolResult{Content: "id is required", IsError: true}, nil
		}
		opts := &schedule.UpdateOpts{}
		if v, ok := args["cron"].(string); ok {
			opts.Cron = &v
		}
		if v, ok := args["prompt"].(string); ok {
			opts.Prompt = &v
		}
		if v, ok := args["enabled"].(bool); ok {
			opts.Enabled = &v
		}
		if opts.Cron == nil && opts.Prompt == nil && opts.Enabled == nil {
			return agent.ToolResult{Content: "at least one of cron, prompt, or enabled is required", IsError: true}, nil
		}
		if err := t.manager.Update(id, opts); err != nil {
			return agent.ToolResult{Content: err.Error(), IsError: true}, nil
		}
		return agent.ToolResult{Content: fmt.Sprintf("Schedule %s updated.", id)}, nil
	case "remove":
		id, _ := args["id"].(string)
		if id == "" {
			return agent.ToolResult{Content: "id is required", IsError: true}, nil
		}
		if err := t.manager.Remove(id); err != nil {
			return agent.ToolResult{Content: err.Error(), IsError: true}, nil
		}
		return agent.ToolResult{Content: fmt.Sprintf("Schedule %s removed.", id)}, nil
	}
	return agent.ToolResult{Content: "unknown action", IsError: true}, nil
}

func (t *ScheduleTool) RequiresApproval() bool {
	return t.action != "list"
}

func (t *ScheduleTool) IsReadOnlyCall(string) bool {
	return t.action == "list"
}

// extractConversationContext 从当前对话快照中提取精简的上下文消息。
// 只保留 user/assistant 的纯文本内容，跳过 system、tool_use、tool_result。
// 最多保留最近 20 条消息，总文本不超过 8000 字符。
func extractConversationContext(ctx context.Context) []schedule.ContextMessage {
	snapshotFn := agent.ConversationSnapshotFromContext(ctx)
	if snapshotFn == nil {
		return nil
	}
	messages := snapshotFn()
	if len(messages) == 0 {
		return nil
	}

	// 过滤：只保留 user/assistant 的纯文本消息
	var filtered []schedule.ContextMessage
	for _, msg := range messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		// 跳过包含 tool_use/tool_result 块但无文本的消息
		if msg.Content.HasBlocks() {
			hasText := false
			for _, b := range msg.Content.Blocks() {
				if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
					hasText = true
					break
				}
			}
			if !hasText {
				continue
			}
		}
		text := strings.TrimSpace(msg.Content.Text())
		if text == "" {
			continue
		}
		filtered = append(filtered, schedule.ContextMessage{
			Role:    msg.Role,
			Content: text,
		})
	}

	// 截取最近 20 条
	const maxMessages = 20
	if len(filtered) > maxMessages {
		filtered = filtered[len(filtered)-maxMessages:]
	}

	// 截断总文本到 8000 字符（从最早的开始丢弃）
	const maxChars = 8000
	totalChars := 0
	for _, m := range filtered {
		totalChars += len(m.Content)
	}
	for totalChars > maxChars && len(filtered) > 1 {
		totalChars -= len(filtered[0].Content)
		filtered = filtered[1:]
	}

	return filtered
}
