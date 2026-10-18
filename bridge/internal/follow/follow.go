// Package follow tails a cmux events file, replaying history first.
package follow

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"time"

	"desktop-controller/bridge/internal/events"
)

type Event = events.Event

// Follow replays path, then emits appended events until ctx is cancelled.
func Follow(ctx context.Context, path string, poll time.Duration) <-chan Event {
	out := make(chan Event, 64)
	go func() {
		defer close(out)
		var offset int64
		var partial []byte
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		for {
			offset, partial = drain(ctx, path, offset, partial, out)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return out
}

func drain(ctx context.Context, path string, offset int64, partial []byte, out chan<- Event) (int64, []byte) {
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("follow: open %s: %v", path, err)
		}
		return offset, partial
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		log.Printf("follow: stat %s: %v", path, err)
		return offset, partial
	}
	if info.Size() < offset {
		offset, partial = 0, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		log.Printf("follow: seek %s: %v", path, err)
		return offset, partial
	}
	r := bufio.NewReader(f)
	for {
		chunk, err := r.ReadBytes('\n')
		offset += int64(len(chunk))
		partial = append(partial, chunk...)
		if err != nil {
			return offset, partial
		}
		if e, perr := events.ParseLine(bytes.TrimSpace(partial)); perr == nil {
			select {
			case out <- e:
			case <-ctx.Done():
				return offset, nil
			}
		}
		partial = nil
	}
}
