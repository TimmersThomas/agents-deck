package link

import (
	"testing"

	"go.bug.st/serial/enumerator"
)

func TestSerialModeStartsWithModemLinesLow(t *testing.T) {
	m := serialMode(115200)
	if m.BaudRate != 115200 {
		t.Fatalf("BaudRate = %d, want 115200", m.BaudRate)
	}
	if m.InitialStatusBits == nil {
		t.Fatal("InitialStatusBits is nil; the library would default DTR and RTS to high")
	}
	if m.InitialStatusBits.DTR || m.InitialStatusBits.RTS {
		t.Fatalf("InitialStatusBits = %+v, want both low", *m.InitialStatusBits)
	}
}

func TestMatchPort(t *testing.T) {
	ports := []*enumerator.PortDetails{
		{Name: "/dev/cu.Bluetooth", IsUSB: false, VID: "303A", PID: "1001"},
		{Name: "/dev/cu.other", IsUSB: true, VID: "0403", PID: "6001"},
		{Name: "/dev/cu.usbmodem101", IsUSB: true, VID: "303A", PID: "1001"},
	}
	tests := []struct {
		name     string
		ports    []*enumerator.PortDetails
		vid, pid string
		wantName string
		wantOK   bool
	}{
		{"exact match", ports, "303A", "1001", "/dev/cu.usbmodem101", true},
		{"case insensitive", ports, "303a", "1001", "/dev/cu.usbmodem101", true},
		{"non-USB ignored", ports[:1], "303A", "1001", "", false},
		{"vid mismatch", ports, "dead", "1001", "", false},
		{"pid mismatch", ports, "303A", "beef", "", false},
		{"no ports", nil, "303A", "1001", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, ok := matchPort(tt.ports, tt.vid, tt.pid)
			if name != tt.wantName || ok != tt.wantOK {
				t.Fatalf("matchPort = (%q, %v), want (%q, %v)", name, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}
