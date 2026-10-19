package protocol

import (
	"encoding/json"
	"fmt"
)

// ActionFocus asks the bridge to bring a card's session to the front in cmux.
const ActionFocus = "focus"

// Action is a request from the display about one card.
type Action struct {
	Name string
	ID   string
}

type actionWire struct {
	V  int    `json:"v"`
	T  string `json:"t"`
	A  string `json:"a"`
	ID string `json:"id"`
}

// ParseAction validates one line from the display. Errors never repeat the
// line: it is untrusted input.
func ParseAction(line []byte) (Action, error) {
	var w actionWire
	if err := json.Unmarshal(line, &w); err != nil {
		return Action{}, fmt.Errorf("parse action: not a JSON object")
	}
	switch {
	case w.V != Version:
		return Action{}, fmt.Errorf("parse action: unsupported version %d", w.V)
	case w.T != "action":
		return Action{}, fmt.Errorf("parse action: not an action frame")
	case w.A != ActionFocus:
		return Action{}, fmt.Errorf("parse action: unknown action")
	case w.ID == "" || len(w.ID) > MaxIDBytes:
		return Action{}, fmt.Errorf("parse action: invalid id")
	}
	return Action{Name: w.A, ID: w.ID}, nil
}
