package tools

import (
	"fmt"
	"hash/fnv"
	"math"
	"sync"
)

func agentColor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	hue := float64(h.Sum32() % 360)
	r, g, b := hslToRGB(hue, 0.65, 0.45)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func hslToRGB(h, s, l float64) (uint8, uint8, uint8) {
	c := (1 - math.Abs(2*l-1)) * s
	hPrime := h / 60
	x := c * (1 - math.Abs(math.Mod(hPrime, 2)-1))
	var r1, g1, b1 float64
	switch {
	case hPrime < 1:
		r1, g1, b1 = c, x, 0
	case hPrime < 2:
		r1, g1, b1 = x, c, 0
	case hPrime < 3:
		r1, g1, b1 = 0, c, x
	case hPrime < 4:
		r1, g1, b1 = 0, x, c
	case hPrime < 5:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	m := l - c/2
	return uint8(math.Round((r1 + m) * 255)),
		uint8(math.Round((g1 + m) * 255)),
		uint8(math.Round((b1 + m) * 255))
}

// Tab registry

type tabRef struct {
	windowIndex int
	tabIndex    int
}

type tabRegistry struct {
	mu   sync.RWMutex
	tabs map[string]tabRef
}

func newTabRegistry() *tabRegistry {
	return &tabRegistry{tabs: make(map[string]tabRef)}
}

func (r *tabRegistry) add(title string, ref tabRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tabs[title] = ref
}

func (r *tabRegistry) lookup(title string) (tabRef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ref, ok := r.tabs[title]
	return ref, ok
}

func (r *tabRegistry) list() map[string]tabRef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]tabRef, len(r.tabs))
	for k, v := range r.tabs {
		out[k] = v
	}
	return out
}
