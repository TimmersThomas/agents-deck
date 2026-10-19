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

const (
	emitEvery      = 250 * time.Millisecond
	heartbeatEvery = 30 * time.Second
)

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
	maxAge := flag.Duration("max-age", 12*time.Hour, "drop cards with no status change for this long")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	open := link.SerialOpener(*vid, *pid, *baud)
	if *toStdout {
		open = func() (io.WriteCloser, error) { return nopCloser{os.Stdout}, nil }
	}
	frames := make(chan []byte, 1)
	go link.NewSender(open, time.Second).Run(ctx, frames)

	run(ctx, follow.Follow(ctx, *eventsPath, 250*time.Millisecond), frames, *maxAge)
}

func run(ctx context.Context, evs <-chan follow.Event, frames chan []byte, maxAge time.Duration) {
	var s state.State
	dirty := true
	lastSent := time.Time{}
	tick := time.NewTicker(emitEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-evs:
			if !ok {
				return
			}
			s = state.Apply(s, e)
			dirty = true
		case now := <-tick.C:
			if !dirty && now.Sub(lastSent) < heartbeatEvery {
				continue
			}
			s = state.Prune(s, now, maxAge)
			b, err := protocol.Render(s.Cards(), now)
			if err != nil {
				log.Printf("render: %v", err)
				continue
			}
			select {
			case <-frames:
			default:
			}
			frames <- b
			dirty, lastSent = false, now
		}
	}
}
