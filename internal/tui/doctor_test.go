package tui

import (
	"testing"
)

func TestDoctorChecks_BasicResults(t *testing.T) {
	checks := runDoctorChecks("/tmp/shannon", "test-key", nil, nil, 28)

	if len(checks) == 0 {
		t.Fatal("expected doctor checks to return results")
	}

	// API key check should pass
	found := false
	for _, c := range checks {
		if c.name == "API key" && c.ok {
			found = true
		}
	}
	if !found {
		t.Error("expected API key check to pass")
	}
}

func TestDoctorChecks_MissingAPIKey(t *testing.T) {
	checks := runDoctorChecks("/tmp/shannon", "", nil, nil, 0)

	for _, c := range checks {
		if c.name == "API key" {
			if c.ok {
				t.Error("expected API key check to fail with empty key")
			}
			return
		}
	}
	t.Error("expected to find API key check")
}

func TestFormatDoctorResults(t *testing.T) {
	checks := []doctorCheck{
		{"Config", true, "/tmp/config.yaml"},
		{"API key", false, "not configured"},
	}
	result := formatDoctorResults(checks)
	if result == "" {
		t.Error("expected non-empty result")
	}
}
