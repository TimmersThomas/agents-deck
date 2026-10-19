package main

import (
	"context"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"desktop-controller/bridge/internal/follow"
	"desktop-controller/bridge/internal/link"
	"desktop-controller/bridge/internal/protocol"
	"desktop-controller/bridge/internal/state"
)

type config struct {
	maxAge         time.Duration
	emitEvery      time.Duration
	heartbeatEvery time.Duration
}

var defaults = config{maxAge: 12 * time.Hour, emitEvery: 250 * time.Millisecond, heartbeatEvery: 30 * time.Second}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	eventsPath := flag.String("events", filepath.Join(home, ".cmuxterm", "events.jsonl"), "cmux events file")
	vid := flag.String("vid", "303A", "USB vendor id")
	pid := flag.String("pid", "1001", "USB product id")
	baud := flag.Int("baud", 115200, "serial baud rate")
	toStdout := flag.Bool("stdout", false, "print frames instead of using serial")
	maxAge := flag.Duration("max-age", defaults.maxAge, "drop cards with no status change for this long")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	open := link.SerialOpener(*vid, *pid, *baud)
	if *toStdout {
		open = func() (io.WriteCloser, error) { return nopCloser{os.Stdout}, nil }
	}
	frames := make(chan []byte, 1)
	go link.NewSender(open, time.Second).Run(ctx, frames)

	run(ctx, follow.Follow(ctx, *eventsPath, 250*time.Millisecond), frames, config{maxAge: *maxAge, emitEvery: defaults.emitEvery, heartbeatEvery: defaults.heartbeatEvery})
}

// run folds events into state and queues a frame on frames at most every
// cfg.emitEvery when something changed, and every cfg.heartbeatEvery regardless.
// frames holds only the newest frame, so a slow consumer never blocks run.
func run(ctx context.Context, evs <-chan follow.Event, frames chan []byte, cfg config) {
	var s state.State
	dirty := true
	lastSent := time.Time{}
	tick := time.NewTicker(cfg.emitEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-evs:
			if !ok {
				return
			}
			var changed bool
			s, changed = state.Apply(s, e)
			dirty = dirty || changed
		case now := <-tick.C:
			pruned := state.Prune(s, now, cfg.maxAge)
			dirty = dirty || pruned.Len() != s.Len()
			s = pruned
			if dirty || now.Sub(lastSent) >= cfg.heartbeatEvery {
				if send(frames, s, now) {
					dirty, lastSent = false, now
				}
			}
		}
	}
}

// send replaces any unconsumed frame with the current state.
func send(frames chan []byte, s state.State, now time.Time) bool {
	b, err := protocol.Render(s.Cards(), now)
	if err != nil {
		log.Printf("render: %v", err)
		return false
	}
	select {
	case <-frames:
	default:
	}
	frames <- b
	return true
}
