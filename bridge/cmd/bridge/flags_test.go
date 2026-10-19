package main

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"
)

func home() (string, error)   { return "/Users/dev", nil }
func noHome() (string, error) { return "", errors.New("no home") }

func TestParseFlagsDefaults(t *testing.T) {
	o, err := parseFlags(nil, home)
	if err != nil {
		t.Fatal(err)
	}
	if o.events != "/Users/dev/.cmuxterm/events.jsonl" || o.baud != 115200 || o.maxAge != 12*time.Hour || o.stdout {
		t.Fatalf("defaults %+v", o)
	}
}

func TestParseFlagsRejectsInvalidValues(t *testing.T) {
	cases := map[string][]string{
		"max-age": {"--max-age=0"},
		"baud":    {"--baud=-1"},
	}
	for name, args := range cases {
		_, err := parseFlags(args, home)
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%v: err = %v, want one naming %s", args, err, name)
		}
	}
	if _, err := parseFlags([]string{"--max-age=-1h"}, home); err == nil {
		t.Error("negative max-age accepted")
	}
}

func TestParseFlagsNeedsHomeOnlyWithoutEvents(t *testing.T) {
	o, err := parseFlags([]string{"--events=/tmp/e.jsonl"}, noHome)
	if err != nil || o.events != "/tmp/e.jsonl" {
		t.Fatalf("got %+v, %v", o, err)
	}
	if _, err := parseFlags(nil, noHome); err == nil {
		t.Fatal("missing home accepted without --events")
	}
}

func TestSIGTERMCancelsContext(t *testing.T) {
	ctx, stop := signalContext(context.Background())
	defer stop()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(bound):
		t.Fatal("SIGTERM did not cancel the context")
	}
}
