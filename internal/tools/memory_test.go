package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeFallback struct{ snippet string }

func (f *fakeFallback) SessionKeyword(_ context.Context, _ string, _ int) ([]any, error) {
	return nil, nil
}
func (f *fakeFallback) MemoryFileSnippet(_ context.Context, _ string) (string, error) {
	return f.snippet, nil
}

func TestMemoryTool_FallbackWhenNoService(t *testing.T) {
	tool := &MemoryTool{Fallback: &fakeFallback{snippet: "memory.md note"}}
	res, err := tool.Run(context.Background(), `{"anchor_mentions":["x"]}`)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.IsError {
		t.Fatalf("res.IsError=true: %s", res.Content)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(res.Content), &body); err != nil {
		t.Fatalf("decode tool result: %v\n%s", err, res.Content)
	}
	if body["source"] != "fallback" {
		t.Fatalf("source=%v want fallback", body["source"])
	}
	if body["evidence_quality"] != "text_search" {
		t.Fatalf("evidence_quality=%v", body["evidence_quality"])
	}
}

func TestMemoryTool_RejectsEmptyAnchorMentions(t *testing.T) {
	tool := &MemoryTool{Fallback: &fakeFallback{}}
	res, _ := tool.Run(context.Background(), `{"anchor_mentions":[]}`)
	if !res.IsError || !strings.Contains(res.Content, "anchor_mentions") {
		t.Fatalf("res=%+v", res)
	}
}

func TestMemoryTool_RejectsMissingAnchorMentions(t *testing.T) {
	tool := &MemoryTool{Fallback: &fakeFallback{}}
	res, _ := tool.Run(context.Background(), `{}`)
	if !res.IsError {
		t.Fatalf("expected error: %s", res.Content)
	}
}

func TestMemoryTool_RejectsMalformedJSON(t *testing.T) {
	tool := &MemoryTool{Fallback: &fakeFallback{}}
	res, _ := tool.Run(context.Background(), `{not json`)
	if !res.IsError {
		t.Fatalf("expected error: %s", res.Content)
	}
}

func TestMemoryTool_Info(t *testing.T) {
	tool := &MemoryTool{Fallback: &fakeFallback{}}
	info := tool.Info()
	if info.Name != "memory_recall" {
		t.Fatalf("name=%q want memory_recall", info.Name)
	}
	if !tool.IsReadOnlyCall("") {
		t.Fatal("memory_recall must be read-only")
	}
	if tool.RequiresApproval() {
		t.Fatal("memory_recall must not require approval")
	}
	// Required field declared.
	found := false
	for _, r := range info.Required {
		if r == "anchor_mentions" {
			found = true
		}
	}
	if !found {
		t.Fatal("anchor_mentions must be in Required")
	}
}
