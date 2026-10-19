package link

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	retry   = 10 * time.Millisecond
	bound   = 2 * time.Second
	settle  = 5 * retry
	longDur = time.Minute
)

// fakePort is an in-memory port; its first failWrites writes fail.
type fakePort struct {
	mu         sync.Mutex
	out        bytes.Buffer
	closes     int
	failWrites int
	shortWrite bool
	closeErr   error
}

func (f *fakePort) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWrites > 0 {
		f.failWrites--
		return 0, errors.New("port gone")
	}
	if f.shortWrite {
		f.out.Write(p[:len(p)-1])
		return len(p) - 1, nil
	}
	return f.out.Write(p)
}

func (f *fakePort) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	return f.closeErr
}

func (f *fakePort) String() string { f.mu.Lock(); defer f.mu.Unlock(); return f.out.String() }
func (f *fakePort) Closes() int    { f.mu.Lock(); defer f.mu.Unlock(); return f.closes }

// hangPort blocks in Write until Close, like a serial port whose peer never reads.
type hangPort struct {
	started chan struct{}
	unblock chan struct{}
	exited  chan struct{}
	once    sync.Once
	mu      sync.Mutex
	closes  int
}

func newHangPort() *hangPort {
	return &hangPort{started: make(chan struct{}, 1), unblock: make(chan struct{}), exited: make(chan struct{})}
}

func (h *hangPort) Write([]byte) (int, error) {
	defer close(h.exited)
	h.started <- struct{}{}
	<-h.unblock
	return 0, io.ErrClosedPipe
}

func (h *hangPort) Close() error {
	h.mu.Lock()
	h.closes++
	h.mu.Unlock()
	h.once.Do(func() { close(h.unblock) })
	return nil
}

func (h *hangPort) Closes() int { h.mu.Lock(); defer h.mu.Unlock(); return h.closes }

// gatePort blocks its first Write until release is closed.
type gatePort struct {
	fakePort
	started chan struct{}
	release chan struct{}
	first   sync.Once
}

func (g *gatePort) Write(p []byte) (int, error) {
	g.first.Do(func() { close(g.started); <-g.release })
	return g.fakePort.Write(p)
}

// openSeq returns an OpenFunc handing out the given results in order, then the last one forever.
type openSeq struct {
	mu    sync.Mutex
	items []openItem
	times []time.Time
}

type openItem struct {
	port io.WriteCloser
	err  error
}

func (o *openSeq) open() (io.WriteCloser, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.times = append(o.times, time.Now())
	i := len(o.times) - 1
	if i >= len(o.items) {
		i = len(o.items) - 1
	}
	return o.items[i].port, o.items[i].err
}

func (o *openSeq) opens() int { o.mu.Lock(); defer o.mu.Unlock(); return len(o.times) }

func seq(items ...openItem) *openSeq { return &openSeq{items: items} }

func ok(p io.WriteCloser) openItem { return openItem{port: p} }
func fail() openItem               { return openItem{err: errors.New("no board")} }

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

func TestWritesFrames(t *testing.T) {
	out := &fakePort{}
	s := NewSender(seq(ok(out)).open, retry)
	frames := newFrames()
	start(t, s, frames)

	frames <- []byte("a\n")
	waitFor(t, func() bool { return out.String() == "a\n" })
}

func TestOpenErrorRetriesAndWritesLatestOnce(t *testing.T) {
	out := &fakePort{}
	o := seq(fail(), fail(), ok(out))
	frames := newFrames("one\n", "latest\n")
	start(t, NewSender(o.open, retry), frames)

	waitFor(t, func() bool { return out.String() == "latest\n" })
	time.Sleep(settle)
	if got := out.String(); got != "latest\n" {
		t.Fatalf("output grew to %q", got)
	}
}

func TestWriteFailureClosesPortAndResendsExactlyOnce(t *testing.T) {
	bad, good := &fakePort{failWrites: 1}, &fakePort{}
	o := seq(ok(bad), ok(good))
	frames := newFrames()
	start(t, NewSender(o.open, retry), frames)

	frames <- []byte("latest\n")
	waitFor(t, func() bool { return good.String() == "latest\n" })
	waitFor(t, func() bool { return bad.Closes() == 1 })
	time.Sleep(settle)
	if got := good.String(); got != "latest\n" {
		t.Fatalf("output after settle = %q, want exactly one resend", got)
	}
	if bad.String() != "" || o.opens() != 2 {
		t.Fatalf("bad output %q, opens %d", bad.String(), o.opens())
	}
}

func TestWaitsRetryAfterWriteFailure(t *testing.T) {
	const wait = 80 * time.Millisecond
	bad, good := &fakePort{failWrites: 1}, &fakePort{}
	o := seq(ok(bad), ok(good))
	frames := newFrames("x\n")
	start(t, NewSender(o.open, wait), frames)

	waitFor(t, func() bool { return good.String() == "x\n" })
	if gap := o.times[1].Sub(o.times[0]); gap < wait {
		t.Fatalf("reopened after %v, want at least %v", gap, wait)
	}
}

func TestCancelDuringRetryWaitReturns(t *testing.T) {
	bad := &fakePort{failWrites: 1}
	o := seq(ok(bad), ok(&fakePort{}))
	frames := newFrames("x\n")
	r := start(t, NewSender(o.open, longDur), frames)

	waitFor(t, func() bool { return bad.Closes() == 1 })
	r.cancel()
	r.waitDone(bound)
	if o.opens() != 1 {
		t.Fatalf("opens = %d, want 1", o.opens())
	}
}

func TestLatestOnlyWhileDisconnected(t *testing.T) {
	out := &fakePort{}
	var mu sync.Mutex
	connected := false
	open := func() (io.WriteCloser, error) {
		mu.Lock()
		defer mu.Unlock()
		if !connected {
			return nil, errors.New("no board")
		}
		return out, nil
	}
	frames := newFrames("a\n", "b\n", "c\n")
	start(t, NewSender(open, retry), frames)

	time.Sleep(settle)
	mu.Lock()
	connected = true
	mu.Unlock()
	waitFor(t, func() bool { return out.String() != "" })
	time.Sleep(settle)
	if got := out.String(); got != "c\n" {
		t.Fatalf("output = %q, want only the newest frame", got)
	}
}

func TestCoalescesQueuedFramesWhenConnected(t *testing.T) {
	g := &gatePort{started: make(chan struct{}), release: make(chan struct{})}
	frames := newFrames()
	start(t, NewSender(seq(ok(g)).open, retry), frames)

	frames <- []byte("a\n")
	<-g.started
	frames <- []byte("b\n")
	frames <- []byte("c\n")
	frames <- []byte("d\n")
	close(g.release)

	waitFor(t, func() bool { return g.String() == "a\nd\n" })
	time.Sleep(settle)
	if got := g.String(); got != "a\nd\n" {
		t.Fatalf("output = %q, want a then only the newest", got)
	}
}

func TestShortWriteIsAnError(t *testing.T) {
	short, good := &fakePort{shortWrite: true}, &fakePort{}
	o := seq(ok(short), ok(good))
	frames := newFrames("frame\n")
	start(t, NewSender(o.open, retry), frames)

	waitFor(t, func() bool { return good.String() == "frame\n" })
	waitFor(t, func() bool { return short.Closes() == 1 })
}

func TestCtxCancelClosesPort(t *testing.T) {
	out := &fakePort{}
	frames := newFrames("a\n")
	r := start(t, NewSender(seq(ok(out)).open, retry), frames)

	waitFor(t, func() bool { return out.String() == "a\n" })
	r.cancel()
	r.waitDone(bound)
	if out.Closes() != 1 {
		t.Fatalf("closes = %d, want 1", out.Closes())
	}
}

func TestCtxCancelWhileDisconnectedReturns(t *testing.T) {
	o := seq(fail())
	r := start(t, NewSender(o.open, retry), newFrames())

	waitFor(t, func() bool { return o.opens() >= 2 })
	r.cancel()
	r.waitDone(bound)
}

func TestClosedChannelClosesPortAndReturns(t *testing.T) {
	out := &fakePort{}
	frames := newFrames("a\n")
	r := start(t, NewSender(seq(ok(out)).open, retry), frames)

	waitFor(t, func() bool { return out.String() == "a\n" })
	close(frames)
	r.waitDone(bound)
	if out.Closes() != 1 {
		t.Fatalf("closes = %d, want 1", out.Closes())
	}
}

func TestClosedChannelWhileDisconnectedReturns(t *testing.T) {
	frames := newFrames()
	close(frames)
	r := start(t, NewSender(seq(fail()).open, retry), frames)
	r.waitDone(bound)
}

func TestClosedChannelWithQueuedFrameWhileDisconnectedReturns(t *testing.T) {
	frames := newFrames("a\n", "b\n")
	close(frames)
	r := start(t, NewSender(seq(fail()).open, retry), frames)
	r.waitDone(bound)
}

func TestClosedChannelWithQueuedFrameWhileConnectedReturns(t *testing.T) {
	out := &fakePort{}
	frames := newFrames("a\n", "b\n")
	close(frames)
	r := start(t, NewSender(seq(ok(out)).open, retry), frames)
	r.waitDone(bound)
	if out.Closes() != 1 {
		t.Fatalf("closes = %d, want 1", out.Closes())
	}
}

func TestHungWriteDoesNotBlockShutdown(t *testing.T) {
	h := newHangPort()
	s := NewSender(seq(ok(h)).open, retry)
	s.writeTimeout = longDur
	frames := newFrames("a\n")
	r := start(t, s, frames)

	<-h.started
	r.cancel()
	r.waitDone(bound / 2)
	waitFor(t, func() bool { return h.Closes() == 1 })
	select {
	case <-h.exited:
	case <-time.After(bound):
		t.Fatal("abandoned write goroutine did not exit after Close")
	}
}

func TestWriteTimeoutTriggersReconnect(t *testing.T) {
	h, good := newHangPort(), &fakePort{}
	o := seq(ok(h), ok(good))
	s := NewSender(o.open, retry)
	s.writeTimeout = 20 * time.Millisecond
	frames := newFrames("a\n")
	start(t, s, frames)

	waitFor(t, func() bool { return good.String() == "a\n" })
	waitFor(t, func() bool { return h.Closes() == 1 })
	select {
	case <-h.exited:
	case <-time.After(bound):
		t.Fatal("abandoned write goroutine did not exit after Close")
	}
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

func TestCloseErrorIsLogged(t *testing.T) {
	logs := &lockedBuf{}
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	out := &fakePort{closeErr: errors.New("close boom")}
	frames := newFrames("a\n")
	r := start(t, NewSender(seq(ok(out)).open, retry), frames)

	waitFor(t, func() bool { return out.String() == "a\n" })
	r.cancel()
	r.waitDone(bound)
	if !strings.Contains(logs.String(), "close boom") {
		t.Fatalf("close error not logged: %q", logs.String())
	}
}
