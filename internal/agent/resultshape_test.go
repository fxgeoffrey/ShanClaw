package agent

import (
	"strings"
	"testing"
)

func TestShapeContextResult_DefaultPassthrough(t *testing.T) {
	content := "plain tool output"
	shaped := shapeContextResult("mock_tool", content, nil)
	if shaped.Text != content {
		t.Fatalf("expected default profile passthrough, got %q", shaped.Text)
	}
	if shaped.Signature != "" {
		t.Fatalf("expected no signature for default profile, got %q", shaped.Signature)
	}
}

func TestShapeTreeResult_SummarizesLargeTree(t *testing.T) {
	content := strings.Repeat("button ref=e1234 label=Open\n", 150)
	shaped := shapeTreeResult(content, nil)

	if !strings.Contains(shaped.Text, "[tree snapshot summary;") {
		t.Fatalf("expected tree summary header, got %q", shaped.Text)
	}
	if strings.Contains(shaped.Text, "ref=e1234") {
		t.Fatal("expected volatile ref IDs to be normalized out of the shaped result")
	}
	if !strings.Contains(shaped.Text, "ref=*") {
		t.Fatal("expected shaped tree excerpt to retain normalized ref markers")
	}
	if shaped.Signature == "" {
		t.Fatal("expected tree signature")
	}
}

func TestShapeTreeResult_CollapsesUnchangedReads(t *testing.T) {
	content := strings.Repeat("button ref=e1234 label=Open\n", 150)
	first := shapeTreeResult(content, nil)
	second := shapeTreeResult(content, &first)

	if !strings.Contains(second.Text, "unchanged since last read") {
		t.Fatalf("expected unchanged collapse, got %q", second.Text)
	}
	if second.Signature != first.Signature {
		t.Fatalf("expected matching signatures, got %q vs %q", second.Signature, first.Signature)
	}
}
