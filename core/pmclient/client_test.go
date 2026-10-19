package pmclient

import (
	"encoding/json"
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
