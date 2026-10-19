// Package link writes frames to the display over USB serial.
//
// At most one port is in flight: after a write times out the port is neither
// closed nor reopened until that write returns, since on Darwin close(2) waits for
// a write in progress and every reopen would leak an fd and pulse DTR/RTS.
package link

import (
	"context"
	"io"
	"log"
	"time"
)

const defaultWriteTimeout = 2 * time.Second

// OpenFunc opens the display port. It is called again after every failure.
type OpenFunc func() (io.WriteCloser, error)

// Sender writes the latest frame to a port that may disappear and come back.
type Sender struct {
	open         OpenFunc
	retry        time.Duration
	writeTimeout time.Duration
	lastOpenErr  string
}

// NewSender returns a Sender that reopens the port via open, waiting retry between attempts.
func NewSender(open OpenFunc, retry time.Duration) *Sender {
	return &Sender{open: open, retry: retry, writeTimeout: defaultWriteTimeout}
}

// Run keeps only the latest frame, writes it on every (re)connect and whenever a
// newer one arrives. It returns when ctx is cancelled or frames is closed; a frame
// still queued at that point may be dropped.
func (s *Sender) Run(ctx context.Context, frames <-chan []byte) {
	var latest []byte
	for ctx.Err() == nil {
		var stop, more bool
		if latest, stop = s.connect(ctx, frames, latest); stop {
			return
		}
		if latest, more = s.waitRetry(ctx, frames, latest); !more {
			return
		}
	}
}

// connect opens and serves one port until it fails; stop is true when Run must return.
func (s *Sender) connect(ctx context.Context, frames <-chan []byte, latest []byte) (_ []byte, stop bool) {
	port, err := s.open()
	if err != nil {
		s.openFailed(err)
		return latest, false
	}
	s.opened()
	latest, stop, pending := s.serve(ctx, port, frames, latest)
	if stop {
		s.shutdown(port, pending == nil)
		return latest, true
	}
	if pending != nil {
		log.Printf("link: port stalled, waiting for write to return")
		var werr error
		if latest, stop, werr = await(ctx, frames, latest, pending); stop {
			s.shutdown(port, false)
			return latest, true
		}
		log.Printf("link: stalled write returned: %v", werr)
	}
	latest, stop, _ = await(ctx, frames, latest, closeAsync(port))
	return latest, stop
}

func (s *Sender) openFailed(err error) {
	if msg := err.Error(); msg != s.lastOpenErr {
		s.lastOpenErr = msg
		log.Printf("link: open: %v", err)
	}
}

func (s *Sender) opened() {
	if s.lastOpenErr != "" {
		s.lastOpenErr = ""
		log.Printf("link: connected")
	}
}

// serve writes frames to port until a write fails or Run must stop. pending is
// non-nil when a write is still blocked; it yields that write's result.
func (s *Sender) serve(ctx context.Context, port io.Writer, frames <-chan []byte, latest []byte) (_ []byte, stop bool, pending <-chan error) {
	latest, open := drainLatest(frames, latest)
	if !open {
		return latest, true, nil
	}
	resync := true
	for {
		if latest != nil {
			pending, err := s.writeFrame(ctx, port, withResync(latest, resync))
			if pending != nil {
				log.Printf("link: write: %v", err)
				return latest, ctx.Err() != nil, pending
			}
			if err != nil {
				log.Printf("link: write: %v", err)
				return latest, false, nil
			}
			resync = false
		}
		select {
		case <-ctx.Done():
			return latest, true, nil
		case f, ok := <-frames:
			if !ok {
				return latest, true, nil
			}
			if latest, open = drainLatest(frames, f); !open {
				return latest, true, nil
			}
		}
	}
}

// shutdown closes port before Run returns. With a write outstanding Close may block
// until that write returns, so it is not waited for.
func (s *Sender) shutdown(port io.Closer, wait bool) {
	done := closeAsync(port)
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
