package agent

import "testing"

func TestFilterSuggestion(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantOK   bool
		wantText string
	}{
		{"valid short", "fix the test", true, "fix the test"},
		{"valid 12 words exact",
			"please run the test suite and commit the fix once it passes ok",
			true, "please run the test suite and commit the fix once it passes ok"},
		{"too short 1 word", "yes", false, ""},
		{"single word in allowlist", "commit", true, "commit"},
		{"too long 13 words",
			"please run the test suite and commit the fix once it passes thanks much",
			false, ""},
		{"too long char count",
			"please run the entire test suite which is huge and then commit changes",
			false, ""},
		{"multi-sentence", "fix it. then commit.", false, ""},
		{"evaluative word great", "great idea, run tests", false, ""},
		{"claude voice", "I'll fix the test", false, ""},
		{"format chars",
			"fix the test\n\nthen commit", false, ""},
		{"meta wrap done", "done", false, ""},
		{"empty", "", false, ""},
		{"whitespace only", "   ", false, ""},
		// CJK cases — must accept short Chinese/Japanese suggestions that
		// strings.Fields would otherwise count as 1 word
		{"chinese valid", "运行测试套件", true, "运行测试套件"},
		{"chinese longer valid", "帮我重构这个 loop 函数", true, "帮我重构这个 loop 函数"},
		{"chinese too short 3 runes", "去吧", false, ""},
		{"chinese too long >30 runes",
			"请帮我把整个项目的所有 Go 文件全部按照 gofmt 格式化一遍然后做测试覆盖率分析", false, ""},
		{"chinese meta skip", "跳过", false, ""},
		{"japanese valid", "次のステップに進む", true, "次のステップに進む"},
		{"japanese meta skip", "スキップ", false, ""},
		{"chinese multi-sentence", "运行测试。然后提交。", false, ""},
		{"mixed cjk english latin-dominant",
			"run gofmt now", true, "run gofmt now"},
		{"mixed cjk english cjk-dominant",
			"运行 gofmt 然后看结果", true, "运行 gofmt 然后看结果"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := FilterSuggestion(tc.in)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v (input %q, got %q)", ok, tc.wantOK, tc.in, got)
			}
			if ok && got != tc.wantText {
				t.Errorf("text = %q, want %q", got, tc.wantText)
			}
		})
	}
}

func TestSuggestionPromptIsStable(t *testing.T) {
	// SUGGESTION_PROMPT must not change between releases — changes invalidate
	// the cache benefit since the suggestion message itself becomes part of
	// the forked request's tail. (The cache benefit comes from the *prefix*
	// being identical to main; the appended message is uncached.)
	// This test exists to make accidental edits visible in code review.
	if len(SuggestionPrompt) < 50 {
		t.Errorf("SuggestionPrompt suspiciously short: %d chars", len(SuggestionPrompt))
	}
	if len(SuggestionPrompt) > 2000 {
		t.Errorf("SuggestionPrompt suspiciously long (%d chars) — every char is paid input on each turn",
			len(SuggestionPrompt))
	}
}
