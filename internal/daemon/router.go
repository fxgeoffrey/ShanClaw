package daemon

import (
	"path/filepath"
	"sync"

	"github.com/Kocoro-lab/shan/internal/session"
)

// SessionCache manages one session.Manager per agent.
// For daemon mode, each agent has a single long-lived session that is
// always resumed. The cache is keyed by agent name ("" = default agent).
type SessionCache struct {
	mu          sync.RWMutex
	managers    map[string]*session.Manager
	shannonDir  string
}

// NewSessionCache creates a cache rooted at the given shannon directory.
// Sessions are stored at:
//   - Named agents: <shannonDir>/agents/<name>/sessions/
//   - Default agent: <shannonDir>/sessions/
func NewSessionCache(shannonDir string) *SessionCache {
	return &SessionCache{
		managers:   make(map[string]*session.Manager),
		shannonDir: shannonDir,
	}
}

// GetOrCreate returns the session.Manager for the given agent, creating one
// if needed. For daemon mode, it auto-resumes the latest session or creates
// a new one if none exists.
func (sc *SessionCache) GetOrCreate(agent string) *session.Manager {
	sc.mu.RLock()
	mgr, ok := sc.managers[agent]
	sc.mu.RUnlock()
	if ok {
		return mgr
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Double-check after write lock
	if mgr, ok := sc.managers[agent]; ok {
		return mgr
	}

	sessDir := sc.sessionsDir(agent)
	mgr = session.NewManager(sessDir)

	// Try to resume the latest session for this agent
	sess, _ := mgr.ResumeLatest()
	if sess == nil {
		mgr.NewSession()
	}

	sc.managers[agent] = mgr
	return mgr
}

// sessionsDir returns the sessions directory for an agent.
func (sc *SessionCache) sessionsDir(agent string) string {
	if agent == "" {
		return filepath.Join(sc.shannonDir, "sessions")
	}
	return filepath.Join(sc.shannonDir, "agents", agent, "sessions")
}
