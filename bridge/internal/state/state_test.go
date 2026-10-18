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

func TestHookDoesNotMutateInput(t *testing.T) {
	a := Apply(State{}, status("Running", "S1", t0))
	b := Apply(a, hook("S1", "/w/DEV-2"))
	if got := a.Cards()[0].Title; got != "S1" {
		t.Fatalf("input title mutated: %q", got)
	}
	if got := b.Cards()[0].Title; got != "DEV-2" {
		t.Fatalf("hook title %q", got)
	}
}

func TestPruneDoesNotMutateInput(t *testing.T) {
	s := Apply(State{}, status("Running", "S1", t0))
	pruned := Prune(s, t0.Add(100*time.Hour), time.Minute)
	if n := len(pruned.Cards()); n != 0 {
		t.Fatalf("pruned state has %d cards", n)
	}
	if n := len(s.Cards()); n != 1 {
		t.Fatalf("input cards mutated: %d", n)
	}
}

func TestPruneKeepsTitleOfSurvivingCard(t *testing.T) {
	s := Apply(State{}, status("Running", "S1", t0))
	s = Apply(s, hook("S1", "/w/DEV-3"))
	s = Prune(s, t0, time.Hour)
	cards := s.Cards()
	if len(cards) != 1 || cards[0].Title != "DEV-3" {
		t.Fatalf("got %+v", cards)
	}
}

func TestSameStatusAndTimeSortByID(t *testing.T) {
	s := Apply(State{}, status("Idle", "b", t0))
	s = Apply(s, status("Idle", "a", t0))
	cards := s.Cards()
	if len(cards) != 2 || cards[0].ID != "a" || cards[1].ID != "b" {
		t.Fatalf("order %+v want a, b", cards)
	}
}

func TestHookOnExistingCardKeepsStatusAndSince(t *testing.T) {
	s := Apply(State{}, status("Running", "S1", t0))
	s = Apply(s, hook("S1", "/w/DEV-4"))
	s = Apply(s, events.Event{Name: "agent.hook.Stop", Hook: "Stop", SurfaceID: "S1", Cwd: "/w/DEV-4", At: t0.Add(time.Hour)})
	c := s.Cards()[0]
	if c.Status != protocol.Working {
		t.Fatalf("status %s", c.Status)
	}
	if c.SinceMs != t0.UnixMilli() {
		t.Fatalf("since %d want %d", c.SinceMs, t0.UnixMilli())
	}
}
