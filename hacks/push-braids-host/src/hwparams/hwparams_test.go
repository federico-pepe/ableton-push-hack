package hwparams

import (
	"os"
	"testing"
)

// NOTE: hw_params_closed.txt was captured live off a real Push 3
// (/proc/asound/PHVAudio/pcm0c/sub0/hw_params before Live opens its side —
// literally the string "closed"). hw_params_open.txt is hand-constructed
// from the standard ALSA /proc hw_params text format (this environment
// has no way to force Live to open the device to capture a real "open"
// snapshot) — same caveat as core/alsaseq/ports_test.go's fixtures.

func openFixture(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	return f
}

func TestParseClosed(t *testing.T) {
	f := openFixture(t, "testdata/hw_params_closed.txt")
	defer f.Close()

	_, ok, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ok {
		t.Error("expected ok=false for a closed device")
	}
}

func TestParseOpen(t *testing.T) {
	f := openFixture(t, "testdata/hw_params_open.txt")
	defer f.Close()

	p, ok, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for an open device")
	}
	want := Params{Channels: 32, Rate: 44100, Period: 128, Buffer: 384}
	if p != want {
		t.Errorf("got %+v, want %+v", p, want)
	}
}

func TestReadMissingCard(t *testing.T) {
	p, ok, err := Read("NoSuchCard12345")
	if err != nil {
		t.Fatalf("expected no error for a missing card, got %v", err)
	}
	if ok {
		t.Errorf("expected ok=false, got %+v", p)
	}
}
