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

const (
	snippetLen    = 80
	firstSkipLogs = 5
	skipLogEvery  = 1000
)

// Follow replays path, then emits appended events until ctx is cancelled.
// It keeps the file open between polls so a rotated file's tail is still read.
func Follow(ctx context.Context, path string, poll time.Duration) <-chan Event {
	out := make(chan Event, 64)
	w := &follower{path: path}
	go func() {
		defer close(out)
		defer w.close()
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		for {
			w.poll(ctx, out)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return out
}

type follower struct {
	path    string
	file    *os.File
	info    os.FileInfo
	reader  *bufio.Reader
	offset  int64
	partial []byte
	skipped int
	missing int // 0 found, 1 first miss (silent), 2 "waiting" logged
	lastErr string
}

func (w *follower) poll(ctx context.Context, out chan<- Event) {
	if ctx.Err() != nil {
		return
	}
	if w.file == nil && !w.reopen() {
		return
	}
	if !w.drain(ctx, out) {
		return
	}
	if w.rotated() && w.drain(ctx, out) {
		w.reopen()
	}
}

// reopen replaces the current handle with a fresh one on path, starting at offset 0.
func (w *follower) reopen() bool {
	w.close()
	f, err := os.Open(w.path)
	if err != nil {
		w.openFailed(err)
		return false
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		w.logErr("stat", err)
		return false
	}
	if w.missing == 2 {
		log.Printf("follow: %s appeared", w.path)
	}
	w.missing, w.lastErr = 0, ""
	w.file, w.info = f, info
	w.reader = bufio.NewReader(f)
	w.offset, w.partial = 0, nil
	return true
}

func (w *follower) close() {
	if w.file != nil {
		w.file.Close()
		w.file, w.info = nil, nil
	}
}

// openFailed stays silent on the first not-exist miss: a one-poll debounce before logging "waiting".
func (w *follower) openFailed(err error) {
	if !os.IsNotExist(err) {
		w.logErr("open", err)
		return
	}
	switch w.missing {
	case 0:
		w.missing = 1
	case 1:
		w.missing = 2
		log.Printf("follow: waiting for %s", w.path)
	}
}

func (w *follower) logErr(op string, err error) {
	if msg := err.Error(); msg != w.lastErr {
		w.lastErr = msg
		log.Printf("follow: %s %s: %v", op, w.path, err)
	}
}

// rotated reports whether path now refers to a different file or the same file shrank.
func (w *follower) rotated() bool {
	pathInfo, err := os.Stat(w.path)
	if err != nil {
		if !os.IsNotExist(err) {
			w.logErr("stat", err)
		}
		return false
	}
	return !os.SameFile(w.info, pathInfo) || pathInfo.Size() < w.offset
}

// drain reads to EOF, emitting complete lines. It returns false if ctx ended.
func (w *follower) drain(ctx context.Context, out chan<- Event) bool {
	for {
		chunk, err := w.reader.ReadBytes('\n')
		w.offset += int64(len(chunk))
		w.partial = append(w.partial, chunk...)
		if err != nil {
			if err != io.EOF {
				w.logErr("read", err)
			}
			return true
		}
		if !w.emit(ctx, out) {
			return false
		}
	}
}

func (w *follower) emit(ctx context.Context, out chan<- Event) bool {
	start := w.offset - int64(len(w.partial))
	line := bytes.TrimSpace(w.partial)
	w.partial = nil
	if len(line) == 0 {
		return true
	}
	e, err := events.ParseLine(line)
	if err != nil {
		w.skip(start, line)
		return true
	}
	select {
	case out <- e:
		return true
	case <-ctx.Done():
		return false
	}
}

func (w *follower) skip(offset int64, line []byte) {
	w.skipped++
	if w.skipped <= firstSkipLogs || w.skipped%skipLogEvery == 0 {
		log.Printf("follow: skipping unparseable line #%d at offset %d: %.*q", w.skipped, offset, snippetLen, line)
	}
}
