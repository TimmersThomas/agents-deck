package state

import (
	"desktop-controller/bridge/internal/events"
	"desktop-controller/bridge/internal/protocol"
)

const (
	endHook      = "SessionEnd"
	surfaceClose = "surface.closed"
)

// A done card ignores later Idle/Waiting/error statuses as teardown noise; only a
// new session, a prompt, or real activity brings it back.
var (
	reviveHooks  = map[string]protocol.Status{"SessionStart": protocol.Idle, "UserPromptSubmit": protocol.Working}
	reviveLabels = map[string]bool{"Running": true, "Running subagents": true, "Needs input": true}
)

func ended(e events.Event) bool { return e.Hook == endHook || e.Name == surfaceClose }

// nextStatus returns the status e moves the surface's card to; ok is false when e
// carries no status for it. Ends and revivals only apply to an existing card.
func nextStatus(card protocol.Card, seen bool, e events.Event) (_ protocol.Status, ok bool) {
	if ended(e) {
		return protocol.Done, seen
	}
	if seen && card.Status == protocol.Done {
		if st, ok := reviveHooks[e.Hook]; ok {
			return st, true
		}
		if !reviveLabels[e.Label] {
			return "", false
		}
	}
	st, ok := labelStatus[e.Label]
	return st, ok
}
