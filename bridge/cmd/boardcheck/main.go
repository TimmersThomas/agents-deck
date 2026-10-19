// Command boardcheck writes one state frame line to the display and prints what the display
// sends back. It opens the real serial port: run it by hand, never from tests.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"desktop-controller/bridge/internal/link"
)

func main() {
	frameFile := flag.String("frame-file", "", "file whose last line is sent as the state frame (e.g. output of bridge --stdout)")
	secs := flag.Int("secs", 5, "seconds to listen")
	vid := flag.String("vid", "303A", "USB vendor id")
	pid := flag.String("pid", "1001", "USB product id")
	flag.Parse()

	frame, err := lastLine(*frameFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "boardcheck:", err)
		os.Exit(1)
	}
	port, err := link.SerialOpener(*vid, *pid, 115200)()
	if err != nil {
		fmt.Fprintln(os.Stderr, "boardcheck:", err)
		os.Exit(1)
	}
	r, ok := port.(io.Reader)
	if !ok {
		fmt.Fprintln(os.Stderr, "boardcheck: port is not readable")
		os.Exit(1)
	}
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 64*1024)
		for sc.Scan() {
			line := sc.Text()
			if len(line) > 200 {
				line = line[:200] + "..."
			}
			fmt.Printf("< %s\n", line)
		}
	}()
	if frame != "" {
		if _, err := port.Write([]byte("\n" + frame + "\n")); err != nil {
			fmt.Fprintln(os.Stderr, "boardcheck: write:", err)
		} else {
			fmt.Printf("> sent %d bytes\n", len(frame)+2)
		}
	}
	time.Sleep(time.Duration(*secs) * time.Second)
	port.Close()
}

func lastLine(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}
