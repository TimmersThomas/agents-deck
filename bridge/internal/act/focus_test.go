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
