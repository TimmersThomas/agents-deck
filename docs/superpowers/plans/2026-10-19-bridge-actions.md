# Bridge actions (tap to focus) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The display can send a `focus` action for a card; the bridge switches cmux to that card's workspace and focuses the workspace's first (leftmost, big) pane.

**Architecture:** Cards carry their cmux workspace id internally (never sent to the display). `link` gains a read side that delivers newline-delimited lines from the port. `protocol.ParseAction` validates them. `run` resolves a card id to its workspace and hands it to a single-flight focus worker, which asks `cmux tree --json --all` for the workspace's first pane in layout order and runs `cmux select-workspace` then `cmux focus-panel`.

**Tech Stack:** Go 1.27, stdlib only, existing `bridge/` module (`desktop-controller/bridge`).

## Global Constraints

- Action frame from the display, one JSON line: `{"v":1,"t":"action","a":"focus","id":"<card id>"}`. Only `focus` exists; the id is the card id (the cmux surface id) from the last state frame, at most `protocol.MaxIDBytes` bytes.
- The display never sends a workspace id or a command: the bridge looks the card up in its own state and ignores unknown ids.
- Focus = `cmux select-workspace --workspace <ws>` then `cmux focus-panel --panel <surface ref> --workspace <ws>`, where `<surface ref>` is the `selected_surface_ref` of the workspace's FIRST pane in layout order (depth-first first leaf of `layout`; fall back to `panes[0]`). The workspace is found by `id` in `cmux tree --json --all`.
- Only fixed cmux sub-commands are run, and every argument is validated first: workspace id matches `^[0-9A-Fa-f-]{36}$`, surface ref matches `^surface:[0-9]+$`. Logs never contain raw serial input.
- The cmux socket is only reachable from processes started inside cmux: the bridge must be launched from a cmux terminal for focus to work. `--act=false` disables actions; `--stdout` implies no actions.
- Frames must not change on the wire: the workspace id is internal (`json:"-"`) and a workspace-only change must not trigger a frame.
- One focus at a time: a tap while a focus is running is dropped and logged.
- Files under 200 lines (production), functions under 50 lines, errors never swallowed, no mutation of shared state.
- Commits: conventional style, trailer `Co-Authored-By: ...` per the controller's instruction.

## File Structure

```
bridge/
  cmd/bridge/main.go            (modify: wire actions, focus worker)
  cmd/bridge/flags.go           (modify: --act, --cmux)
  cmd/bridge/actions_test.go    (create)
  cmd/focus/main.go             (create: manual check, `focus <workspace-uuid>`)
  internal/protocol/protocol.go (modify: Card.WS)
  internal/protocol/action.go   (create: Action, ParseAction)
  internal/protocol/action_test.go
  internal/state/state.go       (modify: WS tracking, Lookup)
  internal/state/workspace_test.go
  internal/act/tree.go          (create: FirstPanel)
  internal/act/focus.go         (create: Runner, ExecRunner, Focuser)
  internal/act/tree_test.go, focus_test.go
  internal/act/testdata/tree.json   (synthetic, real shape)
  internal/link/read.go         (create: readLines)
  internal/link/link.go         (modify: OnLine, start reader)
  internal/link/read_test.go
```

---

### Task 1: Action frames and the internal workspace field

**Files:**
- Create: `bridge/internal/protocol/action.go`, `bridge/internal/protocol/action_test.go`
- Modify: `bridge/internal/protocol/protocol.go` (Card struct)

**Interfaces:**
- Produces: `protocol.Card.WS string` (tag `json:"-"`), `protocol.ActionFocus = "focus"`, `protocol.Action{Name, ID string}`, `protocol.ParseAction(line []byte) (Action, error)`.

- [ ] **Step 1: Write the failing tests** (`bridge/internal/protocol/action_test.go`)

```go
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd bridge && go test ./internal/protocol/`
Expected: FAIL (`undefined: ParseAction`, `unknown field WS`).

- [ ] **Step 3: Implement**

In `protocol.go` replace the `Card` struct with:

```go
type Card struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  Status `json:"status"`
	SinceMs int64  `json:"since_ms"`
	// WS is the cmux workspace id. It stays inside the bridge and is never sent.
	WS string `json:"-"`
}
```

Create `bridge/internal/protocol/action.go`:

```go
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
```

- [ ] **Step 4: Run to verify they pass**

Run: `cd bridge && gofmt -l . ; go vet ./... && go test -race ./...`
Expected: no gofmt output; all packages `ok` (existing tests still pass: `WS` is zero everywhere).

- [ ] **Step 5: Commit**

```bash
git add bridge && git commit -m "feat(bridge): parse display actions and carry workspace id"
```

---

### Task 2: State tracks the workspace

**Files:**
- Modify: `bridge/internal/state/state.go`
- Test: `bridge/internal/state/workspace_test.go`

**Interfaces:**
- Consumes: `protocol.Card.WS` (Task 1), `events.Event.WorkspaceID` (already parsed from `--tab=` on status events and from the top-level field on hooks).
- Produces: `(State).Lookup(id string) (protocol.Card, bool)` returning the card including `WS`; `Apply`'s `changed` ignores `WS`-only differences.

- [ ] **Step 1: Write the failing tests** (`bridge/internal/state/workspace_test.go`; reuses `t0`, `status`, `hook` from `state_test.go`)

```go
package state

import (
	"testing"

	"desktop-controller/bridge/internal/events"
)

func withWS(e events.Event, ws string) events.Event {
	e.WorkspaceID = ws
	return e
}

func TestStatusEventRecordsWorkspace(t *testing.T) {
	s, changed := Apply(State{}, withWS(status("Running", "S1", t0), "W1"))
	if !changed {
		t.Fatal("a new card must count as changed")
	}
	c, ok := s.Lookup("S1")
	if !ok || c.WS != "W1" || c.Title != "S1" {
		t.Fatalf("got %+v ok=%v", c, ok)
	}
}

func TestLookupUnknownCard(t *testing.T) {
	if _, ok := (State{}).Lookup("nope"); ok {
		t.Fatal("unknown id found")
	}
}

func TestEventWithoutWorkspaceKeepsIt(t *testing.T) {
	s, _ := Apply(State{}, withWS(status("Running", "S1", t0), "W1"))
	s, _ = Apply(s, status("Idle", "S1", t0)) // status event with no --tab
	if c, _ := s.Lookup("S1"); c.WS != "W1" {
		t.Fatalf("workspace lost: %+v", c)
	}
}

func TestWorkspaceMoveUpdatesWithoutRedraw(t *testing.T) {
	s, _ := Apply(State{}, withWS(status("Running", "S1", t0), "W1"))
	next, changed := Apply(s, withWS(hook("S1", ""), "W2"))
	if changed {
		t.Fatal("a workspace-only change must not trigger a frame")
	}
	if c, _ := next.Lookup("S1"); c.WS != "W2" {
		t.Fatalf("workspace not updated: %+v", c)
	}
	if c, _ := s.Lookup("S1"); c.WS != "W1" {
		t.Fatalf("input state mutated: %+v", c)
	}
}

func TestWorkspaceOnHookWithoutCardIsIgnored(t *testing.T) {
	s, changed := Apply(State{}, withWS(hook("S9", "/w/x"), "W1"))
	if changed || s.Len() != 0 {
		t.Fatalf("a hook alone must not create a card: len=%d changed=%v", s.Len(), changed)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd bridge && go test ./internal/state/`
Expected: FAIL (`s.Lookup undefined`).

- [ ] **Step 3: Implement** in `bridge/internal/state/state.go`

Replace the `Apply` and `apply` functions with:

```go
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
```

and add, next to `Len`:

```go
// Lookup returns the card with id, including its workspace.
func (s State) Lookup(id string) (protocol.Card, bool) { return s.card(id) }
```

- [ ] **Step 4: Run to verify they pass**

Run: `cd bridge && gofmt -l . ; go vet ./... && go test -race ./...`
Expected: no gofmt output; all packages `ok`.

- [ ] **Step 5: Commit**

```bash
git add bridge && git commit -m "feat(bridge): track each card's cmux workspace"
```

---

### Task 3: Finding and focusing the first pane

**Files:**
- Create: `bridge/internal/act/tree.go`, `bridge/internal/act/focus.go`, `bridge/cmd/focus/main.go`
- Create: `bridge/internal/act/testdata/tree.json`
- Test: `bridge/internal/act/tree_test.go`, `bridge/internal/act/focus_test.go`

**Interfaces:**
- Produces: `act.FirstPanel(treeJSON []byte, workspaceID string) (surfaceRef string, err error)`; `act.Runner func(ctx context.Context, name string, args ...string) ([]byte, error)`; `act.ExecRunner` (a `Runner`); `act.Focuser{Cmux string; Run Runner; Timeout time.Duration}` with `Focus(ctx context.Context, workspaceID string) error`.

- [ ] **Step 1: Create the fixture** (`bridge/internal/act/testdata/tree.json`). Synthetic ids and titles in the real shape of `cmux tree --json --all`. WS A has three panes (the `cw` layout: left big pane, lazygit top right, shell bottom right); WS B has one pane; WS C lists its panes in a different order than the layout (the layout's first leaf is `pane:3002`); WS D has no layout object.

```json
{
  "windows": [
    {
      "ref": "window:1",
      "workspaces": [
        {
          "id": "AAAAAAAA-0000-4000-8000-000000000001",
          "title": "DEV-1-alpha",
          "layout": {
            "direction": "horizontal",
            "split": 0.66,
            "children": [
              {"pane": {"ref": "pane:1001"}},
              {
                "direction": "vertical",
                "split": 0.75,
                "children": [{"pane": {"ref": "pane:1002"}}, {"pane": {"ref": "pane:1003"}}]
              }
            ]
          },
          "panes": [
            {"id": null, "ref": "pane:1001", "index": 0, "selected_surface_ref": "surface:2001", "surfaces": [{"ref": "surface:2001"}]},
            {"id": null, "ref": "pane:1002", "index": 1, "selected_surface_ref": "surface:2002", "surfaces": [{"ref": "surface:2002"}]},
            {"id": null, "ref": "pane:1003", "index": 2, "selected_surface_ref": "surface:2003", "surfaces": [{"ref": "surface:2003"}]}
          ]
        },
        {
          "id": "BBBBBBBB-0000-4000-8000-000000000002",
          "title": "single",
          "layout": {"pane": {"ref": "pane:3001"}},
          "panes": [
            {"id": null, "ref": "pane:3001", "index": 0, "selected_surface_ref": "surface:4002",
             "surfaces": [{"ref": "surface:4001"}, {"ref": "surface:4002"}]}
          ]
        },
        {
          "id": "CCCCCCCC-0000-4000-8000-000000000003",
          "title": "reordered",
          "layout": {
            "direction": "horizontal",
            "split": 0.5,
            "children": [{"pane": {"ref": "pane:3002"}}, {"pane": {"ref": "pane:3001"}}]
          },
          "panes": [
            {"id": null, "ref": "pane:3001", "index": 0, "selected_surface_ref": "surface:5001", "surfaces": [{"ref": "surface:5001"}]},
            {"id": null, "ref": "pane:3002", "index": 1, "selected_surface_ref": "surface:5002", "surfaces": [{"ref": "surface:5002"}]}
          ]
        },
        {
          "id": "DDDDDDDD-0000-4000-8000-000000000004",
          "title": "no layout, no selection",
          "layout": null,
          "panes": [
            {"id": null, "ref": "pane:6001", "index": 0, "selected_surface_ref": "", "surfaces": [{"ref": "surface:7001"}]}
          ]
        },
        {
          "id": "EEEEEEEE-0000-4000-8000-000000000005",
          "title": "unusable surface ref",
          "layout": {"pane": {"ref": "pane:8001"}},
          "panes": [
            {"id": null, "ref": "pane:8001", "index": 0, "selected_surface_ref": "surface:1; rm -rf /", "surfaces": []}
          ]
        },
        {
          "id": "FFFFFFFF-0000-4000-8000-000000000006",
          "title": "empty",
          "layout": null,
          "panes": []
        }
      ]
    }
  ]
}
```

- [ ] **Step 2: Write the failing tests**

`bridge/internal/act/tree_test.go`:

```go
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
```

`bridge/internal/act/focus_test.go`:

```go
package act

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const wsA = "AAAAAAAA-0000-4000-8000-000000000001"

type recorder struct {
	calls   [][]string
	treeOut []byte
	failOn  string
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.failOn != "" && args[0] == r.failOn {
		return nil, errors.New("boom")
	}
	if args[0] == "tree" {
		return r.treeOut, nil
	}
	return nil, nil
}

func newFocuser(r *recorder) Focuser {
	return Focuser{Cmux: "cmux", Run: r.run, Timeout: time.Second}
}

func TestFocusSelectsWorkspaceThenFirstPanel(t *testing.T) {
	r := &recorder{treeOut: fixtureTree(t)}
	if err := newFocuser(r).Focus(context.Background(), wsA); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"cmux", "tree", "--json", "--all"},
		{"cmux", "select-workspace", "--workspace", wsA},
		{"cmux", "focus-panel", "--panel", "surface:2001", "--workspace", wsA},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls:\n got %v\nwant %v", r.calls, want)
	}
}

func TestFocusRejectsInvalidWorkspaceBeforeRunning(t *testing.T) {
	for _, ws := range []string{"", "x", wsA + "; rm -rf /", "--help", strings.Repeat("A", 37)} {
		r := &recorder{treeOut: fixtureTree(t)}
		if err := newFocuser(r).Focus(context.Background(), ws); err == nil {
			t.Errorf("%q accepted", ws)
		}
		if len(r.calls) != 0 {
			t.Errorf("%q ran %v", ws, r.calls)
		}
	}
}

func TestFocusStopsWhenWorkspaceIsNotInTheTree(t *testing.T) {
	r := &recorder{treeOut: fixtureTree(t)}
	err := newFocuser(r).Focus(context.Background(), "00000000-0000-4000-8000-000000000000")
	if err == nil || len(r.calls) != 1 {
		t.Fatalf("err=%v calls=%v: must stop after tree", err, r.calls)
	}
}

func TestFocusStopsOnEachCommandFailure(t *testing.T) {
	for i, sub := range []string{"tree", "select-workspace", "focus-panel"} {
		r := &recorder{treeOut: fixtureTree(t), failOn: sub}
		if err := newFocuser(r).Focus(context.Background(), wsA); err == nil {
			t.Errorf("%s failure not reported", sub)
		}
		if len(r.calls) != i+1 {
			t.Errorf("%s: %d calls, want %d", sub, len(r.calls), i+1)
		}
	}
}

func TestFocusAppliesTimeout(t *testing.T) {
	var deadline time.Time
	f := Focuser{Cmux: "cmux", Timeout: 50 * time.Millisecond, Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		deadline, _ = ctx.Deadline()
		return nil, errors.New("stop")
	}}
	_ = f.Focus(context.Background(), wsA)
	if deadline.IsZero() || time.Until(deadline) > time.Second {
		t.Fatalf("no sensible deadline: %v", deadline)
	}
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `cd bridge && go test ./internal/act/`
Expected: FAIL (`undefined: FirstPanel`, `undefined: Focuser`).

- [ ] **Step 4: Implement**

`bridge/internal/act/tree.go`:

```go
// Package act performs the actions the display asks for.
package act

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

var surfaceRef = regexp.MustCompile(`^surface:[0-9]+$`)

type treeDoc struct {
	Windows []struct {
		Workspaces []workspaceDoc `json:"workspaces"`
	} `json:"windows"`
}

type workspaceDoc struct {
	ID     string     `json:"id"`
	Layout *layoutDoc `json:"layout"`
	Panes  []paneDoc  `json:"panes"`
}

type layoutDoc struct {
	Pane *struct {
		Ref string `json:"ref"`
	} `json:"pane"`
	Children []layoutDoc `json:"children"`
}

type paneDoc struct {
	Ref      string `json:"ref"`
	Selected string `json:"selected_surface_ref"`
	Surfaces []struct {
		Ref string `json:"ref"`
	} `json:"surfaces"`
}

// FirstPanel returns the surface to focus in a workspace: the selected surface of
// its first pane in layout order, which is the big left pane of the cw layout.
func FirstPanel(treeJSON []byte, workspaceID string) (string, error) {
	var doc treeDoc
	if err := json.Unmarshal(treeJSON, &doc); err != nil {
		return "", fmt.Errorf("parse cmux tree: %w", err)
	}
	for _, w := range doc.Windows {
		for _, ws := range w.Workspaces {
			if ws.ID == workspaceID {
				return ws.firstSurface()
			}
		}
	}
	return "", errors.New("workspace not in the cmux tree")
}

func (w workspaceDoc) firstSurface() (string, error) {
	if len(w.Panes) == 0 {
		return "", errors.New("workspace has no panes")
	}
	first := w.Panes[0]
	if w.Layout != nil {
		ref := w.Layout.firstPane()
		for _, p := range w.Panes {
			if p.Ref == ref {
				first = p
			}
		}
	}
	return first.surface()
}

func (l layoutDoc) firstPane() string {
	if l.Pane != nil {
		return l.Pane.Ref
	}
	for _, c := range l.Children {
		if ref := c.firstPane(); ref != "" {
			return ref
		}
	}
	return ""
}

func (p paneDoc) surface() (string, error) {
	ref := p.Selected
	if ref == "" && len(p.Surfaces) > 0 {
		ref = p.Surfaces[0].Ref
	}
	if !surfaceRef.MatchString(ref) {
		return "", errors.New("first pane has no usable surface")
	}
	return ref, nil
}
```

`bridge/internal/act/focus.go`:

```go
package act

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var workspaceID = regexp.MustCompile(`^[0-9A-Fa-f-]{36}$`)

const maxStderr = 200

// Runner runs one external command and returns its stdout.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs the command for real, wrapping failures with the sub-command and a
// short stderr excerpt.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err == nil {
		return out, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		msg := strings.TrimSpace(string(ee.Stderr))
		if len(msg) > maxStderr {
			msg = msg[:maxStderr]
		}
		return out, fmt.Errorf("%s %s: %w: %s", name, args[0], err, msg)
	}
	return out, fmt.Errorf("%s %s: %w", name, args[0], err)
}

// Focuser brings a cmux workspace to the front and focuses its first pane.
type Focuser struct {
	Cmux    string
	Run     Runner
	Timeout time.Duration
}

// Focus runs `cmux tree`, `select-workspace` and `focus-panel`; any failure stops it.
func (f Focuser) Focus(ctx context.Context, ws string) error {
	if !workspaceID.MatchString(ws) {
		return errors.New("focus: invalid workspace id")
	}
	ctx, cancel := context.WithTimeout(ctx, f.Timeout)
	defer cancel()
	tree, err := f.Run(ctx, f.Cmux, "tree", "--json", "--all")
	if err != nil {
		return err
	}
	surface, err := FirstPanel(tree, ws)
	if err != nil {
		return fmt.Errorf("focus: %w", err)
	}
	if _, err := f.Run(ctx, f.Cmux, "select-workspace", "--workspace", ws); err != nil {
		return err
	}
	_, err = f.Run(ctx, f.Cmux, "focus-panel", "--panel", surface, "--workspace", ws)
	return err
}
```

`bridge/cmd/focus/main.go` (manual check: run it inside a cmux terminal):

```go
// Command focus brings a cmux workspace to the front: `focus <workspace-uuid>`.
// It exists to check the cmux commands by hand; run it from a cmux terminal.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"desktop-controller/bridge/internal/act"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: focus <workspace-uuid>")
		os.Exit(2)
	}
	f := act.Focuser{Cmux: "cmux", Run: act.ExecRunner, Timeout: 5 * time.Second}
	if err := f.Focus(context.Background(), os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "focus:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Run to verify they pass**

Run: `cd bridge && gofmt -l . ; go vet ./... && go test -race ./...`
Expected: no gofmt output; all packages `ok`.

- [ ] **Step 6: Commit**

```bash
git add bridge && git commit -m "feat(bridge): focus a workspace's first pane through cmux"
```

---

### Task 4: Reading lines from the port

**Files:**
- Create: `bridge/internal/link/read.go`
- Modify: `bridge/internal/link/link.go` (Sender gets `onLine`, `OnLine`, and starts a reader in `connect`)
- Test: `bridge/internal/link/read_test.go`

**Interfaces:**
- Produces: `(*Sender).OnLine(fn func(line []byte))`, to be called before `Run`; `fn` runs on a reader goroutine per open port, once per newline-terminated line of at most 4096 bytes (longer lines are dropped whole, blank lines skipped, `\r\n` trimmed, the slice is a private copy). The reader stops when the port's `Read` fails, which happens when the port is closed. A port that does not implement `io.Reader` is never read.

- [ ] **Step 1: Write the failing tests** (`bridge/internal/link/read_test.go`)

```go
package link

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func collect(t *testing.T, input string) []string {
	t.Helper()
	var got []string
	readLines(strings.NewReader(input), func(b []byte) { got = append(got, string(b)) })
	return got
}

func TestReadLines(t *testing.T) {
	long := strings.Repeat("x", maxLine+10)
	cases := map[string]struct {
		in   string
		want []string
	}{
		"two lines":                 {"a\nb\n", []string{"a", "b"}},
		"crlf":                      {"a\r\nb\r\n", []string{"a", "b"}},
		"blank lines skipped":       {"\n\na\n\n", []string{"a"}},
		"unterminated tail dropped": {"a\nhalf", []string{"a"}},
		"overlong line dropped":     {long + "\nok\n", []string{"ok"}},
		"overlong then short":       {long + "\n" + "a\n", []string{"a"}},
		"exactly at the limit":      {strings.Repeat("y", maxLine-1) + "\n", []string{strings.Repeat("y", maxLine-1)}},
	}
	for name, c := range cases {
		got := collect(t, c.in)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %d lines, want %d", name, len(got), len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: line %d differs", name, i)
			}
		}
	}
}

func TestReadLinesGivesPrivateCopies(t *testing.T) {
	var got [][]byte
	readLines(strings.NewReader("aaa\nbbb\n"), func(b []byte) { got = append(got, b) })
	if string(got[0]) != "aaa" || string(got[1]) != "bbb" {
		t.Fatalf("slices were reused: %q %q", got[0], got[1])
	}
}

// duplex is a port the test can both read from and write to.
type duplex struct {
	r       *io.PipeReader
	mu      sync.Mutex
	written []byte
	closed  chan struct{}
	once    sync.Once
}

func newDuplex() (*duplex, *io.PipeWriter) {
	pr, pw := io.Pipe()
	return &duplex{r: pr, closed: make(chan struct{})}, pw
}

func (d *duplex) Read(p []byte) (int, error) { return d.r.Read(p) }
func (d *duplex) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.written = append(d.written, p...)
	return len(p), nil
}
func (d *duplex) Close() error {
	d.once.Do(func() { close(d.closed); d.r.Close() })
	return nil
}

func TestSenderDeliversLinesAndStopsReadingOnClose(t *testing.T) {
	port, feed := newDuplex()
	lines := make(chan string, 4)
	s := NewSender(func() (io.WriteCloser, error) { return port, nil }, 10*time.Millisecond)
	s.OnLine(func(b []byte) { lines <- string(b) })

	ctx, cancel := context.WithCancel(context.Background())
	frames := make(chan []byte, 1)
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx, frames) }()

	if _, err := feed.Write([]byte("{\"v\":1}\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-lines:
		if got != `{"v":1}` {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("line not delivered")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
	}
	select {
	case <-port.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("port not closed")
	}
}

func TestSenderWithoutOnLineNeverReads(t *testing.T) {
	port, feed := newDuplex()
	s := NewSender(func() (io.WriteCloser, error) { return port, nil }, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	frames := make(chan []byte, 1)
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx, frames) }()

	got := make(chan error, 1)
	go func() { _, err := feed.Write([]byte("x\n")); got <- err }()
	select {
	case <-got:
		t.Fatal("something consumed the port's input without OnLine")
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	<-done
	feed.Close()
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd bridge && go test ./internal/link/ -run 'ReadLines|Sender(Delivers|Without)'`
Expected: FAIL (`undefined: readLines`, `s.OnLine undefined`).

- [ ] **Step 3: Implement**

`bridge/internal/link/read.go`:

```go
package link

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

const maxLine = 4096

// readLines calls fn with a private copy of every newline-terminated line of r
// until r fails. Blank lines and lines of maxLine bytes or more are dropped.
func readLines(r io.Reader, fn func([]byte)) {
	br := bufio.NewReaderSize(r, maxLine)
	skipping := false
	for {
		line, err := br.ReadSlice('\n')
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			skipping = true
		case err != nil:
			return
		case skipping:
			skipping = false
		default:
			if line = bytes.TrimRight(line, "\r\n"); len(line) > 0 {
				fn(append([]byte(nil), line...))
			}
		}
	}
}
```

In `bridge/internal/link/link.go`:

1. Add the field and method:

```go
type Sender struct {
	open         OpenFunc
	retry        time.Duration
	writeTimeout time.Duration
	lastOpenErr  string
	onLine       func([]byte)
}

// OnLine registers fn for lines the display sends. Call it before Run; fn runs on
// a reader goroutine per open port, so it must not block.
func (s *Sender) OnLine(fn func(line []byte)) { s.onLine = fn }
```

2. In `connect`, directly after `s.opened()` add `s.startReader(port)`, and add:

```go
// startReader reads lines from port until it is closed; ports that cannot be read are ignored.
func (s *Sender) startReader(port io.Writer) {
	if r, ok := port.(io.Reader); ok && s.onLine != nil {
		go readLines(r, s.onLine)
	}
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `cd bridge && gofmt -l . ; go vet ./... && go test -race -count=5 ./internal/link/ && go test -race ./...`
Expected: no gofmt output; all `ok`.

- [ ] **Step 5: Commit**

```bash
git add bridge && git commit -m "feat(bridge): read display lines from the serial port"
```

---

### Task 5: Wire actions into the bridge

**Files:**
- Modify: `bridge/cmd/bridge/main.go`, `bridge/cmd/bridge/flags.go`
- Test: `bridge/cmd/bridge/actions_test.go`, `bridge/cmd/bridge/flags_test.go` (append)

**Interfaces:**
- Consumes: `protocol.Action`/`ParseAction` (Task 1), `state.Lookup` (Task 2), `act.Focuser` (Task 3), `link.Sender.OnLine` (Task 4).
- Produces: `config` gains `actions <-chan protocol.Action` and `focus chan<- string` (nil disables); options gain `act bool` (default true) and `cmux string` (default `cmux`); `focusWorker(ctx, jobs <-chan string, focus func(context.Context, string) error)`.

- [ ] **Step 1: Write the failing tests**

`bridge/cmd/bridge/actions_test.go`:

```go
package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"desktop-controller/bridge/internal/events"
	"desktop-controller/bridge/internal/protocol"
)

const (
	w1 = "AAAAAAAA-0000-4000-8000-000000000001"
)

func running(surface, ws string) events.Event {
	return events.Event{Name: "sidebar.metadata.updated", Label: "Running", SurfaceID: surface, WorkspaceID: ws, At: time.UnixMilli(ms("11:00:00"))}
}

func actionHarness(t *testing.T) (*harness, chan protocol.Action, chan string) {
	t.Helper()
	actions := make(chan protocol.Action, 4)
	focus := make(chan string, 1)
	cfg := testCfg
	cfg.actions, cfg.focus = actions, focus
	return startRun(t, cfg), actions, focus
}

func TestFocusActionReachesTheCardsWorkspace(t *testing.T) {
	h, actions, focus := actionHarness(t)
	h.send(running(s1, w1))
	actions <- protocol.Action{Name: protocol.ActionFocus, ID: s1}
	select {
	case ws := <-focus:
		if ws != w1 {
			t.Fatalf("focused %q, want %q", ws, w1)
		}
	case <-time.After(bound):
		t.Fatal("focus not requested")
	}
}

func TestActionsForUnknownOrWorkspacelessCardsAreIgnored(t *testing.T) {
	h, actions, focus := actionHarness(t)
	h.send(running(s1, ""))
	actions <- protocol.Action{Name: protocol.ActionFocus, ID: "never-seen"}
	actions <- protocol.Action{Name: protocol.ActionFocus, ID: s1}
	select {
	case ws := <-focus:
		t.Fatalf("focused %q for a card that cannot be focused", ws)
	case <-time.After(quiet):
	}
}

func TestBusyFocusDropsTheTap(t *testing.T) {
	h, actions, focus := actionHarness(t)
	h.send(running(s1, w1))
	for i := 0; i < 3; i++ {
		actions <- protocol.Action{Name: protocol.ActionFocus, ID: s1}
	}
	time.Sleep(quiet)
	if got := len(focus); got != 1 {
		t.Fatalf("queued %d focus jobs, want 1 (cap)", got)
	}
}

func TestFocusWorkerRunsJobsOneByOneAndLogsFailures(t *testing.T) {
	jobs := make(chan string)
	var mu sync.Mutex
	var seen []string
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		focusWorker(ctx, jobs, func(_ context.Context, ws string) error {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, ws)
			if ws == "bad" {
				return errors.New("boom")
			}
			return nil
		})
	}()
	for _, ws := range []string{"bad", "good"} {
		select {
		case jobs <- ws:
		case <-time.After(bound):
			t.Fatal("worker stopped taking jobs after a failure")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(bound):
		t.Fatal("worker did not stop on cancel")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "bad" || seen[1] != "good" {
		t.Fatalf("seen %v", seen)
	}
}

func TestRunStillStopsWithActionsWired(t *testing.T) {
	h, _, _ := actionHarness(t)
	h.stop()
}
```

Append to `bridge/cmd/bridge/flags_test.go`:

```go
func TestParseFlagsActionDefaultsAndOverrides(t *testing.T) {
	o, err := parseFlags(nil, home)
	if err != nil || !o.act || o.cmux != "cmux" {
		t.Fatalf("defaults %+v err=%v", o, err)
	}
	o, err = parseFlags([]string{"--act=false", "--cmux=/opt/homebrew/bin/cmux"}, home)
	if err != nil || o.act || o.cmux != "/opt/homebrew/bin/cmux" {
		t.Fatalf("overrides %+v err=%v", o, err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd bridge && go test ./cmd/bridge/`
Expected: FAIL (`unknown field actions`, `o.act undefined`, `undefined: focusWorker`).

- [ ] **Step 3: Implement**

`bridge/cmd/bridge/flags.go`: add to `options` the fields `act bool` and `cmux string`, and in `parseFlags` before `fs.Parse`:

```go
	fs.BoolVar(&o.act, "act", true, "let the display focus sessions in cmux (needs a cmux terminal)")
	fs.StringVar(&o.cmux, "cmux", "cmux", "cmux binary used for actions")
```

`bridge/cmd/bridge/main.go`:

1. Imports: add `"desktop-controller/bridge/internal/act"`.
2. `config` becomes:

```go
type config struct {
	maxAge         time.Duration
	emitEvery      time.Duration
	heartbeatEvery time.Duration
	actions        <-chan protocol.Action // nil: actions disabled
	focus          chan<- string          // workspace ids for focusWorker
}
```

3. Replace the sender wiring in `main` with:

```go
	frames := make(chan []byte, 1)
	sender := link.NewSender(open, time.Second)
	cfg := defaults
	cfg.maxAge = o.maxAge
	if o.act && !o.stdout {
		cfg = withActions(ctx, cfg, sender, act.Focuser{Cmux: o.cmux, Run: act.ExecRunner, Timeout: 5 * time.Second}.Focus)
	}
	go sender.Run(ctx, frames)
	run(ctx, follow.Follow(ctx, o.events, 250*time.Millisecond), frames, cfg)
```

(delete the old `go link.NewSender(...)`, `cfg := defaults` and `cfg.maxAge = o.maxAge` lines).

4. Add:

```go
// withActions routes the display's focus requests to a single focus worker.
func withActions(ctx context.Context, cfg config, sender *link.Sender, focus func(context.Context, string) error) config {
	actions := make(chan protocol.Action, 4)
	jobs := make(chan string, 1)
	sender.OnLine(func(line []byte) {
		a, err := protocol.ParseAction(line)
		if err != nil {
			log.Printf("action: %v", err)
			return
		}
		select {
		case actions <- a:
		default:
			log.Printf("action: queue full, dropped")
		}
	})
	go focusWorker(ctx, jobs, focus)
	cfg.actions, cfg.focus = actions, jobs
	return cfg
}

// focusWorker runs focus jobs one at a time until ctx ends; failures are logged.
func focusWorker(ctx context.Context, jobs <-chan string, focus func(context.Context, string) error) {
	for {
		select {
		case <-ctx.Done():
			return
		case ws := <-jobs:
			if err := focus(ctx, ws); err != nil {
				log.Printf("focus: %v", err)
			}
		}
	}
}

// handleAction queues a focus for the card's workspace; the display only names cards.
func handleAction(s state.State, a protocol.Action, focus chan<- string) {
	card, ok := s.Lookup(a.ID)
	if !ok || card.WS == "" {
		log.Printf("action %s: card unknown or without workspace", a.Name)
		return
	}
	select {
	case focus <- card.WS:
	default:
		log.Printf("action %s: focus busy, dropped", a.Name)
	}
}
```

5. In `run`, add a case to the `select` (a nil `cfg.actions` blocks forever, so it is inert when disabled):

```go
		case a := <-cfg.actions:
			handleAction(s, a, cfg.focus)
```

- [ ] **Step 4: Run to verify they pass**

Run: `cd bridge && gofmt -l . ; go vet ./... && go test -race -count=3 ./...`
Expected: no gofmt output; all packages `ok`.

- [ ] **Step 5: Manual check of the cmux commands (user, inside a cmux terminal)**

Run (the UUID is any `id` of a workspace in `cmux tree --json --all`, e.g. `cmux tree --json --all | jq -r '.windows[0].workspaces[1].id'`):

```bash
cd bridge && go run ./cmd/focus "$(cmux tree --json --all | jq -r '.windows[0].workspaces[1].id')"
```

Expected: cmux switches to that workspace and focuses its left/first pane; no output. A failure prints `focus: ...`.

- [ ] **Step 6: Commit**

```bash
git add bridge && git commit -m "feat(bridge): focus a workspace when the display asks"
```

---

## Self-Review

- **Spec coverage:** tap-to-focus = Tasks 1-5 (action frame, workspace tracking, first-pane rule from the real tree, serial read side, wiring). The display side (sending the action) belongs to the firmware plan.
- **Deferred:** `--act` needs a cmux terminal (socket access); hardware round trip (board sends a tap, cmux switches) is verified in the firmware plan.
- **Type consistency:** `Card.WS`, `Action{Name,ID}`, `ParseAction`, `State.Lookup`, `FirstPanel`, `Focuser.Focus(ctx, ws) error`, `Sender.OnLine(func([]byte))`, `config.actions/focus`, `focusWorker`, `handleAction` are used with the same signatures everywhere.
- **Placeholders:** none.
