package pmclient

import (
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The status of a push-manager from before pass_cc has no such field; the
// current one always sends it (an empty list when nothing passes).
func TestMidiFilterStatusTellsReleasesApart(t *testing.T) {
	for _, tc := range []struct {
		body  string
		owns  bool
		on    bool
		npass int
	}{
		{`{"enabled":false,"available":true}`, false, false, 0},
		{`{"enabled":true,"available":true,"pass_cc":[85,86]}`, true, true, 2},
		{`{"enabled":false,"available":true,"pass_cc":[]}`, true, false, 0},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/midi/filter/status" {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(tc.body))
		}))
		st, err := New(srv.URL).MidiFilterStatus()
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", tc.body, err)
		}
		if st.SurfaceOwning != tc.owns || st.Enabled != tc.on || len(st.PassCC) != tc.npass {
			t.Errorf("%s: got %+v", tc.body, st)
		}
	}
}

func TestLiveListen(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sent)
		w.Write([]byte(`{"ok":false,"on":false,"track":true,"error":"Live offers no MIDI output named 'Stacks Listen'","outputs":["None","Push Manager"]}`))
	}))
	defer srv.Close()
	st, err := New(srv.URL).LiveListen(true, "Stacks Listen")
	if err != nil {
		t.Fatal(err)
	}
	if sent["on"] != true || sent["port"] != "Stacks Listen" {
		t.Errorf("sent %v", sent)
	}
	if st.OK || st.Error == "" || len(st.Outputs) != 2 {
		t.Errorf("got %+v", st)
	}
}

// Push takes the raw route when push-manager has it, and falls back to PNG
// for good (one probe only) when it answers 404.
func TestPushFallsBackWithoutFrameRoute(t *testing.T) {
	for _, hasFrame := range []bool{true, false} {
		var frame, png int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/display/frame":
				frame++
				if !hasFrame {
					http.NotFound(w, r)
				}
			case "/api/display/image":
				png++
			}
		}))
		c := New(srv.URL)
		img := imageNRGBA(960, 160)
		for i := 0; i < 3; i++ {
			if err := c.Push(img); err != nil {
				t.Fatalf("hasFrame=%v: %v", hasFrame, err)
			}
		}
		if err := c.Push(imageNRGBA(100, 40)); err != nil { // wrong size: PNG route
			t.Fatal(err)
		}
		srv.Close()
		wantFrame, wantPNG := 3, 1
		if !hasFrame {
			wantFrame, wantPNG = 1, 4 // one probe, then PNG for good
		}
		if frame != wantFrame || png != wantPNG {
			t.Errorf("hasFrame=%v: frame calls %d image calls %d, want %d and %d",
				hasFrame, frame, png, wantFrame, wantPNG)
		}
	}
}

func imageNRGBA(w, h int) *image.NRGBA { return image.NewNRGBA(image.Rect(0, 0, w, h)) }
