package cloudflow

import "strings"

// SlashCommand is the parsed form of a `/research` or `/swarm` HTTP message.
type SlashCommand struct {
	Type     string // "research" or "swarm"
	Strategy string // research only — "quick" | "standard" | "deep" | "academic"
	Query    string
}

// ParseSlash returns the parsed command if text is a recognized slash directive,
// otherwise nil. Empty queries return nil so callers fall through to the
// default agent loop instead of submitting an empty Gateway task.
func ParseSlash(text string) *SlashCommand {
	if !strings.HasPrefix(text, "/") {
		return nil
	}
	rest := text[1:]
	sp := strings.IndexByte(rest, ' ')
	if sp < 0 {
		return nil // command with no args
	}
	cmd := rest[:sp]
	args := strings.TrimSpace(rest[sp+1:])
	if args == "" {
		return nil
	}
	switch cmd {
	case "research":
		strategy := "standard"
		query := args
		first, afterFirst, hasSpace := strings.Cut(args, " ")
		switch first {
		case "quick", "standard", "deep", "academic":
			if !hasSpace {
				return nil // strategy keyword but no query follows
			}
			strategy = first
			query = strings.TrimSpace(afterFirst)
		}
		if query == "" {
			return nil
		}
		return &SlashCommand{Type: "research", Strategy: strategy, Query: query}
	case "swarm":
		return &SlashCommand{Type: "swarm", Query: args}
	default:
		return nil
	}
}
