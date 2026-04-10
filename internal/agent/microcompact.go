package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
	ctxwin "github.com/Kocoro-lab/ShanClaw/internal/context"
)

const (
	// microCompactMarker is prefixed to LLM-summarized tool results to prevent
	// re-summarization on subsequent compaction passes.
	microCompactMarker = "[micro-compact] "

	// microCompactMinChars is the minimum content length for a Tier 2 result
	// to be eligible for LLM summarization. Below this, head+tail is fine.
	microCompactMinChars = 2000

	// microCompactMaxPerPass caps LLM call *attempts* per compressOldToolResults
	// invocation to prevent latency spikes. Counts both successes and failures.
	microCompactMaxPerPass = 2
)

// microCompactSkipTools lists tools whose results should never be micro-compacted.
// think: internal reasoning, not factual — summarization destroys the purpose.
// cloud_delegate: deliverables for the user, not agent working memory.
// file_read, grep, glob, directory_list: code/search/repo-inspection results where
// the model needs actual content (paths, signatures, line numbers), not summaries.
// browser_*: DOM snapshots and page state ARE the model's eyes for web tasks —
// summarizing them into "the browser navigated to X" blinds the model mid-task.
// These always get mechanical head+tail truncation in Tier 2.
var microCompactSkipTools = map[string]bool{
	"think":                 true,
	"cloud_delegate":        true,
	"file_read":             true,
	"grep":                  true,
	"glob":                  true,
	"directory_list":        true,
	"browser_navigate":      true,
	"browser_navigate_back": true,
	"browser_snapshot":      true,
	"browser_click":         true,
	"browser_type":          true,
	"browser_wait_for":      true,
	"browser_fill_form":     true,
	"browser_hover":         true,
	"browser_mouse_wheel":   true,
	"browser_select_option": true,
	"browser_press_key":     true,
	"browser_tabs":          true,
	"browser_evaluate":      true,
}

const microCompactPrompt = `Summarize this tool result in 1-2 sentences. Preserve exact error strings, file paths, URLs, IDs, and numbers when present. Focus on the final outcome or conclusion.

Tool: %s
Result:
%s`

// microCompactResult uses the small LLM tier to produce a 1-2 sentence semantic
// summary of a tool result. Returns ("", false) if summarization fails or is
// skipped, signaling the caller to fall back to mechanical truncation.
func microCompactResult(ctx context.Context, c ctxwin.Completer, toolName, content string) (string, bool) {
	if c == nil {
		return "", false
	}

	prompt := fmt.Sprintf(microCompactPrompt, toolName, content)

	resp, err := c.Complete(ctx, client.CompletionRequest{
		Messages: []client.Message{
			{Role: "user", Content: client.NewTextContent(prompt)},
		},
		ModelTier:   "small",
		Temperature: 0.0,
		MaxTokens:   200,
	})
	if err != nil || resp.OutputText == "" {
		return "", false
	}

	summary := strings.TrimSpace(resp.OutputText)
	if summary == "" {
		return "", false
	}

	return microCompactMarker + summary, true
}

// isMicroCompacted returns true if the content was already summarized by micro-compact.
func isMicroCompacted(content string) bool {
	return strings.HasPrefix(content, microCompactMarker)
}
