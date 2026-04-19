package memory

import (
	"testing"
	"time"
)

func TestAttachedQuerier_StatusAlwaysReady(t *testing.T) {
	a := NewAttachedQuerier("/tmp/nonexistent.sock", 0)
	if a.Status() != StatusReady {
		t.Fatalf("status=%v want StatusReady", a.Status())
	}
}

func TestAttachedQuerier_DefaultTimeout(t *testing.T) {
	a := NewAttachedQuerier("/tmp/x.sock", 0)
	if a.timeout != 5*time.Second {
		t.Fatalf("timeout=%v want 5s default", a.timeout)
	}
	a2 := NewAttachedQuerier("/tmp/y.sock", 2*time.Second)
	if a2.timeout != 2*time.Second {
		t.Fatalf("timeout=%v want 2s explicit", a2.timeout)
	}
}
