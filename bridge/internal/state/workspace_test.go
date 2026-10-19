package state

import (
	"testing"

	"desktop-controller/bridge/internal/events"
)

func withWS(e events.Event, ws string) events.Event {
	e.WorkspaceID = ws
	return e
}

func TestStatusEventRecordsWorkspace(t *testing.T) {
	s, changed := Apply(State{}, withWS(status("Running", "S1", t0), "W1"))
	if !changed {
		t.Fatal("a new card must count as changed")
	}
	c, ok := s.Lookup("S1")
	if !ok || c.WS != "W1" || c.Title != "S1" {
		t.Fatalf("got %+v ok=%v", c, ok)
	}
}

func TestLookupUnknownCard(t *testing.T) {
	if _, ok := (State{}).Lookup("nope"); ok {
		t.Fatal("unknown id found")
	}
}

func TestEventWithoutWorkspaceKeepsIt(t *testing.T) {
	s, _ := Apply(State{}, withWS(status("Running", "S1", t0), "W1"))
	s, _ = Apply(s, status("Idle", "S1", t0)) // status event with no --tab
	if c, _ := s.Lookup("S1"); c.WS != "W1" {
		t.Fatalf("workspace lost: %+v", c)
	}
}

func TestWorkspaceMoveUpdatesWithoutRedraw(t *testing.T) {
	s, _ := Apply(State{}, withWS(status("Running", "S1", t0), "W1"))
	next, changed := Apply(s, withWS(hook("S1", ""), "W2"))
	if changed {
		t.Fatal("a workspace-only change must not trigger a frame")
	}
	if c, _ := next.Lookup("S1"); c.WS != "W2" {
		t.Fatalf("workspace not updated: %+v", c)
	}
	if c, _ := s.Lookup("S1"); c.WS != "W1" {
		t.Fatalf("input state mutated: %+v", c)
	}
}

func TestWorkspaceOnHookWithoutCardIsIgnored(t *testing.T) {
	s, changed := Apply(State{}, withWS(hook("S9", "/w/x"), "W1"))
	if changed || s.Len() != 0 {
		t.Fatalf("a hook alone must not create a card: len=%d changed=%v", s.Len(), changed)
	}
}
