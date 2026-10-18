package pmclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A fake push-manager: one session at a time, focus then blur, then the
// stream ends; RunSurface must call the handlers and connect again.
func TestRunSurface(t *testing.T) {
	var sessions atomic.Int32
	var mu sync.Mutex
	var header string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/surface", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "{}") })
	mux.HandleFunc("/api/surface/session", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("app") != "riffs" || r.URL.Query().Get("pass_cc") != "85,86" {
			http.Error(w, "bad query "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		n := sessions.Add(1)
		f := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: ready\ndata: {}\n\n")
		f.Flush()
		fmt.Fprint(w, "event: focus\ndata: {\"input\":true}\n\n")
		f.Flush()
		if n == 1 {
			fmt.Fprint(w, ": ping\n\nevent: blur\ndata: {}\n\n")
			f.Flush()
		}
		time.Sleep(50 * time.Millisecond) // then push-manager "restarts"
	})
	mux.HandleFunc("/api/surface/status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		header = r.Header.Get(AppHeader)
		mu.Unlock()
		fmt.Fprint(w, "{}")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	c.App = "riffs"
	var focus, blur atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error)
	go func() {
		done <- c.RunSurface(ctx, []int{85, 86}, SurfaceHandler{
			OnFocus: func() { focus.Add(1) },
			OnBlur:  func() { blur.Add(1) },
		})
	}()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for sessions.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sessions.Load() < 2 {
		t.Fatalf("RunSurface connected %d times, want it to connect again", sessions.Load())
	}
	time.Sleep(100 * time.Millisecond)
	// session 1: focus, blur; session 2: focus, then the stream ended: blur
	if focus.Load() < 2 || blur.Load() < 2 {
		t.Errorf("focus %d, blur %d", focus.Load(), blur.Load())
	}
	if err := c.SurfaceStatus("FUNK", "PLAYING"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if header != "riffs" {
		t.Errorf("calls should carry %s: riffs, got %q", AppHeader, header)
	}
	mu.Unlock()
	cancel()
	<-done
}

func TestRunSurfaceNoSwitcher(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := New(srv.URL)
	c.App = "riffs"
	if c.HasSwitcher() {
		t.Error("a push-manager without /api/surface has no switcher")
	}
	if err := c.RunSurface(context.Background(), nil, SurfaceHandler{}); err != ErrNoSwitcher {
		t.Fatalf("got %v, want ErrNoSwitcher", err)
	}
	c.App = ""
	if err := c.RunSurface(context.Background(), nil, SurfaceHandler{}); err == nil {
		t.Error("RunSurface without App should fail")
	}
}
