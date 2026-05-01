package daemon

import (
	"sync"
	"testing"
)

func TestReadTrackerCache_GetOrCreate_SameIDReturnsSame(t *testing.T) {
	c := NewReadTrackerCache()
	rt1 := c.GetOrCreate("sess-A")
	rt2 := c.GetOrCreate("sess-A")
	if rt1 != rt2 {
		t.Fatal("same sessionID must return the SAME tracker instance")
	}
	if c.Len() != 1 {
		t.Errorf("expected 1 cached tracker, got %d", c.Len())
	}
}

func TestReadTrackerCache_GetOrCreate_DifferentIDsIsolated(t *testing.T) {
	c := NewReadTrackerCache()
	a := c.GetOrCreate("sess-A")
	b := c.GetOrCreate("sess-B")
	if a == b {
		t.Fatal("different sessionIDs must return DIFFERENT trackers (no cross-session dedup leak)")
	}
	if c.Len() != 2 {
		t.Errorf("expected 2 cached trackers, got %d", c.Len())
	}
}

func TestReadTrackerCache_EmptySessionIDIsFresh(t *testing.T) {
	c := NewReadTrackerCache()
	rt1 := c.GetOrCreate("")
	rt2 := c.GetOrCreate("")
	if rt1 == rt2 {
		t.Fatal("empty sessionID must NOT cache — each call returns a fresh uncached tracker")
	}
	if c.Len() != 0 {
		t.Errorf("empty sessionIDs must not be added to the cache, got len=%d", c.Len())
	}
}

func TestReadTrackerCache_NilSafe(t *testing.T) {
	var c *ReadTrackerCache // nil
	rt := c.GetOrCreate("anything")
	if rt == nil {
		t.Fatal("nil-receiver GetOrCreate must return a fresh tracker, not nil")
	}
	c.Forget("anything")  // must not panic
	if c.Len() != 0 {
		t.Errorf("nil-receiver Len() must return 0, got %d", c.Len())
	}
}

func TestReadTrackerCache_Forget(t *testing.T) {
	c := NewReadTrackerCache()
	c.GetOrCreate("sess-A")
	c.GetOrCreate("sess-B")
	if c.Len() != 2 {
		t.Fatalf("setup expected 2, got %d", c.Len())
	}
	c.Forget("sess-A")
	if c.Len() != 1 {
		t.Errorf("after Forget(A), expected len=1, got %d", c.Len())
	}
	// re-create should yield a NEW tracker (Forget evicted the old one)
	prev := c.GetOrCreate("sess-A")
	again := c.GetOrCreate("sess-A")
	if prev != again {
		t.Error("after re-create, subsequent gets should hit cache")
	}
	c.Forget("nonexistent") // must not panic
	c.Forget("")            // must not panic
}

// TestReadTrackerCache_ConcurrentGetOrCreate exercises the mutex by hammering
// the same sessionID from many goroutines. All callers must observe the SAME
// tracker instance — no race-condition double-create.
func TestReadTrackerCache_ConcurrentGetOrCreate(t *testing.T) {
	c := NewReadTrackerCache()
	const N = 100
	var wg sync.WaitGroup
	results := make(chan any, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- c.GetOrCreate("hot-session")
		}()
	}
	wg.Wait()
	close(results)
	first := <-results
	for rt := range results {
		if rt != first {
			t.Fatal("concurrent GetOrCreate produced DIFFERENT trackers — mutex broken")
		}
	}
	if c.Len() != 1 {
		t.Errorf("expected exactly 1 cached tracker for the hot session, got %d", c.Len())
	}
}
