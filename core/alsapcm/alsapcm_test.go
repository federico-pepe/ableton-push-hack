package alsapcm

import (
	"os"
	"testing"
)

// NOTE: fixtures are hand-constructed from the /proc/asound/cards and
// /proc/asound/<id>/pcm<N>p/info formats documented in
// hacks/push-audio-loopback/README.md, not captured off real hardware (this
// environment has no device access). Re-capture from a real Push 3 to fully
// close the loop, same caveat as core/alsaseq/ports_test.go's fixtures.

func openFixture(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	return f
}

func TestParseCardsBare(t *testing.T) {
	f := openFixture(t, "testdata/cards_bare.txt")
	defer f.Close()

	cards, err := ParseCards(f)
	if err != nil {
		t.Fatalf("ParseCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("want 1 card, got %d", len(cards))
	}
	if cards[0].ID != "A3" || cards[0].Index != 0 {
		t.Errorf("got %+v", cards[0])
	}
}

func TestParseCardsWithLoopback(t *testing.T) {
	f := openFixture(t, "testdata/cards_with_loopback.txt")
	defer f.Close()

	cards, err := ParseCards(f)
	if err != nil {
		t.Fatalf("ParseCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("want 2 cards, got %d: %+v", len(cards), cards)
	}
	if cards[1].ID != "PHVAudio" || cards[1].Index != 1 {
		t.Errorf("got %+v", cards[1])
	}
	if cards[1].Name != "Loopback - Loopback" {
		t.Errorf("name = %q", cards[1].Name)
	}
}

func TestParsePCMInfo(t *testing.T) {
	f := openFixture(t, "testdata/pcm_info_loopback.txt")
	defer f.Close()

	device, name, subCount, err := ParsePCMInfo(f)
	if err != nil {
		t.Fatalf("ParsePCMInfo: %v", err)
	}
	if device != 1 {
		t.Errorf("device = %d, want 1", device)
	}
	if name != "Loopback PCM" {
		t.Errorf("name = %q", name)
	}
	if subCount != 8 {
		t.Errorf("subdeviceCount = %d, want 8", subCount)
	}
}

func TestPlaybackDeviceHWDevice(t *testing.T) {
	d := PlaybackDevice{CardID: "PHVAudio", Device: 1}
	if got := d.HWDevice(); got != "hw:PHVAudio,1,0" {
		t.Errorf("HWDevice() = %q, want hw:PHVAudio,1,0", got)
	}
}
