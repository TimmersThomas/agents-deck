package link

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type buf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *buf) Write(p []byte) (int, error) { b.mu.Lock(); defer b.mu.Unlock(); return b.b.Write(p) }
func (b *buf) Close() error                { return nil }
func (b *buf) String() string              { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

type failOnce struct{ failed bool }

func (f *failOnce) Write(p []byte) (int, error) {
	if !f.failed {
		f.failed = true
		return 0, errors.New("port gone")
	}
	return len(p), nil
}
func (f *failOnce) Close() error { return nil }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWritesFrames(t *testing.T) {
	out := &buf{}
	s := NewSender(func() (io.WriteCloser, error) { return out, nil }, 10*time.Millisecond)
	frames := make(chan []byte, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx, frames)

	frames <- []byte("a\n")
	waitFor(t, func() bool { return out.String() == "a\n" })
}

func TestReconnectResendsLatestFrame(t *testing.T) {
	out := &buf{}
	var mu sync.Mutex
	opens := 0
	open := func() (io.WriteCloser, error) {
		mu.Lock()
		defer mu.Unlock()
		opens++
		switch opens {
		case 1:
			return nil, errors.New("no board")
		case 2:
			return &failOnce{}, nil
		default:
			return out, nil
		}
	}
	s := NewSender(open, 10*time.Millisecond)
	frames := make(chan []byte, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx, frames)

	frames <- []byte("latest\n")
	waitFor(t, func() bool { return out.String() == "latest\n" })
}
