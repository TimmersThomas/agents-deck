// Package link writes frames to the display over USB serial.
package link

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

type OpenFunc func() (io.WriteCloser, error)

type Sender struct {
	open  OpenFunc
	retry time.Duration
}

func NewSender(open OpenFunc, retry time.Duration) *Sender {
	return &Sender{open: open, retry: retry}
}

// Run writes the latest frame whenever one arrives and after every reconnect.
func (s *Sender) Run(ctx context.Context, frames <-chan []byte) {
	var latest []byte
	var port io.WriteCloser
	defer func() {
		if port != nil {
			port.Close()
		}
	}()
	for {
		if port == nil {
			p, err := s.open()
			if err != nil {
				log.Printf("link: open: %v", err)
				if !sleep(ctx, s.retry) {
					return
				}
				latest = drainLatest(frames, latest)
				continue
			}
			port = p
			if latest != nil && !s.write(&port, latest) {
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case f, ok := <-frames:
			if !ok {
				return
			}
			latest = f
			s.write(&port, latest)
		}
	}
}

func (s *Sender) write(port *io.WriteCloser, frame []byte) bool {
	if _, err := (*port).Write(frame); err != nil {
		log.Printf("link: write: %v", err)
		(*port).Close()
		*port = nil
		return false
	}
	return true
}

func drainLatest(frames <-chan []byte, latest []byte) []byte {
	for {
		select {
		case f := <-frames:
			latest = f
		default:
			return latest
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// openQuiet opens the port with DTR and RTS low so the ESP32 is not reset on connect.
func openQuiet(name string, baud int) (io.WriteCloser, error) {
	p, err := serial.Open(name, &serial.Mode{BaudRate: baud})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	if err := p.SetDTR(false); err != nil {
		p.Close()
		return nil, fmt.Errorf("clear DTR: %w", err)
	}
	if err := p.SetRTS(false); err != nil {
		p.Close()
		return nil, fmt.Errorf("clear RTS: %w", err)
	}
	return p, nil
}

// SerialOpener finds the port by USB VID/PID on every call, so it survives resets.
func SerialOpener(vid, pid string, baud int) OpenFunc {
	return func() (io.WriteCloser, error) {
		ports, err := enumerator.GetDetailedPortsList()
		if err != nil {
			return nil, fmt.Errorf("list ports: %w", err)
		}
		for _, p := range ports {
			if p.IsUSB && strings.EqualFold(p.VID, vid) && strings.EqualFold(p.PID, pid) {
				return openQuiet(p.Name, baud)
			}
		}
		return nil, fmt.Errorf("no USB device %s:%s", vid, pid)
	}
}
