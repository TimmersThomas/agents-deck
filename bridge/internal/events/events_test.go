package events

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseHookLine(t *testing.T) {
	line := `{"name":"agent.hook.Stop","seq":5,"occurred_at":"2026-10-18T10:01:00.000Z","workspace_id":"W1","surface_id":"S1","payload":{"hook_event_name":"Stop","cwd":"/w/x"}}`

	e, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if e.Seq != 5 || e.Name != "agent.hook.Stop" || e.Hook != "Stop" || e.SurfaceID != "S1" || e.WorkspaceID != "W1" || e.Cwd != "/w/x" || e.Label != "" {
		t.Fatalf("unexpected event: %+v", e)
	}
	if !e.At.Equal(time.Date(2026, 10, 18, 10, 1, 0, 0, time.UTC)) {
		t.Fatalf("bad time: %v", e.At)
	}
}

func TestParseSetStatus(t *testing.T) {
	line := `{"name":"sidebar.metadata.updated","seq":4,"occurred_at":"2026-10-18T10:00:30.000Z","workspace_id":null,"surface_id":null,"payload":{"command":"set_status","args":"claude_code Needs input --icon=bell.fill --color=#4C8DFF --tab=W1 --panel=S1 --pid=4952"}}`

	e, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if e.Label != "Needs input" || e.SurfaceID != "S1" || e.WorkspaceID != "W1" || e.Hook != "" {
		t.Fatalf("unexpected event: %+v", e)
	}
}

func TestParseLabelWithoutFlags(t *testing.T) {
	line := `{"name":"sidebar.metadata.updated","seq":4,"occurred_at":"2026-10-18T10:00:30.000Z","payload":{"command":"set_status","args":"claude_code Idle --tab=W1 --panel=S1"}}`
	e, err := ParseLine([]byte(line))
	if err != nil || e.Label != "Idle" {
		t.Fatalf("got %+v, %v", e, err)
	}
}

func TestParseIgnoresOtherStatusKeys(t *testing.T) {
	line := `{"name":"sidebar.metadata.updated","seq":4,"occurred_at":"2026-10-18T10:00:30.000Z","payload":{"command":"set_status","args":"codex Running --tab=W1 --panel=S1"}}`
	e, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if e.Label != "" || e.SurfaceID != "" {
		t.Fatalf("expected empty label and surface, got %+v", e)
	}
}

func TestParseNullSurfaceAndNoHook(t *testing.T) {
	e, err := ParseLine([]byte(`{"name":"window.keyed","seq":3,"occurred_at":"2026-10-18T10:00:06.000Z","workspace_id":"W1","surface_id":null,"payload":{"is_key_window":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if e.SurfaceID != "" || e.Hook != "" || e.Label != "" {
		t.Fatalf("expected empty surface, hook and label, got %+v", e)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := ParseLine([]byte("not json")); err == nil {
		t.Fatal("expected error")
	}
}

func TestFixtureParses(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, l := range lines {
		if _, err := ParseLine([]byte(l)); err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
	}
}
