package follow

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

const testPoll = 10 * time.Millisecond

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func captureLog(t *testing.T) *syncBuf {
	t.Helper()
	sb := &syncBuf{}
	prev := log.Writer()
	log.SetOutput(sb)
	t.Cleanup(func() { log.SetOutput(prev) })
	return sb
}

func waitLog(t *testing.T, sb *syncBuf, substr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(sb.String(), substr) {
		if time.Now().After(deadline) {
			t.Fatalf("log never contained %q; got:\n%s", substr, sb.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func startFollow(t *testing.T, path string) (<-chan Event, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return Follow(ctx, path, testPoll), cancel
}

func TestPartialLineWaitsForNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	appendTo(t, path, line("1"))
	ch, _ := startFollow(t, path)
	if got := next(t, ch); got != 1 {
		t.Fatalf("replay got %d", got)
	}

	full := line("2")
	half := len(full) / 2
	appendTo(t, path, full[:half])
	select {
	case e := <-ch:
		t.Fatalf("event %d emitted for a partial line", e.Seq)
	case <-time.After(5 * testPoll):
	}

	appendTo(t, path, full[half:])
	if got := next(t, ch); got != 2 {
		t.Fatalf("completed line got %d", got)
	}
}

func TestCancelClosesChannelWhileSendBlocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var sb strings.Builder
	for i := 1; i <= 500; i++ {
		sb.WriteString(line(fmt.Sprint(i)))
	}
	appendTo(t, path, sb.String())
	ch, cancel := startFollow(t, path)

	time.Sleep(5 * testPoll)
	cancel()

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
}

func TestRenameRotationDeliversOldTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	appendTo(t, path, line("1"))
	ch, _ := startFollow(t, path)
	if got := next(t, ch); got != 1 {
		t.Fatalf("replay got %d", got)
	}

	appendTo(t, path, line("2"))
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(line("3")), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := next(t, ch); got != 2 {
		t.Fatalf("old tail: got %d, want 2", got)
	}
	if got := next(t, ch); got != 3 {
		t.Fatalf("new file: got %d, want 3", got)
	}
}

func TestMissingFileAppearsLater(t *testing.T) {
	sb := captureLog(t)
	path := filepath.Join(t.TempDir(), "events.jsonl")
	ch, _ := startFollow(t, path)

	waitLog(t, sb, "waiting for "+path)
	appendTo(t, path, line("4"))
	if got := next(t, ch); got != 4 {
		t.Fatalf("got %d", got)
	}
	waitLog(t, sb, "appeared")

	if n := strings.Count(sb.String(), "waiting for"); n != 1 {
		t.Fatalf("waiting logged %d times, want 1:\n%s", n, sb.String())
	}
}

func TestGarbageLineIsLoggedWithOffset(t *testing.T) {
	sb := captureLog(t)
	path := filepath.Join(t.TempDir(), "events.jsonl")
	long := strings.Repeat("x", 200)
	appendTo(t, path, "garbage\n"+long+"\n"+line("7"))
	ch, _ := startFollow(t, path)

	if got := next(t, ch); got != 7 {
		t.Fatalf("valid line after garbage got %d", got)
	}
	out := sb.String()
	if !strings.Contains(out, "offset 0") || !strings.Contains(out, "garbage") {
		t.Fatalf("missing offset-bearing skip message:\n%s", out)
	}
	if !strings.Contains(out, "offset 8") {
		t.Fatalf("second skip should report offset 8:\n%s", out)
	}
	if !strings.Contains(out, strings.Repeat("x", 80)) || strings.Contains(out, strings.Repeat("x", 81)) {
		t.Fatalf("line snippet not capped at 80 chars:\n%s", out)
	}
}

func TestSkipLoggingIsRateLimited(t *testing.T) {
	sb := captureLog(t)
	path := filepath.Join(t.TempDir(), "events.jsonl")
	appendTo(t, path, strings.Repeat("garbage\n", 2500)+line("1"))
	ch, _ := startFollow(t, path)

	if got := next(t, ch); got != 1 {
		t.Fatalf("got %d", got)
	}
	// 5 first skips, then the 1000th and 2000th.
	if n := strings.Count(sb.String(), "unparseable"); n != 7 {
		t.Fatalf("logged %d skip lines, want 7", n)
	}
}

func TestInPlaceTruncateDropsPartialBuffer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	appendTo(t, path, line("1")+line("2"))
	ch, _ := startFollow(t, path)
	next(t, ch)
	next(t, ch)

	appendTo(t, path, line("3")[:20])
	time.Sleep(3 * testPoll)
	if err := os.WriteFile(path, []byte(line("8")), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := next(t, ch); got != 8 {
		t.Fatalf("after truncate got %d, want 8", got)
	}
}
