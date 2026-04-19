package daemon

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/config"
	"github.com/Kocoro-lab/ShanClaw/internal/session"
	"github.com/Kocoro-lab/ShanClaw/internal/tools"
)

// daemonFallback adapts existing session search and the agent's MEMORY.md to
// the tools.FallbackQuery interface. Used when the structured memory
// service (Kocoro Cloud memory sidecar) is unavailable so memory_recall
// degrades gracefully instead of erroring.
type daemonFallback struct {
	sessionMgr *session.Manager
}

// Compile-time check that *daemonFallback satisfies tools.FallbackQuery.
var _ tools.FallbackQuery = (*daemonFallback)(nil)

func (d *daemonFallback) SessionKeyword(_ context.Context, query string, limit int) ([]any, error) {
	if d.sessionMgr == nil {
		return nil, nil
	}
	hits, err := d.sessionMgr.Search(query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, h)
	}
	return out, nil
}

// MemoryFileSnippet does a best-effort grep of ~/.shannon/MEMORY.md for query
// terms. Returns the joined matching lines (capped at 4KB). Empty string +
// nil error if the file is absent or no match is found. Agent-scoped MEMORY.md
// lookup needs per-run context that the daemon-level adapter doesn't have, so
// only the global file is checked here.
func (d *daemonFallback) MemoryFileSnippet(_ context.Context, query string) (string, error) {
	dir := config.ShannonDir()
	if dir == "" {
		return "", nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return "", nil
	}
	candidates := []string{
		filepath.Join(dir, "MEMORY.md"),
	}
	const cap = 4096
	for _, p := range candidates {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		var matches []string
		scanner := bufio.NewScanner(f)
		total := 0
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(strings.ToLower(line), q) {
				matches = append(matches, line)
				total += len(line) + 1
				if total > cap {
					break
				}
			}
		}
		f.Close()
		if len(matches) > 0 {
			return strings.Join(matches, "\n"), nil
		}
	}
	return "", nil
}
