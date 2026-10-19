package state

import (
	"testing"
	"time"

	"desktop-controller/bridge/internal/events"
)

// step applies e and drops the changed flag.
func step(s State, e events.Event) State {
	next, _ := Apply(s, e)
	return next
}

func TestApplyReportsOnlyRenderedChanges(t *testing.T) {
	s := State{}
	cases := []struct {
		name string
		e    events.Event
		want bool
	}{
		{"title before any card", hookAt("SessionStart", "S1", worktree, t0), false},
		{"unrelated event", events.Event{Name: "window.keyed", At: t0}, false},
		{"new card", status("Idle", "S1", t0), true},
		{"same status", status("Idle", "S1", at(time.Second)), false},
		{"tool hook in subdirectory", hookAt("PreToolUse", "S1", worktree+"/src", at(time.Second)), false},
		{"same title", hookAt("UserPromptSubmit", "S1", worktree, at(2*time.Second)), false},
		{"status change", status("Running", "S1", at(2*time.Second)), true},
		{"label mapping to same status", status("Running subagents", "S1", at(3*time.Second)), false},
		{"title change", hookAt("UserPromptSubmit", "S1", "/w/DEV-10-other", at(4*time.Second)), true},
		{"other surface title only", hookAt("SessionStart", "S2", "/w/DEV-11", at(4*time.Second)), false},
		{"session end", hookAt("SessionEnd", "S1", worktree, at(5*time.Second)), true},
		{"teardown noise", status("Idle", "S1", at(5*time.Second)), false},
		{"surface closed after end", closed("S1", at(6*time.Second)), false},
		{"close without card", closed("S3", at(6*time.Second)), false},
	}
	for _, tc := range cases {
		var changed bool
		s, changed = Apply(s, tc.e)
		if changed != tc.want {
			t.Errorf("%s: changed = %v, want %v", tc.name, changed, tc.want)
		}
	}
}

func TestLenCountsCards(t *testing.T) {
	s := step(step(State{}, status("Idle", "S1", t0)), status("Idle", "S2", t0))
	if got, pruned := s.Len(), Prune(s, at(time.Hour), time.Minute).Len(); got != 2 || pruned != 0 {
		t.Fatalf("Len = %d before prune, %d after", got, pruned)
	}
}
