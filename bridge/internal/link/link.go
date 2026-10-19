// Package link writes frames to the display over USB serial.
package link

import (
	"context"
	"errors"
	"io"
	"log"
	"time"
)

const defaultWriteTimeout = 2 * time.Second

var errWriteTimeout = errors.New("write timed out")

// OpenFunc opens the display port. It is called again after every failure.
type OpenFunc func() (io.WriteCloser, error)

// Sender writes the latest frame to a port that may disappear and come back.
type Sender struct {
	open         OpenFunc
	retry        time.Duration
	writeTimeout time.Duration
}

// NewSender returns a Sender that reopens the port via open, waiting retry between attempts.
func NewSender(open OpenFunc, retry time.Duration) *Sender {
	return &Sender{open: open, retry: retry, writeTimeout: defaultWriteTimeout}
}

// Run keeps only the latest frame, writes it on every (re)connect and whenever a
// newer one arrives. It returns when ctx is cancelled or frames is closed.
func (s *Sender) Run(ctx context.Context, frames <-chan []byte) {
	var latest []byte
	for ctx.Err() == nil {
		port, err := s.open()
		if err != nil {
			log.Printf("link: open: %v", err)
		} else {
			var done bool
			if latest, done = s.serve(ctx, port, frames, latest); done {
				return
			}
		}
		var more bool
		if latest, more = s.waitRetry(ctx, frames, latest); !more {
			return
		}
	}
}

// serve writes frames to port until a write fails (done=false) or Run must stop (done=true).
func (s *Sender) serve(ctx context.Context, port io.WriteCloser, frames <-chan []byte, latest []byte) (_ []byte, done bool) {
	abandoned := false
	defer func() { s.closePort(port, !abandoned) }()
	latest, open := drainLatest(frames, latest)
	if !open {
		return latest, true
	}
	for {
		if latest != nil {
			if err := s.writeFrame(ctx, port, latest); err != nil {
				log.Printf("link: write: %v", err)
				abandoned = true
				return latest, ctx.Err() != nil
			}
		}
		select {
		case <-ctx.Done():
			return latest, true
		case f, ok := <-frames:
			if !ok {
				return latest, true
			}
			if latest, open = drainLatest(frames, f); !open {
				return latest, true
			}
		}
	}
}

// writeFrame writes in a goroutine so a stalled port cannot block Run. After a
// timeout or cancel the goroutine is abandoned; it exits once Close unblocks Write.
func (s *Sender) writeFrame(ctx context.Context, port io.Writer, frame []byte) error {
	res := make(chan error, 1)
	go func() {
		n, err := port.Write(frame)
		if err == nil && n != len(frame) {
			err = io.ErrShortWrite
		}
		res <- err
	}()
	t := time.NewTimer(s.writeTimeout)
	defer t.Stop()
	select {
	case err := <-res:
		return err
	case <-t.C:
		return errWriteTimeout
	case <-ctx.Done():
		return ctx.Err()
	}
}

// closePort closes in a goroutine since Close may block; wait bounds how long to wait for it.
func (s *Sender) closePort(port io.Closer, wait bool) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := port.Close(); err != nil {
			log.Printf("link: close: %v", err)
		}
	}()
	if !wait {
		return
	}
	select {
	case <-done:
	case <-time.After(s.writeTimeout):
		log.Printf("link: close did not finish within %v", s.writeTimeout)
	}
}

// waitRetry waits retry while still consuming frames; more is false when Run must stop.
func (s *Sender) waitRetry(ctx context.Context, frames <-chan []byte, latest []byte) (_ []byte, more bool) {
	t := time.NewTimer(s.retry)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return latest, false
		case <-t.C:
			return latest, true
		case f, ok := <-frames:
			if !ok {
				return latest, false
			}
			var open bool
			if latest, open = drainLatest(frames, f); !open {
				return latest, false
			}
		}
	}
}

// drainLatest returns the newest queued frame, or latest if none is queued.
// open is false once frames is closed; a closed channel never overwrites latest.
func drainLatest(frames <-chan []byte, latest []byte) (_ []byte, open bool) {
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				return latest, false
			}
			latest = f
		default:
			return latest, true
		}
	}
}
