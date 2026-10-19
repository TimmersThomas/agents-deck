package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"desktop-controller/bridge/internal/link"
	"desktop-controller/bridge/internal/state"
)

const (
	w2 = "AAAAAAAA-0000-4000-8000-000000000002"
	w3 = "AAAAAAAA-0000-4000-8000-000000000003"

	focusLine = `{"v":1,"t":"action","a":"focus","id":"` + s1 + `"}` + "\n"
)

type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// waitLog waits until the captured log holds at least n copies of substr.
func (l *logBuf) waitLog(t *testing.T, substr string, n int) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if strings.Count(l.String(), substr) >= n {
			return
		}
		time.Sleep(emit)
	}
	t.Fatalf("log never held %d x %q; log:\n%s", n, substr, l.String())
}

func captureLog(t *testing.T) *logBuf {
	t.Helper()
	l := &logBuf{}
	prev := log.Writer()
	log.SetOutput(l)
	t.Cleanup(func() { log.SetOutput(prev) })
	return l
}

// duplexPort is an in-memory serial port: tests feed it through pw, writes are recorded.
type duplexPort struct {
	pr *io.PipeReader
	pw *io.PipeWriter
	mu sync.Mutex
	wr bytes.Buffer
}

func newDuplexPort() *duplexPort {
	pr, pw := io.Pipe()
	return &duplexPort{pr: pr, pw: pw}
}

func (p *duplexPort) Read(b []byte) (int, error) { return p.pr.Read(b) }

func (p *duplexPort) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.wr.Write(b)
}

func (p *duplexPort) Close() error {
	p.pw.Close()
	return p.pr.Close()
}

// feed writes one line to the port; it fails the test if nothing reads it.
func (p *duplexPort) feed(t *testing.T, line string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { _, err := p.pw.Write([]byte(line)); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("feed: %v", err)
		}
	case <-time.After(bound):
		t.Fatal("feed: nothing is reading the port")
	}
}

// wiring is withActions as main builds it, on a fake port and a fake focus.
type wiring struct {
	cfg    config
	port   *duplexPort
	sender *link.Sender
	focus  chan string
	ctx    context.Context
	cancel context.CancelFunc
}

// newWiring registers cleanup that cancels and joins the sender; call before starting run.
func newWiring(t *testing.T) *wiring {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	w := &wiring{port: newDuplexPort(), focus: make(chan string, 8), ctx: ctx, cancel: cancel}
	w.sender = link.NewSender(func() (io.WriteCloser, error) { return w.port, nil }, time.Millisecond)
	fake := func(ctx context.Context, ws string) error {
		select {
		case w.focus <- ws:
		case <-ctx.Done():
		}
		return nil
	}
	w.cfg = withActions(ctx, testCfg, w.sender, fake)
	t.Cleanup(cancel)
	return w
}

// runSender starts the sender and joins it at test end.
func (w *wiring) runSender(t *testing.T, frames <-chan []byte) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); w.sender.Run(w.ctx, frames) }()
	t.Cleanup(func() {
		w.cancel()
		select {
		case <-done:
		case <-time.After(bound):
			t.Error("sender did not return after cancel")
		}
	})
}

func startWired(t *testing.T) (*harness, *wiring) {
	t.Helper()
	w := newWiring(t)
	h := startRun(t, w.cfg)
	w.runSender(t, h.frames)
	return h, w
}

func TestWiredFocusLineReachesTheFocusFunction(t *testing.T) {
	h, w := startWired(t)
	h.send(running(s1, w1))
	w.port.feed(t, focusLine)
	select {
	case ws := <-w.focus:
		if ws != w1 {
			t.Fatalf("focused %q, want %q", ws, w1)
		}
	case <-time.After(bound):
		t.Fatal("focus not requested through the port")
	}
}

func TestWiredGarbageAndUnknownIDsFocusNothingAndAreNotLogged(t *testing.T) {
	logs := captureLog(t)
	h, w := startWired(t)
	h.send(running(s1, w1))
	const garbage, unknown = "GARBAGE-SENTINEL-7f3a", "UNKNOWN-SENTINEL-9c1d"
	w.port.feed(t, garbage+"\n")
	w.port.feed(t, `{"v":1,"t":"action","a":"focus","id":"`+unknown+`"}`+"\n")
	logs.waitLog(t, "parse action", 1)
	logs.waitLog(t, "card unknown", 1)
	select {
	case ws := <-w.focus:
		t.Fatalf("focused %q for garbage or an unknown id", ws)
	case <-time.After(quiet):
	}
	if out := logs.String(); strings.Contains(out, garbage) || strings.Contains(out, unknown) {
		t.Fatalf("log repeats untrusted input:\n%s", out)
	}
}

func TestWiredGarbageFloodIsRateLimited(t *testing.T) {
	logs := captureLog(t)
	h, w := startWired(t)
	h.send(running(s1, w1))
	const sentinel = "FLOOD-SENTINEL-4b2e"
	for i := 0; i < 50; i++ {
		w.port.feed(t, fmt.Sprintf("%s %d\n", sentinel, i))
	}
	w.port.feed(t, focusLine)
	select {
	case <-w.focus:
	case <-time.After(bound):
		t.Fatal("valid line after the flood was not handled")
	}
	out := logs.String()
	if got := strings.Count(out, "parse action"); got != 5 {
		t.Fatalf("%d parse-error logs for 50 garbage lines, want 5; log:\n%s", got, out)
	}
	if strings.Contains(out, sentinel) {
		t.Fatalf("log repeats untrusted input:\n%s", out)
	}
}

func TestUnfocusableCardLogsAreRateLimitedAndNameTheCause(t *testing.T) {
	logs := captureLog(t)
	var s state.State
	s, _ = state.Apply(s, running(s1, ""))
	jobs, al := make(chan string, 1), newActionLogs()
	for i := 0; i < 50; i++ {
		handleAction(s, actionFor("never-seen-SENTINEL"), jobs, al)
		handleAction(s, actionFor(s1), jobs, al)
	}
	out := logs.String()
	if got := strings.Count(out, "card unknown"); got != 5 {
		t.Fatalf("%d 'card unknown' logs, want 5; log:\n%s", got, out)
	}
	if got := strings.Count(out, "card has no workspace"); got != 5 {
		t.Fatalf("%d 'card has no workspace' logs, want 5; log:\n%s", got, out)
	}
	if strings.Contains(out, "SENTINEL") || strings.Contains(out, s1) || len(jobs) != 0 {
		t.Fatalf("id leaked or focus queued (%d); log:\n%s", len(jobs), out)
	}
}

func TestWiredQueueFullDropsAndKeepsReading(t *testing.T) {
	logs := captureLog(t)
	w := newWiring(t)
	w.runSender(t, make(chan []byte, 1)) // run is not started: nobody drains cfg.actions
	const extra = 3
	for i := 0; i < cap(w.cfg.actions)+extra; i++ {
		w.port.feed(t, focusLine)
	}
	w.port.feed(t, "MARKER\n")
	logs.waitLog(t, "parse action", 1)
	if got := strings.Count(logs.String(), "queue full"); got != extra {
		t.Fatalf("%d queue-full drops, want %d; log:\n%s", got, extra, logs.String())
	}
	if got := len(w.cfg.actions); got != cap(w.cfg.actions) {
		t.Fatalf("%d actions queued, want %d", got, cap(w.cfg.actions))
	}
}

// gatedFocus blocks every focus call until release is closed.
type gatedFocus struct {
	started chan string
	release chan struct{}
	mu      sync.Mutex
	ran     []string
}

func (g *gatedFocus) focus(ctx context.Context, ws string) error {
	g.started <- ws
	select {
	case <-g.release:
	case <-ctx.Done():
	}
	g.mu.Lock()
	g.ran = append(g.ran, ws)
	g.mu.Unlock()
	return nil
}

func (g *gatedFocus) done() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.ran...)
}

func busyState(t *testing.T) state.State {
	t.Helper()
	var s state.State
	for _, c := range [][2]string{{s1, w1}, {s2, w2}, {s3, w3}} {
		s, _ = state.Apply(s, running(c[0], c[1]))
	}
	return s
}

func TestMidFocusQueuesOneTapAndDropsTheRest(t *testing.T) {
	logs := captureLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	g := &gatedFocus{started: make(chan string, 4), release: make(chan struct{})}
	workerDone := make(chan struct{})
	jobs := make(chan string, 1) // as withActions makes it
	go func() { defer close(workerDone); focusWorker(ctx, jobs, g.focus) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-workerDone:
		case <-time.After(bound):
			t.Error("worker did not stop")
		}
	})

	s := busyState(t)
	tap := func(id string) {
		t.Helper()
		done := make(chan struct{})
		go func() { defer close(done); handleAction(s, actionFor(id), jobs, newActionLogs()) }()
		select {
		case <-done:
		case <-time.After(bound):
			t.Fatal("handleAction blocked on a busy worker")
		}
	}

	tap(s1)
	select {
	case ws := <-g.started:
		if ws != w1 {
			t.Fatalf("worker took %q first, want %q", ws, w1)
		}
	case <-time.After(bound):
		t.Fatal("worker never started the first focus")
	}
	tap(s2)
	if got := len(jobs); got != 1 {
		t.Fatalf("%d queued jobs while busy, want 1", got)
	}
	tap(s3)
	if got := strings.Count(logs.String(), "focus busy"); got != 1 {
		t.Fatalf("%d 'focus busy' logs, want 1; log:\n%s", got, logs.String())
	}

	close(g.release)
	select {
	case ws := <-g.started:
		if ws != w2 {
			t.Fatalf("worker took %q second, want %q", ws, w2)
		}
	case <-time.After(bound):
		t.Fatal("queued tap never ran")
	}
	select {
	case ws := <-g.started:
		t.Fatalf("dropped tap ran: %q", ws)
	case <-time.After(quiet):
	}
	if got := g.done(); len(got) != 2 || got[0] != w1 || got[1] != w2 {
		t.Fatalf("ran %v, want [%s %s]", got, w1, w2)
	}
}

func TestNewFocuserIsReadyToRun(t *testing.T) {
	f := newFocuser("my-cmux")
	if f.Cmux != "my-cmux" {
		t.Fatalf("Cmux = %q", f.Cmux)
	}
	if f.Timeout != focusTimeout || f.Timeout <= 0 {
		t.Fatalf("Timeout = %v, want focusTimeout (%v) > 0", f.Timeout, focusTimeout)
	}
	if f.Run == nil {
		t.Fatal("Run is nil")
	}
}

func TestWarnIfMissing(t *testing.T) {
	logs := captureLog(t)
	if err := warnIfMissing("no-such-cmux-binary-7f3a"); err == nil {
		t.Fatal("no error for a binary that does not exist")
	}
	if !strings.Contains(logs.String(), "focus will fail") {
		t.Fatalf("no warning logged: %q", logs.String())
	}
	if err := warnIfMissing(os.Args[0]); err != nil {
		t.Fatalf("existing path reported missing: %v", err)
	}
}
