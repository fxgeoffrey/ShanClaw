package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kocoro-lab/ShanClaw/internal/memory"
)

type fakeFallback struct {
	snippet      string
	hits         []any
	gotQuery     string
	gotLimit     int
	snippetQuery string
}

func (f *fakeFallback) SessionKeyword(_ context.Context, q string, limit int) ([]any, error) {
	f.gotQuery = q
	f.gotLimit = limit
	return f.hits, nil
}
func (f *fakeFallback) MemoryFileSnippet(_ context.Context, q string) (string, error) {
	f.snippetQuery = q
	return f.snippet, nil
}

func TestMemoryTool_FallbackWhenNoService(t *testing.T) {
	tool := &MemoryTool{Fallback: &fakeFallback{snippet: "memory.md note"}}
	res, err := tool.Run(context.Background(), `{"anchor_mentions":["x"]}`)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.IsError {
		t.Fatalf("res.IsError=true: %s", res.Content)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(res.Content), &body); err != nil {
		t.Fatalf("decode tool result: %v\n%s", err, res.Content)
	}
	if body["source"] != "fallback" {
		t.Fatalf("source=%v want fallback", body["source"])
	}
	if body["evidence_quality"] != "text_search" {
		t.Fatalf("evidence_quality=%v", body["evidence_quality"])
	}
}

func TestMemoryTool_RejectsEmptyAnchorMentions(t *testing.T) {
	tool := &MemoryTool{Fallback: &fakeFallback{}}
	res, _ := tool.Run(context.Background(), `{"anchor_mentions":[]}`)
	if !res.IsError || !strings.Contains(res.Content, "anchor_mentions") {
		t.Fatalf("res=%+v", res)
	}
}

func TestMemoryTool_RejectsMissingAnchorMentions(t *testing.T) {
	tool := &MemoryTool{Fallback: &fakeFallback{}}
	res, _ := tool.Run(context.Background(), `{}`)
	if !res.IsError {
		t.Fatalf("expected error: %s", res.Content)
	}
}

func TestMemoryTool_RejectsMalformedJSON(t *testing.T) {
	tool := &MemoryTool{Fallback: &fakeFallback{}}
	res, _ := tool.Run(context.Background(), `{not json`)
	if !res.IsError {
		t.Fatalf("expected error: %s", res.Content)
	}
}

type stubQuerier struct {
	status memory.ServiceStatus
	env    *memory.ResponseEnvelope
	class  memory.ErrorClass
	err    error
	callN  int
	seq    []memory.ErrorClass // optional: deliver i-th class on i-th call
}

func (s *stubQuerier) Status() memory.ServiceStatus { return s.status }
func (s *stubQuerier) Query(_ context.Context, _ memory.QueryIntent) (*memory.ResponseEnvelope, memory.ErrorClass, error) {
	s.callN++
	if len(s.seq) > 0 {
		c := s.seq[0]
		s.seq = s.seq[1:]
		return s.env, c, nil
	}
	return s.env, s.class, s.err
}

func TestMemoryTool_ClassOK(t *testing.T) {
	env := &memory.ResponseEnvelope{
		Reason:        "ok",
		BundleVersion: "0.4.0",
		Candidates: []memory.QueryCandidate{
			{Value: "v", Score: 0.9, Evidence: "observed", SupportingEventIDs: []string{"e1"}},
		},
	}
	tool := &MemoryTool{
		Service:  &stubQuerier{status: memory.StatusReady, env: env, class: memory.ClassOK},
		Fallback: &fakeFallback{},
	}
	res, err := tool.Run(context.Background(), `{"anchor_mentions":["x"]}`)
	if err != nil || res.IsError {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var body map[string]any
	json.Unmarshal([]byte(res.Content), &body)
	if body["source"] != "memory_sidecar" {
		t.Fatalf("source=%v", body["source"])
	}
	if body["evidence_quality"] != "structured" {
		t.Fatalf("evidence_quality=%v", body["evidence_quality"])
	}
	if body["bundle_version"] != "0.4.0" {
		t.Fatalf("bundle_version=%v", body["bundle_version"])
	}
	cands, _ := body["candidates"].([]any)
	if len(cands) != 1 {
		t.Fatalf("candidates=%+v", cands)
	}
}

func TestMemoryTool_ClassDegraded(t *testing.T) {
	env := &memory.ResponseEnvelope{
		Reason:        "degraded",
		BundleVersion: "0.4.0",
		Candidates:    []memory.QueryCandidate{{Value: "v", Evidence: "observed"}},
	}
	tool := &MemoryTool{
		Service:  &stubQuerier{status: memory.StatusReady, env: env, class: memory.ClassOK},
		Fallback: &fakeFallback{},
	}
	res, _ := tool.Run(context.Background(), `{"anchor_mentions":["x"]}`)
	var body map[string]any
	json.Unmarshal([]byte(res.Content), &body)
	if body["evidence_quality"] != "structured_degraded" {
		t.Fatalf("evidence_quality=%v", body["evidence_quality"])
	}
	warnings, _ := body["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatal("expected degraded warning")
	}
	w0, _ := warnings[0].(map[string]any)
	if msg, _ := w0["message"].(string); !strings.Contains(msg, "degraded") {
		t.Fatalf("first warning should be the degraded notice; got %+v", w0)
	}
}

func TestMemoryTool_ClassRetryable_OneRetryThenFallback(t *testing.T) {
	sq := &stubQuerier{status: memory.StatusReady, seq: []memory.ErrorClass{memory.ClassRetryable, memory.ClassRetryable}}
	tool := &MemoryTool{Service: sq, Fallback: &fakeFallback{}}
	res, _ := tool.Run(context.Background(), `{"anchor_mentions":["x"]}`)
	var body map[string]any
	json.Unmarshal([]byte(res.Content), &body)
	if body["source"] != "fallback_after_retry" {
		t.Fatalf("source=%v", body["source"])
	}
	if body["fallback_reason"] != "retryable_failed" {
		t.Fatalf("fallback_reason=%v", body["fallback_reason"])
	}
	if sq.callN != 2 {
		t.Fatalf("expected 2 calls, got %d", sq.callN)
	}
}

func TestMemoryTool_ClassPermanent_SurfacesDiagnostics(t *testing.T) {
	env := &memory.ResponseEnvelope{
		Reason: "error",
		Error: &memory.ErrorObject{
			Code:    "validation_error",
			Message: "bad mode",
			Details: map[string]any{"sub_code": "schema_validation"},
		},
	}
	tool := &MemoryTool{
		Service:  &stubQuerier{status: memory.StatusReady, env: env, class: memory.ClassPermanent},
		Fallback: &fakeFallback{},
	}
	res, _ := tool.Run(context.Background(), `{"anchor_mentions":["x"]}`)
	if !res.IsError {
		t.Fatal("permanent should be IsError")
	}
	var body map[string]any
	json.Unmarshal([]byte(res.Content), &body)
	warnings, _ := body["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatal("expected warnings with sub_code")
	}
	w0, _ := warnings[0].(map[string]any)
	if w0["sub_code"] != "schema_validation" {
		t.Fatalf("w0=%+v", w0)
	}
}

func TestMemoryTool_ClassUnavailable_FallsBack(t *testing.T) {
	tool := &MemoryTool{
		Service:  &stubQuerier{status: memory.StatusReady, class: memory.ClassUnavailable},
		Fallback: &fakeFallback{},
	}
	res, _ := tool.Run(context.Background(), `{"anchor_mentions":["x"]}`)
	var body map[string]any
	json.Unmarshal([]byte(res.Content), &body)
	if body["source"] != "fallback" {
		t.Fatalf("source=%v", body["source"])
	}
	if body["fallback_reason"] != "service_unavailable" {
		t.Fatalf("fallback_reason=%v", body["fallback_reason"])
	}
}

func TestMemoryTool_Info(t *testing.T) {
	tool := &MemoryTool{Fallback: &fakeFallback{}}
	info := tool.Info()
	if info.Name != "memory_recall" {
		t.Fatalf("name=%q want memory_recall", info.Name)
	}
	if !tool.IsReadOnlyCall("") {
		t.Fatal("memory_recall must be read-only")
	}
	if tool.RequiresApproval() {
		t.Fatal("memory_recall must not require approval")
	}
	// Required field declared.
	found := false
	for _, r := range info.Required {
		if r == "anchor_mentions" {
			found = true
		}
	}
	if !found {
		t.Fatal("anchor_mentions must be in Required")
	}
}

// TestMemoryTool_FallbackInvokesProvider locks the contract that fallback()
// actually delegates to FallbackQuery.SessionKeyword + MemoryFileSnippet
// (regression: earlier the fallback returned an empty envelope and the
// provider plumbing was dead code).
func TestMemoryTool_FallbackInvokesProvider(t *testing.T) {
	fb := &fakeFallback{
		snippet: "MEMORY.md hit line",
		hits:    []any{map[string]any{"id": "sess1", "snippet": "from session_search"}},
	}
	tool := &MemoryTool{Fallback: fb}
	res, _ := tool.Run(context.Background(), `{"anchor_mentions":["foo","bar"],"result_limit":7}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if fb.gotQuery != "foo bar" {
		t.Fatalf("session keyword query = %q want %q", fb.gotQuery, "foo bar")
	}
	if fb.gotLimit != 7 {
		t.Fatalf("session keyword limit = %d want 7", fb.gotLimit)
	}
	if fb.snippetQuery != "foo bar" {
		t.Fatalf("memory snippet query = %q want %q", fb.snippetQuery, "foo bar")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(res.Content), &body); err != nil {
		t.Fatal(err)
	}
	cands, _ := body["candidates"].([]any)
	if len(cands) != 2 {
		t.Fatalf("expected 2 fallback candidates (1 session hit + 1 MEMORY.md snippet), got %d: %+v", len(cands), cands)
	}
}
