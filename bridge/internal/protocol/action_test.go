package protocol

import (
	"strings"
	"testing"
	"time"
)

func TestParseActionFocus(t *testing.T) {
	a, err := ParseAction([]byte(`{"v":1,"t":"action","a":"focus","id":"0000BBBB-0000-4000-8000-000000000001"}`))
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != ActionFocus || a.ID != "0000BBBB-0000-4000-8000-000000000001" {
		t.Fatalf("got %+v", a)
	}
}

func TestParseActionRejectsBadInput(t *testing.T) {
	long := strings.Repeat("a", MaxIDBytes+1)
	cases := map[string]string{
		"not json":        `focus`,
		"wrong version":   `{"v":2,"t":"action","a":"focus","id":"x"}`,
		"missing version": `{"t":"action","a":"focus","id":"x"}`,
		"wrong type":      `{"v":1,"t":"state","a":"focus","id":"x"}`,
		"unknown action":  `{"v":1,"t":"action","a":"rm -rf","id":"x"}`,
		"missing id":      `{"v":1,"t":"action","a":"focus"}`,
		"empty id":        `{"v":1,"t":"action","a":"focus","id":""}`,
		"id too long":     `{"v":1,"t":"action","a":"focus","id":"` + long + `"}`,
	}
	for name, line := range cases {
		if _, err := ParseAction([]byte(line)); err == nil {
			t.Errorf("%s: accepted %q", name, line)
		}
	}
}

func TestParseActionErrorDoesNotEchoInput(t *testing.T) {
	_, err := ParseAction([]byte(`{"v":1,"t":"action","a":"SECRET-PROMPT-TEXT","id":"x"}`))
	if err == nil || strings.Contains(err.Error(), "SECRET-PROMPT-TEXT") {
		t.Fatalf("error must not echo the action name: %v", err)
	}
}

func TestWorkspaceIsNotRendered(t *testing.T) {
	now := time.UnixMilli(5)
	plain, err := Render([]Card{{ID: "s", Title: "t", Status: Idle, SinceMs: 1}}, now)
	if err != nil {
		t.Fatal(err)
	}
	withWS, err := Render([]Card{{ID: "s", WS: "W", Title: "t", Status: Idle, SinceMs: 1}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(withWS) {
		t.Fatalf("workspace leaked into the frame:\n%s\n%s", plain, withWS)
	}
}
