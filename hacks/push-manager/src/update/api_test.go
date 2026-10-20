package update

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newAPI(t *testing.T, f *fixture) (*API, http.Handler) {
	t.Helper()
	store := OpenStore(filepath.Join(f.dir, "settings.json"))
	f.c.cfg.AutoCheck = store.AutoCheck
	f.c.cfg.IncludePrerelease = store.IncludePrerelease
	a := &API{C: f.c, S: store}
	mux := http.NewServeMux()
	a.Register(mux)
	return a, mux
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

func TestAPICheckThenStatus(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	_, h := newAPI(t, f)
	if w := do(h, "GET", "/api/update/status", ""); !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("before check: %s", w.Body)
	}
	w := do(h, "POST", "/api/update/check", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"visible":true`) || !strings.Contains(w.Body.String(), `"version":"0.1.11-alpha"`) {
		t.Fatalf("after check: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/api/update/check", ""); w.Code != 429 {
		t.Fatalf("second check = %d, want 429", w.Code)
	}
}

func TestAPIFailedCheckIsStatusNotError(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	f.code = 500
	_, h := newAPI(t, f)
	w := do(h, "POST", "/api/update/check", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"last_error":"github answered 500`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestAPIDismissAndSettings(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	_, h := newAPI(t, f)
	do(h, "POST", "/api/update/check", "")
	if w := do(h, "POST", "/api/update/dismiss", `{"version":"0.1.11-alpha"}`); !strings.Contains(w.Body.String(), `"visible":false`) {
		t.Fatalf("dismiss: %s", w.Body)
	}
	if w := do(h, "POST", "/api/update/dismiss", `{}`); w.Code != 400 {
		t.Fatalf("empty dismiss = %d", w.Code)
	}
	w := do(h, "POST", "/api/update/settings", `{"include_prerelease":false}`)
	if !strings.Contains(w.Body.String(), `"include_prerelease":false`) || !strings.Contains(w.Body.String(), `"auto_check":true`) {
		t.Fatalf("settings: %s", w.Body)
	}
	// The change must survive a reload.
	if OpenStore(filepath.Join(f.dir, "settings.json")).Get().IncludePrerelease {
		t.Fatal("setting not saved")
	}
}

func TestAPIAutoCheckOffHidesNextCheck(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	_, h := newAPI(t, f)
	do(h, "POST", "/api/update/check", "")
	do(h, "POST", "/api/update/settings", `{"auto_check":false}`)
	if w := do(h, "GET", "/api/update/status", ""); !strings.Contains(w.Body.String(), `"next_check":null`) {
		t.Fatalf("%s", w.Body)
	}
}

func TestAPIMethodsAndBadBodies(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	_, h := newAPI(t, f)
	for _, c := range [][3]string{
		{"POST", "/api/update/status", "405"},
		{"GET", "/api/update/check", "405"},
		{"GET", "/api/update/dismiss", "405"},
		{"DELETE", "/api/update/settings", "405"},
		{"POST", "/api/update/settings", "400"},
	} {
		if w := do(h, c[0], c[1], "not json"); itoa(int64(w.Code)) != c[2] {
			t.Errorf("%s %s = %d, want %s", c[0], c[1], w.Code, c[2])
		}
	}
}

func TestStoreKeepsURLAndDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	if s := OpenStore(p).Get(); !s.AutoCheck || !s.IncludePrerelease {
		t.Fatalf("defaults = %+v", s)
	}
	st := OpenStore(p)
	st.s.URL = "http://bench"
	st.Set(false, true)
	if s := OpenStore(p).Get(); s.URL != "http://bench" || s.AutoCheck {
		t.Fatalf("reload = %+v", s)
	}
}

func TestAPIInstallOnlyTheNewestRelease(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	r := newRel(t)
	r.ready("0.1.11-alpha")
	f.body = `[{"tag_name":"v0.1.11-alpha","prerelease":true,"draft":false,"html_url":"https://x","assets":[` +
		`{"name":"manifest.json","browser_download_url":"` + r.release.Assets[0].URL + `","size":1},` +
		`{"name":"` + r.release.Assets[1].Name + `","browser_download_url":"` + r.release.Assets[1].URL + `","size":1}]}]`
	store := OpenStore(filepath.Join(f.dir, "settings.json"))
	f.c.cfg.IncludePrerelease = store.IncludePrerelease
	api := &API{C: f.c, S: store, A: r.app, Dir: r.dir}
	mux := http.NewServeMux()
	api.Register(mux)
	do(mux, "POST", "/api/update/check", "")

	if w := do(mux, "POST", "/api/update/apply", `{"version":"0.1.9"}`); w.Code != 409 {
		t.Fatalf("old version = %d", w.Code)
	}
	if w := do(mux, "POST", "/api/update/apply", `{}`); w.Code != 400 {
		t.Fatalf("empty = %d", w.Code)
	}
	if w := do(mux, "GET", "/api/update/apply", ""); w.Code != 405 {
		t.Fatalf("GET = %d", w.Code)
	}
	if w := do(mux, "POST", "/api/update/apply", `{"version":"0.1.11-alpha"}`); w.Code != 202 || !strings.Contains(w.Body.String(), `"apply":{"phase"`) {
		t.Fatalf("apply = %d %s", w.Code, w.Body)
	}
	for i := 0; i < 200 && r.startedN() == 0 && r.app.State().Phase != "failed"; i++ { // let it finish before the temp dirs go
		time.Sleep(10 * time.Millisecond)
	}
	if r.startedN() != 1 {
		t.Fatalf("helper not started: %+v", r.app.State())
	}
}

func TestAPIResultAndAck(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	store := OpenStore(filepath.Join(f.dir, "settings.json"))
	os.WriteFile(filepath.Join(f.dir, resultFile), []byte(`{"ok":false,"version":"0.1.11-alpha","error":"boom"}`), 0o644)
	api := &API{C: f.c, S: store, Dir: f.dir}
	mux := http.NewServeMux()
	api.Register(mux)
	if w := do(mux, "GET", "/api/update/status", ""); !strings.Contains(w.Body.String(), `"error":"boom"`) {
		t.Fatalf("%s", w.Body)
	}
	if w := do(mux, "POST", "/api/update/ack", ""); !strings.Contains(w.Body.String(), `"result":null`) {
		t.Fatalf("%s", w.Body)
	}
}
