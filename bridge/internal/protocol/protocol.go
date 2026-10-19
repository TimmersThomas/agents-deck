// Package protocol defines the JSON-lines frames sent to the display.
package protocol

import (
	"encoding/json"
	"time"
)

const Version = 1

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
	Cards []Card `json:"cards"`
}

// Render returns the state frame as a single newline-terminated JSON line.
func Render(cards []Card, now time.Time) ([]byte, error) {
	if cards == nil {
		cards = []Card{}
	}
	b, err := json.Marshal(frame{V: Version, T: "state", NowMs: now.UnixMilli(), Cards: cards})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
