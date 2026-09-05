package main

// iopage.go — the on-screen MIDI-input / audio-output picker: page 3 of
// the existing Shift+Device param UI (see params.go/display.go). One
// scrollable list combining both choices under a header each, instead of
// two side-by-side lists with a focus toggle — simpler to build and to
// use with only D-Pad Up/Down + Select available, and this list is short
// enough in practice that scrolling between the two sections costs
// nothing.

import (
	"fmt"
	"image"
	"log"
	"sync"

	"github.com/federico-pepe/ableton-push-hack/core/alsapcm"
	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
)

type ioRowKind int

const (
	ioRowHeader ioRowKind = iota
	ioRowMidi
	ioRowDevice
	ioRowChannel
)

// loopbackChannels is push-audio-loopback's fixed channel count (see that
// hack's README) — used only to offer a full range of channel pairs here,
// independent of whatever a live PCM session actually negotiated.
const loopbackChannels = 32

type ioRow struct {
	kind          ioRowKind
	label         string
	midiPort      alsaseq.Port
	device        alsapcm.PlaybackDevice
	channelOffset int
}

// ioState is the I/O picker's own state: the combined row list (rebuilt
// from live ALSA enumeration on every navigation step and every redraw —
// this page is opened rarely and ALSA's /proc reads are cheap, so there is
// no need to cache), and the current cursor/scroll position.
type ioState struct {
	mu      sync.Mutex
	hackDir string
	rt      *sharedConfig
	cursor  int
	scroll  int
	rows    []ioRow
}

func newIOState(hackDir string, rt *sharedConfig) *ioState {
	return &ioState{hackDir: hackDir, rt: rt}
}

// buildRows re-enumerates MIDI ports and playback devices and marks
// whichever one sharedConfig currently points at.
//
// The MIDI list is filtered to Push3's own client (alsaseq.Push3ClientDefault)
// only — early testing picked an unfiltered readable port that looked
// plausible ("Ableton Push 3 User Port") but silently carries no pad/button
// events at all, breaking the Shift+Device chord with no visible error.
// Push3 exposes only three ports, already distinctly named ("Live Port",
// "User Port", "External Port" — see /proc/asound/seq/clients), so
// filtering to just this client removes the footgun without losing any
// real choice.
func (io *ioState) buildRows() []ioRow {
	curClient, curPort := io.rt.getMIDI()
	curDevice := io.rt.getPCM()
	curOffset := io.rt.getChannelOffset()

	var rows []ioRow
	rows = append(rows, ioRow{kind: ioRowHeader, label: "MIDI INPUT"})
	ports, _ := alsaseq.EnumPorts(alsaseq.CapRead)
	for _, p := range ports {
		if p.Addr.Client != alsaseq.Push3ClientDefault {
			continue
		}
		mark := "  "
		if p.Addr.Client == curClient && p.Addr.Port == curPort {
			mark = "> "
		}
		rows = append(rows, ioRow{
			kind:     ioRowMidi,
			label:    fmt.Sprintf("%s%s", mark, p.PortName),
			midiPort: p,
		})
	}

	rows = append(rows, ioRow{kind: ioRowHeader, label: "AUDIO OUTPUT"})
	devices, _ := alsapcm.EnumPlaybackDevices()
	for _, d := range devices {
		mark := "  "
		if d.HWDevice() == curDevice {
			mark = "> "
		}
		rows = append(rows, ioRow{
			kind:   ioRowDevice,
			label:  fmt.Sprintf("%s%s (%s)", mark, d.Name, d.HWDevice()),
			device: d,
		})
	}

	rows = append(rows, ioRow{kind: ioRowHeader, label: "AUDIO CHANNEL"})
	for ch := 0; ch+1 < loopbackChannels; ch += 2 {
		mark := "  "
		if ch == curOffset {
			mark = "> "
		}
		rows = append(rows, ioRow{
			kind:          ioRowChannel,
			label:         fmt.Sprintf("%sCh %d-%d", mark, ch+1, ch+2),
			channelOffset: ch,
		})
	}
	return rows
}

// refreshLocked rebuilds the row list and keeps the cursor on a
// selectable row (never a header). Caller must hold io.mu.
func (io *ioState) refreshLocked() {
	io.rows = io.buildRows()
	n := len(io.rows)
	if io.cursor >= n {
		io.cursor = n - 1
	}
	if io.cursor < 0 {
		io.cursor = 0
	}
	if n > 0 && io.rows[io.cursor].kind == ioRowHeader {
		io.cursor = (io.cursor + 1) % n
	}
}

// moveCursor steps the cursor by one row in delta's direction (its
// magnitude is ignored — Push's D-Pad only ever sends a press, not an
// accelerating value like the encoders), skipping over header rows.
func (io *ioState) moveCursor(delta int) {
	io.mu.Lock()
	defer io.mu.Unlock()
	io.refreshLocked()
	n := len(io.rows)
	if n == 0 {
		return
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	for i := 0; i < n; i++ {
		io.cursor = (io.cursor + dir + n) % n
		if io.rows[io.cursor].kind != ioRowHeader {
			break
		}
	}
}

// commit applies the currently highlighted row to sharedConfig and
// persists it to braids-config.json. Applying it live (rather than only
// on the next process start) is what lets watchHWParams/watchMIDI pick it
// up on their next poll tick with no restart.
func (io *ioState) commit() {
	io.mu.Lock()
	defer io.mu.Unlock()
	io.refreshLocked()
	if io.cursor < 0 || io.cursor >= len(io.rows) {
		return
	}
	row := io.rows[io.cursor]
	switch row.kind {
	case ioRowMidi:
		io.rt.setMIDI(row.midiPort.Addr.Client, row.midiPort.Addr.Port)
	case ioRowDevice:
		io.rt.setPCM(row.device.HWDevice())
	case ioRowChannel:
		io.rt.setChannelOffset(row.channelOffset)
	default:
		return
	}
	if err := saveConfig(io.hackDir, io.rt.snapshot()); err != nil {
		log.Printf("io picker: saving %s: %v", configFileName, err)
	}
}

const ioRowH = 13

// render draws the combined list. screenW/screenH come from display.go.
func (io *ioState) render() *image.NRGBA {
	io.mu.Lock()
	defer io.mu.Unlock()
	io.refreshLocked()

	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))
	gfx.FillRect(img, 0, 0, screenW, screenH, widgets.Default.Black)

	rows := make([]widgets.ListRow, len(io.rows))
	for i, r := range io.rows {
		if r.kind == ioRowHeader {
			rows[i] = widgets.ListRow{Text: "-- " + r.label + " --", TextCol: widgets.Default.Gray}
			continue
		}
		rows[i] = widgets.ListRow{Text: r.label, TextCol: widgets.Default.White}
	}

	visRows := (screenH - ioRowH) / ioRowH
	if io.cursor < io.scroll {
		io.scroll = io.cursor
	}
	if io.cursor >= io.scroll+visRows {
		io.scroll = io.cursor - visRows + 1
	}

	v := widgets.ListView{
		Rows:       rows,
		Cursor:     io.cursor,
		Scroll:     io.scroll,
		Breadcrumb: "BRAIDS - I/O - UP/DOWN select, SELECT to confirm",
		EmptyText:  "no MIDI ports or audio devices found",
	}
	widgets.RenderList(img, widgets.Default, v, 0, screenW, ioRowH, screenH)
	return img
}
