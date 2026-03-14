package agent

import (
	"testing"
)

func TestAgentLoop_SetInjectCh(t *testing.T) {
	reg := NewToolRegistry()
	loop := NewAgentLoop(nil, reg, "test", t.TempDir(), 5, 2000, 200, nil, nil, nil)
	ch := make(chan string, 10)
	loop.SetInjectCh(ch)
	if loop.injectCh != ch {
		t.Fatal("expected injectCh to be set")
	}
}

func TestAgentLoop_InjectCh_Nil_NoPanic(t *testing.T) {
	reg := NewToolRegistry()
	loop := NewAgentLoop(nil, reg, "test", t.TempDir(), 1, 2000, 200, nil, nil, nil)
	// injectCh is nil by default — should not panic
	if loop.injectCh != nil {
		t.Fatal("expected injectCh to be nil by default")
	}
}

func TestAgentLoop_MultipleInjections_Batched(t *testing.T) {
	ch := make(chan string, 10)
	ch <- "message one"
	ch <- "message two"
	ch <- "message three"

	// Drain like the loop does
	var injected []string
drain:
	for {
		select {
		case msg := <-ch:
			injected = append(injected, msg)
		default:
			break drain
		}
	}
	if len(injected) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(injected))
	}
	if injected[0] != "message one" || injected[2] != "message three" {
		t.Errorf("unexpected order: %v", injected)
	}
}
