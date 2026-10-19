package link

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type running struct {
	t      *testing.T
	cancel context.CancelFunc
	done   chan struct{}
}

func start(t *testing.T, s *Sender, frames <-chan []byte) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{t: t, cancel: cancel, done: make(chan struct{})}
	go func() { defer close(r.done); s.Run(ctx, frames) }()
	t.Cleanup(func() { r.stop() })
	return r
}

func (r *running) waitDone(d time.Duration) bool {
	r.t.Helper()
	select {
	case <-r.done:
		return true
	case <-time.After(d):
		r.t.Errorf("Run did not return within %v", d)
		return false
	}
}

func (r *running) stop() { r.cancel(); r.waitDone(bound) }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(time.Millisecond)
	}
}

func newFrames(frames ...string) chan []byte {
	ch := make(chan []byte, 16)
	for _, f := range frames {
		ch <- []byte(f)
	}
	return ch
}

type lockedBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func captureLogs(t *testing.T) *lockedBuf {
	t.Helper()
	logs := &lockedBuf{}
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return logs
}
