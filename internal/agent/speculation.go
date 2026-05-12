package agent

import (
	"context"
	"fmt"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

// BuildSpeculationRequest returns a CompletionRequest that pre-runs the next
// assistant turn assuming the user accepts the suggestion text. Thin wrapper
// over BuildForkedRequest — the appended message is the actual suggestion
// (not the meta SuggestionPrompt), so the model produces a real response we
// can serve instantly if the user accepts.
//
// CACHE SAFETY: inherits the BuildForkedRequest invariant — byte-equal to main
// except for the one appended message, SkipCacheWrite, and ForkedKind.
func BuildSpeculationRequest(main client.CompletionRequest, suggestionText string) client.CompletionRequest {
	out, _ := BuildForkedRequest(main, ForkOptions{
		AppendMessages: []client.Message{{Role: "user", Content: client.NewTextContent(suggestionText)}},
		SkipCacheWrite: true,
		DebugKind:      "speculation",
	})
	return out
}

// RunSpeculation runs a single forked LLM call that pre-computes the
// assistant's response to suggestionText. Returns the assistant's content
// or empty string + error on gateway failure. No filter is applied — the
// output is destined for display verbatim if accepted.
//
// MVP scope: single Complete() call, no tool execution. Speculation that
// would have triggered tool_use is shown as the raw text leading up to the
// tool_use block (truncated at that boundary). A future Phase can extend
// to a full forked AgentLoop run with skipTranscript.
func RunSpeculation(ctx context.Context, llm client.LLMClient, main client.CompletionRequest, suggestionText string) (string, error) {
	req := BuildSpeculationRequest(main, suggestionText)

	resp, err := llm.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("speculation gateway call: %w", err)
	}
	if resp == nil || resp.OutputText == "" {
		return "", nil
	}
	return resp.OutputText, nil
}
