// Package protocol defines the JSON-lines frames sent to the display.
//
// A frame holds at most MaxCards cards, titles of at most MaxTitleRunes runes and
// ids of at most MaxIDBytes bytes (state drops longer ids). json.Marshal writes any
// rune or id byte as at most 6 bytes (<-style escapes for <, >, &, control
// characters and U+2028/9; an invalid byte becomes a 3-byte U+FFFD), so a frame
// never exceeds MaxFrameBytes:
//
//	envelope  {"v":1,"t":"state","now_ms":<20>,"total":<19>,"cards":[...]}\n  = 89
//	per card  {"id":"<64*6>","title":"<48*6>","status":"<7>","since_ms":<20>} = 743
//	total     89 + 16*743 + 15 commas                                         = 11992
package protocol

import (
	"encoding/json"
	"time"
)

// Version is the protocol version carried in every frame.
const Version = 1

const (
	MaxCards      = 16
	MaxTitleRunes = 48
	MaxIDBytes    = 64
	MaxFrameBytes = 11992
)

// Status is a card's display state.
type Status string

const (
	Working Status = "working"
	Waiting Status = "waiting"
	Idle    Status = "idle"
	Error   Status = "error"
	Done    Status = "done"
)

type Card struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  Status `json:"status"`
	SinceMs int64  `json:"since_ms"`
}

type frame struct {
	V     int    `json:"v"`
	T     string `json:"t"`
	NowMs int64  `json:"now_ms"`
	Total int    `json:"total"`
	Cards []Card `json:"cards"`
}

// Render returns the state frame as a single newline-terminated JSON line. cards
// must already be sorted by urgency: only the first MaxCards are sent, total
// counts them all.
func Render(cards []Card, now time.Time) ([]byte, error) {
	b, err := json.Marshal(frame{V: Version, T: "state", NowMs: now.UnixMilli(), Total: len(cards), Cards: capped(cards)})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func capped(cards []Card) []Card {
	n := min(len(cards), MaxCards)
	out := make([]Card, n)
	for i, c := range cards[:n] {
		c.Title = truncateRunes(c.Title, MaxTitleRunes)
		out[i] = c
	}
	return out
}

// truncateRunes counts an invalid UTF-8 byte as one rune, as json.Marshal does.
func truncateRunes(s string, max int) string {
	n := 0
	for i := range s {
		if n == max {
			return s[:i]
		}
		n++
	}
	return s
}
