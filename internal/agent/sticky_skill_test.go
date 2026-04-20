package agent

import (
	"strings"
	"testing"
)

func TestBuildStickySkillReminder_HappyPath(t *testing.T) {
	got := buildStickySkillReminder("kocoro", "Route all platform ops through http://localhost:7533.")
	if !strings.HasPrefix(got, "<system-reminder>") || !strings.HasSuffix(got, "</system-reminder>") {
		t.Errorf("reminder is not wrapped in <system-reminder>: %q", got)
	}
	if !strings.Contains(got, "skill=kocoro") {
		t.Errorf("reminder missing skill name tag: %q", got)
	}
	if !strings.Contains(got, "Route all platform ops") {
		t.Errorf("reminder missing snippet body: %q", got)
	}
}

func TestBuildStickySkillReminder_EmptyInputsReturnEmpty(t *testing.T) {
	cases := []struct {
		name, snippet string
	}{
		{"", "snippet"},
		{"skill", ""},
		{"", ""},
		{"   ", "snippet"},
		{"skill", "   "},
	}
	for _, c := range cases {
		if got := buildStickySkillReminder(c.name, c.snippet); got != "" {
			t.Errorf("buildStickySkillReminder(%q, %q) = %q, want empty", c.name, c.snippet, got)
		}
	}
}

func TestParseUseSkillName(t *testing.T) {
	cases := []struct {
		args string
		want string
	}{
		{`{"skill_name":"kocoro","args":"foo"}`, "kocoro"},
		{`{"skill_name":"kocoro"}`, "kocoro"},
		{`{"args":"foo"}`, ""},
		{`not-json`, ""},
		{``, ""},
		{`{"skill_name":""}`, ""},
	}
	for _, c := range cases {
		if got := parseUseSkillName(c.args); got != c.want {
			t.Errorf("parseUseSkillName(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}
