package main

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coredisplay "github.com/federico-pepe/ableton-push-hack/core/display"
)

// A session over real HTTP: ready, then focus when the app is opened;
// closing the connection gives the surface to Home; a frame from an app
// that does not own the screen is refused before it reaches it.
func TestSurfaceSessionHTTP(t *testing.T) {
	dir := t.TempDir()
	writeHack(t, dir, "riffs", `{"id":"riffs","name":"Riffs","port":7712,"surface":{"label":"RIFFS","color":"sky"}}`)
	hacksDir, uiTabsPath = dir, filepath.Join(dir, "ui_tabs.json")
	initdDir, rcdGlob = filepath.Join(dir, "init.d"), filepath.Join(dir, "rc*.d")
	saved := surface
	fio := &fakeIO{t: time.Now()}
	surface = newSurface(fio, homeEntries, setStarPref)
	defer func() {
		surface = saved
		hacksDir = "/data/push-hack/hacks"
		initdDir, rcdGlob = "/etc/init.d", "/etc/rc*.d"
		uiPrefs = nil
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/surface", handleSurface)
	mux.HandleFunc("/api/surface/session", handleSurfaceSession)
	mux.HandleFunc("/api/surface/open", handleSurfaceOpen)
	mux.HandleFunc("/api/display/frame", handleDisplayFrame)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if resp, _ := http.Get(srv.URL + "/api/surface/session?app=nope"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a session for an unknown app: %d, want 404", resp.StatusCode)
	}

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/surface/session?app=riffs&pass_cc=85,86", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				events <- name
			}
		}
		close(events)
	}()
	next := func(want string) {
		t.Helper()
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("event %q, want %q", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no %q event", want)
		}
	}
	next("ready")

	open, _ := http.Post(srv.URL+"/api/surface/open", "application/json", strings.NewReader(`{"id":"riffs"}`))
	if open.StatusCode != http.StatusOK {
		t.Fatalf("open: %d", open.StatusCode)
	}
	next("focus")
	if o, app := surface.Owner(); o != ownApp || app != "riffs" || !fio.intercept {
		t.Fatalf("after open: %s %s, intercept %v", o, app, fio.intercept)
	}

	// a frame from someone else, or from no app, never reaches the screen
	frame := bytes.Repeat([]byte{0}, coredisplay.FrameBytes)
	for _, app := range []string{"", "stacks"} {
		r, _ := http.NewRequest("POST", srv.URL+"/api/display/frame", bytes.NewReader(frame))
		r.Header.Set(appHeader, app)
		fr, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if fr.StatusCode != http.StatusConflict {
			t.Errorf("a frame from %q while riffs owns the screen: %d, want 409", app, fr.StatusCode)
		}
	}

	cancel() // the app goes away
	deadline := time.Now().Add(3 * time.Second)
	for {
		if o, _ := surface.Owner(); o == ownHome {
			break
		}
		if time.Now().After(deadline) {
			o, _ := surface.Owner()
			t.Fatalf("the owning app's session ended; owner %s, want home", o)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(fio.view.Notice, "RIFFS STOPPED") {
		t.Errorf("Home should say riffs stopped: %q", fio.view.Notice)
	}
}
