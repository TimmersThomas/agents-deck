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
