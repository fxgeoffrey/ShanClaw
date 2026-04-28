package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
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

// readSessionsInDir loads every <id>.json session file from sessionsDir,
// sorted by ModTime ascending so callers can reason about ordering. The
// helper tolerates non-session files in the directory by skipping unmarshal
// errors.
func readSessionsInDir(t *testing.T, sessionsDir string) []session.Session {
	t.Helper()
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		t.Fatalf("read sessions dir %q: %v", sessionsDir, err)
	}
	type entryWithMod struct {
		name    string
		modTime time.Time
	}
	var ordered []entryWithMod
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		ordered = append(ordered, entryWithMod{entry.Name(), info.ModTime()})
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].modTime.Before(ordered[j].modTime)
	})
	var sessions []session.Session
	for _, ow := range ordered {
		data, err := os.ReadFile(filepath.Join(sessionsDir, ow.name))
		if err != nil {
			continue
		}
		var sess session.Session
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}
		sessions = append(sessions, sess)
	}
	return sessions
}

// TestHandleMessage_SlashWarmResume_ReusesRoutedSession verifies that a second
// slash on the same routed lane resumes the prior run's session via
// route.sessionID — instead of creating a fresh session each time.
//
// Without the warm-resume case in the resolution switch, the second call
// falls through to `default: sessMgr.NewSession()` and creates a second
// session file, fragmenting local transcript continuity.
func TestHandleMessage_SlashWarmResume_ReusesRoutedSession(t *testing.T) {
	_, gwSrv := newFakeGateway(t, researchSSEEvents, "ok")
	srv := newSlashTestServer(t, gwSrv.URL)

	// Use a slack-channel route so the route key is "default:slack:%23dev"
	// (non-agent route). Both requests share the same source+channel so
	// EnsureRouteKey produces the same RouteKey on each request.
	body1 := `{"text":"/research first query","source":"slack","channel":"#dev"}`
	body2 := `{"text":"/research second query","source":"slack","channel":"#dev"}`

	for i, body := range []string{body1, body2} {
		req, _ := http.NewRequest(http.MethodPost,
			fmt.Sprintf("http://127.0.0.1:%d/message", srv.Port()),
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		// Drain the stream so the deferred unlock runs before we issue request 2.
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// Default sessions dir (route key "default:slack:%23dev" maps to "" agent).
	sessionsDir := srv.deps.SessionCache.SessionsDir("")
	sessions := readSessionsInDir(t, sessionsDir)

	if len(sessions) != 1 {
		var ids []string
		for _, s := range sessions {
			ids = append(ids, s.ID)
		}
		t.Fatalf("expected exactly 1 session file (warm resume), got %d (ids=%v)", len(sessions), ids)
	}

	sess := sessions[0]
	// Should contain both pairs of (user + assistant) = 4 messages.
	if got := len(sess.Messages); got != 4 {
		var roles []string
		for _, m := range sess.Messages {
			roles = append(roles, m.Role+":"+m.Content.Text())
		}
		t.Fatalf("expected 4 messages (2 user+assistant pairs), got %d: %v", got, roles)
	}

	// Verify the order: user1, assistant1, user2, assistant2.
	expectedRoles := []string{"user", "assistant", "user", "assistant"}
	for i, want := range expectedRoles {
		if sess.Messages[i].Role != want {
			t.Errorf("message %d: role=%q, want %q", i, sess.Messages[i].Role, want)
		}
	}
	if !strings.Contains(sess.Messages[0].Content.Text(), "first query") {
		t.Errorf("expected first user message to contain 'first query', got %q", sess.Messages[0].Content.Text())
	}
	if !strings.Contains(sess.Messages[2].Content.Text(), "second query") {
		t.Errorf("expected second user message to contain 'second query', got %q", sess.Messages[2].Content.Text())
	}
}

// TestHandleMessage_SlashConcurrent_SerializesViaRouteLock verifies that two
// concurrent slash requests on the same route serialize via the route lock:
// the first acquires the lock and runs to completion, the second sees busy
// and fails fast with reason="active_run_not_ready" — without persisting its
// user message (because RunSlashWorkflow returns ErrSlashRouteBusy before
// any sessMgr.Save()).
func TestHandleMessage_SlashConcurrent_SerializesViaRouteLock(t *testing.T) {
	// Fake Gateway whose SSE response blocks until the test releases it.
	// Buffered channel so both `release <- struct{}{}` sends succeed even if
	// goroutine 2 never reaches the SSE handler (it bounces at lock acquisition).
	release := make(chan struct{}, 4)
	var submitCount atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/stream":
			n := submitCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			id := fmt.Sprintf("wf-%d", n)
			json.NewEncoder(w).Encode(map[string]any{"workflow_id": id, "task_id": id})

		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/stream/sse":
			// Block until released so goroutine 1's lock stays held while
			// goroutine 2 fires.
			<-release
			w.Header().Set("Content-Type", "text/event-stream")
			f, _ := w.(http.Flusher)
			fmt.Fprintf(w, "event: AGENT_STARTED\ndata: %s\n\n", `{"agent_id":"r"}`)
			if f != nil {
				f.Flush()
			}
			fmt.Fprintf(w, "event: thread.message.completed\ndata: %s\n\n", `{"response":"ok"}`)
			if f != nil {
				f.Flush()
			}
			fmt.Fprintf(w, "event: WORKFLOW_COMPLETED\ndata: %s\n\n", `{}`)
			if f != nil {
				f.Flush()
			}

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/tasks/"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"task_id": "t-test", "result": "ok"})

		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	srv := newSlashTestServer(t, gateway.URL)

	type result struct {
		body string
		err  error
	}
	doRequest := func() result {
		req, _ := http.NewRequest(http.MethodPost,
			fmt.Sprintf("http://127.0.0.1:%d/message", srv.Port()),
			strings.NewReader(`{"text":"/research foo","source":"slack","channel":"#dev"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return result{err: err}
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return result{body: string(b)}
	}

	ch := make(chan result, 2)
	// Goroutine 1 fires first and blocks in the SSE stream.
	go func() { ch <- doRequest() }()

	// Wait until goroutine 1 has acquired the route lock and submitted to
	// gateway. Polling is more reliable than a fixed sleep.
	deadline := time.Now().Add(2 * time.Second)
	for submitCount.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if submitCount.Load() < 1 {
		t.Fatal("goroutine 1 did not submit to gateway in time")
	}

	// Now poll until goroutine 1 actually holds the route lock. The route key
	// for slack:#dev is "default:slack:%23dev". TryLock against the same key
	// returning busy means we know goroutine 1's RunSlashWorkflow has acquired
	// the lock and the second request will see busy too.
	const routeKey = "default:slack:%23dev"
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, busy := srv.deps.SessionCache.TryLockRouteWithManager(routeKey, srv.deps.SessionCache.SessionsDir(""))
		if busy {
			break
		}
		// Got the lock; release it so goroutine 1 can keep running. (TryLock
		// returns the entry locked when busy=false — must unlock to avoid
		// disturbing the test setup.)
		srv.deps.SessionCache.UnlockRoute(routeKey)
		time.Sleep(5 * time.Millisecond)
	}

	// Goroutine 2 fires now — must hit the busy path and bounce immediately.
	go func() { ch <- doRequest() }()

	// Give goroutine 2 a moment to land at the lock and bounce.
	time.Sleep(50 * time.Millisecond)

	// Release goroutine 1's SSE stream so it completes.
	release <- struct{}{}
	// Extra release in case the SSE handler is somehow re-entered (defensive).
	release <- struct{}{}

	var bodies [2]string
	for i := 0; i < 2; i++ {
		r := <-ch
		if r.err != nil {
			t.Fatalf("request %d: %v", i, r.err)
		}
		bodies[i] = r.body
	}

	// Exactly one body should be the successful "done" workflow.
	// Exactly one body should contain "active_run_not_ready".
	var doneCount, busyCount int
	for _, b := range bodies {
		if strings.Contains(b, "event: done") && strings.Contains(b, `"reply":"ok"`) {
			doneCount++
		}
		if strings.Contains(b, "active_run_not_ready") {
			busyCount++
		}
	}
	if doneCount != 1 || busyCount != 1 {
		t.Fatalf("expected 1 done + 1 active_run_not_ready, got done=%d busy=%d\nbody[0]=%q\nbody[1]=%q",
			doneCount, busyCount, bodies[0], bodies[1])
	}

	// Transcript integrity: only ONE pair (user + assistant) should be on disk.
	// The losing request's user message must NOT have been appended — RunSlashWorkflow
	// returns ErrSlashRouteBusy BEFORE sessMgr.Save() runs.
	sessionsDir := srv.deps.SessionCache.SessionsDir("")
	sessions := readSessionsInDir(t, sessionsDir)
	if len(sessions) != 1 {
		t.Fatalf("expected exactly 1 session file, got %d", len(sessions))
	}
	if got := len(sessions[0].Messages); got != 2 {
		var roles []string
		for _, m := range sessions[0].Messages {
			roles = append(roles, m.Role)
		}
		t.Fatalf("expected exactly 2 messages on disk (user+assistant from winner), got %d: roles=%v",
			got, roles)
	}
}
