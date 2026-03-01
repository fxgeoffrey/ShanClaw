package agent

import (
	"context"
	"testing"

	"github.com/Kocoro-lab/shan/internal/client"
)

func TestToolRegistry_Get(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockTool{name: "file_read"})

	tool, ok := reg.Get("file_read")
	if !ok {
		t.Fatal("expected to find file_read")
	}
	if tool.Info().Name != "file_read" {
		t.Errorf("expected 'file_read', got %q", tool.Info().Name)
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestToolRegistry_Schemas(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockTool{name: "file_read"})
	reg.Register(&mockTool{name: "bash"})

	schemas := reg.Schemas()
	if len(schemas) != 2 {
		t.Errorf("expected 2 schemas, got %d", len(schemas))
	}
}

type mockTool struct {
	name string
}

func (m *mockTool) Info() ToolInfo {
	return ToolInfo{
		Name:        m.name,
		Description: "mock tool",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (m *mockTool) Run(ctx context.Context, args string) (ToolResult, error) {
	return ToolResult{Content: "mock result"}, nil
}

func (m *mockTool) RequiresApproval() bool { return false }

type mockNativeTool struct {
	name string
}

func (m *mockNativeTool) Info() ToolInfo {
	return ToolInfo{Name: m.name, Description: "native tool"}
}
func (m *mockNativeTool) Run(ctx context.Context, args string) (ToolResult, error) {
	return ToolResult{Content: "ok"}, nil
}
func (m *mockNativeTool) RequiresApproval() bool { return false }
func (m *mockNativeTool) NativeToolDef() *client.NativeToolDef {
	return &client.NativeToolDef{
		Type:            "computer_20251124",
		Name:            "computer",
		DisplayWidthPx:  1280,
		DisplayHeightPx: 800,
	}
}

func TestToolRegistry_SchemasIncludesNativeTool(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockNativeTool{name: "computer"})
	reg.Register(&mockTool{name: "bash"})

	schemas := reg.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}
	// Native tool should use its own type
	if schemas[0].Type != "computer_20251124" {
		t.Errorf("expected type 'computer_20251124', got %q", schemas[0].Type)
	}
	if schemas[0].Name != "computer" {
		t.Errorf("expected name 'computer', got %q", schemas[0].Name)
	}
	if schemas[0].DisplayWidthPx != 1280 {
		t.Errorf("expected display_width_px 1280, got %d", schemas[0].DisplayWidthPx)
	}
	// Standard tool should use function type
	if schemas[1].Type != "function" {
		t.Errorf("expected type 'function' for bash, got %q", schemas[1].Type)
	}
}

func TestToolResult_ImagesField(t *testing.T) {
	result := ToolResult{
		Content: "Screenshot captured",
		IsError: false,
		Images: []ImageBlock{
			{MediaType: "image/png", Data: "iVBORfakedata"},
		},
	}
	if len(result.Images) != 1 {
		t.Errorf("expected 1 image, got %d", len(result.Images))
	}
	if result.Images[0].MediaType != "image/png" {
		t.Errorf("expected image/png, got %s", result.Images[0].MediaType)
	}
}
