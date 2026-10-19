// Package logx holds small logging helpers.
package logx

import "sync"

const (
	firstAllowed = 5
	allowEvery   = 1000
)

// Limiter lets the first few calls through, then every thousandth, so a noisy
// source cannot flood the log. The zero value is ready; it is safe for concurrent use.
type Limiter struct {
	mu sync.Mutex
	n  uint64
}

// Next counts one call and returns the running count and whether to log it.
func (l *Limiter) Next() (n uint64, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.n++
	return l.n, l.n <= firstAllowed || l.n%allowEvery == 0
}

// Allow reports whether this call should be logged.
func (l *Limiter) Allow() bool {
	_, ok := l.Next()
	return ok
}
