package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
	"github.com/Kocoro-lab/ShanClaw/internal/config"
	"github.com/Kocoro-lab/ShanClaw/internal/session"
)

// fakeGateway is a stub Gateway HTTP server for slash routing tests.
// It tracks call counts via atomic counters so tests can assert whether
// the gateway was or wasn't contacted.
type fakeGateway struct {
	submitCalls atomic.Int32 // POST /api/v1/tasks/stream
	sseEvents   []string     // SSE lines served per GET /api/v1/stream/sse
	taskResult  string       // body of GET /api/v1/tasks/{id}
}

// newFakeGateway builds a test HTTP server that stubs the three Gateway endpoints
// cloudflow.Run uses.
//
//   - POST /api/v1/tasks/stream → 201 {"workflow_id":"wf-test","task_id":"t-test"}
//   - GET  /api/v1/stream/sse   → SSE stream of sseEvents lines
//   - GET  /api/v1/tasks/{id}   → 200 {"task_id":"t-test","result":"<taskResult>"}
//
// All other paths return 404.
func newFakeGateway(t *testing.T, sseEvents []string, taskResult string) (*fakeGateway, *httptest.Server) {
	t.Helper()
	fg := &fakeGateway{sseEvents: sseEvents, taskResult: taskResult}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/stream":
			fg.submitCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{
				"workflow_id": "wf-test",
				"task_id":     "t-test",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/stream/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			f, ok := w.(http.Flusher)
			if !ok {
				t.Error("httptest.Server doesn't support Flusher")
				return
			}
			for _, line := range fg.sseEvents {
				fmt.Fprint(w, line)
				f.Flush()
			}

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/tasks/"):
			w.Header().Set("Content-Type", "application/json")
			result := fg.taskResult
			if result == "" {
				result = "ok"
			}
			json.NewEncoder(w).Encode(map[string]string{
				"task_id": "t-test",
				"result":  result,
			})

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return fg, srv
}

// newSlashTestServer builds a minimal daemon Server wired to a fake Gateway.
// It starts listening on an ephemeral port and registers t.Cleanup to stop it.
func newSlashTestServer(t *testing.T, gwURL string) *Server {
	t.Helper()
	dir := t.TempDir()
	sc := NewSessionCache(dir)
	gw := client.NewGatewayClient(gwURL, "test-key")
	deps := &ServerDeps{
		ShannonDir:   dir,
		AgentsDir:    dir,
		SessionCache: sc,
		GW:           gw,
		Config: &config.Config{
			APIKey: "test-key",
		},
	}
	c := NewClient("ws://localhost:1/x", "", func(msg MessagePayload) string { return "" }, nil)
	srv := NewServer(0, c, deps, "test")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Start(ctx)
	// Wait for the port to be assigned.
	for i := 0; i < 50; i++ {
		if srv.Port() != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return srv
}

// readSSE reads all SSE events from body until EOF. Returns a slice of
// (event, data) pairs in encounter order.
func readSSE(t *testing.T, body *http.Response) []struct{ event, data string } {
	t.Helper()
	var events []struct{ event, data string }
	scanner := bufio.NewScanner(body.Body)
	var curEvent, curData string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			curEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			curData = strings.TrimPrefix(line, "data: ")
		case line == "":
			if curEvent != "" || curData != "" {
				events = append(events, struct{ event, data string }{curEvent, curData})
				curEvent, curData = "", ""
			}
		}
	}
	if err := scanner.Err(); err != nil && !strings.Contains(err.Error(), "use of closed") {
		t.Logf("readSSE scanner error (may be normal on early close): %v", err)
	}
	return events
}

// --- Research workflow SSE events for stub ---
var researchSSEEvents = []string{
	"event: AGENT_STARTED\ndata: {\"agent_id\":\"researcher\",\"message\":\"Agent working...\"}\n\n",
	"event: thread.message.completed\ndata: {\"response\":\"ok\"}\n\n",
	"event: WORKFLOW_COMPLETED\ndata: {\"message\":\"ok\"}\n\n",
}

// --- Swarm workflow SSE events for stub ---
var swarmSSEEvents = []string{
	"event: TASKLIST_UPDATED\ndata: {\"payload\":{\"tasks\":[{\"status\":\"completed\"},{\"status\":\"completed\"},{\"status\":\"pending\"}]}}\n\n",
	"event: thread.message.completed\ndata: {\"response\":\"swarm done\"}\n\n",
	"event: WORKFLOW_COMPLETED\ndata: {\"message\":\"swarm done\"}\n\n",
}

// TestHandleMessage_SlashResearch_RoutesToCloudflow verifies /research routes
// to cloudflow.Run and produces a done event with RunAgentResult JSON.
func TestHandleMessage_SlashResearch_RoutesToCloudflow(t *testing.T) {
	fg, gwSrv := newFakeGateway(t, researchSSEEvents, "ok")
	srv := newSlashTestServer(t, gwSrv.URL)

	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/message", srv.Port()),
		strings.NewReader(`{"text":"/research deep what is X","source":"shanclaw"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	events := readSSE(t, resp)
	t.Logf("events: %+v", events)

	// Verify gateway was called.
	if fg.submitCalls.Load() == 0 {
		t.Error("expected gateway submit to be called at least once")
	}

	var gotCloudAgent, gotDone bool
	var doneResult RunAgentResult
	for _, ev := range events {
		if ev.event == "cloud_agent" {
			gotCloudAgent = true
		}
		if ev.event == "done" {
			gotDone = true
			if err := json.Unmarshal([]byte(ev.data), &doneResult); err != nil {
				t.Fatalf("decode done event: %v (data=%q)", err, ev.data)
			}
		}
	}

	if !gotCloudAgent {
		t.Error("expected at least one cloud_agent event")
	}
	if !gotDone {
		t.Fatal("expected a done event")
	}
	if doneResult.Reply == "" {
		t.Error("expected non-empty Reply in done payload")
	}
	if doneResult.SessionID == "" {
		t.Error("expected non-empty SessionID in done payload")
	}
}

// TestHandleMessage_SlashSwarm_RoutesToCloudflow verifies /swarm produces
// cloud_progress events from TASKLIST_UPDATED.
func TestHandleMessage_SlashSwarm_RoutesToCloudflow(t *testing.T) {
	fg, gwSrv := newFakeGateway(t, swarmSSEEvents, "swarm done")
	srv := newSlashTestServer(t, gwSrv.URL)

	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/message", srv.Port()),
		strings.NewReader(`{"text":"/swarm plan a launch","source":"shanclaw"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	events := readSSE(t, resp)
	t.Logf("events: %+v", events)

	if fg.submitCalls.Load() == 0 {
		t.Error("expected gateway submit to be called at least once")
	}

	var gotProgress bool
	for _, ev := range events {
		if ev.event == "cloud_progress" {
			gotProgress = true
		}
	}
	if !gotProgress {
		t.Error("expected at least one cloud_progress event from TASKLIST_UPDATED")
	}
}

// TestHandleMessage_NonSlash_StillRunsAgentLoop verifies that regular messages
// do NOT trigger the fake Gateway — the slash detection must not intercept them.
func TestHandleMessage_NonSlash_StillRunsAgentLoop(t *testing.T) {
	fg, gwSrv := newFakeGateway(t, nil, "")
	srv := newSlashTestServer(t, gwSrv.URL)

	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/message", srv.Port()),
		strings.NewReader(`{"text":"regular user message","source":"shanclaw"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	// Read events — we may get an error event because RunAgent can't complete
	// without a real LLM, but that's OK. What matters is no gateway submit call.
	events := readSSE(t, resp)
	t.Logf("events: %+v", events)

	// Gateway must NOT have received a task submission.
	if fg.submitCalls.Load() > 0 {
		t.Error("regular message should NOT trigger the Gateway submit endpoint")
	}

	// Must NOT contain cloud_agent event (which would only come from cloudflow path).
	for _, ev := range events {
		if ev.event == "cloud_agent" {
			t.Errorf("regular message should not produce cloud_agent events, got: %+v", ev)
		}
	}
}

// TestHandleMessage_SlashWithoutSSE_Returns400 verifies that slash commands
// without Accept: text/event-stream are rejected with 400.
func TestHandleMessage_SlashWithoutSSE_Returns400(t *testing.T) {
	fg, gwSrv := newFakeGateway(t, nil, "")
	srv := newSlashTestServer(t, gwSrv.URL)

	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/message", srv.Port()),
		strings.NewReader(`{"text":"/research foo","source":"shanclaw"}`))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately NOT setting Accept: text/event-stream

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var buf strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
	}
	body := buf.String()
	if !strings.Contains(body, "text/event-stream") {
		t.Errorf("expected body to mention text/event-stream, got: %q", body)
	}

	if fg.submitCalls.Load() > 0 {
		t.Error("gateway must NOT be called for a 400 rejection")
	}
}

// TestHandleMessage_SlashWithAttachments_Returns400 verifies that slash commands
// with multimodal content blocks are rejected with 400.
func TestHandleMessage_SlashWithAttachments_Returns400(t *testing.T) {
	fg, gwSrv := newFakeGateway(t, nil, "")
	srv := newSlashTestServer(t, gwSrv.URL)

	body := `{"text":"/research foo","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}]}`
	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/message", srv.Port()),
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var buf strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
	}
	respBody := buf.String()
	if !strings.Contains(respBody, "attachments") {
		t.Errorf("expected body to mention attachments, got: %q", respBody)
	}

	if fg.submitCalls.Load() > 0 {
		t.Error("gateway must NOT be called for a 400 rejection")
	}
}

// TestHandleMessage_SlashOnActiveRoute_BypassesInjection verifies that a slash
// request on a held route returns an SSE error with reason="active_run_not_ready"
// and does NOT inject into the active run.
func TestHandleMessage_SlashOnActiveRoute_BypassesInjection(t *testing.T) {
	fg, gwSrv := newFakeGateway(t, nil, "")
	srv := newSlashTestServer(t, gwSrv.URL)

	// Manually hold the route lock to simulate an active run.
	// The route key for agent "foo" is "agent:foo".
	routeKey := "agent:foo"
	sessDir := srv.deps.SessionCache.SessionsDir("foo")
	route := srv.deps.SessionCache.LockRouteWithManager(routeKey, sessDir)
	// Capture the manager to verify it's unchanged after the slash attempt.
	managerBefore := route.manager

	// Release the lock after the test so the server can shut down cleanly.
	defer srv.deps.SessionCache.UnlockRoute(routeKey)

	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/message", srv.Port()),
		strings.NewReader(`{"text":"/research what is X","agent":"foo","source":"shanclaw"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	events := readSSE(t, resp)
	t.Logf("events: %+v", events)

	// Must receive an error event with reason=active_run_not_ready.
	var gotBusy bool
	for _, ev := range events {
		if ev.event == "error" {
			var payload map[string]string
			if err := json.Unmarshal([]byte(ev.data), &payload); err == nil {
				if payload["reason"] == "active_run_not_ready" {
					gotBusy = true
				}
			}
		}
		// Must NOT have an injected event — slash never injects.
		if ev.event == "injected" {
			t.Error("slash on active route must NOT produce an injected event")
		}
	}
	if !gotBusy {
		t.Errorf("expected SSE error with reason=active_run_not_ready, got events: %+v", events)
	}

	// Gateway must NOT have been called.
	if fg.submitCalls.Load() > 0 {
		t.Error("gateway must NOT be called when route is busy")
	}

	// Manager must be unchanged — no side effects on the held route.
	if route.manager != managerBefore {
		t.Error("held route's manager should be unchanged after busy rejection")
	}
}

// TestHandleMessage_SlashWithNamedAgent_RoutesToAgentLane verifies that a slash
// request with agent="researcher" persists the user/assistant pair to the
// researcher agent's session directory.
func TestHandleMessage_SlashWithNamedAgent_RoutesToAgentLane(t *testing.T) {
	fg, gwSrv := newFakeGateway(t, researchSSEEvents, "result from researcher agent")
	srv := newSlashTestServer(t, gwSrv.URL)

	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/message", srv.Port()),
		strings.NewReader(`{"text":"/research foo","agent":"researcher","source":"shanclaw"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	events := readSSE(t, resp)
	t.Logf("events: %+v", events)

	// Expect a done event.
	var gotDone bool
	for _, ev := range events {
		if ev.event == "done" {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("expected a done event")
	}

	// Gateway must have been called.
	if fg.submitCalls.Load() == 0 {
		t.Error("expected gateway submit to be called")
	}

	// Verify user+assistant messages were persisted to the researcher agent's
	// sessions directory, not the default sessions directory.
	researcherSessionsDir := srv.deps.SessionCache.SessionsDir("researcher")
	if _, err := os.Stat(researcherSessionsDir); os.IsNotExist(err) {
		t.Fatalf("researcher sessions dir not created: %s", researcherSessionsDir)
	}

	// Find a session file and verify it has our user message.
	entries, err := os.ReadDir(researcherSessionsDir)
	if err != nil {
		t.Fatalf("read researcher sessions dir: %v", err)
	}
	var foundUserMsg bool
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(researcherSessionsDir, entry.Name()))
		if err != nil {
			continue
		}
		var sess session.Session
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}
		for _, msg := range sess.Messages {
			if msg.Role == "user" {
				if strings.Contains(msg.Content.Text(), "/research foo") {
					foundUserMsg = true
				}
			}
		}
	}
	if !foundUserMsg {
		t.Error("expected to find user message '/research foo' in researcher agent's sessions directory")
	}
}

// --- TryLockRouteWithManager unit tests ---

// TestTryLockRouteWithManager_Idle verifies that acquiring a lock on an idle
// route returns the entry (busy=false) and the manager is initialized.
func TestTryLockRouteWithManager_Idle(t *testing.T) {
	dir := t.TempDir()
	sc := NewSessionCache(dir)
	sessDir := sc.SessionsDir("")

	entry, busy := sc.TryLockRouteWithManager("agent:test", sessDir)
	if busy {
		t.Fatal("expected busy=false for idle route")
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.manager == nil {
		t.Error("expected manager to be initialized")
	}

	// UnlockRoute should release cleanly.
	sc.UnlockRoute("agent:test")
}

// TestTryLockRouteWithManager_Held verifies that a held route returns busy=true
// without blocking.
func TestTryLockRouteWithManager_Held(t *testing.T) {
	dir := t.TempDir()
	sc := NewSessionCache(dir)
	sessDir := sc.SessionsDir("")

	// Acquire the lock via LockRouteWithManager (simulates an active run).
	_ = sc.LockRouteWithManager("agent:busy", sessDir)
	defer sc.UnlockRoute("agent:busy")

	// TryLock on the same key must return immediately with busy=true.
	done := make(chan bool, 1)
	go func() {
		_, busy := sc.TryLockRouteWithManager("agent:busy", sessDir)
		done <- busy
	}()

	select {
	case busy := <-done:
		if !busy {
			t.Error("expected busy=true for held route")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("TryLockRouteWithManager blocked instead of returning immediately")
	}
}

// TestTryLockRouteWithManager_EmptyKey verifies that an empty key returns
// (nil, false) — same as LockRouteWithManager's behavior for empty keys.
func TestTryLockRouteWithManager_EmptyKey(t *testing.T) {
	dir := t.TempDir()
	sc := NewSessionCache(dir)

	entry, busy := sc.TryLockRouteWithManager("", "")
	if busy {
		t.Error("expected busy=false for empty key")
	}
	if entry != nil {
		t.Error("expected nil entry for empty key")
	}
}
