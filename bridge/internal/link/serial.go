package link

import (
	"fmt"
	"io"
	"strings"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// serialMode starts DTR and RTS low; the library would otherwise assert both,
// which resets an ESP32 on open.
func serialMode(baud int) *serial.Mode {
	return &serial.Mode{BaudRate: baud, InitialStatusBits: &serial.ModemOutputBits{}}
}

func matchPort(ports []*enumerator.PortDetails, vid, pid string) (string, bool) {
	for _, p := range ports {
		if p.IsUSB && strings.EqualFold(p.VID, vid) && strings.EqualFold(p.PID, pid) {
			return p.Name, true
		}
	}
	return "", false
}

// SerialOpener finds the port by USB VID/PID on every call, so it survives resets.
func SerialOpener(vid, pid string, baud int) OpenFunc {
	return func() (io.WriteCloser, error) {
		ports, err := enumerator.GetDetailedPortsList()
		if err != nil {
			return nil, fmt.Errorf("list ports: %w", err)
		}
		name, ok := matchPort(ports, vid, pid)
		if !ok {
			return nil, fmt.Errorf("no USB device %s:%s", vid, pid)
		}
		p, err := serial.Open(name, serialMode(baud))
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", name, err)
		}
		return p, nil
	}
}
