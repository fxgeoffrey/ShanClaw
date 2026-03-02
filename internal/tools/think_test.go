package tools

import (
	"context"
	"testing"
)

func TestThinkTool_ReturnsThought(t *testing.T) {
	tool := &ThinkTool{}

	result, err := tool.Run(context.Background(), `{"thought":"I should read the file first"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if result.Content != "I should read the file first" {
		t.Errorf("expected thought text back, got %q", result.Content)
	}
}

func TestThinkTool_EmptyThought(t *testing.T) {
	tool := &ThinkTool{}

	result, err := tool.Run(context.Background(), `{"thought":""}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for empty thought")
	}
}

func TestThinkTool_InvalidJSON(t *testing.T) {
	tool := &ThinkTool{}

	result, err := tool.Run(context.Background(), `not json`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestThinkTool_Info(t *testing.T) {
	tool := &ThinkTool{}
	info := tool.Info()

	if info.Name != "think" {
		t.Errorf("expected name 'think', got %q", info.Name)
	}
	if tool.RequiresApproval() {
		t.Error("think tool should not require approval")
	}
}
