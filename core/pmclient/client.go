// Package pmclient is an HTTP client for push-manager's display and Live
// endpoints — the "hook", as code: it's how a hack draws on Push 3's screen
// or reads Live's tempo without ever touching ALSA, shm, or libusb directly.
// See CLAUDE.md's "Display-owning hacks" section for why this indirection
// exists — push-manager is the sole shared-memory writer.
package pmclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"time"
)

// Client talks to a push-manager instance over HTTP.
type Client struct {
	Base string
	HTTP *http.Client
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

	resp, err := c.HTTP.Do(req)
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

	resp, err := c.HTTP.Do(req)
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

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("midi/filter: unexpected status %s", resp.Status)
	}
	return nil
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
