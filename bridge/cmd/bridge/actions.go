package main

import (
	"context"
	"log"
	"os/exec"
	"time"

	"desktop-controller/bridge/internal/act"
	"desktop-controller/bridge/internal/link"
	"desktop-controller/bridge/internal/protocol"
	"desktop-controller/bridge/internal/state"
)

const focusTimeout = 5 * time.Second

func newFocuser(cmux string) act.Focuser {
	return act.Focuser{Cmux: cmux, Run: act.ExecRunner, Timeout: focusTimeout}
}

// warnIfMissing only looks the binary up; a missing cmux is not fatal because the
// display still works without focus.
func warnIfMissing(bin string) error {
	_, err := exec.LookPath(bin)
	if err != nil {
		log.Printf("bridge: cmux %q not found, focus will fail: %v", bin, err)
	}
	return err
}

// withActions routes the display's focus requests to a single focus worker. While
// one focus runs, one further tap is queued and later taps are dropped.
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
// While a focus runs one more is queued (jobs has room for one), later ones are dropped.
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
