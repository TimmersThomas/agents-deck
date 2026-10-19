package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"desktop-controller/bridge/internal/events"
	"desktop-controller/bridge/internal/protocol"
)

const (
	w1 = "AAAAAAAA-0000-4000-8000-000000000001"
)

func running(surface, ws string) events.Event {
	return events.Event{Name: "sidebar.metadata.updated", Label: "Running", SurfaceID: surface, WorkspaceID: ws, At: time.UnixMilli(ms("11:00:00"))}
}

func actionHarness(t *testing.T) (*harness, chan protocol.Action, chan string) {
	t.Helper()
	actions := make(chan protocol.Action, 4)
	focus := make(chan string, 1)
	cfg := testCfg
	cfg.actions, cfg.focus = actions, focus
	return startRun(t, cfg), actions, focus
}

func TestFocusActionReachesTheCardsWorkspace(t *testing.T) {
	h, actions, focus := actionHarness(t)
	h.send(running(s1, w1))
	actions <- protocol.Action{Name: protocol.ActionFocus, ID: s1}
	select {
	case ws := <-focus:
		if ws != w1 {
			t.Fatalf("focused %q, want %q", ws, w1)
		}
	case <-time.After(bound):
		t.Fatal("focus not requested")
	}
}

func TestActionsForUnknownOrWorkspacelessCardsAreIgnored(t *testing.T) {
	h, actions, focus := actionHarness(t)
	h.send(running(s1, ""))
	actions <- protocol.Action{Name: protocol.ActionFocus, ID: "never-seen"}
	actions <- protocol.Action{Name: protocol.ActionFocus, ID: s1}
	select {
	case ws := <-focus:
		t.Fatalf("focused %q for a card that cannot be focused", ws)
	case <-time.After(quiet):
	}
}

func TestBusyFocusDropsTheTap(t *testing.T) {
	h, actions, focus := actionHarness(t)
	h.send(running(s1, w1))
	for i := 0; i < 3; i++ {
		actions <- protocol.Action{Name: protocol.ActionFocus, ID: s1}
	}
	time.Sleep(quiet)
	if got := len(focus); got != 1 {
		t.Fatalf("queued %d focus jobs, want 1 (cap)", got)
	}
}

func TestFocusWorkerRunsJobsOneByOneAndLogsFailures(t *testing.T) {
	jobs := make(chan string)
	var mu sync.Mutex
	var seen []string
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		focusWorker(ctx, jobs, func(_ context.Context, ws string) error {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, ws)
			if ws == "bad" {
				return errors.New("boom")
			}
			return nil
		})
	}()
	for _, ws := range []string{"bad", "good"} {
		select {
		case jobs <- ws:
		case <-time.After(bound):
			t.Fatal("worker stopped taking jobs after a failure")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(bound):
		t.Fatal("worker did not stop on cancel")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "bad" || seen[1] != "good" {
		t.Fatalf("seen %v", seen)
	}
}

func TestRunStillStopsWithActionsWired(t *testing.T) {
	h, _, _ := actionHarness(t)
	h.stop()
}
