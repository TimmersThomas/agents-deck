package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"time"

	"desktop-controller/bridge/internal/act"
	"desktop-controller/bridge/internal/follow"
	"desktop-controller/bridge/internal/link"
	"desktop-controller/bridge/internal/protocol"
	"desktop-controller/bridge/internal/state"
)

type config struct {
	maxAge         time.Duration
	emitEvery      time.Duration
	heartbeatEvery time.Duration
	actions        <-chan protocol.Action // nil: actions disabled
	focus          chan<- string          // workspace ids for focusWorker
}

var defaults = config{maxAge: 12 * time.Hour, emitEvery: 250 * time.Millisecond, heartbeatEvery: 30 * time.Second}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func main() {
	o, err := parseFlags(os.Args[1:], os.UserHomeDir)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatalf("bridge: %v", err)
	}
	ctx, stop := signalContext(context.Background())
	defer stop()

	open := link.SerialOpener(o.vid, o.pid, o.baud)
	if o.stdout {
		open = func() (io.WriteCloser, error) { return nopCloser{os.Stdout}, nil }
	}
	frames := make(chan []byte, 1)
	sender := link.NewSender(open, time.Second)
	cfg := defaults
	cfg.maxAge = o.maxAge
	if o.act && !o.stdout {
		cfg = withActions(ctx, cfg, sender, act.Focuser{Cmux: o.cmux, Run: act.ExecRunner, Timeout: 5 * time.Second}.Focus)
	}
	go sender.Run(ctx, frames)
	run(ctx, follow.Follow(ctx, o.events, 250*time.Millisecond), frames, cfg)
}

// run queues a frame every cfg.emitEvery when something changed and every
// cfg.heartbeatEvery regardless, replacing any unconsumed frame so it never blocks.
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
		case a := <-cfg.actions:
			handleAction(s, a, cfg.focus)
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

// withActions routes the display's focus requests to a single focus worker.
func withActions(ctx context.Context, cfg config, sender *link.Sender, focus func(context.Context, string) error) config {
	actions := make(chan protocol.Action, 4)
	jobs := make(chan string, 1)
	sender.OnLine(func(line []byte) {
		a, err := protocol.ParseAction(line)
		if err != nil {
			log.Printf("action: %v", err)
			return
		}
		select {
		case actions <- a:
		default:
			log.Printf("action: queue full, dropped")
		}
	})
	go focusWorker(ctx, jobs, focus)
	cfg.actions, cfg.focus = actions, jobs
	return cfg
}

// focusWorker runs focus jobs one at a time until ctx ends; failures are logged.
func focusWorker(ctx context.Context, jobs <-chan string, focus func(context.Context, string) error) {
	for {
		select {
		case <-ctx.Done():
			return
		case ws := <-jobs:
			if err := focus(ctx, ws); err != nil {
				log.Printf("focus: %v", err)
			}
		}
	}
}

// handleAction queues a focus for the card's workspace; the display only names cards.
func handleAction(s state.State, a protocol.Action, focus chan<- string) {
	card, ok := s.Lookup(a.ID)
	if !ok || card.WS == "" {
		log.Printf("action %s: card unknown or without workspace", a.Name)
		return
	}
	select {
	case focus <- card.WS:
	default:
		log.Printf("action %s: focus busy, dropped", a.Name)
	}
}
