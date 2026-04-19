package memory

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSpawner records spawn count and lets the test control whether each
// spawn cycle reports failure or success at the WaitReady step.
type fakeSpawner struct {
	failsRemaining atomic.Int32 // first N Spawn or WaitReady calls fail
	spawned        atomic.Int32
	waitReadyErr   error
	waitErr        error
	onReadyHit     atomic.Int32 // observed via the Supervisor's onReady callback
}

func (f *fakeSpawner) Spawn(ctx context.Context) error {
	f.spawned.Add(1)
	if f.failsRemaining.Load() > 0 {
		f.failsRemaining.Add(-1)
		return errors.New("simulated spawn failure")
	}
	return nil
}

func (f *fakeSpawner) WaitReady(ctx context.Context, _ time.Duration) error {
	return f.waitReadyErr
}

func (f *fakeSpawner) Wait() error {
	return f.waitErr
}

func TestSupervisor_BackoffAndDegradedAfterBudget(t *testing.T) {
	sp := &fakeSpawner{}
	sp.failsRemaining.Store(10) // always fail
	sup := NewSupervisor(sp, 3, func() { sp.onReadyHit.Add(1) })
	sup.testBackoff = func(int) time.Duration { return 1 * time.Millisecond }
	final := sup.Run(context.Background())
	if final != StateDegraded {
		t.Fatalf("final=%v want Degraded", final)
	}
	if got := sp.spawned.Load(); got < 3 {
		t.Fatalf("spawned=%d want >=3", got)
	}
	if sp.onReadyHit.Load() != 0 {
		t.Fatal("onReady should not fire when sidecar never becomes ready")
	}
}

func TestSupervisor_RecoversFromColdStartFailure(t *testing.T) {
	// Spec acceptance #53: first WaitReady failure must be recoverable.
	sp := &fakeSpawner{}
	sp.failsRemaining.Store(2)                 // first 2 Spawns fail; 3rd succeeds
	sp.waitErr = errors.New("simulated crash") // child exits after becoming ready
	sup := NewSupervisor(sp, 4, func() { sp.onReadyHit.Add(1) })
	sup.testBackoff = func(int) time.Duration { return 1 * time.Millisecond }
	final := sup.Run(context.Background())
	if sp.onReadyHit.Load() == 0 {
		t.Fatal("onReady should have fired at least once")
	}
	if final != StateDegraded {
		// Eventually budget should run out because Wait keeps returning err.
		t.Fatalf("final=%v want Degraded", final)
	}
}

func TestSupervisor_CtxCancelExitsCleanly(t *testing.T) {
	sp := &fakeSpawner{}
	sp.failsRemaining.Store(100)
	sup := NewSupervisor(sp, 100, nil)
	sup.testBackoff = func(int) time.Duration { return 50 * time.Millisecond }
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	final := sup.Run(ctx)
	if final != StateStopped {
		t.Fatalf("final=%v want Stopped", final)
	}
}
