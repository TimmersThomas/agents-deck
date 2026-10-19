package act

import (
	"os"
	"testing"
)

func fixtureTree(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/tree.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFirstPanel(t *testing.T) {
	cases := map[string]struct {
		ws, want string
	}{
		"left big pane of a three-pane workspace":                  {"AAAAAAAA-0000-4000-8000-000000000001", "surface:2001"},
		"single pane uses its selected surface":                    {"BBBBBBBB-0000-4000-8000-000000000002", "surface:4002"},
		"layout order beats panes order":                           {"CCCCCCCC-0000-4000-8000-000000000003", "surface:5002"},
		"no layout falls back to first pane and its first surface": {"DDDDDDDD-0000-4000-8000-000000000004", "surface:7001"},
	}
	tree := fixtureTree(t)
	for name, c := range cases {
		got, err := FirstPanel(tree, c.ws)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v want %q", name, got, err, c.want)
		}
	}
}

func TestFirstPanelErrors(t *testing.T) {
	tree := fixtureTree(t)
	cases := map[string]string{
		"unknown workspace":      "00000000-0000-4000-8000-000000000000",
		"unsafe surface ref":     "EEEEEEEE-0000-4000-8000-000000000005",
		"workspace has no panes": "FFFFFFFF-0000-4000-8000-000000000006",
	}
	for name, ws := range cases {
		if got, err := FirstPanel(tree, ws); err == nil {
			t.Errorf("%s: expected an error, got %q", name, got)
		}
	}
	if _, err := FirstPanel([]byte("not json"), "x"); err == nil {
		t.Error("bad JSON accepted")
	}
}
