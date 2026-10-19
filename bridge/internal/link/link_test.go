package link

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWritesFrames(t *testing.T) {
	out := &fakePort{}
	s := NewSender(seq(ok(out)).open, retry)
	frames := newFrames()
	start(t, s, frames)

	frames <- []byte("a\n")
	waitFor(t, func() bool { return out.String() == "\na\n" })
}

func TestOpenErrorRetriesAndWritesLatestOnce(t *testing.T) {
	out := &fakePort{}
	o := seq(fail(), fail(), ok(out))
	frames := newFrames("one\n", "latest\n")
	start(t, NewSender(o.open, retry), frames)

	waitFor(t, func() bool { return out.String() == "\nlatest\n" })
	time.Sleep(settle)
	if got := out.String(); got != "\nlatest\n" {
		t.Fatalf("output grew to %q", got)
	}
}

func TestOpenErrorLoggedOnlyWhenItChanges(t *testing.T) {
	logs := captureLogs(t)
	out := &fakePort{}
	o := seq(failWith("no board"), failWith("no board"), failWith("busy"), failWith("busy"), ok(out))
	start(t, NewSender(o.open, retry), newFrames("a\n"))

	waitFor(t, func() bool { return out.String() == "\na\n" })
	got := logs.String()
	if n := strings.Count(got, "no board"); n != 1 {
		t.Fatalf("logged %q %d times, want 1:\n%s", "no board", n, got)
	}
	if n := strings.Count(got, "busy"); n != 1 {
		t.Fatalf("logged %q %d times, want 1:\n%s", "busy", n, got)
	}
	if n := strings.Count(got, "link: connected"); n != 1 {
		t.Fatalf("recovery logged %d times, want 1:\n%s", n, got)
	}
}

func TestWriteFailureClosesPortAndResendsExactlyOnce(t *testing.T) {
	bad, good := &fakePort{failWrites: 1}, &fakePort{}
	o := seq(ok(bad), ok(good))
	frames := newFrames()
	start(t, NewSender(o.open, retry), frames)

	frames <- []byte("latest\n")
	waitFor(t, func() bool { return good.String() == "\nlatest\n" })
	waitFor(t, func() bool { return bad.Closes() == 1 })
	time.Sleep(settle)
	if got := good.String(); got != "\nlatest\n" {
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

	waitFor(t, func() bool { return good.String() == "\nx\n" })
	if gap := o.gap(1); gap < wait {
		t.Fatalf("reopened after %v, want at least %v", gap, wait)
	}
}

func TestNewlinePrefixOnlyOnFirstWriteAfterEachConnect(t *testing.T) {
	first, second := &fakePort{}, &fakePort{}
	frames := newFrames("a\n")
	start(t, NewSender(seq(ok(first), ok(second)).open, retry), frames)

	waitFor(t, func() bool { return first.String() == "\na\n" })
	frames <- []byte("b\n")
	waitFor(t, func() bool { return first.String() == "\na\nb\n" })
	first.failNext()
	frames <- []byte("c\n")
	waitFor(t, func() bool { return second.String() == "\nc\n" })
	frames <- []byte("d\n")
	waitFor(t, func() bool { return second.String() == "\nc\nd\n" })
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
	if got := out.String(); got != "\nc\n" {
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

	waitFor(t, func() bool { return g.String() == "\na\nd\n" })
	time.Sleep(settle)
	if got := g.String(); got != "\na\nd\n" {
		t.Fatalf("output = %q, want a then only the newest", got)
	}
}

func TestShortWriteIsAnError(t *testing.T) {
	short, good := &fakePort{shortWrite: true}, &fakePort{}
	o := seq(ok(short), ok(good))
	frames := newFrames("frame\n")
	start(t, NewSender(o.open, retry), frames)

	waitFor(t, func() bool { return good.String() == "\nframe\n" })
	waitFor(t, func() bool { return short.Closes() == 1 })
}
