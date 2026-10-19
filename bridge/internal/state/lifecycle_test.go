package state

import (
	"testing"
	"time"

	"desktop-controller/bridge/internal/events"
	"desktop-controller/bridge/internal/protocol"
)

const worktree = "/Users/u/projects/app/worktrees/DEV-9-done"

func closed(surface string, at time.Time) events.Event {
	return events.Event{Name: "surface.closed", SurfaceID: surface, WorkspaceID: "W1", At: at}
}

func at(d time.Duration) time.Time { return t0.Add(d) }

func only(t *testing.T, s State) protocol.Card {
	t.Helper()
	cards := s.Cards()
	if len(cards) != 1 {
		t.Fatalf("got %d cards, want 1: %+v", len(cards), cards)
	}
	return cards[0]
}

func live(t *testing.T) State {
	t.Helper()
	s := step(State{}, hookAt("SessionStart", "S1", worktree, t0))
	s = step(s, status("Idle", "S1", t0))
	s = step(s, hookAt("UserPromptSubmit", "S1", worktree, at(time.Second)))
	s = step(s, status("Running", "S1", at(time.Second)))
	s = step(s, hookAt("Stop", "S1", worktree, at(time.Minute)))
	return step(s, status("Idle", "S1", at(time.Minute)))
}

func TestSessionEndMarksCardDone(t *testing.T) {
	s := step(live(t), hookAt("SessionEnd", "S1", worktree, at(2*time.Minute)))
	s = step(s, events.Event{Name: "feed.item.received", Hook: "SessionEnd", SurfaceID: "S1", At: at(2*time.Minute + time.Second)})
	c := only(t, s)
	if c.Status != protocol.Done || c.SinceMs != at(2*time.Minute).UnixMilli() || c.Title != "DEV-9-done" {
		t.Fatalf("got %+v, want done since the first SessionEnd", c)
	}
}

func TestSurfaceClosedMarksCardDoneAndKeepsSince(t *testing.T) {
	s := step(live(t), closed("S1", at(3*time.Minute)))
	if c := only(t, s); c.Status != protocol.Done || c.SinceMs != at(3*time.Minute).UnixMilli() {
		t.Fatalf("got %+v", c)
	}
	s = step(step(live(t), hookAt("SessionEnd", "S1", worktree, at(2*time.Minute))), closed("S1", at(3*time.Minute)))
	if c := only(t, s); c.SinceMs != at(2*time.Minute).UnixMilli() {
		t.Fatalf("surface.closed moved since of a done card: %+v", c)
	}
}

func TestEndWithoutCardIsIgnored(t *testing.T) {
	s := step(State{}, closed("S9", t0))
	s = step(s, hookAt("SessionEnd", "S9", worktree, t0))
	if n := len(s.Cards()); n != 0 {
		t.Fatalf("got %d cards, want none", n)
	}
}

func TestTeardownNoiseDoesNotReviveDoneCard(t *testing.T) {
	s := step(live(t), hookAt("SessionEnd", "S1", worktree, at(2*time.Minute)))
	for _, label := range []string{"Idle", "Waiting", "API error", "Connection dropped"} {
		s = step(s, status(label, "S1", at(2*time.Minute+time.Second)))
		s = step(s, hookAt("Stop", "S1", worktree, at(2*time.Minute+time.Second)))
		s = step(s, hookAt("Notification", "S1", worktree, at(3*time.Minute)))
	}
	if c := only(t, s); c.Status != protocol.Done {
		t.Fatalf("teardown noise revived the card: %+v", c)
	}
}

func TestDoneCardRevives(t *testing.T) {
	ended := step(live(t), hookAt("SessionEnd", "S1", worktree, at(2*time.Minute)))
	cases := []struct {
		name string
		e    events.Event
		want protocol.Status
	}{
		{"SessionStart", hookAt("SessionStart", "S1", worktree, at(time.Hour)), protocol.Idle},
		{"UserPromptSubmit", hookAt("UserPromptSubmit", "S1", worktree, at(time.Hour)), protocol.Working},
		{"Running", status("Running", "S1", at(time.Hour)), protocol.Working},
		{"Running subagents", status("Running subagents", "S1", at(time.Hour)), protocol.Working},
		{"Needs input", status("Needs input", "S1", at(time.Hour)), protocol.Waiting},
	}
	for _, tc := range cases {
		c := only(t, step(ended, tc.e))
		if c.Status != tc.want || c.SinceMs != at(time.Hour).UnixMilli() {
			t.Errorf("%s: got %+v, want %s since the revival", tc.name, c, tc.want)
		}
	}
}

func TestReviveHooksDoNotCreateCards(t *testing.T) {
	s := step(State{}, hookAt("UserPromptSubmit", "S1", worktree, t0))
	if n := len(s.Cards()); n != 0 {
		t.Fatalf("got %d cards, want none", n)
	}
}

func TestDoneSortsLast(t *testing.T) {
	s := step(State{}, status("Idle", "idle", t0))
	s = step(s, status("Running", "ended", at(time.Hour)))
	s = step(s, closed("ended", at(2*time.Hour)))
	cards := s.Cards()
	if len(cards) != 2 || cards[0].ID != "idle" || cards[1].ID != "ended" {
		t.Fatalf("order %+v, want idle then ended", cards)
	}
}
