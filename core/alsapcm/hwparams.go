// hw_params that Live negotiated on the loopback card. A hack opens its own
// side with the same channels, rate and period. Plain Go, no cgo.

package alsapcm

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// HWParams: one snapshot of an open PCM stream.
type HWParams struct {
	Channels, Rate, Period, Buffer int
}

// ParseHWParams reads a hw_params file. ok is false (and err nil) when the stream is
// closed, which is the normal state before Live opens its side.
func ParseHWParams(r io.Reader) (p HWParams, ok bool, err error) {
	sc := bufio.NewScanner(r)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err = sc.Err(); err != nil {
		return HWParams{}, false, err
	}
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "closed" {
		return HWParams{}, false, nil
	}
	for _, ln := range lines {
		k, v, found := strings.Cut(ln, ":")
		if !found {
			continue
		}
		v = strings.TrimSpace(v)
		if i := strings.IndexByte(v, ' '); i >= 0 {
			v = v[:i]
		}
		n, e := strconv.Atoi(v)
		if e != nil {
			continue
		}
		switch strings.TrimSpace(k) {
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
		return HWParams{}, false, nil
	}
	return p, true, nil
}

// HWParamsPath is the proc file of the capture side of device 0: the side
// Live opens.
func HWParamsPath(card string) string {
	return filepath.Join("/proc/asound", card, "pcm0c", "sub0", "hw_params")
}

// ReadHWParams reads the live params of a card. A missing card or a closed stream is
// ok=false with no error.
func ReadHWParams(card string) (HWParams, bool, error) {
	f, err := os.Open(HWParamsPath(card))
	if os.IsNotExist(err) {
		return HWParams{}, false, nil
	}
	if err != nil {
		return HWParams{}, false, err
	}
	defer f.Close()
	return ParseHWParams(f)
}
