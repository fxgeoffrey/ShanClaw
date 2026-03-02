package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Kocoro-lab/shan/internal/audit"
	"github.com/Kocoro-lab/shan/internal/client"
	"github.com/Kocoro-lab/shan/internal/hooks"
	"github.com/Kocoro-lab/shan/internal/instructions"
	"github.com/Kocoro-lab/shan/internal/permissions"
	"github.com/Kocoro-lab/shan/internal/prompt"
)

// ErrMaxIterReached is returned when the agent loop hits the iteration limit
// but has partial work to return. Callers can check errors.Is(err, ErrMaxIterReached)
// to distinguish truncated results from hard failures.
var ErrMaxIterReached = errors.New("agent loop reached iteration limit")

const baseSystemPrompt = `You are Shannon, an AI assistant running in a CLI terminal on the user's macOS computer. You have both local tools (file ops, shell, GUI control) and remote server tools (web search, research, analytics, multi-agent workflows).

## Core Rules
- Always use tools to perform actions. Never claim you did something without a tool call.
- Be concise. Summarize tool results — do not echo raw output.
- Read before modifying: use file_read before file_edit, screenshot before unfamiliar GUI interactions.
- Avoid over-engineering. Only do what was asked.

## Verification & Stopping
- After GUI actions (applescript, computer), take a screenshot to verify the outcome before proceeding. Think: "Did the expected change happen?"
- If an action fails or produces no visible change after 2 attempts, STOP. Try a fundamentally different method, or ask the user.
- Do not brute-force a blocked approach. Consider alternatives or ask the user.
- If a tool call is denied, do not re-attempt the same call.

## Multi-Step Tasks
- For complex tasks, state your plan AND include tool calls in the same response. Do not output a plan without acting on it.
- After each step, verify the outcome before proceeding to the next.
- When multiple tool calls are independent, make them in parallel.

## Task Routing
When a task would benefit from specialized processing beyond your local tools, suggest the appropriate command:
- /research [quick|standard] <query> — deep research, multi-source analysis, fact-checking. Use when the user needs thorough investigation across many sources.
- /swarm <query> — multi-agent collaboration for complex workflows requiring coordination, decomposition, or multiple perspectives.
- Keep direct actions (file ops, shell, GUI) for yourself. Only suggest routing for tasks that genuinely need it.

## Tool Selection

### Files & Code
- file_read, file_write, file_edit: file operations. Always read before editing.
- glob: find files by pattern. Use instead of find/ls.
- grep: search file contents. Use instead of grep/rg in bash.
- directory_list: list directory contents.
- bash: shell commands, tests, builds. Only when no dedicated tool exists.

### GUI & Desktop (macOS)
- applescript: open/control apps, window management. Returns text only — no visual feedback. Follow with screenshot to verify.
- screenshot: capture the screen. Use to verify GUI state or show the user what you see.
- computer: mouse/keyboard control (click, type, hotkey, move). Returns a screenshot after each action automatically. Use for precise GUI interaction when applescript cannot target an element.
- notify: macOS notifications.
- clipboard: system clipboard read/write.

### Web & Network
- Server-side tools (web_search, web_fetch, etc.) are preferred for web tasks — faster and more reliable.
- For logged-in or interactive sites: applescript to open browser + screenshot + computer to interact.
- browser: isolated headless Chrome, no cookies/sessions. Only for own sites or simple fetches — public sites block with CAPTCHA.
- http: direct HTTP requests.

### System
- system_info: OS/hardware information.
- process: list/manage running processes.`

type TurnUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	CostUSD      float64
	LLMCalls     int
}

type EventHandler interface {
	OnToolCall(name string, args string)
	OnToolResult(name string, args string, result ToolResult, elapsed time.Duration)
	OnText(text string)
	OnStreamDelta(delta string)
	OnApprovalNeeded(tool string, args string) bool
	OnUsage(usage TurnUsage)
}

type AgentLoop struct {
	client            *client.GatewayClient
	tools             *ToolRegistry
	modelTier         string
	handler           EventHandler
	shannonDir        string
	maxIter           int
	resultTrunc       int
	argsTrunc         int
	permissions       *permissions.PermissionsConfig
	auditor           *audit.AuditLogger
	hookRunner        *hooks.HookRunner
	mcpContext        string
	bypassPermissions bool
	enableStreaming    bool
}

func NewAgentLoop(gw *client.GatewayClient, tools *ToolRegistry, modelTier string, shannonDir string, maxIter int, resultTrunc int, argsTrunc int, perms *permissions.PermissionsConfig, auditor *audit.AuditLogger, hookRunner *hooks.HookRunner) *AgentLoop {
	if maxIter <= 0 {
		maxIter = 25
	}
	if resultTrunc <= 0 {
		resultTrunc = 2000
	}
	if argsTrunc <= 0 {
		argsTrunc = 200
	}
	return &AgentLoop{
		client:      gw,
		tools:       tools,
		modelTier:   modelTier,
		shannonDir:  shannonDir,
		maxIter:     maxIter,
		resultTrunc: resultTrunc,
		argsTrunc:   argsTrunc,
		permissions: perms,
		auditor:     auditor,
		hookRunner:  hookRunner,
	}
}

func (a *AgentLoop) SetHandler(h EventHandler) {
	a.handler = h
}

func (a *AgentLoop) SetModelTier(tier string) {
	a.modelTier = tier
}

func (a *AgentLoop) SetMCPContext(ctx string) {
	a.mcpContext = ctx
}

func (a *AgentLoop) SetBypassPermissions(bypass bool) {
	a.bypassPermissions = bypass
}

func (a *AgentLoop) SetEnableStreaming(enable bool) {
	a.enableStreaming = enable
}

func (a *AgentLoop) Run(ctx context.Context, userMessage string, history []client.Message) (string, *TurnUsage, error) {
	// Build system prompt using prompt builder with instructions/memory
	var toolNames []string
	for _, t := range a.tools.All() {
		toolNames = append(toolNames, t.Info().Name)
	}

	cwd, _ := os.Getwd()
	memory, _ := instructions.LoadMemory(a.shannonDir, 200)
	instrText, _ := instructions.LoadInstructions(a.shannonDir, ".", 4000)

	systemPrompt := prompt.BuildSystemPrompt(prompt.PromptOptions{
		BasePrompt:   baseSystemPrompt,
		Memory:       memory,
		Instructions: instrText,
		ToolNames:    toolNames,
		MCPContext:   a.mcpContext,
		CWD:          cwd,
	})

	messages := make([]client.Message, 0)
	messages = append(messages, client.Message{Role: "system", Content: client.NewTextContent(systemPrompt)})
	if history != nil {
		messages = append(messages, history...)
	}
	messages = append(messages, client.Message{Role: "user", Content: client.NewTextContent(userMessage)})

	toolSchemas := a.tools.Schemas()
	usage := &TurnUsage{}

	// Loop behavior constants
	const (
		maxExactDuplicates     = 3 // stop after N identical consecutive tool calls (same name + args)
		maxSameToolCalls       = 8 // stop after N consecutive calls to same tool (different args)
		maxConsecutiveTextOnly = 2 // allows N-1 plan continuations before stopping
	)

	// GUI/automation tools that naturally require many repeated calls —
	// exempt from the same-tool counter (sameToolCount) since screenshot→action
	// loops are normal. Note: exact-duplicate detection (dupCount) still applies
	// to repeatable tools — identical calls even for GUI tools are suspicious.
	repeatableTools := map[string]bool{
		"screenshot": true, "computer": true, "applescript": true, "browser": true,
	}

	// Loop detection state
	var (
		lastToolCall        string // exact signature (name+args)
		lastToolName        string // just the tool name
		dupCount            int    // consecutive exact-duplicate count
		sameToolCount       int    // consecutive same-tool count (any args)
		totalToolCalls      int    // total tool calls executed this turn
		consecutiveTextOnly int    // consecutive planning responses (before tool use)
		lastText            string // last text output for graceful degradation
	)

	for i := 0; i < a.maxIter; i++ {
		// Call LLM — streaming or blocking
		var resp *client.CompletionResponse
		var err error
		req := client.CompletionRequest{
			Messages:  messages,
			ModelTier: a.modelTier,
			Tools:     toolSchemas,
		}
		if a.enableStreaming && a.handler != nil {
			resp, err = a.client.CompleteStream(ctx, req, func(delta client.StreamDelta) {
				a.handler.OnStreamDelta(delta.Text)
			})
			// Fall back to non-streaming if gateway doesn't support it
			if err != nil {
				resp, err = a.client.Complete(ctx, req)
			}
		} else {
			resp, err = a.client.Complete(ctx, req)
		}
		if err != nil {
			return "", usage, fmt.Errorf("LLM call failed: %w", err)
		}

		usage.InputTokens += resp.Usage.InputTokens
		usage.OutputTokens += resp.Usage.OutputTokens
		usage.TotalTokens += resp.Usage.TotalTokens
		usage.CostUSD += resp.Usage.CostUSD
		usage.LLMCalls++

		// Handle text-only responses (no tool calls in this LLM response).
		//
		// Three stop/continue rules:
		// 1. After tool use (totalToolCalls > 0): text = final summary. Stop.
		// 2. Text looks like a plan (numbered steps, tool mentions): continue
		//    up to maxConsecutiveTextOnly times so the model can act on its plan.
		// 3. Otherwise: direct answer. Stop immediately.
		//
		// Plan detection is language-agnostic — uses numbered steps (1. / 1、/ 1))
		// and tool name references (always English) rather than keywords.
		if !resp.HasToolCalls() {
			if resp.OutputText != "" {
				lastText = resp.OutputText
			}
			if a.handler != nil {
				a.handler.OnText(resp.OutputText)
			}

			// After tool use, text-only = final answer.
			if totalToolCalls > 0 {
				return resp.OutputText, usage, nil
			}

			// Before tool use: continue if this looks like a plan.
			if isPlanningResponse(resp.OutputText, toolNames) {
				consecutiveTextOnly++
				if consecutiveTextOnly < maxConsecutiveTextOnly {
					messages = append(messages, client.Message{
						Role:    "assistant",
						Content: client.NewTextContent(resp.OutputText),
					})
					continue
				}
			}

			return resp.OutputText, usage, nil
		}

		// Model made tool calls — reset planning counter
		consecutiveTextOnly = 0

		// Execute all tool calls
		toolCalls := resp.AllToolCalls()
		var allResults strings.Builder
		if resp.OutputText != "" {
			allResults.WriteString(resp.OutputText)
			allResults.WriteString("\n\n")
			lastText = resp.OutputText
		}

		for _, fc := range toolCalls {
			totalToolCalls++
			argsStr := fc.ArgumentsString()

			// Loop detection
			callSig := fc.Name + ":" + argsStr
			if callSig == lastToolCall {
				dupCount++
			} else {
				lastToolCall = callSig
				dupCount = 1
			}
			if fc.Name == lastToolName {
				// GUI/automation tools naturally repeat — don't penalize them.
				if !repeatableTools[fc.Name] {
					sameToolCount++
				}
			} else {
				lastToolName = fc.Name
				sameToolCount = 1
			}
			if dupCount >= maxExactDuplicates || sameToolCount >= maxSameToolCalls {
				messages = append(messages, client.Message{
					Role:    "user",
					Content: client.NewTextContent("You've called the same tool repeatedly. Please use the results already available and provide your answer now."),
				})
				finalResp, err := a.client.Complete(ctx, client.CompletionRequest{
					Messages:  messages,
					ModelTier: a.modelTier,
				})
				if err != nil {
					return "", usage, fmt.Errorf("LLM call failed: %w", err)
				}
				usage.InputTokens += finalResp.Usage.InputTokens
				usage.OutputTokens += finalResp.Usage.OutputTokens
				usage.TotalTokens += finalResp.Usage.TotalTokens
				usage.CostUSD += finalResp.Usage.CostUSD
				usage.LLMCalls++
				if a.handler != nil {
					a.handler.OnText(finalResp.OutputText)
				}
				return finalResp.OutputText, usage, nil
			}

			if a.handler != nil {
				a.handler.OnToolCall(fc.Name, argsStr)
			}

			tool, ok := a.tools.Get(fc.Name)
			if !ok {
				fmt.Fprintf(&allResults, "I called %s(%s).\n\nError: unknown tool: %s\n\n", fc.Name, truncateStr(argsStr, a.argsTrunc), fc.Name)
				continue
			}

			// Permission check
			decision, wasApproved := a.checkPermissionAndApproval(fc.Name, argsStr, tool, resp.OutputText)
			if decision == "deny" {
				a.logAudit(fc.Name, argsStr, "tool call denied by permission policy", decision, false, 0)
				fmt.Fprintf(&allResults, "I called %s(%s).\n\nError: tool call denied by permission policy\n\n", fc.Name, truncateStr(argsStr, a.argsTrunc))
				if a.handler != nil {
					a.handler.OnToolResult(fc.Name, argsStr, ToolResult{Content: "denied by policy", IsError: true}, 0)
				}
				continue
			}
			if decision == "ask" && !wasApproved {
				a.logAudit(fc.Name, argsStr, "tool call denied by user", decision, false, 0)
				fmt.Fprintf(&allResults, "I called %s(%s).\n\nError: tool call denied by user\n\n", fc.Name, truncateStr(argsStr, a.argsTrunc))
				if a.handler != nil {
					a.handler.OnToolResult(fc.Name, argsStr, ToolResult{Content: "denied by user", IsError: true}, 0)
				}
				continue
			}

			// Pre-tool-use hook
			if a.hookRunner != nil {
				hookDecision, hookReason, hookErr := a.hookRunner.RunPreToolUse(ctx, fc.Name, argsStr, "")
				if hookErr != nil {
					fmt.Fprintf(os.Stderr, "[hooks] pre-tool-use error: %v\n", hookErr)
				}
				if hookDecision == "deny" {
					a.logAudit(fc.Name, argsStr, "tool call denied by hook: "+hookReason, "deny", false, 0)
					fmt.Fprintf(&allResults, "I called %s(%s).\n\nError: tool call denied by hook: %s\n\n", fc.Name, truncateStr(argsStr, a.argsTrunc), hookReason)
					continue
				}
			}

			startTime := time.Now()
			result, runErr := tool.Run(ctx, argsStr)
			elapsed := time.Since(startTime)
			if runErr != nil {
				result = ToolResult{Content: fmt.Sprintf("tool error: %v", runErr), IsError: true}
			}

			// Skip sanitizeResult for image results (base64 data is intentional)
			if len(result.Images) == 0 {
				result.Content = sanitizeResult(result.Content)
			}

			if a.hookRunner != nil {
				_ = a.hookRunner.RunPostToolUse(ctx, fc.Name, argsStr, result.Content, "")
			}

			a.logAudit(fc.Name, argsStr, result.Content, decision, wasApproved, elapsed.Milliseconds())

			if a.handler != nil {
				a.handler.OnToolResult(fc.Name, argsStr, result, elapsed)
			}

			if len(result.Images) > 0 {
				// Build content blocks: text result + image blocks
				var blocks []client.ContentBlock
				cleanResult := stripLineNumbers(result.Content)
				text := fmt.Sprintf("I called %s(%s).\n\nResult:\n%s",
					fc.Name, truncateStr(argsStr, a.argsTrunc), truncateStr(cleanResult, a.resultTrunc))
				blocks = append(blocks, client.ContentBlock{Type: "text", Text: text})
				for _, img := range result.Images {
					blocks = append(blocks, client.ContentBlock{
						Type: "image",
						Source: &client.ImageSource{
							Type:      "base64",
							MediaType: img.MediaType,
							Data:      img.Data,
						},
					})
				}
				// Image content must be in a user role message (Anthropic API requirement)
				messages = append(messages, client.Message{
					Role:    "user",
					Content: client.NewBlockContent(blocks),
				})
			} else {
				cleanResult := stripLineNumbers(result.Content)
				if result.IsError {
					fmt.Fprintf(&allResults, "I called %s(%s).\n\nError: %s\n\n", fc.Name, truncateStr(argsStr, a.argsTrunc), truncateStr(cleanResult, a.resultTrunc))
				} else {
					fmt.Fprintf(&allResults, "I called %s(%s).\n\nResult:\n%s\n\n", fc.Name, truncateStr(argsStr, a.argsTrunc), truncateStr(cleanResult, a.resultTrunc))
				}
			}
		}

		// Add all tool results as a single assistant message (skip if all results were image tools)
		if allResults.Len() > 0 {
			messages = append(messages, client.Message{
				Role:    "assistant",
				Content: client.NewTextContent(allResults.String()),
			})
		}
	}

	// Graceful degradation: return last text with a sentinel error so the
	// caller knows the loop was truncated (not a clean completion).
	if lastText != "" {
		return lastText, usage, ErrMaxIterReached
	}
	return "", usage, fmt.Errorf("agent loop exceeded %d iterations", a.maxIter)
}

// checkPermissionAndApproval runs the permission engine check, then falls back
// to the existing RequiresApproval/SafeChecker logic if needed.
// Returns (decision, wasApproved). decision is "allow", "deny", or "ask".
// wasApproved is true if the tool call should proceed.
func (a *AgentLoop) checkPermissionAndApproval(toolName, argsStr string, tool Tool, outputText string) (string, bool) {
	// Bypass mode: skip all permission checks including hard-blocks
	if a.bypassPermissions {
		return "allow", true
	}

	// Run permission engine checks based on tool type
	if a.permissions != nil {
		decision, _ := permissions.CheckToolCall(toolName, argsStr, a.permissions)
		if decision != "" {
			if decision == "deny" {
				return "deny", false
			}
			if decision == "allow" {
				return "allow", true
			}
			// decision == "ask" — fall through to existing approval logic
		}
	}

	// Existing RequiresApproval + SafeChecker logic
	needsApproval := tool.RequiresApproval()
	if needsApproval {
		if checker, ok := tool.(SafeChecker); ok && checker.IsSafeArgs(argsStr) {
			needsApproval = false
		}
	}
	if needsApproval {
		approved := false
		if a.handler != nil {
			approved = a.handler.OnApprovalNeeded(toolName, argsStr)
		}
		return "ask", approved
	}
	return "allow", true
}

// logAudit writes an audit entry if the auditor is configured.
func (a *AgentLoop) logAudit(toolName, argsStr, outputSummary, decision string, approved bool, durationMs int64) {
	if a.auditor == nil {
		return
	}
	a.auditor.Log(audit.AuditEntry{
		Timestamp:     time.Now(),
		SessionID:     "",
		ToolName:      toolName,
		InputSummary:  argsStr,
		OutputSummary: outputSummary,
		Decision:      decision,
		Approved:      approved,
		DurationMs:    durationMs,
	})
}

// base64ImagePattern matches long base64 strings that start with known image signatures.
// PNG starts with iVBOR, JPEG with /9j/.
var base64ImagePattern = regexp.MustCompile(`(?:(?:"[^"]*(?:base64|image|data)[^"]*"\s*:\s*")|(?:^|\s))([/+A-Za-z0-9](?:iVBOR|/9j/)[A-Za-z0-9+/=\s]{200,})`)

// rawBase64Pattern matches any standalone base64 blob of 500+ chars (likely binary data).
var rawBase64Pattern = regexp.MustCompile(`[A-Za-z0-9+/]{500,}={0,2}`)

// sanitizeResult replaces base64 image blobs in tool output with a short placeholder
// to avoid polluting LLM context and terminal output with huge binary strings.
func sanitizeResult(content string) string {
	result := base64ImagePattern.ReplaceAllStringFunc(content, func(match string) string {
		// Estimate original byte size (base64 is ~4/3 ratio)
		b64Len := len(strings.Map(func(r rune) rune {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
				return r
			}
			return -1
		}, match))
		bytes := b64Len * 3 / 4
		return fmt.Sprintf("[image: %d bytes]", bytes)
	})
	// Catch any remaining large base64 blobs not matched by the image-specific pattern
	result = rawBase64Pattern.ReplaceAllStringFunc(result, func(match string) string {
		bytes := len(match) * 3 / 4
		return fmt.Sprintf("[binary data: %d bytes]", bytes)
	})
	return result
}

// lineNumPrefix matches the "  42 | " prefix added by file_read.
var lineNumPrefix = regexp.MustCompile(`(?m)^\s*\d+\s*\| `)

// stripLineNumbers removes line-number prefixes from file_read output
// so the LLM sees clean content (saves tokens, prevents verbatim echo).
func stripLineNumbers(s string) string {
	return lineNumPrefix.ReplaceAllString(s, "")
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// planStepPattern matches numbered/bulleted list items in any language.
// Covers: "1. " "1、" "1) " "1：" "- " "* " "• " "· "
// CJK punctuation (、：) doesn't require trailing whitespace since CJK text has no word spaces.
var planStepPattern = regexp.MustCompile(`(?m)^\s*(?:\d+[.)]\s|\d+[、：]|[-*•·]\s)`)

// isPlanningResponse detects text-only responses that indicate the model is
// planning to use tools next. This is language-agnostic — it checks for
// structural patterns (numbered/bulleted lists) and tool name references
// (tool names are always English regardless of user language).
//
// Returns true for:
//
//	"Plan:\n1. Build the project\n2. Run tests" (English, numbered)
//	"计划：\n1. 构建项目\n2. 运行 bash 测试"      (Chinese, numbered + tool name)
//
// Returns false for:
//
//	"The answer is 42."                        (short direct answer)
//	"Done. File updated."                      (summary after tool use)
func isPlanningResponse(text string, toolNames []string) bool {
	if len(text) < 50 {
		return false
	}

	// Structural: numbered or bulleted list items
	if planStepPattern.MatchString(text) {
		return true
	}

	// Semantic: mentions 2+ available tool names → planning tool usage.
	// Only check names ≥ 5 chars to avoid false positives on short common
	// words like "http", "grep", "bash", "glob", "process".
	lower := strings.ToLower(text)
	toolMentions := 0
	for _, name := range toolNames {
		if len(name) < 5 {
			continue
		}
		if strings.Contains(lower, strings.ToLower(name)) {
			toolMentions++
			if toolMentions >= 2 {
				return true
			}
		}
	}

	return false
}
