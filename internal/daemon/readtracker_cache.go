package daemon

import (
	"sync"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
)

// ReadTrackerCache holds long-lived agent.ReadTracker instances keyed by
// session ID. Each daemon RunAgent call creates a fresh AgentLoop; without
// a shared tracker, file_read dedup history is lost between user messages
// of the same session — breaking plan #7's session-scoped dedup goal in
// the production daemon path. (TUI keeps its own AgentLoop instance long-
// lived so it doesn't need this; CLI one-shots are fresh processes by
// design.)
//
// One cache per ServerDeps; entries persist for the daemon's lifetime.
// Memory pressure is bounded — each entry is one ReadTracker holding a
// per-(path, offset, limit) entry map. A power user with 10K active sessions
// each having read 100 distinct file ranges = ~16 MB. We don't evict;
// daemon restarts wipe the cache, which is the right behavior because
// AgentLoop's own per-binary tools_h hash also changes on restart.
type ReadTrackerCache struct {
	mu       sync.Mutex
	trackers map[string]*agent.ReadTracker
}

// NewReadTrackerCache returns an empty cache.
func NewReadTrackerCache() *ReadTrackerCache {
	return &ReadTrackerCache{trackers: make(map[string]*agent.ReadTracker)}
}

// GetOrCreate returns the tracker for sessionID, creating one if absent.
// Empty sessionID returns a fresh (uncached) tracker — caller is in a path
// where session continuity is not meaningful (e.g. cache-bypass sources).
func (c *ReadTrackerCache) GetOrCreate(sessionID string) *agent.ReadTracker {
	if c == nil {
		return agent.NewReadTracker()
	}
	if sessionID == "" {
		return agent.NewReadTracker()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if rt, ok := c.trackers[sessionID]; ok {
		return rt
	}
	rt := agent.NewReadTracker()
	c.trackers[sessionID] = rt
	return rt
}

// Forget drops a session's tracker. Called by SessionManager.OnSessionClose
// to release memory when a session is closed/deleted by the user. Safe to
// call with empty sessionID or unknown ID.
func (c *ReadTrackerCache) Forget(sessionID string) {
	if c == nil || sessionID == "" {
		return
	}
	c.mu.Lock()
	delete(c.trackers, sessionID)
	c.mu.Unlock()
}

// Len returns the number of cached trackers — for diagnostics / tests.
func (c *ReadTrackerCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.trackers)
}
