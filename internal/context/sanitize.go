package context

import (
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
	"github.com/Kocoro-lab/ShanClaw/internal/runstatus"
)

// SanitizeHistory repairs malformed message history that would cause API errors.
// Specifically handles:
//   - tool role messages with plain text (no tool_result blocks) → dropped
//   - assistant messages that are just "[tool_call: ...]" placeholders → dropped
//   - consecutive assistant messages without intervening user → merged into one
//   - assistant error messages (friendly run-failure output) → dropped
//   - orphaned tool_use blocks (no matching tool_result follows) → stripped
//
// Returns a new slice; the original is not modified.
func SanitizeHistory(messages []client.Message) []client.Message {
	if len(messages) == 0 {
		return messages
	}

	// First pass: drop invalid messages.
	var cleaned []client.Message
	for _, msg := range messages {
		if shouldDrop(msg) {
			continue
		}
		cleaned = append(cleaned, msg)
	}

	// Second pass: fix consecutive same-role messages.
	// Claude API requires strict user/assistant alternation.
	merged := mergeConsecutiveRoles(cleaned)

	// Third pass: strip orphaned tool_use and tool_result blocks.
	// Runs after role merging so adjacency checks are reliable.
	// Both Anthropic and OpenAI reject conversations where tool_use/tool_result
	// blocks lack their matching counterpart.
	stripped := stripOrphanedToolPairs(merged)

	// Final pass: stripping may create new consecutive same-role sequences
	// (e.g. dropping an empty assistant leaves two adjacent user messages).
	result := mergeConsecutiveRoles(stripped)

	return result
}

// stripOrphanedToolPairs removes unpaired tool_use and tool_result blocks.
// A tool_use in assistant[i] is valid only if messages[i+1] is a user message
// containing a tool_result with the same ID, and vice versa. Pairing is
// per-position: the same ID reused in a non-adjacent pair does not count.
// If stripping leaves a message with no content, it is dropped.
func stripOrphanedToolPairs(messages []client.Message) []client.Message {
	// Per-message set of valid tool IDs. An ID is valid at position i only
	// if it forms a proper adjacent pair (assistant[i] ↔ user[i+1]).
	validAt := make([]map[string]bool, len(messages))

	for i := 0; i+1 < len(messages); i++ {
		if messages[i].Role != "assistant" || !messages[i].Content.HasBlocks() {
			continue
		}
		next := messages[i+1]
		if next.Role != "user" || !next.Content.HasBlocks() {
			continue
		}

		useIDs := make(map[string]bool)
		for _, b := range messages[i].Content.Blocks() {
			if b.Type == "tool_use" && b.ID != "" {
				useIDs[b.ID] = true
			}
		}

		for _, b := range next.Content.Blocks() {
			if b.Type == "tool_result" && b.ToolUseID != "" && useIDs[b.ToolUseID] {
				if validAt[i] == nil {
					validAt[i] = make(map[string]bool)
				}
				if validAt[i+1] == nil {
					validAt[i+1] = make(map[string]bool)
				}
				validAt[i][b.ToolUseID] = true
				validAt[i+1][b.ToolUseID] = true
			}
		}
	}

	var out []client.Message
	for i, msg := range messages {
		if !msg.Content.HasBlocks() {
			out = append(out, msg)
			continue
		}

		switch msg.Role {
		case "assistant":
			kept := stripUnpairedBlocks(msg.Content.Blocks(), "tool_use", validAt[i])
			if kept == nil {
				continue
			}
			out = append(out, client.Message{Role: msg.Role, Content: client.NewBlockContent(kept)})

		case "user":
			kept := stripUnpairedBlocks(msg.Content.Blocks(), "tool_result", validAt[i])
			if kept == nil {
				continue
			}
			out = append(out, client.Message{Role: msg.Role, Content: client.NewBlockContent(kept)})

		default:
			out = append(out, msg)
		}
	}
	return out
}

// stripUnpairedBlocks removes blocks of blockType whose ID is not in validIDs
// for this position. For tool_use, checks block.ID; for tool_result, checks
// block.ToolUseID. Returns nil if no blocks remain.
func stripUnpairedBlocks(blocks []client.ContentBlock, blockType string, validIDs map[string]bool) []client.ContentBlock {
	hasOrphan := false
	for _, b := range blocks {
		if b.Type != blockType {
			continue
		}
		id := toolBlockID(b)
		if id != "" && !validIDs[id] {
			hasOrphan = true
			break
		}
	}
	if !hasOrphan {
		return blocks
	}

	var kept []client.ContentBlock
	for _, b := range blocks {
		if b.Type == blockType {
			id := toolBlockID(b)
			if id != "" && !validIDs[id] {
				continue
			}
		}
		kept = append(kept, b)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// toolBlockID returns the tool pairing ID for a block: ID for tool_use,
// ToolUseID for tool_result.
func toolBlockID(b client.ContentBlock) string {
	switch b.Type {
	case "tool_use":
		return b.ID
	case "tool_result":
		return b.ToolUseID
	}
	return ""
}

// mergeConsecutiveRoles collapses consecutive same-role messages, keeping the later one.
func mergeConsecutiveRoles(messages []client.Message) []client.Message {
	var out []client.Message
	for i, msg := range messages {
		if i > 0 && msg.Role == messages[i-1].Role {
			switch msg.Role {
			case "assistant", "user":
				out[len(out)-1] = msg
				continue
			}
		}
		out = append(out, msg)
	}
	return out
}

// shouldDrop returns true for messages that are malformed or would cause API errors.
func shouldDrop(msg client.Message) bool {
	text := msg.Content.Text()

	switch msg.Role {
	case "tool":
		// Legacy tool-role messages are from old heartbeat persistence.
		// The current protocol uses user-role messages with tool_result blocks.
		// Drop all tool-role messages unconditionally — they are not recognized
		// by the pairing pass and would be rejected by the API.
		return true

	case "assistant":
		// Drop placeholder tool call text (from old heartbeat bug).
		if strings.HasPrefix(text, "[tool_call:") {
			return true
		}
		// Drop error marker from old heartbeat failures.
		if text == "[error: agent failed to respond]" {
			return true
		}
		// Drop persisted friendly run-failure messages — they contain no useful
		// context and just waste tokens.
		if isFriendlyError(text) {
			return true
		}
	}

	return false
}

// isFriendlyError returns true for friendly run-failure messages.
func isFriendlyError(text string) bool {
	return runstatus.IsFriendlyMessage(text)
}
