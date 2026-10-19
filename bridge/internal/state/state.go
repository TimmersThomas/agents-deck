// Package state folds cmux events into display cards.
package state

import (
	"path/filepath"
	"sort"
	"time"

	"desktop-controller/bridge/internal/events"
	"desktop-controller/bridge/internal/protocol"
)

const fallbackTitleLen = 8

// State is immutable: Apply and Prune return new values and never edit the maps they were given.
type State struct {
	cards  map[string]protocol.Card
	titles map[string]string
}

var labelStatus = map[string]protocol.Status{
	"Running":            protocol.Working,
	"Running subagents":  protocol.Working,
	"Waiting":            protocol.Working,
	"Needs input":        protocol.Waiting,
	"Idle":               protocol.Idle,
	"API error":          protocol.Error,
	"Connection dropped": protocol.Error,
}

// titleHooks are the hooks whose cwd is the session root; later hooks report
// subdirectories the agent moved into.
var titleHooks = map[string]bool{"SessionStart": true, "UserPromptSubmit": true}

func withTitle(s State, id, title string) State {
	if s.titles[id] == title {
		return s
	}
	titles := make(map[string]string, len(s.titles)+1)
	for k, v := range s.titles {
		titles[k] = v
	}
	titles[id] = title
	return State{cards: s.cards, titles: titles}
}

func withCard(s State, c protocol.Card) State {
	cards := make(map[string]protocol.Card, len(s.cards)+1)
	for k, v := range s.cards {
		cards[k] = v
	}
	cards[c.ID] = c
	return State{cards: cards, titles: s.titles}
}

// Apply folds e into s. changed reports whether the rendered cards differ: a new
// card, or a different status, since or title on an existing one.
func Apply(s State, e events.Event) (_ State, changed bool) {
	before, had := s.card(e.SurfaceID)
	next := apply(s, e)
	after, has := next.card(e.SurfaceID)
	return next, had != has || rendered(before) != rendered(after)
}

// rendered drops what the display never sees.
func rendered(c protocol.Card) protocol.Card {
	c.WS = ""
	return c
}

func apply(s State, e events.Event) State {
	if e.SurfaceID == "" || len(e.SurfaceID) > protocol.MaxIDBytes {
		return s
	}
	if titleHooks[e.Hook] && e.Cwd != "" {
		s = withTitle(s, e.SurfaceID, filepath.Base(e.Cwd))
	}
	s = withWorkspace(s, e)
	card, seen := s.cards[e.SurfaceID]
	status, ok := nextStatus(card, seen, e)
	if !ok || (seen && card.Status == status) {
		return s
	}
	ws := card.WS
	if e.WorkspaceID != "" {
		ws = e.WorkspaceID
	}
	return withCard(s, protocol.Card{ID: e.SurfaceID, WS: ws, Status: status, SinceMs: e.At.UnixMilli()})
}

// withWorkspace follows a surface that moved to another workspace.
func withWorkspace(s State, e events.Event) State {
	card, ok := s.cards[e.SurfaceID]
	if !ok || e.WorkspaceID == "" || card.WS == e.WorkspaceID {
		return s
	}
	card.WS = e.WorkspaceID
	return withCard(s, card)
}

// Prune drops cards whose last status change is older than maxAge.
func Prune(s State, now time.Time, maxAge time.Duration) State {
	cutoff := now.Add(-maxAge).UnixMilli()
	cards := make(map[string]protocol.Card, len(s.cards))
	for k, c := range s.cards {
		if c.SinceMs >= cutoff {
			cards[k] = c
		}
	}
	return State{cards: cards, titles: s.titles}
}

var rank = map[protocol.Status]int{
	protocol.Waiting: 0, protocol.Error: 1, protocol.Working: 2, protocol.Idle: 3, protocol.Done: 4,
}

func (s State) title(id string) string {
	if t, ok := s.titles[id]; ok {
		return t
	}
	if len(id) > fallbackTitleLen {
		return id[:fallbackTitleLen]
	}
	return id
}

func (s State) card(id string) (protocol.Card, bool) {
	c, ok := s.cards[id]
	if ok {
		c.Title = s.title(id)
	}
	return c, ok
}

// Len returns the number of cards.
func (s State) Len() int { return len(s.cards) }

// Lookup returns the card with id, including its workspace.
func (s State) Lookup(id string) (protocol.Card, bool) { return s.card(id) }

// Cards returns the cards sorted by urgency, then newest status change, then id.
func (s State) Cards() []protocol.Card {
	out := make([]protocol.Card, 0, len(s.cards))
	for id, c := range s.cards {
		c.Title = s.title(id)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if rank[out[i].Status] != rank[out[j].Status] {
			return rank[out[i].Status] < rank[out[j].Status]
		}
		if out[i].SinceMs != out[j].SinceMs {
			return out[i].SinceMs > out[j].SinceMs
		}
		return out[i].ID < out[j].ID
	})
	return out
}
