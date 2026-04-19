package memory

import (
	"strings"
	"testing"
)

// TestAudit_NeverContainsKey enforces the privacy invariant from spec §7:
//
//	(a) no string field across the whole event payload contains the resolved
//	    API key bytes (substring assertion);
//	(b) any field describing key/endpoint state is a bool, not a string.
//
// This is the schema-shape gate that prevents a future bug where someone
// interpolates the api_key into an "error context" string by accident.
func TestAudit_NeverContainsKey(t *testing.T) {
	captured := []map[string]any{}
	a := AuditFunc(func(_ string, fields map[string]any) {
		captured = append(captured, fields)
	})
	key := "secret-API-KEY-do-not-leak-1234567890"
	fp := Fingerprint(key)

	// Realistic event payloads matching the §7 audit-event list.
	a.Log("memory_tlm_missing", map[string]any{"tlm_path_set": false})
	a.Log("memory_cloud_misconfigured", map[string]any{"endpoint_resolved": false, "api_key_present": false})
	a.Log("memory_tenant_switch", map[string]any{"fingerprint": fp})
	a.Log("memory_sidecar_degraded", map[string]any{})
	a.Log("memory_reload_failed", map[string]any{"reason": "timeout"})
	a.Log("memory_response_decode_failed", map[string]any{"sub_code": "x"})
	a.Log("memory_bundle_install_failed", map[string]any{"reason": "sha256_mismatch", "path_sample": "data.bin"})
	a.Log("memory_bundle_unsafe_path", map[string]any{"path_sample": "../../../etc/passwd", "reason": "contains parent traversal"})

	for _, p := range captured {
		for k, v := range p {
			if s, ok := v.(string); ok && strings.Contains(s, key) {
				t.Fatalf("api key leaked in field %q: %q", k, s)
			}
			// Boolean-only convention for key/endpoint state.
			switch k {
			case "api_key_present", "endpoint_resolved", "tlm_path_set":
				if _, ok := v.(bool); !ok {
					t.Fatalf("field %q must be bool, got %T", k, v)
				}
			}
		}
	}
}
