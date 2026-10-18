package pmclient

// surface.go -- a hack as an app of Push Hack Home.
//
// push-manager owns Push's screen, buttons and pads and lends them to one
// app at a time: Shift + Help opens Home, the user picks the app, and the
// app is told it has the surface (focus) and when it no longer has it
// (blur). An app does not open itself with a gesture, does not switch the
// display or the intercept, and draws only between focus and blur.
//
//	pm := pmclient.New("http://127.0.0.1:7701")
//	pm.App = "riffs" // its hack.json declares "surface"
//	go pm.RunSurface(ctx, passToLive, pmclient.SurfaceHandler{
//		OnFocus: func() { active.Store(true) },
//		OnBlur:  func() { active.Store(false) },
//	})
//
// RunSurface holds the session and reconnects when push-manager restarts.
// Send frames with PushFrame (it names the app), the card's two lines with
// SurfaceStatus, and SurfaceLeave from the app's own LIVE button.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNoSwitcher is RunSurface's answer when push-manager has no Push Hack
// Home (an older release): the hack has to take the surface the old way.
var ErrNoSwitcher = errors.New("push-manager has no Push Hack Home (GET /api/surface is 404)")

// SurfaceHandler is what the app does when it gets and loses the surface.
// Both run on RunSurface's goroutine and should return quickly.
type SurfaceHandler struct {
	OnFocus func() // the app owns the surface: draw, and act on presses
	OnBlur  func() // it does not: stop drawing, ignore presses
}

// HasSwitcher reports whether push-manager has Push Hack Home.
func (c *Client) HasSwitcher() bool {
	resp, err := c.HTTP.Get(c.Base + "/api/surface")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// sessionQuiet is how long a session may stay silent before RunSurface
// takes push-manager for gone: it pings every second.
const sessionQuiet = 5 * time.Second

// RunSurface holds the app's session with push-manager until ctx ends,
// connecting again after a push-manager restart. passCC are the buttons
// that still reach Live while the app owns the surface (Play, Record...).
// It returns ErrNoSwitcher at once if push-manager has no Home, and an
// error if c.App is not set.
func (c *Client) RunSurface(ctx context.Context, passCC []int, h SurfaceHandler) error {
	if c.App == "" {
		return errors.New("RunSurface: set Client.App to the hack's id first")
	}
	resp, err := c.HTTP.Get(c.Base + "/api/surface")
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return ErrNoSwitcher
		}
	}
	var pass []string
	for _, cc := range passCC {
		pass = append(pass, strconv.Itoa(cc))
	}
	u := c.Base + "/api/surface/session?app=" + url.QueryEscape(c.App) + "&pass_cc=" + strings.Join(pass, ",")
	wait := 500 * time.Millisecond
	for ctx.Err() == nil {
		if c.session(ctx, u, h) {
			wait = 500 * time.Millisecond // it was up: try again soon
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
		wait = min(wait*2, 5*time.Second)
	}
	return ctx.Err()
}

// session runs one session until it ends, and reports whether it got as
// far as ready.
func (c *Client) session(ctx context.Context, u string, h SurfaceHandler) bool {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(sctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{}).Do(req) // no timeout: the stream stays open
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-sctx.Done():
				return
			}
		}
	}()
	focused, ready := false, false
	defer func() {
		if focused && h.OnBlur != nil {
			h.OnBlur() // the session is gone, and the surface with it
		}
	}()
	quiet := time.NewTimer(sessionQuiet)
	defer quiet.Stop()
	name := ""
	for {
		select {
		case <-quiet.C:
			return ready // push-manager stopped pinging
		case line, ok := <-lines:
			if !ok {
				return ready
			}
			quiet.Reset(sessionQuiet)
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case line == "" && name != "":
				switch name {
				case "ready":
					ready = true
				case "focus":
					if !focused && h.OnFocus != nil {
						h.OnFocus()
					}
					focused = true
				case "blur":
					if focused && h.OnBlur != nil {
						h.OnBlur()
					}
					focused = false
				}
				name = ""
			}
		}
	}
}

func (c *Client) surfacePost(path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.post(c.Base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var msg bytes.Buffer
		msg.ReadFrom(resp.Body) //nolint:errcheck
		return fmt.Errorf("%s: %s %s", path, resp.Status, strings.TrimSpace(msg.String()))
	}
	return nil
}

// SurfaceStatus sets the two lines on the app's card in Home (ASCII; about
// 14 characters show). Send it when they change, at most twice a second.
func (c *Client) SurfaceStatus(line1, line2 string) error {
	return c.surfacePost("/api/surface/status", map[string]any{"app": c.App, "lines": []string{line1, line2}})
}

// SurfaceInput switches the owning app between the whole surface (true)
// and the screen only (false: the pads and buttons go to Live).
func (c *Client) SurfaceInput(on bool) error {
	return c.surfacePost("/api/surface/input", map[string]any{"app": c.App, "input": on})
}

// SurfaceLeave gives the surface up: to "live" (the app's own LIVE
// button) or to "home".
func (c *Client) SurfaceLeave(to string) error {
	return c.surfacePost("/api/surface/leave", map[string]any{"app": c.App, "to": to})
}

// SurfaceOpen asks push-manager to open this app on Push, as if it had
// been chosen in Home (a hack's web panel offering "open on Push"). It is
// refused while another app owns the surface.
func (c *Client) SurfaceOpen() error {
	return c.surfacePost("/api/surface/open", map[string]any{"id": c.App})
}
