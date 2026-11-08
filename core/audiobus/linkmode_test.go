package audiobus

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// fakeHost serves both push-manager and the service on one port. The probe
// reads the service on 127.0.0.1:<port>, so the manager entry names that port.
func fakeHost(t *testing.T, mode *string) *LinkProbe {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/hacks/installed":
			u, _ := url.Parse("http://" + r.Host)
			fmt.Fprintf(w, `[{"id":"other","port":1},{"id":"push-audio-loopback","port":%s}]`, u.Port())
		case "/api/status":
			fmt.Fprintf(w, `{"mode":%q}`, *mode)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	p := NewLinkProbe()
	p.manager = srv.URL
	return p
}

func TestLinkProbe(t *testing.T) {
	mode := "link"
	p := fakeHost(t, &mode)
	if !p.fetch() {
		t.Fatal("link mode not seen")
	}
	mode = "card"
	if p.fetch() {
		t.Fatal("card mode seen as link")
	}
	mode = "link"
	if !p.fetch() {
		t.Fatal("link mode not seen after switch back")
	}
}

// Link returns the cached answer and fills it in the background.
func TestLinkCached(t *testing.T) {
	mode := "link"
	p := fakeHost(t, &mode)
	if p.Link() {
		t.Fatal("first call must not block; cache starts false")
	}
	for i := 0; i < 100 && !p.Link(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !p.Link() {
		t.Fatal("background refresh never set Link")
	}
}

func TestLinkProbeUnreachable(t *testing.T) {
	p := NewLinkProbe()
	p.manager = "http://127.0.0.1:" + strconv.Itoa(1) // nothing listens
	if p.fetch() {
		t.Fatal("unreachable manager must count as not Link")
	}
}

func TestLinkProbeOldService(t *testing.T) {
	mode := "" // old service: no mode field value
	p := fakeHost(t, &mode)
	if p.fetch() {
		t.Fatal("missing mode must count as not Link")
	}
}
