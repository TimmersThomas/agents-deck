package link

import (
	"bytes"
	"errors"
	"io"
	"sync"
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

func (f *fakePort) failNext()      { f.mu.Lock(); defer f.mu.Unlock(); f.failWrites = 1 }
func (f *fakePort) String() string { f.mu.Lock(); defer f.mu.Unlock(); return f.out.String() }
func (f *fakePort) Closes() int    { f.mu.Lock(); defer f.mu.Unlock(); return f.closes }

// hangPort blocks in Write until Close, like a port whose Close interrupts a pending write.
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

// stuckPort blocks its first Write until release is closed; Close does not wake it,
// as on Darwin where close(2) waits for a write in progress.
type stuckPort struct {
	started   chan struct{}
	release   chan struct{}
	first     sync.Once
	mu        sync.Mutex
	returned  bool
	closes    int
	earlyShut bool
}

func newStuckPort() *stuckPort {
	return &stuckPort{started: make(chan struct{}), release: make(chan struct{})}
}

func (p *stuckPort) Write(b []byte) (int, error) {
	p.first.Do(func() {
		close(p.started)
		<-p.release
		p.mu.Lock()
		p.returned = true
		p.mu.Unlock()
	})
	return 0, errors.New("stuck write failed")
}

func (p *stuckPort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closes++
	if !p.returned {
		p.earlyShut = true
	}
	return nil
}

func (p *stuckPort) Closes() int { p.mu.Lock(); defer p.mu.Unlock(); return p.closes }

// closedEarly reports whether Close ran while the write was still blocked.
func (p *stuckPort) closedEarly() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.earlyShut }

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

func (o *openSeq) gap(i int) time.Duration {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.times[i].Sub(o.times[i-1])
}

func seq(items ...openItem) *openSeq { return &openSeq{items: items} }

func ok(p io.WriteCloser) openItem { return openItem{port: p} }
func fail() openItem               { return failWith("no board") }
func failWith(msg string) openItem { return openItem{err: errors.New(msg)} }
