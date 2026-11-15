package main

import (
	"encoding/binary"
	"testing"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

// Push3's lights as the device view reads them: 127 is its device view, 14
// another view, 0 a repaint (no view) that must change nothing.
func light(cc, val uint32) []byte {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data[4:], cc)
	binary.LittleEndian.PutUint32(data[8:], val)
	return data
}

func TestLightHandler(t *testing.T) {
	d := devView
	h := lightHandler{d}
	view := func() string { return d.status()["push_view"].(string) }
	h.Fixed(alsaseq.EvController, alsaseq.Addr{}, light(uint32(CCDeviceView), 127))
	if view() != "device" {
		t.Fatalf("127: %s", view())
	}
	h.Fixed(alsaseq.EvController, alsaseq.Addr{}, light(uint32(CCDeviceView), 0)) // a repaint
	if view() != "device" {
		t.Fatalf("a repaint changed the view: %s", view())
	}
	h.Fixed(alsaseq.EvController, alsaseq.Addr{}, light(uint32(CCMixerView), 127)) // only 110 counts
	h.Fixed(alsaseq.EvController, alsaseq.Addr{}, light(uint32(CCDeviceView), 14))
	if view() != "other" {
		t.Fatalf("14: %s", view())
	}
	h.Fixed(alsaseq.EvNoteOn, alsaseq.Addr{}, light(uint32(CCDeviceView), 127))
	if view() != "other" {
		t.Fatal("a note changed the view")
	}
}

// A device is the app's by its own name, or by a plugin inside the rack Live
// selected (a rack named by an older preset, or by the user).
func TestTargetFor(t *testing.T) {
	apps := []deviceViewApp{{ID: "other", Bundle: "Other"}, {ID: "pung", Bundle: "Pung"}}
	classes := func(bundle string) map[string]bool {
		if bundle == "Pung" {
			return map[string]bool{"Braids": true, "OB-Xd": true}
		}
		return map[string]bool{"Thing": true}
	}
	for _, c := range []struct {
		names []string
		want  string
	}{
		{[]string{"Braids"}, "pung"},
		{[]string{"Thing"}, "other"},
		{[]string{"My Lead"}, ""},                // the rack's own name: no class
		{[]string{"Wavetable", "OB-Xd"}, "pung"}, // a plugin inside it
		{nil, ""},
		{[]string{""}, ""},
	} {
		if got := targetFor(apps, classes, c.names); got != c.want {
			t.Errorf("%v: %q, want %q", c.names, got, c.want)
		}
	}
}
