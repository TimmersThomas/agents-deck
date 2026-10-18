// Package events parses cmux's newline-delimited event stream.
package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	statusKey   = "claude_code "
	statusEvent = "sidebar.metadata.updated"
)

type Event struct {
	Seq         int64
	Name        string
	At          time.Time
	WorkspaceID string
	SurfaceID   string
	Cwd         string
	Hook        string
	Label       string
}

type wire struct {
	Seq         int64     `json:"seq"`
	Name        string    `json:"name"`
	At          time.Time `json:"occurred_at"`
	WorkspaceID *string   `json:"workspace_id"`
	SurfaceID   *string   `json:"surface_id"`
	Payload     struct {
		Hook    string `json:"hook_event_name"`
		Cwd     string `json:"cwd"`
		Command string `json:"command"`
		Args    string `json:"args"`
	} `json:"payload"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func ParseLine(line []byte) (Event, error) {
	var w wire
	if err := json.Unmarshal(line, &w); err != nil {
		return Event{}, fmt.Errorf("parse event: %w", err)
	}
	e := Event{
		Seq:         w.Seq,
		Name:        w.Name,
		At:          w.At,
		WorkspaceID: deref(w.WorkspaceID),
		SurfaceID:   deref(w.SurfaceID),
		Cwd:         w.Payload.Cwd,
		Hook:        w.Payload.Hook,
	}
	if w.Name == statusEvent && w.Payload.Command == "set_status" {
		e = withStatus(e, w.Payload.Args)
	}
	return e, nil
}

// withStatus reads "claude_code <Label> --flag=v ..." into Label, SurfaceID and WorkspaceID.
func withStatus(e Event, args string) Event {
	rest, ok := strings.CutPrefix(args, statusKey)
	if !ok {
		return e
	}
	label, flags, _ := strings.Cut(rest, " --")
	e.Label = strings.TrimSpace(label)
	for _, f := range strings.Fields(flags) {
		f = strings.TrimLeft(f, "-")
		if v, ok := strings.CutPrefix(f, "panel="); ok {
			e.SurfaceID = v
		}
		if v, ok := strings.CutPrefix(f, "tab="); ok {
			e.WorkspaceID = v
		}
	}
	return e
}
