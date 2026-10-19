// Command focus brings a cmux workspace to the front: `focus <workspace-uuid>`.
// It exists to check the cmux commands by hand; run it from a cmux terminal.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"desktop-controller/bridge/internal/act"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: focus <workspace-uuid>")
		os.Exit(2)
	}
	f := act.Focuser{Cmux: "cmux", Run: act.ExecRunner, Timeout: 5 * time.Second}
	if err := f.Focus(context.Background(), os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "focus:", err)
		os.Exit(1)
	}
}
