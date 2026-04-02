package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kocoro-lab/ShanClaw/internal/cwdctx"
)

func TestFileRead_ResolvesRelativePathFromSessionCWD(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)

	ctx := cwdctx.WithSessionCWD(context.Background(), dir)
	tool := &FileReadTool{}
	result, err := tool.Run(ctx, `{"path":"test.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content == "" {
		t.Fatal("expected file content")
	}
}

func TestGlob_ResolvesRelativeRootFromSessionCWD(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	ctx := cwdctx.WithSessionCWD(context.Background(), dir)
	tool := &GlobTool{}
	result, err := tool.Run(ctx, `{"pattern":"*.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "main.go" {
		t.Fatalf("expected main.go, got %q", result.Content)
	}
}

func TestDirectoryList_ResolvesRelativePathFromSessionCWD(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)

	ctx := cwdctx.WithSessionCWD(context.Background(), dir)
	tool := &DirectoryListTool{}
	result, err := tool.Run(ctx, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.Content == "" {
		t.Fatalf("unexpected: error=%v content=%q", result.IsError, result.Content)
	}
}

func TestGrep_ResolvesRelativePathFromSessionCWD(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("findme here\n"), 0644)

	ctx := cwdctx.WithSessionCWD(context.Background(), dir)
	tool := &GrepTool{}
	result, err := tool.Run(ctx, `{"pattern":"findme"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}
