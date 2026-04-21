package agent

import (
	"fmt"
	"strings"
	"testing"
)

// Probe tests — temporarily added to measure detector strictness against real
// workflows. NOT meant to ship; should be deleted after analysis.
//
// REVISED after codex review:
//   - probe was overcounting nudges per tool call; loop.go counts per iteration
//     based on worstAction across all tool calls in that iteration
//   - probe didn't set batchTolerant; loop.go always includes "bash"
//   - probe injected resultSig manually; loop.go derives from
//     extractResultSignature(result.Content) — many tools (click, type, scroll
//     in internal/tools/browser.go) emit content WITHOUT URLs

type probeStep struct {
	tool   string
	args   string
	err    string // non-empty → error result
	result string // raw tool result content; resSig derived via extractResultSignature
	nonAct bool

	// startsNewIter: this call is the first tool call of a new iteration.
	// All subsequent calls until the next startsNewIter belong to the same
	// iteration and contribute to the same worstAction.
	startsNewIter bool
}

const probeMaxNudges = 3  // mirrors loop.go maxNudges
const probeNudgeWindow = 5 // mirrors loop.go nudgeWindowIters

// runProbeRealistic mirrors loop.go more faithfully:
//   - batchTolerant = {bash} (loop.go:1334-ish)
//   - nudge accounting per iteration via worstAction (loop.go:2784-2805)
//   - result_sig derived from result content via extractResultSignature
func runProbeRealistic(t *testing.T, name string, steps []probeStep) {
	t.Helper()
	ld := NewLoopDetector()
	ld.batchTolerant = map[string]bool{"bash": true}

	nudges := newNudgeWindow(probeMaxNudges, probeNudgeWindow)
	iterIdx := 0
	worstAction := LoopContinue
	worstMsg := ""

	flushIter := func(endStep int) bool {
		if worstAction == LoopForceStop {
			t.Logf("[%s] iter %d (ended @ step %d): FORCE_STOP (detector) — %s", name, iterIdx, endStep, worstMsg)
			t.Logf("  → workflow killed at iteration %d, step %d/%d", iterIdx, endStep, len(steps))
			return false
		}
		if worstAction == LoopNudge {
			escalated := nudges.recordAndCheck(iterIdx)
			t.Logf("[%s] iter %d (ended @ step %d): NUDGE (recents=%d in last %d iters) — %s",
				name, iterIdx, endStep, len(nudges.recents), probeNudgeWindow, worstMsg)
			if escalated {
				t.Logf("  → ESCALATED FORCE_STOP at iteration %d, step %d/%d (maxNudges=%d, window=%d)",
					iterIdx, endStep, len(steps), probeMaxNudges, probeNudgeWindow)
				return false
			}
		}
		return true
	}

	for i, s := range steps {
		if s.startsNewIter && i > 0 {
			if !flushIter(i) {
				return
			}
			worstAction = LoopContinue
			worstMsg = ""
			iterIdx++
		} else if i == 0 {
			iterIdx = 1
		}

		isErr := s.err != ""
		resSig := extractResultSignature(s.result)
		ld.Record(s.tool, s.args, isErr, s.err, resSig, s.nonAct)
		action, msg := ld.Check(s.tool)
		if action > worstAction {
			worstAction = action
			worstMsg = msg
		}
	}
	if !flushIter(len(steps)) {
		return
	}
	t.Logf("[%s] completed all %d steps across %d iterations (recents=%d in last %d iters, no force-stop)",
		name, len(steps), iterIdx, len(nudges.recents), probeNudgeWindow)
}

// =====================================================================
// PRODUCTION-OBSERVED BUG: use_skill 3x same args
// =====================================================================

func TestProbe_UseSkillRepeated_OneCallPerIter(t *testing.T) {
	steps := []probeStep{
		{tool: "use_skill", args: `{"skill_name":"kocoro"}`, startsNewIter: true},
		{tool: "use_skill", args: `{"skill_name":"kocoro"}`, startsNewIter: true},
		{tool: "use_skill", args: `{"skill_name":"kocoro"}`, startsNewIter: true},
		{tool: "use_skill", args: `{"skill_name":"kocoro"}`, startsNewIter: true},
		{tool: "use_skill", args: `{"skill_name":"kocoro"}`, startsNewIter: true},
	}
	runProbeRealistic(t, "use_skill_3x_consec_dup", steps)
}

// =====================================================================
// CODEX-FLAGGED MISS #1: ConsecutiveDup/ExactDup ignore IsError
// fail → fail → success retry pattern is killed at 3 same-args
// =====================================================================

func TestProbe_FailFailSuccessRetry_NoNetworkProgress(t *testing.T) {
	// Selector flaky: same browser_click args, fail twice then succeed.
	// Real Playwright pattern. Should NOT force-stop on the success.
	steps := []probeStep{
		{tool: "browser_click", args: `{"ref":"e1"}`, err: "element not found", startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"e1"}`, err: "element not found", startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"e1"}`, result: "Clicked: e1", startsNewIter: true},
	}
	runProbeRealistic(t, "fail_fail_success_same_args_3x", steps)
}

func TestProbe_FailFailFailRetryThenChange(t *testing.T) {
	// 3 fails on same args, then model changes ref. Should the 3rd fail kill it?
	steps := []probeStep{
		{tool: "browser_click", args: `{"ref":"e1"}`, err: "element not found", startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"e1"}`, err: "element not found", startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"e1"}`, err: "element not found", startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"e2"}`, result: "Clicked: e2", startsNewIter: true},
	}
	runProbeRealistic(t, "fail_x3_then_different_args", steps)
}

// =====================================================================
// CODEX-FLAGGED CASE: same URL different ref (form fill, codex priority)
// =====================================================================

func TestProbe_FormFill_DifferentRefSameURL(t *testing.T) {
	// browser internal tool: click result is "Clicked: <ref>" — NO URL.
	// So extractResultSignature returns "" → no FamilyNoProgress trigger
	// from result_sig. Pure ConsecutiveDup / NoProgress only.
	var steps []probeStep
	for i := 0; i < 10; i++ {
		steps = append(steps,
			probeStep{
				tool:          "browser",
				args:          fmt.Sprintf(`{"action":"click","ref":"e%d"}`, i),
				result:        fmt.Sprintf("Clicked: e%d", i),
				startsNewIter: true,
			},
			probeStep{
				tool:          "browser",
				args:          fmt.Sprintf(`{"action":"type","ref":"e%d","text":"v%d"}`, i, i),
				result:        fmt.Sprintf("Typed into: e%d", i),
				startsNewIter: true,
			},
		)
	}
	runProbeRealistic(t, "form_fill_internal_browser_no_url_in_result", steps)
}

// Same as above but simulates Playwright MCP where snapshot/click results
// DO contain the page URL — per codex this is the worst case for browser
// FamilyNoProgress to spuriously fire.
func TestProbe_FormFill_DifferentRefStableURL(t *testing.T) {
	const pageURL = "https://app.example.com/settings/profile"
	var steps []probeStep
	for i := 0; i < 10; i++ {
		steps = append(steps,
			probeStep{
				tool:          "browser_click",
				args:          fmt.Sprintf(`{"ref":"e%d"}`, i),
				result:        fmt.Sprintf("Click on field %d at %s", i, pageURL),
				startsNewIter: true,
			},
			probeStep{
				tool:          "browser_type",
				args:          fmt.Sprintf(`{"ref":"e%d","text":"v%d"}`, i, i),
				result:        fmt.Sprintf("Typed v%d into field %d at %s", i, i, pageURL),
				startsNewIter: true,
			},
		)
	}
	runProbeRealistic(t, "form_fill_playwright_mcp_with_url_in_result", steps)
}

// =====================================================================
// MULTIPLE TOOL CALLS PER ITERATION (codex correction: probe was overcounting)
// =====================================================================

func TestProbe_PlaywrightHappyPath_OneCallPerIter(t *testing.T) {
	const pageURL = "https://example.com"
	steps := []probeStep{
		{tool: "browser_navigate", args: `{"url":"https://example.com/login"}`, result: "Navigated to: " + pageURL + "/login\nTitle: Login", startsNewIter: true},
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "/login\nTitle: Login\n[e1] textbox: username", startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"e1"}`, result: "Clicked: e1", startsNewIter: true},
		{tool: "browser_type", args: `{"ref":"e1","text":"alice"}`, result: "Typed: alice", startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"e2"}`, result: "Clicked: e2", startsNewIter: true},
		{tool: "browser_type", args: `{"ref":"e2","text":"secret"}`, result: "Typed: secret", startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"e3"}`, result: "Clicked: e3", startsNewIter: true},
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "/dashboard\nTitle: Dashboard", startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"e4"}`, result: "Clicked: e4", startsNewIter: true},
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "/profile\nTitle: Profile", startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"e5"}`, result: "Clicked: e5", startsNewIter: true},
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "/settings\nTitle: Settings", startsNewIter: true},
	}
	runProbeRealistic(t, "playwright_happy_path_per_iter_with_real_results", steps)
}

func TestProbe_PlaywrightHappyPath_MultiCallPerIter(t *testing.T) {
	// Same flow but model groups type+click in same iteration. Real Playwright
	// MCP often does this — fewer LLM round trips.
	const pageURL = "https://example.com"
	steps := []probeStep{
		// iter 1: navigate alone
		{tool: "browser_navigate", args: `{"url":"https://example.com/login"}`, result: "Navigated to: " + pageURL + "/login", startsNewIter: true},
		// iter 2: snapshot alone
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "/login", startsNewIter: true},
		// iter 3: click+type+click+type (4 tool calls in one iteration)
		{tool: "browser_click", args: `{"ref":"e1"}`, result: "Clicked: e1", startsNewIter: true},
		{tool: "browser_type", args: `{"ref":"e1","text":"alice"}`, result: "Typed"},
		{tool: "browser_click", args: `{"ref":"e2"}`, result: "Clicked: e2"},
		{tool: "browser_type", args: `{"ref":"e2","text":"secret"}`, result: "Typed"},
		// iter 4: submit click + snapshot
		{tool: "browser_click", args: `{"ref":"e3"}`, result: "Clicked: e3", startsNewIter: true},
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "/dashboard"},
		// iter 5-7: navigate to other pages
		{tool: "browser_click", args: `{"ref":"e4"}`, result: "Clicked: e4", startsNewIter: true},
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "/profile"},
		{tool: "browser_click", args: `{"ref":"e5"}`, result: "Clicked: e5", startsNewIter: true},
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "/settings"},
	}
	runProbeRealistic(t, "playwright_happy_path_multi_call_per_iter", steps)
}

// =====================================================================
// SNAPSHOT POLLING (waiting for modal/element)
// =====================================================================

func TestProbe_SnapshotPolling(t *testing.T) {
	const pageURL = "https://example.com"
	steps := []probeStep{
		{tool: "browser_navigate", args: `{"url":"https://example.com"}`, result: "Navigated to: " + pageURL, startsNewIter: true},
		{tool: "browser_click", args: `{"ref":"trigger"}`, result: "Clicked: trigger", startsNewIter: true},
		// Poll for modal: same args, same URL, same result content
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "\nTitle: Page (no modal)", startsNewIter: true},
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "\nTitle: Page (no modal)", startsNewIter: true},
		{tool: "browser_snapshot", args: `{}`, result: "URL: " + pageURL + "\nTitle: Page (modal visible)", startsNewIter: true},
	}
	runProbeRealistic(t, "snapshot_polling_3x_for_modal", steps)
}

// =====================================================================
// BASH INVESTIGATION (with batchTolerant set, codex correction)
// =====================================================================

func TestProbe_BashInvestigation_BatchTolerant(t *testing.T) {
	commands := []string{
		"git log --oneline -20",
		"ls internal/agent",
		"cat internal/agent/loop.go | head -50",
		"grep -rn 'foo' internal/",
		"find . -name '*.go' | head",
		"go test ./internal/agent",
		"cat go.mod",
		"git status",
		"git diff HEAD~5 --stat",
		"ls -la ~/.shannon/logs/",
		"tail -100 ~/.shannon/logs/audit.log",
		"grep ERROR ~/.shannon/logs/*.log",
		"cat ~/.shannon/config.yaml",
		"ls ~/.shannon/agents/",
		"go build ./...",
	}
	var steps []probeStep
	for _, c := range commands {
		steps = append(steps, probeStep{
			tool:          "bash",
			args:          fmt.Sprintf(`{"command":%q}`, c),
			result:        "(some output)",
			startsNewIter: true,
		})
	}
	runProbeRealistic(t, "bash_15_unique_commands_with_batchTolerant", steps)
}

// =====================================================================
// DIRECT TEST: extractResultSignature behavior on browser-like content
// =====================================================================

func TestProbe_ResultSignature_BrowserContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"navigate_response", "Navigated to: https://example.com/login\nTitle: Login\nWelcome..."},
		{"click_response", "Clicked: e1"},
		{"type_response", "Typed into: e1"},
		{"snapshot_internal", "URL: https://example.com/profile\nTitle: Profile\n[e1] button: Save"},
		{"snapshot_with_anchors", "URL: https://example.com\n[e1] link to https://example.com/about\n[e2] link to https://example.com/contact"},
		{"playwright_mcp_click", "Click on field at https://app.example.com/settings/profile"},
		{"empty", ""},
		{"text_no_url", "(some output)"},
	}
	for _, c := range cases {
		sig := extractResultSignature(c.content)
		t.Logf("[%-25s] sig=%q", c.name, sig)
	}
}

// =====================================================================
// CODEX-FLAGGED CASE: ExactDup window detector on interleaved snapshots
// =====================================================================

func TestProbe_InterleavedSnapshots(t *testing.T) {
	// snapshot → click → snapshot → click → snapshot → ...
	// Each snapshot has args={} so 3+ in window trips ExactDup.
	const pageURL = "https://example.com"
	steps := []probeStep{
		{tool: "browser_navigate", args: `{"url":"https://example.com"}`, result: "Navigated to: " + pageURL, startsNewIter: true},
	}
	for i := 0; i < 5; i++ {
		steps = append(steps,
			probeStep{tool: "browser_snapshot", args: `{}`, result: fmt.Sprintf("URL: %s\nstate %d", pageURL, i), startsNewIter: true},
			probeStep{tool: "browser_click", args: fmt.Sprintf(`{"ref":"e%d"}`, i), result: fmt.Sprintf("Clicked: e%d", i), startsNewIter: true},
		)
	}
	runProbeRealistic(t, "interleaved_snapshot_click_5_pairs", steps)
}

// =====================================================================
// Helper to detect false-positive on real bash multi-step debugging
// =====================================================================

func TestProbe_ConfirmExtractSigOnNoURL(t *testing.T) {
	// Confirm: when result has no URL, sig is empty so result_sig comparison is skipped.
	cases := []string{"Clicked: e5", "Typed into: e2", "Scrolled to: 200", "Done"}
	for _, c := range cases {
		sig := extractResultSignature(c)
		if sig != "" {
			t.Errorf("expected empty sig for content %q, got %q", c, sig)
		}
	}
	// And confirm a content with URL produces non-empty sig.
	if extractResultSignature("see https://example.com/foo for details") == "" {
		t.Errorf("expected non-empty sig for URL-containing content")
	}
}

// =====================================================================
// REPLAY: real production session 2026-04-09-5bd9c6a93306 (Teams workflow)
// 27 browser_* tool calls across iterations 2..30
// This session did NOT actually force-stop (model recognized the loop and
// asked the user). But would the detector have killed it? Run it and see.
// =====================================================================

func TestProbe_ReplayTeamsSession(t *testing.T) {
	type call struct {
		iter  int
		name  string
		args  string
		isErr bool
		sig   string // already extracted via extractResultSignature
	}
	calls := []call{
		{2, "browser_navigate", `{"url":"https://teams.microsoft.com"}`, false, "https://teams.microsoft.com,https://teams.microsoft.com/v2/"},
		{3, "browser_snapshot", `null`, false, "https://teams.microsoft.com/v2/"},
		{4, "browser_snapshot", `{"filename":"teams_initial_snapshot.md"}`, false, "https://teams.cloud.microsoft/"},
		{5, "browser_navigate", `{"url":"https://teams.microsoft.com/l/chat/0/0?users=shenghao.hu@ptmind.com"}`, false, "https://go.microsoft.com/fwlink/,https://teams.microsoft.com/l/chat/0/0"},
		{6, "browser_snapshot", `{}`, false, "https://go.microsoft.com/fwlink/,https://teams.microsoft.com/dl/launcher/launcher.html"},
		{7, "browser_click", `{"ref":"e12","element":"button \"Use the web app instead\""}`, false, "https://teams.cloud.microsoft/"},
		{8, "browser_snapshot", `{}`, false, "https://go.microsoft.com/fwlink/,https://teams.microsoft.com/dl/launcher/launcher.html"},
		{9, "browser_snapshot", `{}`, false, "https://go.microsoft.com/fwlink/,https://teams.microsoft.com/dl/launcher/launcher.html"},
		{11, "browser_press_key", `{"key":"a"}`, false, "https://teams.cloud.microsoft/"},
		{12, "browser_press_key", `{"key":"Enter"}`, false, "https://teams.cloud.microsoft/"},
		{13, "browser_press_key", `{"key":"Home"}`, false, ""},
		{14, "browser_press_key", `{"key":"Shift+ArrowLeft"}`, false, ""},
		{15, "browser_press_key", `{"key":"Shift+ArrowLeft"}`, false, ""},
		{16, "browser_press_key", `{"key":"Enter"}`, false, "https://teams.cloud.microsoft/"},
		{17, "browser_press_key", `{"key":"Escape"}`, false, ""},
		{18, "browser_press_key", `{"key":"", "timeout":  120}`, true, ""},
		{19, "browser_press_key", `{"key":"Home"}`, false, ""},
		{20, "browser_press_key", `{"key":"Shift+ArrowLeft"}`, false, "https://teams.cloud.microsoft/"},
		{21, "browser_press_key", `{"key":"Tab"}`, false, "https://teams.cloud.microsoft/"},
		{22, "browser_press_key", `{"key":"Tab"}`, false, "https://teams.cloud.microsoft/"},
		{23, "browser_press_key", `{"key":"Escape"}`, false, ""},
		{24, "browser_press_key", `{"key":"End"}`, false, ""},
		{25, "browser_press_key", `{"key":"Tab"}`, false, ""},
		{26, "browser_press_key", `{"key":"Tab"}`, false, ""},
		{28, "browser_type", `{"ref":"e95","text":"from shanclaw","submit":true}`, true, ""},
		{29, "browser_type", `{"ref":"e95","text":"from shanclaw","submit":true}`, true, ""},
		{30, "browser_type", `{"ref":"e95","text":"from shanclaw","submit":true}`, true, ""},
	}

	ld := NewLoopDetector()
	ld.batchTolerant = map[string]bool{"bash": true}

	nudges := newNudgeWindow(probeMaxNudges, probeNudgeWindow)
	prevIter := -1
	worstAction := LoopContinue
	worstMsg := ""
	worstAtCall := 0

	flushIter := func(callIdx int, atIter int) bool {
		if worstAction == LoopForceStop {
			t.Logf("REPLAY: iter %d (call #%d): FORCE_STOP (detector) — %s", atIter, worstAtCall, worstMsg)
			t.Logf("  → KILLED at original session iter %d (this was call #%d of 27)", atIter, worstAtCall)
			return false
		}
		if worstAction == LoopNudge {
			escalated := nudges.recordAndCheck(atIter)
			t.Logf("REPLAY: iter %d (call #%d): NUDGE (recents=%d in last %d iters) — %s",
				atIter, worstAtCall, len(nudges.recents), probeNudgeWindow, worstMsg)
			if escalated {
				t.Logf("  → ESCALATED FORCE_STOP at session iter %d (call #%d, maxNudges=%d, window=%d)",
					atIter, worstAtCall, probeMaxNudges, probeNudgeWindow)
				return false
			}
		}
		return true
	}

	for i, c := range calls {
		if c.iter != prevIter && prevIter != -1 {
			if !flushIter(i, prevIter) {
				return
			}
			worstAction = LoopContinue
			worstMsg = ""
		}
		prevIter = c.iter

		errMsg := ""
		if c.isErr {
			errMsg = "Error in tool"
		}
		ld.Record(c.name, c.args, c.isErr, errMsg, c.sig, false)
		action, msg := ld.Check(c.name)
		if action > worstAction {
			worstAction = action
			worstMsg = msg
			worstAtCall = i + 1
		}
	}
	if !flushIter(len(calls), prevIter) {
		return
	}
	t.Logf("REPLAY survived: completed all %d calls (%d iters), recents=%d in last %d iters", len(calls), prevIter, len(nudges.recents), probeNudgeWindow)
}

// =====================================================================
// Final sanity: existing single-call ConsecutiveDup baseline
// =====================================================================

func TestProbe_ConsecDup_BaselineSanity(t *testing.T) {
	// Sanity check that the basic ConsecutiveDup detector still fires at 3.
	ld := NewLoopDetector()
	for i := 0; i < 3; i++ {
		ld.Record("web_search", `{"q":"test"}`, false, "", "", false)
	}
	action, _ := ld.Check("web_search")
	if action != LoopForceStop {
		t.Errorf("baseline broken: expected LoopForceStop at 3 same-args, got %v", action)
	}
	t.Logf("baseline OK: web_search 3x → %v (force-stop = %d)", action, LoopForceStop)
}

var _ = strings.Builder{} // keep strings import in case future cases need it
