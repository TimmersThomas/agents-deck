package link

import (
	"context"
	"errors"
	"io"
	"log"
	"time"
)

var errWriteTimeout = errors.New("write timed out")

// withResync prefixes the first frame after a connect with a newline, so the
// firmware drops whatever partial line an earlier, interrupted write left behind.
func withResync(frame []byte, resync bool) []byte {
	if !resync {
		return frame
	}
	out := make([]byte, 0, len(frame)+1)
	out = append(out, '\n')
	return append(out, frame...)
}

// writeFrame writes in a goroutine so a stalled port cannot block Run. If the write
// has not returned by the timeout or cancel, pending yields its result once it does.
func (s *Sender) writeFrame(ctx context.Context, port io.Writer, frame []byte) (pending <-chan error, err error) {
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
		return nil, err
	case <-t.C:
		return res, errWriteTimeout
	case <-ctx.Done():
		return res, ctx.Err()
	}
}

// closeAsync closes port in a goroutine, logs a Close error and yields it on the result.
func closeAsync(port io.Closer) <-chan error {
	res := make(chan error, 1)
	go func() {
		err := port.Close()
		if err != nil {
			log.Printf("link: close: %v", err)
		}
		res <- err
	}()
	return res
}

// await consumes frames until done yields; stop is true when Run must return first.
func await(ctx context.Context, frames <-chan []byte, latest []byte, done <-chan error) (_ []byte, stop bool, err error) {
	for {
		select {
		case err := <-done:
			return latest, false, err
		case <-ctx.Done():
			return latest, true, nil
		case f, ok := <-frames:
			if !ok {
				return latest, true, nil
			}
			var open bool
			if latest, open = drainLatest(frames, f); !open {
				return latest, true, nil
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
