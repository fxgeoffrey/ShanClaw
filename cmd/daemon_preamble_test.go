package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Kocoro-lab/ShanClaw/internal/daemon"
)

func TestDaemonEventHandler_OnPreamble_DropsEmptyText(t *testing.T) {
	received := make(chan daemon.DaemonMessage, 2)
	srv := httptestNewWebSocketServer(t, received)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client := daemon.NewClient(wsURL, "", nil, nil)
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := &daemonEventHandler{wsClient: client, messageID: "msg-123"}
	handler.OnPreamble("")

	select {
	case dm := <-received:
		t.Fatalf("empty preamble should not be forwarded, got message type=%q payload=%s", dm.Type, string(dm.Payload))
	case <-time.After(100 * time.Millisecond):
	}

	handler.OnPreamble("Reading the four files.")

	select {
	case dm := <-received:
		if dm.Type != daemon.MsgTypeEvent || dm.MessageID != "msg-123" {
			t.Fatalf("unexpected daemon message: %+v", dm)
		}
		var payload daemon.DaemonEventPayload
		if err := json.Unmarshal(dm.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.EventType != "LLM_OUTPUT" || payload.Message != "Reading the four files." {
			t.Fatalf("unexpected payload: %+v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive non-empty preamble")
	}
}

func httptestNewWebSocketServer(t *testing.T, received chan<- daemon.DaemonMessage) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var dm daemon.DaemonMessage
			if err := conn.ReadJSON(&dm); err != nil {
				return
			}
			received <- dm
		}
	}))
}
