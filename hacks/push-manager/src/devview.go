// devview.go -- an app's view in place of Push's own device view.
//
// A hack with Live devices (core/livedevice) can show its own view of a
// device instead of Push's device view: it declares "surface.device_view"
// and "live_devices" in hack.json, and the user switches it per app (Home's
// DEVICE button, or the Display page). Then, while
//
//   - Push shows its device view, and
//   - the device selected in Live is one of the hack's,
//
// the app owns the surface as if it were opened from Home (Surface.Follow),
// and gives it back when either stops. Device View pressed in the app shows
// Push's own device view for that device; Mixer, Clip and Session go on to
// Push (Surface.ViewButton).
//
// Two things are watched, both read-only:
//
//   - Push's view: Push3 lights the button of the view it shows (127, the
//     others 14, and 0 while it repaints; measured 2026-11-14, Live
//     13.0b25) and sends the lights to
//     every subscriber of its output port, so a subscription reads them
//     without touching push_hook. Presses of the view buttons count too:
//     Push3 sends no lights at push-manager's start.
//   - Live's selected device: the PushHack Remote Script's "device" topic
//     (its name), matched against the class names in each app's bundle.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
	"github.com/federico-pepe/ableton-push-hack/core/livedevice"
)

// pushOutputPortName is Push3's own output: the LEDs it sets go out here.
const pushOutputPortName = "Ableton Internal Output"

type devViewState struct {
	mu sync.Mutex
	// Push's view, from its lights: pushDevice is the Device View button lit.
	pushDevice, pushKnown bool
	// Live's selected device
	name, key string
	track     json.RawMessage // its track, and its index there (-1 none)
	device    int
	liveUp    bool
	target    string // the app whose device is selected ("" none)
	// the plugins inside the selected device when it is a rack, for the
	// selection innerKey (asked of Live once per selection)
	innerKey string
	inner    []string

	apps     []deviceViewApp
	appsAt   time.Time
	names    map[string]map[string]bool // bundle -> its class names
	namesMod map[string]time.Time
}

var devView = &devViewState{names: map[string]map[string]bool{}, namesMod: map[string]time.Time{}}

// startDeviceView runs the two watchers. After waitForBootSettle: it opens
// /dev/snd/seq.
func startDeviceView() {
	go devView.watchLights()
	go devView.watchLive()
}

// ---- Push's view ------------------------------------------------------------------

type lightHandler struct{ d *devViewState }

func (h lightHandler) Fixed(evType uint8, src alsaseq.Addr, data []byte) {
	if evType != alsaseq.EvController {
		return
	}
	cc := binary.LittleEndian.Uint32(data[4:])
	val := binary.LittleEndian.Uint32(data[8:])
	// 127: Push shows its device view; 14: another view. 0 is no view at
	// all: Push3 clears every light, then sets them again, whenever it gets
	// the buttons back (measured: 110 = 0, then 127, when an app gives the
	// surface back). Read as "another view", it lifted the hold and the app
	// opened again at once.
	if cc == CCDeviceView && val != 0 {
		h.d.setPushDevice(val >= 64)
	}
}

func (h lightHandler) VarLen(uint8, alsaseq.Addr, []byte) {}

// watchLights subscribes to Push3's output and follows it when Push3
// restarts (a new client number).
func (d *devViewState) watchLights() {
	for {
		p, ok := alsaseq.FindByName(pushOutputPortName, alsaseq.CapRead)
		if !ok {
			time.Sleep(5 * time.Second)
			continue
		}
		c, err := alsaseq.Open()
		if err != nil {
			log.Printf("devview: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}
		c.SetName("Push Manager Device View") //nolint:errcheck
		if _, err := c.CreatePort("Device View", alsaseq.CapWrite|alsaseq.CapSubsWrite, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
			c.Close()
			time.Sleep(10 * time.Second)
			continue
		}
		if err := c.Subscribe(p.Addr); err != nil {
			log.Printf("devview: subscribe to %s %d:%d: %v", pushOutputPortName, p.Addr.Client, p.Addr.Port, err)
			c.Close()
			time.Sleep(5 * time.Second)
			continue
		}
		log.Printf("devview: reading Push's lights from %d:%d", p.Addr.Client, p.Addr.Port)
		done := make(chan struct{})
		go func() {
			err := c.ReadLoop(lightHandler{d})
			log.Printf("devview: reading Push's lights stopped: %v", err)
			close(done)
		}()
		misses := 0
		for watching := true; watching; {
			select {
			case <-done:
				watching = false
			case <-time.After(5 * time.Second):
				q, ok := alsaseq.FindByName(pushOutputPortName, alsaseq.CapRead)
				if !ok {
					// Twice in a row before it counts: the port list can
					// fail to read while another client goes away (seen
					// when Pung restarted), and starting over forgets
					// Push's view.
					if misses++; misses >= 2 {
						watching = false
					}
					break
				}
				misses = 0
				if q.Addr != p.Addr {
					watching = false // Push3 restarted elsewhere: follow it
					break
				}
				// A restarted Push3 gets the same client number back (the
				// kernel reuses it), and its new port has no subscriber. The
				// kernel's own list says whether we are still on it (an
				// EBUSY from the ioctl did not: the watcher stayed deaf after
				// a Live restart, 2026-11-15).
				if alsaseq.Subscribed(p.Addr, c.Addr()) {
					break
				}
				if err := c.Subscribe(p.Addr); err == nil || errors.Is(err, syscall.EBUSY) {
					log.Printf("devview: subscribed again to %d:%d (Push3 restarted)", p.Addr.Client, p.Addr.Port)
					d.mu.Lock()
					d.pushKnown = false
					d.mu.Unlock()
				} else {
					log.Printf("devview: subscribe to %d:%d: %v (starting over)", p.Addr.Client, p.Addr.Port, err)
					watching = false
				}
			}
		}
		c.Close()
		<-done
		d.mu.Lock()
		d.pushKnown = false // a new Push3 has not said yet
		d.mu.Unlock()
	}
}

// pressed: a view button pressed while Live had the buttons. The app opens
// from Push's light, not from the press: the press reaches push-manager and
// Push3 at once, and an app taking the buttons before Push3 has read it
// leaves Push in its old view (seen on device: after Help, the mixer). So
// the press only says what the light cannot:
//
//   - Device View in Push's own device view of a held device brings the
//     app's view back (the button toggles between the two). The light does
//     not change then, so the state is looked at again once Push3 has read
//     the press.
//   - Before Push3 has sent any light (push-manager just started), the
//     press is the only news of its view.
func (d *devViewState) pressed(cc uint8) {
	d.mu.Lock()
	known := d.pushKnown
	d.mu.Unlock()
	switch cc {
	case CCDeviceView:
		queueSurface(surface.ClearHold)
		time.AfterFunc(pushReadsPress, func() {
			if !known {
				d.setPushDevice(true)
			}
			d.evaluate()
		})
	case CCMixerView, CCClipView, CCSessionView:
		if !known {
			d.setPushDevice(false)
		}
	}
}

// pushReadsPress: time for Push3 to read a press push-manager also got.
const pushReadsPress = 300 * time.Millisecond

func (d *devViewState) setPushDevice(on bool) {
	d.mu.Lock()
	changed := !d.pushKnown || d.pushDevice != on
	d.pushDevice, d.pushKnown = on, true
	d.mu.Unlock()
	if changed {
		d.evaluate()
	}
}

// ---- Live's selected device ---------------------------------------------------------

// watchLive keeps one subscription to the PushHack Remote Script's "device"
// topic (a topic, not a polling loop: every call runs on Live's engine
// thread), and connects again whenever Live restarts.
func (d *devViewState) watchLive() {
	for {
		conn, err := net.DialTimeout("tcp", liveBridgeAddr, 3*time.Second)
		if err != nil {
			d.setLive(false)
			time.Sleep(3 * time.Second)
			continue
		}
		fmt.Fprintln(conn, `{"id":1,"cmd":"subscribe","topics":["device"]}`)
		r := bufio.NewReaderSize(conn, 1<<20)
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				break
			}
			var ev struct {
				T     string `json:"t"`
				Topic string `json:"topic"`
				Data  struct {
					Track  json.RawMessage `json:"track"`
					Device int             `json:"device"`
					Name   string          `json:"name"`
				} `json:"data"`
			}
			if json.Unmarshal(line, &ev) != nil || ev.T != "event" || ev.Topic != "device" {
				continue
			}
			d.setSelection(ev.Data.Name, ev.Data.Track, ev.Data.Device,
				fmt.Sprintf("%s/%d/%s", ev.Data.Track, ev.Data.Device, ev.Data.Name))
		}
		conn.Close()
		d.setLive(false)
		time.Sleep(3 * time.Second)
	}
}

func (d *devViewState) setLive(up bool) {
	d.mu.Lock()
	changed := d.liveUp != up
	d.liveUp = up
	if !up {
		d.name, d.key, d.device, d.innerKey, d.inner = "", "", -1, "", nil
	}
	d.mu.Unlock()
	if changed && !up {
		d.evaluate()
	}
}

func (d *devViewState) setSelection(name string, track json.RawMessage, device int, key string) {
	d.mu.Lock()
	changed := !d.liveUp || d.key != key
	d.liveUp, d.name, d.key = true, name, key
	d.track, d.device = append(json.RawMessage(nil), track...), device
	d.mu.Unlock()
	if changed { // the topic also fires for every parameter change
		d.evaluate()
	}
}

// ---- deciding -------------------------------------------------------------------------

// evaluate hands the state to the surface. Cheap enough per change: the app
// list is read from disk at most every 2 s, a class file only when it
// changed.
func (d *devViewState) evaluate() {
	d.mu.Lock()
	if time.Since(d.appsAt) > 2*time.Second {
		d.apps, d.appsAt = deviceViewApps(), time.Now()
	}
	target := ""
	if d.name != "" {
		target = targetFor(d.apps, d.classNames, []string{d.name})
	}
	// Not one of the classes by its name: Live selects the rack around a
	// plugin (a track's top level), and a rack's name is a preset's or the
	// user's ("My Lead"). Then the
	// plugins inside it decide, asked of Live once per selection.
	if target == "" && d.name != "" && d.device >= 0 {
		key, track, dev := d.key, d.track, d.device
		if d.innerKey != key {
			d.mu.Unlock()
			inner := rackPlugins(track, dev) // Live's engine thread: not under d.mu
			d.mu.Lock()
			if d.key != key {
				d.mu.Unlock()
				return // the selection moved on meanwhile: its own evaluate decides
			}
			d.innerKey, d.inner = key, inner
		}
		target = targetFor(d.apps, d.classNames, d.inner)
	}
	d.target = target
	key, device := d.key, d.pushKnown && d.pushDevice
	d.mu.Unlock()
	queueSurface(func() { surface.Follow(target, key, device) })
}

// targetFor: the first app with a device view that has a class of one of
// these names, or "".
func targetFor(apps []deviceViewApp, classNames func(bundle string) map[string]bool, names []string) string {
	for _, a := range apps {
		cn := classNames(a.Bundle)
		for _, n := range names {
			if n != "" && cn[n] {
				return a.ID
			}
		}
	}
	return ""
}

// rackPlugins: the plugins in the first chain of the device at the top of a
// track, when it is a rack (nil when it is not, or Live does not answer). A
// variable for the tests.
var rackPlugins = func(track json.RawMessage, device int) []string {
	data, err := bridgeCall("device", "list", map[string]any{"track": track, "path": []int{device}, "chain": 0}, bridgeShortTimeout)
	if err != nil {
		return nil
	}
	var r struct {
		Devices []struct {
			Name  string `json:"name"`
			Class string `json:"class"`
		} `json:"devices"`
	}
	if json.Unmarshal(data, &r) != nil {
		return nil
	}
	var out []string
	for _, x := range r.Devices {
		if x.Class == "PluginDevice" {
			out = append(out, x.Name)
		}
	}
	return out
}

// classNames: the device names in a bundle's class file. d.mu held.
func (d *devViewState) classNames(bundle string) map[string]bool {
	path := livedevice.ClassesPath(livedevice.VST3Dir, bundle)
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if names, ok := d.names[bundle]; ok && d.namesMod[bundle].Equal(st.ModTime()) {
		return names
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc livedevice.ClassFile
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	names := map[string]bool{}
	for _, c := range doc.Classes {
		names[c.Name] = true
	}
	d.names[bundle], d.namesMod[bundle] = names, st.ModTime()
	return names
}

// status is what /api/surface reports.
func (d *devViewState) status() map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	push := "unknown"
	if d.pushKnown {
		push = map[bool]string{true: "device", false: "other"}[d.pushDevice]
	}
	return map[string]any{"push_view": push, "live": d.liveUp, "selected": d.name, "app": d.target}
}
