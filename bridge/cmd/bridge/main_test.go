package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"desktop-controller/bridge/internal/events"
	"desktop-controller/bridge/internal/protocol"
)

const (
	emit  = 5 * time.Millisecond
	quiet = 40 * emit
	bound = 2 * time.Second
	s1    = "0000BBBB-0000-4000-8000-000000000001"
	s2    = "0000BBBB-0000-4000-8000-000000000002"
	s3    = "0000BBBB-0000-4000-8000-000000000003"
	s4    = "0000BBBB-0000-4000-8000-000000000004"
)

// The fixture is dated 2026-10-18 and Prune uses wall-clock time.
var testCfg = config{maxAge: 100000 * time.Hour, emitEvery: emit, heartbeatEvery: time.Hour}

type frame struct {
	Total int             `json:"total"`
	Cards []protocol.Card `json:"cards"`
}

func ms(clock string) int64 {
	t, err := time.Parse(time.RFC3339, "2026-10-18T"+clock+"Z")
	if err != nil {
		panic(err)
	}
	return t.UnixMilli()
}

var wantSession = frame{Total: 4, Cards: []protocol.Card{
	{ID: s4, Title: "DEV-24-delta", Status: protocol.Waiting, SinceMs: ms("11:08:20")},
	{ID: s1, Title: "DEV-21-alpha", Status: protocol.Working, SinceMs: ms("11:01:00")},
	{ID: s3, Title: "DEV-23-gamma", Status: protocol.Done, SinceMs: ms("11:07:00")},
	{ID: s2, Title: "DEV-22-beta", Status: protocol.Done, SinceMs: ms("11:04:00")},
}}

func fixture(t *testing.T, path string) []events.Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []events.Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		e, err := events.ParseLine(sc.Bytes())
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

type harness struct {
	t      *testing.T
	evs    chan events.Event
	frames chan []byte
	cancel context.CancelFunc
	done   chan struct{}
}

func startRun(t *testing.T, cfg config) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, evs: make(chan events.Event), frames: make(chan []byte, 1), cancel: cancel, done: make(chan struct{})}
	go func() { defer close(h.done); run(ctx, h.evs, h.frames, cfg) }()
	t.Cleanup(h.stop)
	return h
}

func (h *harness) send(es ...events.Event) {
	h.t.Helper()
	for _, e := range es {
		select {
		case h.evs <- e:
		case <-time.After(bound):
			h.t.Fatal("run stopped consuming events")
		}
	}
}

func (h *harness) stop() {
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(bound):
		h.t.Error("run did not return after cancel")
	}
}

func decode(t *testing.T, b []byte) frame {
	t.Helper()
	var f frame
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("frame %q: %v", b, err)
	}
	return f
}

// next returns the next frame, or ok=false if none arrives within d.
func (h *harness) next(d time.Duration) (_ frame, ok bool) {
	select {
	case b := <-h.frames:
		return decode(h.t, b), true
	case <-time.After(d):
		return frame{}, false
	}
}

// settle waits for the frame matching want, then for the frames to go quiet.
func (h *harness) settle(want frame) {
	h.t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if f, ok := h.next(quiet); ok && reflect.DeepEqual(f, want) {
			if extra, ok := h.next(quiet); ok {
				h.t.Fatalf("unexpected frame after settling: %+v", extra)
			}
			return
		}
	}
	h.t.Fatalf("no frame matched %+v", want)
}
