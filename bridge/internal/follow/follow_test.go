package follow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func line(seq string) string {
	return `{"name":"agent.hook.Stop","seq":` + seq + `,"occurred_at":"2026-10-18T10:00:00.000Z","surface_id":"S","payload":{"hook_event_name":"Stop"}}` + "\n"
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func next(t *testing.T, ch <-chan Event) int64 {
	t.Helper()
	select {
	case e := <-ch:
		return e.Seq
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return 0
	}
}

func TestReplayThenFollowThenRotate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	appendTo(t, path, line("1")+"garbage\n"+line("2"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := Follow(ctx, path, 10*time.Millisecond)
	if next(t, ch) != 1 || next(t, ch) != 2 {
		t.Fatal("replay order wrong")
	}

	appendTo(t, path, line("3"))
	if got := next(t, ch); got != 3 {
		t.Fatalf("follow got %d", got)
	}

	if err := os.WriteFile(path, []byte(line("9")), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := next(t, ch); got != 9 {
		t.Fatalf("after rotation got %d", got)
	}
}
