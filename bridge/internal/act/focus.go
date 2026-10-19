package act

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var workspaceID = regexp.MustCompile(`^[0-9A-Fa-f-]{36}$`)

const maxStderr = 200

// Runner runs one external command and returns its stdout.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs the command for real, wrapping failures with the sub-command and a
// short stderr excerpt.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err == nil {
		return out, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		msg := strings.TrimSpace(string(ee.Stderr))
		if len(msg) > maxStderr {
			msg = msg[:maxStderr]
		}
		return out, fmt.Errorf("%s %s: %w: %s", name, args[0], err, msg)
	}
	return out, fmt.Errorf("%s %s: %w", name, args[0], err)
}

// Focuser brings a cmux workspace to the front and focuses its first pane.
type Focuser struct {
	Cmux    string
	Run     Runner
	Timeout time.Duration
}

// Focus runs `cmux tree`, `select-workspace` and `focus-panel`; any failure stops it.
func (f Focuser) Focus(ctx context.Context, ws string) error {
	if !workspaceID.MatchString(ws) {
		return errors.New("focus: invalid workspace id")
	}
	ctx, cancel := context.WithTimeout(ctx, f.Timeout)
	defer cancel()
	tree, err := f.Run(ctx, f.Cmux, "tree", "--json", "--all")
	if err != nil {
		return err
	}
	surface, err := FirstPanel(tree, ws)
	if err != nil {
		return fmt.Errorf("focus: %w", err)
	}
	if _, err := f.Run(ctx, f.Cmux, "select-workspace", "--workspace", ws); err != nil {
		return err
	}
	_, err = f.Run(ctx, f.Cmux, "focus-panel", "--panel", surface, "--workspace", ws)
	return err
}
