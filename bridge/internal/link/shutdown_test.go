package link

import (
	"errors"
	"strings"
	"testing"
	"time"
)

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

func TestCtxCancelClosesPort(t *testing.T) {
	out := &fakePort{}
	frames := newFrames("a\n")
	r := start(t, NewSender(seq(ok(out)).open, retry), frames)

	waitFor(t, func() bool { return out.String() == "\na\n" })
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

	waitFor(t, func() bool { return out.String() == "\na\n" })
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

func TestCloseErrorIsLogged(t *testing.T) {
	logs := captureLogs(t)
	out := &fakePort{closeErr: errors.New("close boom")}
	frames := newFrames("a\n")
	r := start(t, NewSender(seq(ok(out)).open, retry), frames)

	waitFor(t, func() bool { return out.String() == "\na\n" })
	r.cancel()
	r.waitDone(bound)
	if !strings.Contains(logs.String(), "close boom") {
		t.Fatalf("close error not logged: %q", logs.String())
	}
}

func TestCloseErrorAfterWriteFailureIsLogged(t *testing.T) {
	logs := captureLogs(t)
	bad := &fakePort{failWrites: 1, closeErr: errors.New("close boom")}
	good := &fakePort{}
	start(t, NewSender(seq(ok(bad), ok(good)).open, retry), newFrames("a\n"))

	waitFor(t, func() bool { return good.String() == "\na\n" })
	if !strings.Contains(logs.String(), "close boom") {
		t.Fatalf("close error not logged: %q", logs.String())
	}
}
