package daemon

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/audit"
)

// memoryAuditAdapter bridges the memory package's AuditLogger interface to
// the daemon's *audit.AuditLogger (which writes AuditEntry rows). The adapter
// never inspects field values — it is the memory package's responsibility to
// keep API key bytes out of the payload (see internal/memory/audit_test.go
// privacy invariant). Mirrors the syncAuditAdapter pattern in server.go.
type memoryAuditAdapter struct {
	logger *audit.AuditLogger
}

func (a memoryAuditAdapter) Log(event string, fields map[string]any) {
	if a.logger == nil {
		return
	}
	// Render fields as a stable, compact string. JSON gives us deterministic
	// formatting and is already what the rest of audit.log uses.
	var summary string
	if data, err := json.Marshal(fields); err == nil {
		summary = string(data)
	} else {
		summary = fmt.Sprintf("%v", fields)
	}
	a.logger.Log(audit.AuditEntry{
		Timestamp:    time.Now(),
		ToolName:     event,
		InputSummary: summary,
		Decision:     "logged",
		Approved:     true,
	})
}
