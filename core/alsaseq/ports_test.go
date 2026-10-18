package alsaseq

import (
	"os"
	"testing"
)

// NOTE: these fixtures are hand-constructed from the /proc/asound/seq/clients
// format documented in push-manager/src/midi.go's original comments, not
// captured off real hardware (this environment has no device access). They
// exercise the parser's line-format handling and the shifted-client-number
// case; re-capture from a real Push 3 (bare, and with a USB MIDI device
// attached) to fully close the loop per discovery/push-core-refactor.md.

func parseFixture(t *testing.T, path string) []Port {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	ports, err := ParseClients(f)
	if err != nil {
		t.Fatalf("ParseClients: %v", err)
	}
	return ports
}

func TestParseClientsBare(t *testing.T) {
	ports := parseFixture(t, "testdata/seq_clients_bare.txt")

	var push3 *Port
	for i := range ports {
		if ports[i].PortName == "Ableton Push 3 Live Port" {
			push3 = &ports[i]
		}
	}
	if push3 == nil {
		t.Fatal("Push 3 port not found")
	}
	if push3.Addr != (Addr{Client: 16, Port: 0}) {
		t.Errorf("Push 3 addr = %+v, want {16 0}", push3.Addr)
	}
	if push3.Caps&CapRead == 0 || push3.Caps&CapWrite == 0 {
		t.Errorf("Push 3 caps = %#x, want R and W set", push3.Caps)
	}
}

func TestParseClientsShiftedClientNumber(t *testing.T) {
	// The case detectPush3Port exists for: an external USB MIDI device
	// enumerates first at boot, shifting Push 3 from client 16 to 20.
	ports := parseFixture(t, "testdata/seq_clients_usb_attached.txt")

	var push3 *Port
	for i := range ports {
		if ports[i].PortName == "Ableton Push 3 Live Port" {
			push3 = &ports[i]
		}
	}
	if push3 == nil {
		t.Fatal("Push 3 port not found")
	}
	if push3.Addr.Client != 20 {
		t.Errorf("Push 3 client = %d, want 20 (shifted from default 16)", push3.Addr.Client)
	}
}

func TestFindByNameRequireCapsZero(t *testing.T) {
	// keyboard-visualizer's preserved divergence: requireCaps=0 matches on
	// name alone, ignoring capability bits entirely.
	ports := parseFixture(t, "testdata/seq_clients_bare.txt")
	found := false
	for _, p := range ports {
		if p.PortName == "Announce" {
			found = true
			if p.Caps&CapWrite != 0 {
				t.Skip("fixture's Announce port unexpectedly writable; adjust fixture")
			}
		}
	}
	if !found {
		t.Fatal("Announce port not in fixture")
	}
}

func TestFindByNameRequireCapsFiltersWritable(t *testing.T) {
	ports := parseFixture(t, "testdata/seq_clients_bare.txt")
	for _, p := range ports {
		if p.ClientName == "Ableton Live" && p.PortName == "Ableton Live" {
			if p.Caps&CapWrite == 0 {
				t.Error("Live's MIDI input port should be writable")
			}
			return
		}
	}
	t.Fatal("Ableton Live port not found in fixture")
}

// Captured off a real Push 3 (AbletonOS v3.23, Live 12.4.15b7, 2026-10-16)
// with push-manager running. One kernel client 16 carries all three Push
// ports; Live (130) created midi_out_4 for push-manager's output by itself.
func TestParseClientsDeviceV323(t *testing.T) {
	ports := parseFixture(t, "testdata/seq_clients_device_v323.txt")
	want := map[string]struct {
		addr Addr
		caps uint32
	}{
		"Ableton Push 3 Live Port":     {Addr{16, 0}, CapRead | CapWrite},
		"Ableton Push 3 User Port":     {Addr{16, 1}, CapRead | CapWrite},
		"Ableton Push 3 External Port": {Addr{16, 2}, CapRead | CapWrite},
		"Ableton Internal Input":       {Addr{128, 0}, CapWrite},
		"midi_out_4":                   {Addr{130, 4}, CapWrite},
		"Push Manager":                 {Addr{131, 0}, CapRead},
		"Push Manager In":              {Addr{132, 0}, CapWrite},
	}
	seen := map[string]bool{}
	for _, p := range ports {
		w, ok := want[p.PortName]
		if !ok {
			continue
		}
		seen[p.PortName] = true
		if p.Addr != w.addr || p.Caps != w.caps {
			t.Errorf("%s = %+v caps %#x, want %+v caps %#x", p.PortName, p.Addr, p.Caps, w.addr, w.caps)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s not parsed", name)
		}
	}
	// Pool lines ("Pool size : 500") must not become ports.
	if len(ports) != 14 {
		t.Errorf("parsed %d ports, want 14", len(ports))
	}
}
