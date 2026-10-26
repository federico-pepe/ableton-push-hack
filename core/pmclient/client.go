// Package pmclient is an HTTP client for push-manager's display and Live
// endpoints — the "hook", as code: it's how a hack draws on Push 3's screen
// or reads Live's tempo without ever touching ALSA, shm, or libusb directly.
// See CLAUDE.md's "Display-owning hacks" section for why this indirection
// exists — push-manager is the sole shared-memory writer.
package pmclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/display"
)

// ErrNoFrameRoute is PushFrame's error when push-manager has no
// /api/display/frame (a release before raw frames). Push falls back on it.
var ErrNoFrameRoute = errors.New("push-manager has no /api/display/frame (update Push Hack)")

// Client talks to a push-manager instance over HTTP.
type Client struct {
	Base string
	HTTP *http.Client

	// App is the hack's id when it is an app of Push Hack Home (its
	// hack.json declares "surface"). Every call that writes to the screen
	// or the intercept then says so (the X-Push-Hack-App header), and
	// push-manager lets it through only while this app owns the surface.
	App string

	frame   []byte // PushFrame's reusable buffer
	noFrame bool   // push-manager has no raw-frame route: Push uses PushImage
}

// AppHeader is the header that names the app a call comes from.
const AppHeader = "X-Push-Hack-App"

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.App != "" {
		req.Header.Set(AppHeader, c.App)
	}
	return c.HTTP.Do(req)
}

func (c *Client) post(url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return c.do(req)
}

// New returns a Client with a 3s timeout HTTP client, matching every
// existing caller's timeout.
func New(base string) *Client {
	return &Client{Base: base, HTTP: &http.Client{Timeout: 3 * time.Second}}
}

// SetMode sets push-manager's display mode (0=passthrough, 1=bar, 2=takeover).
func (c *Client) SetMode(mode int) error {
	req, err := http.NewRequest(http.MethodPost, c.Base+"/api/display/mode",
		bytes.NewBufferString(fmt.Sprintf(`{"mode":%d}`, mode)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("display/mode: unexpected status %s", resp.Status)
	}
	return nil
}

// PushImage POSTs img as a PNG to push-manager's display image endpoint.
func (c *Client) PushImage(img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("encode png: %w", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", "frame.png")
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write image: %w", err)
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.Base+"/api/display/image", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("display/image: unexpected status %s", resp.Status)
	}
	return nil
}

// Status is push-manager's GET /api/display/status response.
type Status struct {
	Connected bool `json:"connected"`
	// Mode is the display mode push-display is in: 0 Live's own screen,
	// 2 takeover. A hack that holds takeover can see a Push3 restart (the
	// hook starts in 0) and take the display again.
	Mode int `json:"mode"`
}

// DisplayStatus reports whether push-display's shared-memory framebuffer is
// attached (i.e. whether display calls will actually reach the screen).
func (c *Client) DisplayStatus() (Status, error) {
	var status Status
	resp, err := c.HTTP.Get(c.Base + "/api/display/status")
	if err != nil {
		return status, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return status, err
	}
	return status, nil
}

// SetMidiFilter enables or disables push-manager's MIDI intercept
// (POST /api/midi/filter) — while enabled, push-display's hook drops pad/
// button MIDI before it reaches Live, the same mechanism push-manager's own
// Shadow UI uses so its own chord-driven UI doesn't also play notes into
// Live. A hack that takes over the display to read pad MIDI as its own
// control surface (not as notes for Live) should enable this alongside its
// own takeover, and disable it when it lets go.
func (c *Client) SetMidiFilter(enabled bool) error {
	req, err := http.NewRequest(http.MethodPost, c.Base+"/api/midi/filter",
		bytes.NewBufferString(fmt.Sprintf(`{"enabled":%t}`, enabled)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("midi/filter: unexpected status %s", resp.Status)
	}
	return nil
}

// SetMidiFilterPass is SetMidiFilter with buttons that still reach Live
// while intercept is on: passCC lists CC numbers (0-87) -- typically Play
// (85, which also stops) and Record (86) -- so a hack that owns the
// surface leaves the transport with Live. Turning the filter off clears the
// list. Needs push-display and push-manager from the same release; an older
// pair ignores the list and blocks every button, as before.
func (c *Client) SetMidiFilterPass(enabled bool, passCC []int) error {
	body, err := json.Marshal(map[string]any{"enabled": enabled, "pass_cc": passCC})
	if err != nil {
		return err
	}
	resp, err := c.post(c.Base+"/api/midi/filter", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("midi/filter: unexpected status %s", resp.Status)
	}
	return nil
}

// FilterStatus is push-manager's MIDI intercept state (GET
// /api/midi/filter/status).
type FilterStatus struct {
	Enabled bool
	PassCC  []int
	// SurfaceOwning is true when push-manager knows pass_cc -- the release
	// with pass-through buttons, LED hold-back and /api/display/frame. An
	// older push-manager answers without the field: a hack that owns the
	// surface would then lose Play to the filter and get a 404 per frame,
	// so it should say "update Push Hack" instead of starting.
	SurfaceOwning bool
}

// MidiFilterStatus reads the intercept state, and from it whether this
// push-manager supports hacks that own the surface.
func (c *Client) MidiFilterStatus() (FilterStatus, error) {
	var raw struct {
		Enabled bool   `json:"enabled"`
		PassCC  *[]int `json:"pass_cc"`
	}
	resp, err := c.HTTP.Get(c.Base + "/api/midi/filter/status")
	if err != nil {
		return FilterStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return FilterStatus{}, fmt.Errorf("midi/filter/status: unexpected status %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return FilterStatus{}, err
	}
	st := FilterStatus{Enabled: raw.Enabled, SurfaceOwning: raw.PassCC != nil}
	if raw.PassCC != nil {
		st.PassCC = *raw.PassCC
	}
	return st, nil
}

// Scale is the Set's scale, the one Push's Scale button edits.
type Scale struct {
	Supported bool   // false on a Live without the scale API
	Root      int    // 0-11, 0 = C
	Name      string // Live's name: "Minor", "Dorian", ...
	Intervals []int  // semitones from Root
	Mode      bool   // Scale Mode (Push's "In Key")
}

// LiveScale reads the Set's scale (GET /api/live/scale). It needs the
// Browser Bridge Remote Script (0.3 or later) active in Live; without it
// the error says why.
func (c *Client) LiveScale() (Scale, error) {
	var body struct {
		OK        bool   `json:"ok"`
		Error     string `json:"error"`
		Supported bool   `json:"supported"`
		Root      int    `json:"root"`
		Name      string `json:"name"`
		Intervals []int  `json:"intervals"`
		Mode      bool   `json:"mode"`
	}
	resp, err := c.HTTP.Get(c.Base + "/api/live/scale")
	if err != nil {
		return Scale{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Scale{}, fmt.Errorf("live/scale: unexpected status %s (update Push Hack)", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Scale{}, err
	}
	if !body.OK {
		return Scale{}, fmt.Errorf("live/scale: %s", body.Error)
	}
	return Scale{Supported: body.Supported, Root: body.Root, Name: body.Name, Intervals: body.Intervals, Mode: body.Mode}, nil
}

// Listen is the listen track's state, as Browser Bridge reports it.
type Listen struct {
	OK         bool     `json:"ok"`
	Error      string   `json:"error"`
	On         bool     `json:"on"`
	Track      bool     `json:"track"`      // the "Stacks Listen" track exists
	Input      string   `json:"input"`      // what its input is, e.g. "1-Piano"
	Channel    string   `json:"channel"`    // "Post FX"
	Output     string   `json:"output"`     // the hack's port
	Monitoring int      `json:"monitoring"` // 0 In, 1 Auto, 2 Off
	Source     string   `json:"source"`     // the track it listens to
	Outputs    []string `json:"outputs"`    // on failure: the MIDI outputs Live offers
}

// LiveListen turns the listen track on or off: a silent MIDI track in the
// Set whose input follows the selected track and whose output is port, so
// a hack receives the notes played on Push's pads (POST /api/live/listen).
// Needs Browser Bridge 0.4 or later active in Live. A reply with OK false
// carries the reason in Error.
func (c *Client) LiveListen(on bool, port string) (Listen, error) {
	body, err := json.Marshal(map[string]any{"on": on, "port": port})
	if err != nil {
		return Listen{}, err
	}
	resp, err := c.post(c.Base+"/api/live/listen", "application/json", bytes.NewReader(body))
	if err != nil {
		return Listen{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Listen{}, fmt.Errorf("live/listen: unexpected status %s (update Push Hack)", resp.Status)
	}
	var st Listen
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return Listen{}, err
	}
	return st, nil
}

// PushFrame sends one 960x160 frame as raw pixels to POST
// /api/display/frame: encoded with display.EncodeFrame, no PNG on either
// side. Measured on device, a PNG frame through PushImage costs ~11 ms;
// this is the path for a hack that redraws many times a second. Not safe
// for concurrent use: it reuses one buffer per Client. Set display mode 2
// (SetMode) first, as with PushImage.
func (c *Client) PushFrame(img *image.NRGBA) error {
	if img.Rect != image.Rect(0, 0, display.VisW, display.VisH) {
		return fmt.Errorf("PushFrame: want a %dx%d image at the origin, got %v",
			display.VisW, display.VisH, img.Rect)
	}
	if c.frame == nil {
		c.frame = make([]byte, display.FrameBytes)
	}
	display.EncodeFrame(img, c.frame)
	resp, err := c.post(c.Base+"/api/display/frame", "application/octet-stream", bytes.NewReader(c.frame))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNoFrameRoute
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("display/frame: unexpected status %s", resp.Status)
	}
	return nil
}

// Push sends one frame the quickest way push-manager allows: PushFrame for a
// 960x160 NRGBA, PushImage for anything else, and PushImage for good once
// push-manager answers that it has no raw-frame route. A hack that redraws
// calls this and needs no version check. Set display mode 2 first. Not safe
// for concurrent use (PushFrame's buffer).
func (c *Client) Push(img image.Image) error {
	n, ok := img.(*image.NRGBA)
	if ok && !c.noFrame && n.Rect == image.Rect(0, 0, display.VisW, display.VisH) {
		err := c.PushFrame(n)
		if !errors.Is(err, ErrNoFrameRoute) {
			return err
		}
		c.noFrame = true
	}
	return c.PushImage(img)
}

// Tempo returns the current Live song BPM via GET /api/live/tempo.
func (c *Client) Tempo() (float64, error) {
	resp, err := c.HTTP.Get(c.Base + "/api/live/tempo")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var body struct {
		OK  bool    `json:"ok"`
		BPM float64 `json:"bpm"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	if !body.OK || body.BPM <= 0 {
		return 0, fmt.Errorf("live/tempo: no valid bpm")
	}
	return body.BPM, nil
}
