package link

import (
	"strings"
	"testing"
	"time"
)

const stallTimeout = 20 * time.Millisecond

func stalledSender(o *openSeq) *Sender {
	s := NewSender(o.open, retry)
	s.writeTimeout = stallTimeout
	return s
}

func TestStalledWriteBlocksReopenUntilItReturns(t *testing.T) {
	stuck, good := newStuckPort(), &fakePort{}
	o := seq(ok(stuck), ok(good))
	frames := newFrames("a\n")
	start(t, stalledSender(o), frames)

	<-stuck.started
	for _, f := range []string{"b\n", "c\n", "d\n"} {
		time.Sleep(3 * retry)
		frames <- []byte(f)
	}
	time.Sleep(stallTimeout + 10*retry)
	if n := o.opens(); n != 1 {
		t.Fatalf("opens = %d while a write is stalled, want 1", n)
	}
	if len(frames) != 0 {
		t.Fatalf("%d frames left unconsumed while stalled", len(frames))
	}

	close(stuck.release)
	waitFor(t, func() bool { return good.String() == "\nd\n" })
	if stuck.closedEarly() || stuck.Closes() != 1 {
		t.Fatalf("closedEarly=%v closes=%d, want one close after the write returned", stuck.closedEarly(), stuck.Closes())
	}
	if n := o.opens(); n != 2 {
		t.Fatalf("opens = %d after release, want 2", n)
	}
}

func TestCancelWhileStalledReturnsPromptly(t *testing.T) {
	stuck := newStuckPort()
	o := seq(ok(stuck), ok(&fakePort{}))
	r := start(t, stalledSender(o), newFrames("a\n"))
	t.Cleanup(func() { close(stuck.release) })

	<-stuck.started
	time.Sleep(stallTimeout + 5*retry)
	r.cancel()
	r.waitDone(bound / 4)
	if n := o.opens(); n != 1 {
		t.Fatalf("opens = %d, want 1", n)
	}
}

func TestClosedChannelWhileStalledReturns(t *testing.T) {
	stuck := newStuckPort()
	frames := newFrames("a\n")
	r := start(t, stalledSender(seq(ok(stuck))), frames)
	t.Cleanup(func() { close(stuck.release) })

	<-stuck.started
	time.Sleep(stallTimeout + 5*retry)
	close(frames)
	r.waitDone(bound / 4)
}

func TestStallLoggedOnce(t *testing.T) {
	logs := captureLogs(t)
	stuck := newStuckPort()
	start(t, stalledSender(seq(ok(stuck), ok(&fakePort{}))), newFrames("a\n"))
	t.Cleanup(func() { close(stuck.release) })

	<-stuck.started
	time.Sleep(stallTimeout + 10*retry)
	if n := strings.Count(logs.String(), "port stalled"); n != 1 {
		t.Fatalf("stall logged %d times, want 1:\n%s", n, logs.String())
	}
}
