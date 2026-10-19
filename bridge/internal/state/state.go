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

func Apply(s State, e events.Event) State {
	if e.SurfaceID == "" {
		return s
	}
	if titleHooks[e.Hook] && e.Cwd != "" {
		s = withTitle(s, e.SurfaceID, filepath.Base(e.Cwd))
	}
	status, ok := labelStatus[e.Label]
	if !ok {
		return s
	}
	if card, seen := s.cards[e.SurfaceID]; seen && card.Status == status {
		return s
	}
	return withCard(s, protocol.Card{ID: e.SurfaceID, Status: status, SinceMs: e.At.UnixMilli()})
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

var rank = map[protocol.Status]int{protocol.Waiting: 0, protocol.Error: 1, protocol.Working: 2, protocol.Idle: 3}

func (s State) title(id string) string {
	if t, ok := s.titles[id]; ok {
		return t
	}
	if len(id) > fallbackTitleLen {
		return id[:fallbackTitleLen]
	}
	return id
}

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
