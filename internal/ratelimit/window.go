package ratelimit

import (
	"sync"
	"time"
)

// Window is a fixed-window counter per key (not thread-safe across processes).
type Window struct {
	mu sync.Mutex
	m  map[string]*windowEntry
}

type windowEntry struct {
	count int
	reset time.Time
}

func NewWindow() *Window {
	return &Window{m: make(map[string]*windowEntry)}
}

// Allow increments the count for key if under limit within the window duration.
// The window resets when now > reset time.
func (w *Window) Allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return true
	}
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	e := w.m[key]
	if e == nil || now.After(e.reset) {
		w.m[key] = &windowEntry{count: 1, reset: now.Add(window)}
		return true
	}
	if e.count >= limit {
		return false
	}
	e.count++
	return true
}
