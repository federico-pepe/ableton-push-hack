// Package alsapcm enumerates ALSA sound cards and their playback devices, for
// hacks that let a user pick an audio output device (e.g. push-braids-host's
// on-screen I/O page). Built ahead of a second consumer, on request, so its
// shape is not yet proven against more than one real use case.
package alsapcm

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Card is one ALSA sound card as listed in /proc/asound/cards.
type Card struct {
	Index int    // card index, e.g. 0
	ID    string // short card ID, e.g. "PHVAudio"
	Name  string // long name, e.g. "Loopback - Loopback"
}

var cardLineRe = regexp.MustCompile(`^\s*(\d+)\s+\[([^]]*)\]:\s*(.*)$`)

// ParseCards parses the /proc/asound/cards text format. Split out as a
// testable seam, same pattern as core/alsaseq.ParseClients.
func ParseCards(r io.Reader) ([]Card, error) {
	var cards []Card
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		m := cardLineRe.FindStringSubmatch(line)
		if m == nil {
			continue // skip continuation lines (the indented second line per card)
		}
		idx, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		cards = append(cards, Card{
			Index: idx,
			ID:    strings.TrimSpace(m[2]),
			Name:  strings.TrimSpace(m[3]),
		})
	}
	return cards, scanner.Err()
}

// PlaybackDevice is one playback-capable PCM device on a card, as described
// by /proc/asound/<id>/pcm<N>p/info.
type PlaybackDevice struct {
	CardIndex      int
	CardID         string
	Device         int
	Name           string
	SubdeviceCount int
}

// HWDevice returns the ALSA device string for this device's subdevice 0,
// e.g. "hw:PHVAudio,1,0".
func (d PlaybackDevice) HWDevice() string {
	return "hw:" + d.CardID + "," + strconv.Itoa(d.Device) + ",0"
}

// ParsePCMInfo parses one /proc/asound/<id>/pcm<N>p/info file's "key: value"
// lines. Split out as a testable seam.
func ParsePCMInfo(r io.Reader) (device int, name string, subdeviceCount int, err error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])
		switch key {
		case "device":
			device, _ = strconv.Atoi(val)
		case "name":
			name = val
		case "subdevices_count":
			subdeviceCount, _ = strconv.Atoi(val)
		}
	}
	return device, name, subdeviceCount, scanner.Err()
}

// EnumCards reads /proc/asound/cards and returns every card found.
func EnumCards() ([]Card, error) {
	f, err := os.Open("/proc/asound/cards")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseCards(f)
}

// EnumPlaybackDevices returns every playback-capable PCM device on every
// ALSA card, by globbing /proc/asound/<id>/pcm*p/info for each card found in
// /proc/asound/cards.
func EnumPlaybackDevices() ([]PlaybackDevice, error) {
	cards, err := EnumCards()
	if err != nil {
		return nil, err
	}
	var devices []PlaybackDevice
	for _, c := range cards {
		matches, err := filepath.Glob("/proc/asound/" + c.ID + "/pcm*p/info")
		if err != nil {
			continue
		}
		for _, path := range matches {
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			dev, name, subCount, err := ParsePCMInfo(f)
			f.Close()
			if err != nil {
				continue
			}
			devices = append(devices, PlaybackDevice{
				CardIndex:      c.Index,
				CardID:         c.ID,
				Device:         dev,
				Name:           name,
				SubdeviceCount: subCount,
			})
		}
	}
	return devices, nil
}
