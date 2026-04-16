package context

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

const summarizePrompt = `Compress the following conversation into a concise summary using a two-phase approach.

Phase 1 — Write a chronological analysis inside <analysis> tags:
- Walk through the conversation in order
- Note every user correction, decision, or preference change
- Track files read, modified, or created
- Record errors, blockers, and their resolutions

Phase 2 — Write the final summary inside <summary> tags:
- Distill the analysis into what a continuation needs to know
- Preserve user corrections and decisions (these are highest priority)
- Include current task state and next steps
- Be factual and brief

Format your response as:
<analysis>
[chronological walkthrough]
</analysis>
<summary>
[concise summary for continuation]
</summary>`

// Completer is the interface for making LLM completion calls.
// Satisfied by *client.GatewayClient.
type Completer interface {
	Complete(ctx context.Context, req client.CompletionRequest) (*client.CompletionResponse, error)
}

// UsageReporter receives the usage payload from successful helper-model calls.
// Callers decide how to aggregate or persist it.
type UsageReporter func(client.Usage, string)

// buildTranscript 将消息序列化为文本 transcript，跳过 system 消息。
func buildTranscript(messages []client.Message) string {
	var sb strings.Builder
	for _, m := range messages {
		if m.Role == "system" {
			continue
		}
		if t := messageText(m); t != "" {
			fmt.Fprintf(&sb, "[%s]: %s\n\n", m.Role, t)
		}
	}
	return sb.String()
}

// GenerateSummary calls the LLM (small tier) to summarize a conversation.
// It strips the system message from the input to avoid wasting tokens.
// Serializes both plain text and block content (tool_use, tool_result).
func GenerateSummary(ctx context.Context, c Completer, messages []client.Message) (string, error) {
	return GenerateSummaryWithUsage(ctx, c, messages, nil)
}

// GenerateSummaryWithUsage is GenerateSummary plus an optional usage callback
// for callers that need to account for helper-model work.
func GenerateSummaryWithUsage(ctx context.Context, c Completer, messages []client.Message, report UsageReporter) (string, error) {
	req := client.CompletionRequest{
		Messages: []client.Message{
			{Role: "system", Content: client.NewTextContent(summarizePrompt)},
			{Role: "user", Content: client.NewTextContent(buildTranscript(messages))},
		},
		ModelTier:   "small",
		Temperature: 0.2,
		MaxTokens:   2000,
	}

	resp, err := c.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarization failed: %w", err)
	}
	if report != nil {
		report(resp.Usage, resp.Model)
	}

	return extractSummary(resp.OutputText), nil
}

const userSummarizePrompt = `You are a conversation summarizer. Read the following conversation and produce a clear, well-structured Markdown summary for a human reader.

Requirements:
- Write in the SAME LANGUAGE as the conversation (if the conversation is in Chinese, write in Chinese; if in English, write in English, etc.)
- Use Markdown formatting with headers and bullet points
- Focus on: what was discussed, key decisions made, work completed, and remaining action items
- Be concise but comprehensive — a reader should understand the conversation's outcome without reading the full transcript
- Do NOT include internal LLM terminology (tool_call, context window, tokens, etc.)
- Do NOT wrap the output in code fences — output raw Markdown directly`

// SummarizeForUser 调用 LLM 生成面向人类阅读的会话摘要。
func SummarizeForUser(ctx context.Context, c Completer, messages []client.Message) (string, error) {
	req := client.CompletionRequest{
		Messages: []client.Message{
			{Role: "system", Content: client.NewTextContent(userSummarizePrompt)},
			{Role: "user", Content: client.NewTextContent(buildTranscript(messages))},
		},
		ModelTier:   "small",
		Temperature: 0.2,
		MaxTokens:   2000,
	}

	resp, err := c.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("user summarization failed: %w", err)
	}

	return strings.TrimSpace(resp.OutputText), nil
}

// extractSummary extracts the <summary> content from a two-phase response.
// If <summary> tags are present, returns their content.
// If missing, strips any <analysis> block and returns the remainder.
// Never returns raw <analysis> content — ShapeHistory injects the summary verbatim.
func extractSummary(raw string) string {
	raw = strings.TrimSpace(raw)

	// Try to extract <summary>...</summary>
	if start := strings.Index(raw, "<summary>"); start >= 0 {
		after := raw[start+len("<summary>"):]
		if end := strings.Index(after, "</summary>"); end >= 0 {
			return strings.TrimSpace(after[:end])
		}
		// Opening tag but no closing — take everything after the tag
		return strings.TrimSpace(after)
	}

	// No <summary> tags — strip <analysis>...</analysis> and return remainder
	result := raw
	for {
		start := strings.Index(result, "<analysis>")
		if start < 0 {
			break
		}
		end := strings.Index(result, "</analysis>")
		if end < 0 {
			// Opening tag but no closing — strip from <analysis> onward
			result = result[:start]
			break
		}
		result = result[:start] + result[end+len("</analysis>"):]
	}

	result = strings.TrimSpace(result)
	if result == "" {
		// Everything was analysis with no summary — return empty.
		// ShapeHistory handles empty summaries gracefully (sliding window only).
		// Returning raw here would leak <analysis> scratch work into context.
		return ""
	}
	return result
}

// messageText extracts readable text from a message, handling both plain text
// and block content (tool_use, tool_result, text blocks).
func messageText(m client.Message) string {
	// Plain text message
	if !m.Content.HasBlocks() {
		return m.Content.Text()
	}

	// Block content — serialize each block type
	var sb strings.Builder
	for _, b := range m.Content.Blocks() {
		switch b.Type {
		case "text":
			sb.WriteString(b.Text)
		case "tool_use":
			fmt.Fprintf(&sb, "[tool_call: %s]", b.Name)
		case "tool_result":
			text := client.ToolResultText(b)
			if text != "" {
				// Truncate long tool results for the summary (rune-safe)
				if r := []rune(text); len(r) > 500 {
					text = string(r[:500]) + "..."
				}
				fmt.Fprintf(&sb, "[tool_result: %s]", text)
			}
		}
		sb.WriteString(" ")
	}
	return strings.TrimSpace(sb.String())
}
