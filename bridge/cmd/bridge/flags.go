package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

type options struct {
	events   string
	vid, pid string
	baud     int
	stdout   bool
	maxAge   time.Duration
	act      bool
	cmux     string
}

// parseFlags only needs the home directory when --events is not given.
func parseFlags(args []string, homeDir func() (string, error)) (options, error) {
	var o options
	fs := flag.NewFlagSet("bridge", flag.ContinueOnError)
	fs.StringVar(&o.events, "events", "", "cmux events file (default ~/.cmuxterm/events.jsonl)")
	fs.StringVar(&o.vid, "vid", "303A", "USB vendor id")
	fs.StringVar(&o.pid, "pid", "1001", "USB product id")
	fs.IntVar(&o.baud, "baud", 115200, "serial baud rate")
	fs.BoolVar(&o.stdout, "stdout", false, "print frames instead of using serial")
	fs.BoolVar(&o.act, "act", true, "let the display focus sessions in cmux (needs a cmux terminal)")
	fs.StringVar(&o.cmux, "cmux", "cmux", "cmux binary used for actions")
	fs.DurationVar(&o.maxAge, "max-age", defaults.maxAge, "drop cards with no status change for this long")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if o.maxAge <= 0 {
		return options{}, fmt.Errorf("--max-age must be positive, got %v", o.maxAge)
	}
	if o.baud <= 0 {
		return options{}, fmt.Errorf("--baud must be positive, got %d", o.baud)
	}
	if o.events == "" {
		home, err := homeDir()
		if err != nil {
			return options{}, fmt.Errorf("no home directory for the default --events path: %w", err)
		}
		o.events = filepath.Join(home, ".cmuxterm", "events.jsonl")
	}
	return o, nil
}

func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}
