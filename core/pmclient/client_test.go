package pmclient

import (
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
