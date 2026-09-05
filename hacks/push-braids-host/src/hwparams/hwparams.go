// Package hwparams parses Live's negotiated ALSA hw_params off /proc, so
// push-braids-host's PCM device can be reopened with the channels/rate/
// period/buffer Live actually asked for, instead of a value someone
// guessed and hardcoded. Kept free of cgo so it's plain-Go testable on any
// host (see hwparams_test.go's fixtures) — push-braids-host itself needs
// cgo (dlopen + libasound) and can only be built/tested via Docker.
package hwparams

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Params is one snapshot of a PCM device's negotiated parameters, read
// from /proc/asound/<card>/pcm<N>c/sub0/hw_params.
type Params struct {
	Channels int
	Rate     int
	Period   int
	Buffer   int
}

// Parse parses one hw_params file's content. ok is false (with a nil
// error) when the device is not currently open by anything — the file's
// entire body is the literal string "closed", which is the normal,
// expected state whenever Live hasn't opened its side yet, not an error.
func Parse(r io.Reader) (p Params, ok bool, err error) {
	scanner := bufio.NewScanner(r)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return Params{}, false, err
	}
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "closed" {
		return Params{}, false, nil
	}

	for _, line := range lines {
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])
		// "rate: 44100 (44100/1)" — only the leading integer matters.
		if sp := strings.IndexByte(val, ' '); sp >= 0 {
			val = val[:sp]
		}
		n, convErr := strconv.Atoi(val)
		if convErr != nil {
			continue
		}
		switch key {
		case "channels":
			p.Channels = n
		case "rate":
			p.Rate = n
		case "period_size":
			p.Period = n
		case "buffer_size":
			p.Buffer = n
		}
	}
	if p.Channels == 0 || p.Rate == 0 || p.Period == 0 {
		return Params{}, false, nil
	}
	return p, true, nil
}

// Path is the well-known /proc path for a Loopback card's capture side of
// device 0 — the side Live actually opens (see docs/push3-dsp-hosting.md
// and hacks/push-audio-loopback/README.md for the cross-wiring: writes to
// device 1's playback side arrive on device 0's capture side, which is the
// side whose hw_params reflect what Live negotiated).
func Path(cardID string) string {
	return filepath.Join("/proc/asound", cardID, "pcm0c", "sub0", "hw_params")
}

// Read reads and parses the live hw_params file for cardID. Returns
// ok=false, err=nil if the card doesn't exist yet (push-audio-loopback not
// loaded) or nothing has opened it yet — both are normal "not ready"
// states a caller should retry, not treat as fatal.
func Read(cardID string) (Params, bool, error) {
	f, err := os.Open(Path(cardID))
	if os.IsNotExist(err) {
		return Params{}, false, nil
	}
	if err != nil {
		return Params{}, false, err
	}
	defer f.Close()
	return Parse(f)
}
