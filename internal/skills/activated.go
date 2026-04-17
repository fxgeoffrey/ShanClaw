package skills

import (
	"context"
	"sort"
	"sync"
)

type activatedKey struct{}

// ActivatedSet tracks which skills have been activated via use_skill
// in the current agent run. Used by bash tool to decide which skill
// secrets to expose as environment variables. Thread-safe.
type ActivatedSet struct {
	mu sync.Mutex
	m  map[string]struct{}
}

// NewActivatedSet returns an empty set.
func NewActivatedSet() *ActivatedSet {
	return &ActivatedSet{m: make(map[string]struct{})}
}

// Add records a skill as activated. No-op on nil receiver.
func (s *ActivatedSet) Add(name string) {
	if s == nil || name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[name] = struct{}{}
}

// Names returns the sorted list of activated skill names.
func (s *ActivatedSet) Names() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// WithActivatedSet stores the set in the context.
func WithActivatedSet(ctx context.Context, s *ActivatedSet) context.Context {
	return context.WithValue(ctx, activatedKey{}, s)
}

// ActivatedFromContext retrieves the set from context, or nil if unset.
func ActivatedFromContext(ctx context.Context) *ActivatedSet {
	s, _ := ctx.Value(activatedKey{}).(*ActivatedSet)
	return s
}
