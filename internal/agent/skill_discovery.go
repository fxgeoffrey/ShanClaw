package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
	ctxwin "github.com/Kocoro-lab/ShanClaw/internal/context"
	"github.com/Kocoro-lab/ShanClaw/internal/skills"
)

const discoveryPrompt = `Match the user message to available skills. Output skill names ONLY — no explanations, no commentary, no questions. The user may write in any language; skill descriptions are in English. Match by INTENT.

Rules:
- One skill name per line, nothing else
- "none" if no skill matches
- Be liberal: if the task COULD benefit from a skill, include it

Available skills:
%s

User message:
%s

Respond with skill names only:`

const discoveryTimeout = 5 * time.Second

// discoverRelevantSkills calls a small-tier model to identify which skills
// are relevant to the user's message. Returns matched skills and usage.
// On timeout or error, returns nil (caller should proceed without discovery).
func discoverRelevantSkills(ctx context.Context, c ctxwin.Completer, userText string, loaded []*skills.Skill) ([]*skills.Skill, client.Usage) {
	if len(loaded) == 0 || userText == "" {
		return nil, client.Usage{}
	}

	var catalog strings.Builder
	for _, s := range loaded {
		fmt.Fprintf(&catalog, "- %s: %s\n", s.Name, s.Description)
	}

	prompt := fmt.Sprintf(discoveryPrompt, catalog.String(), userText)

	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	resp, err := c.Complete(ctx, client.CompletionRequest{
		Messages: []client.Message{
			{Role: "user", Content: client.NewTextContent(prompt)},
		},
		ModelTier:   "small",
		Temperature: 0.0,
		MaxTokens:   30,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[skill-discovery] error: %v\n", err)
		return nil, client.Usage{}
	}

	matched := parseDiscoveryOutput(resp.OutputText, loaded)
	if len(matched) > 0 {
		names := make([]string, len(matched))
		for i, s := range matched {
			names[i] = s.Name
		}
		fmt.Fprintf(os.Stderr, "[skill-discovery] matched: %s\n", strings.Join(names, ", "))
	} else {
		fmt.Fprintf(os.Stderr, "[skill-discovery] no match (raw: %q)\n", resp.OutputText)
	}
	return matched, resp.Usage
}

// parseDiscoveryOutput validates discovery model output against loaded skills.
func parseDiscoveryOutput(output string, loaded []*skills.Skill) []*skills.Skill {
	nameSet := make(map[string]*skills.Skill, len(loaded))
	for _, s := range loaded {
		nameSet[s.Name] = s
	}

	var matched []*skills.Skill
	seen := make(map[string]bool)
	for _, line := range strings.Split(output, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.EqualFold(name, "none") {
			continue
		}
		if s, ok := nameSet[name]; ok && !seen[name] {
			matched = append(matched, s)
			seen[name] = true
		}
	}
	return matched
}

// formatDiscoveryHint builds a <system-reminder> for matched skills.
func formatDiscoveryHint(matched []*skills.Skill) string {
	if len(matched) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<system-reminder>\nSkills relevant to your task:\n")
	for _, s := range matched {
		desc := s.Description
		runes := []rune(desc)
		if len(runes) > 120 {
			desc = string(runes[:117]) + "..."
		}
		fmt.Fprintf(&sb, "- %s: %s\n", s.Name, desc)
	}
	sb.WriteString("\nCall use_skill(\"<name>\") to load full instructions before proceeding.\n</system-reminder>")
	return sb.String()
}
