package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Kocoro-lab/shan/internal/agent"
)

// MemoryAppendTool appends entries to the agent's MEMORY.md under flock,
// preventing concurrent sessions from clobbering each other's writes.
// The memory directory is read from context (set by AgentLoop.Run).
type MemoryAppendTool struct{}

type memoryAppendArgs struct {
	Content string `json:"content"`
}

func (t *MemoryAppendTool) Info() agent.ToolInfo {
	return agent.ToolInfo{
		Name:        "memory_append",
		Description: "Append new entries to MEMORY.md. Use this instead of file_write or file_edit for memory updates. Writes are atomic and safe under concurrent sessions.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "New entries to append (markdown bullet points)",
				},
			},
		},
		Required: []string{"content"},
	}
}

func (t *MemoryAppendTool) Run(ctx context.Context, argsJSON string) (agent.ToolResult, error) {
	var args memoryAppendArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if strings.TrimSpace(args.Content) == "" {
		return agent.ToolResult{Content: "content must not be empty", IsError: true}, nil
	}

	memPath, err := memoryPathFromContext(ctx)
	if err != nil {
		return agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	if err := flockAppend(memPath, args.Content); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error appending: %v", err), IsError: true}, nil
	}

	return agent.ToolResult{Content: fmt.Sprintf("appended to %s", memPath)}, nil
}

func (t *MemoryAppendTool) RequiresApproval() bool { return false }

// memoryPathFromContext resolves the MEMORY.md path from the agent's memory
// directory stored in context. Returns an error if memory is not configured.
func memoryPathFromContext(ctx context.Context) (string, error) {
	dir := agent.MemoryDirFromContext(ctx)
	if dir == "" {
		return "", fmt.Errorf("memory not configured for this agent")
	}
	return filepath.Join(dir, "MEMORY.md"), nil
}

func flockAppend(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) //nolint:errcheck

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	// Ensure content starts on a new line
	if !strings.HasPrefix(content, "\n") {
		content = "\n" + content
	}

	_, err = f.WriteString(content)
	return err
}
