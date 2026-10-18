package state

import (
	"testing"
	"time"

	"desktop-controller/bridge/internal/events"
	"desktop-controller/bridge/internal/protocol"
)

var t0 = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

func status(label, surface string, at time.Time) events.Event {
	return events.Event{Name: "sidebar.metadata.updated", Label: label, SurfaceID: surface, At: at}
}

func hook(surface, cwd string) events.Event {
	return events.Event{Name: "agent.hook.SessionStart", Hook: "SessionStart", SurfaceID: surface, Cwd: cwd, At: t0}
}

func TestApplyMapsLabelsToStatus(t *testing.T) {
	cases := map[string]protocol.Status{
		"Running":            protocol.Working,
		"Running subagents":  protocol.Working,
		"Waiting":            protocol.Working,
		"Needs input":        protocol.Waiting,
		"Idle":               protocol.Idle,
		"API error":          protocol.Error,
		"Connection dropped": protocol.Error,
	}
	for label, want := range cases {
		cards := Apply(State{}, status(label, "S1", t0)).Cards()
		if len(cards) != 1 || cards[0].Status != want {
			t.Errorf("%s: got %+v want %s", label, cards, want)
		}
	}
}

func TestApplyIgnoresUnknownLabelSurfacelessAndHooksAlone(t *testing.T) {
	s := Apply(State{}, status("Something new", "S1", t0))
	s = Apply(s, status("Idle", "", t0))
	s = Apply(s, hook("S1", "/w/DEV-1"))
	if len(s.Cards()) != 0 {
		t.Fatalf("expected no cards, got %+v", s.Cards())
	}
}

func TestTitleFromHookEvenWhenItArrivesLate(t *testing.T) {
	s := Apply(State{}, status("Running", "ABCDEFGHIJKL", t0))
	if got := s.Cards()[0].Title; got != "ABCDEFGH" {
		t.Fatalf("fallback title %q", got)
	}
	s = Apply(s, hook("ABCDEFGHIJKL", "/w/worktrees/DEV-1-fix"))
	if got := s.Cards()[0].Title; got != "DEV-1-fix" {
		t.Fatalf("title %q", got)
	}
}

func TestSinceOnlyChangesWithStatus(t *testing.T) {
	s := Apply(State{}, status("Running", "S1", t0))
	s = Apply(s, status("Running subagents", "S1", t0.Add(time.Minute)))
	if got := s.Cards()[0].SinceMs; got != t0.UnixMilli() {
		t.Fatalf("since moved: %d", got)
	}
	s = Apply(s, status("Idle", "S1", t0.Add(2*time.Minute)))
	if got := s.Cards()[0].SinceMs; got != t0.Add(2*time.Minute).UnixMilli() {
		t.Fatalf("since not updated on status change: %d", got)
	}
}

func TestApplyDoesNotMutateInput(t *testing.T) {
	a := Apply(State{}, status("Idle", "S1", t0))
	_ = Apply(a, status("Needs input", "S1", t0))
	if a.Cards()[0].Status != protocol.Idle {
		t.Fatal("input state was mutated")
	}
}

func TestCardsSortedByUrgencyThenNewest(t *testing.T) {
	s := Apply(State{}, status("Idle", "idle", t0))
	s = Apply(s, status("Running", "work-old", t0))
	s = Apply(s, status("Running", "work-new", t0.Add(time.Minute)))
	s = Apply(s, status("API error", "err", t0))
	s = Apply(s, status("Needs input", "wait", t0.Add(-time.Hour)))
	var order []string
	for _, c := range s.Cards() {
		order = append(order, c.ID)
	}
	want := []string{"wait", "err", "work-new", "work-old", "idle"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v want %v", order, want)
		}
	}
}

func TestPruneDropsStaleCardsOfAnyStatus(t *testing.T) {
	s := Apply(State{}, status("Idle", "old-idle", t0))
	s = Apply(s, status("Running", "old-run", t0))
	s = Apply(s, status("Running", "fresh", t0.Add(12*time.Hour)))
	s = Prune(s, t0.Add(13*time.Hour), 12*time.Hour)
	cards := s.Cards()
	if len(cards) != 1 || cards[0].ID != "fresh" {
		t.Fatalf("got %+v", cards)
	}
}
