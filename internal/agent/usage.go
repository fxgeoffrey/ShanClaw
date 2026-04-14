package agent

import (
	"context"
	"sync"
)

// usageEmitKey is a context value used by the agent loop to expose a per-run
// usage sink to tools. Tools that report their own cost (gateway tools calling
// xAI/Grok, SerpAPI, etc.) call EmitUsage with the converted TurnUsage; the
// handler attached to the loop receives it like any other OnUsage event.
type usageEmitKey struct{}

// WithUsageEmit returns a context carrying a usage emitter. Callers (typically
// the agent loop at tool dispatch time) wrap their ctx with this before
// handing it to a tool's Run method.
func WithUsageEmit(ctx context.Context, emit func(TurnUsage)) context.Context {
	if emit == nil {
		return ctx
	}
	return context.WithValue(ctx, usageEmitKey{}, emit)
}

// EmitUsage forwards a usage report through the ctx-attached emitter.
// Safe to call from any tool — no-op if the ctx has no emitter.
func EmitUsage(ctx context.Context, u TurnUsage) {
	if emit, ok := ctx.Value(usageEmitKey{}).(func(TurnUsage)); ok && emit != nil {
		emit(u)
	}
}

// UsageProvider is the optional interface a handler can satisfy to expose
// its accumulated usage. Callers (daemon runner, CLI, TUI) type-assert and
// read Usage() at end-of-run for persistence/display. Returning TurnUsage
// (not a pointer) means the snapshot is immutable from the caller's side.
type UsageProvider interface {
	Usage() TurnUsage
}

// UsageAccumulator is a thread-safe collector that handlers embed to
// aggregate TurnUsage events across a run/session. It is intentionally
// decoupled from TurnUsage's cache-telemetry state (which is loop-internal)
// so it can be safely mutated from any goroutine.
//
// Typical flow:
//  1. Handler embeds an UsageAccumulator (value or pointer).
//  2. Handler's OnUsage(u TurnUsage) calls accumulator.Add(u).
//  3. Caller (cmd/root.go, daemon runner, TUI) queries accumulator.Snapshot()
//     at end-of-run and persists it into the session.
//
// The zero value is ready to use.
type UsageAccumulator struct {
	mu    sync.Mutex
	total TurnUsage
}

// Add merges an incoming TurnUsage delta into the running total.
// Model carries "last seen wins" — tracks the most recent model
// identifier reported by the gateway.
func (a *UsageAccumulator) Add(u TurnUsage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.total.InputTokens += u.InputTokens
	a.total.OutputTokens += u.OutputTokens
	a.total.TotalTokens += u.TotalTokens
	a.total.CostUSD += u.CostUSD
	a.total.CacheReadTokens += u.CacheReadTokens
	a.total.CacheCreationTokens += u.CacheCreationTokens
	a.total.LLMCalls += u.LLMCalls
	if u.Model != "" {
		a.total.Model = u.Model
	}
}

// Snapshot returns the current cumulative totals.
// Safe to call from any goroutine.
func (a *UsageAccumulator) Snapshot() TurnUsage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.total
}

// Reset clears accumulated totals. Use between independent runs in a
// long-lived handler (e.g. daemon per-message handler reuse).
func (a *UsageAccumulator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.total = TurnUsage{}
}
