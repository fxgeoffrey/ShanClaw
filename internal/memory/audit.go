package memory

// AuditLogger is the structured-event sink used by the memory subsystem.
// Daemon supplies a real audit-backed adapter (see internal/daemon/memory_audit.go);
// tests pass an AuditFunc or a capturing stub. Implementations MUST NOT log
// raw API key bytes — see the boolean-only convention enforced by audit_test.go.
type AuditLogger interface {
	Log(event string, fields map[string]any)
}

// AuditFunc adapts a plain function into AuditLogger.
type AuditFunc func(event string, fields map[string]any)

func (f AuditFunc) Log(event string, fields map[string]any) { f(event, fields) }
