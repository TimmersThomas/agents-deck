package main

import (
	"reflect"
	"testing"
	"time"

	"desktop-controller/bridge/internal/events"
	"desktop-controller/bridge/internal/protocol"
)

func TestReplaySessionFixture(t *testing.T) {
	h := startRun(t, testCfg)
	h.send(fixture(t, "../../testdata/session.jsonl")...)
	h.settle(wantSession)
}

func TestReplayOriginalFixture(t *testing.T) {
	h := startRun(t, testCfg)
	h.send(fixture(t, "../../testdata/events.jsonl")...)
	h.settle(frame{Total: 1, Cards: []protocol.Card{
		{ID: "S1", Title: "DEV-1-fix", Status: protocol.Idle, SinceMs: ms("10:01:00")},
	}})
}

func unrelated(at time.Time) []events.Event {
	return []events.Event{
		{Name: "window.keyed", WorkspaceID: "W1", At: at},
		{Name: "agent.hook.PreToolUse", Hook: "PreToolUse", SurfaceID: s1, Cwd: "/w/DEV-21-alpha/src", At: at},
		{Name: "sidebar.metadata.updated", Label: "Waiting", SurfaceID: s2, At: at},
		{Name: "agent.hook.Notification", Hook: "Notification", SurfaceID: s2, Cwd: "/w/x", At: at},
		{Name: "surface.closed", SurfaceID: "0000BBBB-0000-4000-8000-00000000000e", At: at},
	}
}

func TestBurstYieldsOnlyRealChanges(t *testing.T) {
	h := startRun(t, testCfg)
	h.send(fixture(t, "../../testdata/session.jsonl")...)
	h.settle(wantSession)

	at := time.UnixMilli(ms("11:10:00"))
	for i := 0; i < 20; i++ {
		h.send(unrelated(at)...)
	}
	if f, ok := h.next(quiet); ok {
		t.Fatalf("unrelated burst produced a frame: %+v", f)
	}

	h.send(unrelated(at)...)
	h.send(events.Event{Name: "sidebar.metadata.updated", Label: "Needs input", SurfaceID: s1, At: at})
	h.send(unrelated(at)...)
	want := wantSession
	want.Cards = []protocol.Card{
		{ID: s1, Title: "DEV-21-alpha", Status: protocol.Waiting, SinceMs: ms("11:10:00")},
		{ID: s4, Title: "DEV-24-delta", Status: protocol.Waiting, SinceMs: ms("11:08:20")},
		wantSession.Cards[2], wantSession.Cards[3],
	}
	f, ok := h.next(bound)
	if !ok || !reflect.DeepEqual(f, want) {
		t.Fatalf("got %+v (ok=%v), want %+v", f, ok, want)
	}
	if extra, ok := h.next(quiet); ok {
		t.Fatalf("one change produced a second frame: %+v", extra)
	}
}

func TestHeartbeatWhenIdle(t *testing.T) {
	cfg := testCfg
	cfg.heartbeatEvery = 30 * time.Millisecond
	h := startRun(t, cfg)
	h.send(fixture(t, "../../testdata/session.jsonl")...)
	for i := 0; i < 3; i++ {
		f, ok := h.next(bound)
		if !ok {
			t.Fatalf("no heartbeat frame %d", i)
		}
		if i > 0 && !reflect.DeepEqual(f, wantSession) {
			t.Fatalf("heartbeat %d = %+v", i, f)
		}
	}
}

func TestNeverBlocksOnUnconsumedFrames(t *testing.T) {
	h := startRun(t, testCfg)
	es := fixture(t, "../../testdata/session.jsonl")
	for _, e := range es {
		h.send(e)
		time.Sleep(emit / 2)
	}
	time.Sleep(quiet)
	h.stop()
	select {
	case b := <-h.frames:
		if f := decode(t, b); !reflect.DeepEqual(f, wantSession) {
			t.Fatalf("queued frame %+v, want the latest state", f)
		}
	default:
		t.Fatal("no frame queued")
	}
}
