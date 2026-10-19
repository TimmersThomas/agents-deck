package act

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxStderr     = 200
	captureStderr = 1024
	waitDelay     = time.Second
)

// ExecRunner runs the command for real. Failures name the sub-command and carry a
// quoted stderr excerpt; a cancelled or expired ctx is wrapped so errors.Is sees it.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%s: no sub-command", name)
	}
	var stdout bytes.Buffer
	stderr := &headBuffer{max: captureStderr}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &stdout, stderr
	cmd.WaitDelay = waitDelay // a child that keeps the pipes open must not outlive ctx
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stdout.Bytes(), fmt.Errorf("%s %s: %w (%v)", name, args[0], ctxErr, err)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return stdout.Bytes(), fmt.Errorf("%s %s: %w: %q", name, args[0], err, excerpt(stderr.Bytes()))
	}
	return stdout.Bytes(), fmt.Errorf("%s %s: %w", name, args[0], err)
}

// excerpt trims b and cuts it to maxStderr bytes without splitting a rune.
func excerpt(b []byte) string {
	msg := strings.TrimSpace(string(b))
	if len(msg) <= maxStderr {
		return msg
	}
	cut := maxStderr
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut]
}

// headBuffer keeps the first max bytes written and drops the rest.
type headBuffer struct {
	bytes.Buffer
	max int
}

func (h *headBuffer) Write(p []byte) (int, error) {
	if room := h.max - h.Len(); room > 0 {
		h.Buffer.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}
