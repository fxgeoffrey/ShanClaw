package memory

import (
	"context"
	"testing"
)

func TestService_Disabled(t *testing.T) {
	s := NewService(Config{Provider: "disabled"}, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Status() != StatusDisabled {
		t.Fatalf("status=%v want StatusDisabled", s.Status())
	}
	_, class, _ := s.Query(context.Background(), QueryIntent{})
	if class != ClassUnavailable {
		t.Fatalf("disabled service Query class=%v want ClassUnavailable", class)
	}
}

func TestService_LocalNoTLM(t *testing.T) {
	captured := []string{}
	a := AuditFunc(func(ev string, _ map[string]any) { captured = append(captured, ev) })
	cfg := Config{Provider: "local", TLMPath: "/definitely/not/a/real/path/for/tlm"}
	s := NewService(cfg, a)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Status() != StatusUnavailable {
		t.Fatalf("status=%v want StatusUnavailable", s.Status())
	}
	found := false
	for _, e := range captured {
		if e == "memory_tlm_missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected memory_tlm_missing audit, got %v", captured)
	}
}

func TestService_CloudMissingAPIKey(t *testing.T) {
	captured := []map[string]any{}
	a := AuditFunc(func(ev string, fields map[string]any) {
		if ev == "memory_cloud_misconfigured" {
			captured = append(captured, fields)
		}
	})
	cfg := Config{Provider: "cloud", Endpoint: "https://x", APIKey: "", TLMPath: "/bin/echo"}
	s := NewService(cfg, a)
	_ = s.Start(context.Background())
	if s.Status() != StatusUnavailable {
		t.Fatalf("status=%v want StatusUnavailable", s.Status())
	}
	if len(captured) == 0 {
		t.Fatal("expected memory_cloud_misconfigured audit")
	}
	f := captured[0]
	if f["endpoint_resolved"] != true {
		t.Fatalf("endpoint_resolved=%v want true", f["endpoint_resolved"])
	}
	if f["api_key_present"] != false {
		t.Fatalf("api_key_present=%v want false", f["api_key_present"])
	}
}

func TestService_CloudMissingEndpoint(t *testing.T) {
	captured := []map[string]any{}
	a := AuditFunc(func(ev string, fields map[string]any) {
		if ev == "memory_cloud_misconfigured" {
			captured = append(captured, fields)
		}
	})
	cfg := Config{Provider: "cloud", Endpoint: "", APIKey: "k", TLMPath: "/bin/echo"}
	s := NewService(cfg, a)
	_ = s.Start(context.Background())
	if s.Status() != StatusUnavailable {
		t.Fatalf("status=%v want StatusUnavailable", s.Status())
	}
	if len(captured) == 0 {
		t.Fatal("expected memory_cloud_misconfigured audit")
	}
	f := captured[0]
	if f["endpoint_resolved"] != false {
		t.Fatalf("endpoint_resolved=%v want false", f["endpoint_resolved"])
	}
	if f["api_key_present"] != true {
		t.Fatalf("api_key_present=%v want true", f["api_key_present"])
	}
}

func TestService_StatusString(t *testing.T) {
	cases := []struct {
		s    ServiceStatus
		want string
	}{
		{StatusDisabled, "disabled"},
		{StatusInitializing, "initializing"},
		{StatusReady, "ready"},
		{StatusDegraded, "degraded"},
		{StatusUnavailable, "unavailable"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Fatalf("%v.String()=%q want %q", tc.s, got, tc.want)
		}
	}
}
