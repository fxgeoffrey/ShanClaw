package agent

import (
	"sync"
	"time"
)

// Suggestion is a single per-session suggestion record.
type Suggestion struct {
	// Text is the filtered suggestion text returned to Desktop.
	Text string
	// SuggestedAt is the time GenerateSuggestion returned successfully.
	SuggestedAt time.Time
	// SpeculationText is the pre-run assistant response if speculation
	// completed; empty if speculation is disabled, in flight, or failed.
	SpeculationText string
	// AcceptedAt is non-nil if the user accepted via POST /suggestion/accept.
	// The runner uses this to skip the next main-turn LLM call when the
	// speculated response is served instantly.
	AcceptedAt *time.Time
}

// SuggestionState holds the latest prompt suggestion per session.
// Cleared on new turn start (suggestion_handler.OnTurnStart) and on session
// close so a stale suggestion never resurfaces against a different conversation
// state.
//
// Concurrent-safe — all mutating methods take the write lock, all reads use the
// read lock. Get returns a copy of the Suggestion struct so callers cannot
// mutate internal state via the returned pointer.
type SuggestionState struct {
	mu    sync.RWMutex
	items map[string]*Suggestion // key: session id
}

// NewSuggestionState returns an empty state container.
func NewSuggestionState() *SuggestionState {
	return &SuggestionState{items: make(map[string]*Suggestion)}
}

// Set stores a new suggestion for sessionID, overwriting any prior entry.
// Should be called from GenerateSuggestion's caller after filtering succeeds.
func (s *SuggestionState) Set(sessionID, text string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[sessionID] = &Suggestion{
		Text:        text,
		SuggestedAt: at,
	}
}

// Get returns a snapshot of the current suggestion for sessionID, or
// (nil, false) if none. The returned *Suggestion is a fresh copy — mutating
// it does not affect the internal state.
func (s *SuggestionState) Get(sessionID string) (*Suggestion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.items[sessionID]
	if !ok {
		return nil, false
	}
	cp := *v
	return &cp, true
}

// Clear removes any suggestion for sessionID. Called on new turn start
// and on session close.
func (s *SuggestionState) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, sessionID)
}

// MarkAccepted records that the user accepted the current suggestion for
// sessionID. Returns false if no current suggestion exists for the session.
func (s *SuggestionState) MarkAccepted(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[sessionID]
	if !ok {
		return false
	}
	now := time.Now()
	v.AcceptedAt = &now
	return true
}

// SetSpeculation stores the pre-run response for the suggestion identified
// by (sessionID, suggestionText). If the current suggestion has been
// superseded (different text) or no entry exists, the speculation is silently
// discarded — stale speculations must not overwrite live state.
func (s *SuggestionState) SetSpeculation(sessionID, suggestionText, speculation string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[sessionID]
	if !ok {
		return
	}
	if v.Text != suggestionText {
		return // stale — current suggestion has moved on
	}
	v.SpeculationText = speculation
}
