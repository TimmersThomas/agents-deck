package act

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// fakeCmd writes a #!/bin/sh script into a temp dir and returns its path; tests never run cmux.
func fakeCmd(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fakecmd")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExecRunnerReturnsStdout(t *testing.T) {
	out, err := ExecRunner(context.Background(), fakeCmd(t, "echo hello"), "tree")
	if err != nil || string(out) != "hello\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestExecRunnerFailureNamesSubcommandAndQuotesStderr(t *testing.T) {
	cmd := fakeCmd(t, "echo 'bad thing' >&2; exit 3")
	_, err := ExecRunner(context.Background(), cmd, "select-workspace", "--workspace", "x")
	if err == nil {
		t.Fatal("non-zero exit not reported")
	}
	for _, want := range []string{cmd + " select-workspace", "exit status 3", `"bad thing"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestExecRunnerQuotesControlCharactersFromStderr(t *testing.T) {
	_, err := ExecRunner(context.Background(), fakeCmd(t, `printf '\033[31mred\033[0m' >&2; exit 1`), "tree")
	if err == nil || strings.ContainsRune(err.Error(), 0x1b) || !strings.Contains(err.Error(), `\x1b[31m`) {
		t.Fatalf("control characters not quoted: %q", err)
	}
}

func TestExecRunnerCutsStderrOnARuneBoundary(t *testing.T) {
	_, err := ExecRunner(context.Background(), fakeCmd(t, "printf '%s' \""+strings.Repeat("a", maxStderr-1)+"éé\" >&2; exit 1"), "tree")
	if err == nil || !utf8.ValidString(err.Error()) || strings.Contains(err.Error(), `\x`) || strings.Contains(err.Error(), "é") {
		t.Fatalf("excerpt not cut cleanly before the split rune: %q", err)
	}
}

func TestExecRunnerReportsADeadlineAndReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ExecRunner(ctx, fakeCmd(t, "sleep 5 &\nwait"), "tree")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a wrapped deadline", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v for a 200ms timeout", d)
	}
}

func TestExecRunnerIsBoundedWhenAChildHoldsStdoutOpen(t *testing.T) {
	start := time.Now()
	_, err := ExecRunner(context.Background(), fakeCmd(t, "sleep 6 &\necho hi"), "tree")
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("took %v: a lingering child held ExecRunner", d)
	}
	if err == nil {
		t.Fatal("abandoned output not reported")
	}
}

func TestExecRunnerWithoutArgsReturnsAnError(t *testing.T) {
	if _, err := ExecRunner(context.Background(), fakeCmd(t, "echo hi")); err == nil {
		t.Fatal("empty args accepted")
	}
}
