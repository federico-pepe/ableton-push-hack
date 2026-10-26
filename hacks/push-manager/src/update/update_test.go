package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const twoReleases = `[
 {"tag_name":"v0.1.11-alpha","prerelease":true,"draft":false,"html_url":"https://x/11","assets":[{"name":"push-hack-0.1.11-alpha.tar.gz","browser_download_url":"https://x/a.tgz","size":123}]},
 {"tag_name":"v0.1.10","prerelease":false,"draft":false,"html_url":"https://x/10","assets":[]},
 {"tag_name":"v0.1.12-alpha","prerelease":true,"draft":true,"html_url":"https://x/12","assets":[]},
 {"tag_name":"not-a-version","prerelease":false,"draft":false,"html_url":"https://x/n","assets":[]}
]`

type fixture struct {
	srv   *httptest.Server
	hits  atomic.Int32
	body  string
	code  int
	etag  string
	gotIN string // last If-None-Match
	hdr   http.Header
	now   time.Time
	c     *Checker
	dir   string
}

func newFixture(t *testing.T, installed string) *fixture {
	t.Helper()
	f := &fixture{body: twoReleases, code: 200, etag: `"e1"`, now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.gotIN = r.Header.Get("If-None-Match")
		for k, v := range f.hdr {
			w.Header()[k] = v
		}
		if f.code == 200 {
			if f.gotIN == f.etag {
				w.WriteHeader(304)
				return
			}
			w.Header().Set("ETag", f.etag)
		}
		w.WriteHeader(f.code)
		if f.code == 200 {
			w.Write([]byte(f.body))
		}
	}))
	t.Cleanup(f.srv.Close)
	f.dir = t.TempDir()
	f.c = New(Config{
		Installed:         installed,
		URL:               f.srv.URL,
		StatePath:         filepath.Join(f.dir, "state.json"),
		Now:               func() time.Time { return f.now },
		IncludePrerelease: func() bool { return true },
		Timeout:           500 * time.Millisecond,
		Logf:              func(string, ...any) {},
	})
	return f
}

func TestCheckFindsNewestIncludingPrerelease(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	if err := f.c.Check(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	s := f.c.Status()
	if s.Latest == nil || s.Latest.Tag != "v0.1.11-alpha" || !s.Available {
		t.Fatalf("status = %+v", s)
	}
	if len(s.Latest.Assets) != 1 || s.Latest.Assets[0].Size != 123 {
		t.Fatalf("assets = %+v", s.Latest.Assets)
	}
	if !s.LastCheck.Equal(f.now) || s.LastError != "" {
		t.Fatalf("last check/error = %v %q", s.LastCheck, s.LastError)
	}
}

func TestDraftsAndBadTagsIgnored(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	f.c.Check(context.Background(), true)
	for _, r := range f.c.Status().Releases {
		if r.Tag == "v0.1.12-alpha" || r.Tag == "not-a-version" {
			t.Fatalf("kept %s", r.Tag)
		}
	}
}

func TestPrereleaseSwitch(t *testing.T) {
	f := newFixture(t, "0.1.9")
	include := true
	f.c.cfg.IncludePrerelease = func() bool { return include }
	f.c.Check(context.Background(), true)
	if got := f.c.Status().Latest.Tag; got != "v0.1.11-alpha" {
		t.Fatalf("with pre-releases: %s", got)
	}
	include = false // no new request needed
	hits := f.hits.Load()
	if got := f.c.Status().Latest.Tag; got != "v0.1.10" {
		t.Fatalf("without pre-releases: %s", got)
	}
	if f.hits.Load() != hits {
		t.Fatal("toggling the switch made a request")
	}
}

func TestNoUpdateWhenCurrentIsNewest(t *testing.T) {
	f := newFixture(t, "0.1.11-alpha")
	f.c.Check(context.Background(), true)
	if f.c.Status().Available {
		t.Fatal("available for the same version")
	}
	f2 := newFixture(t, "0.2.0")
	f2.c.Check(context.Background(), true)
	if f2.c.Status().Available {
		t.Fatal("available for an older release")
	}
}

func TestDevBuildNeverAvailable(t *testing.T) {
	f := newFixture(t, "dev")
	f.c.Check(context.Background(), true)
	if f.c.Status().Available {
		t.Fatal("dev build offered an update")
	}
}

func TestETagSentAnd304KeepsReleases(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	f.c.Check(context.Background(), true)
	f.now = f.now.Add(25 * time.Hour)
	if err := f.c.Check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if f.gotIN != `"e1"` {
		t.Fatalf("If-None-Match = %q", f.gotIN)
	}
	s := f.c.Status()
	if s.Latest == nil || s.Latest.Tag != "v0.1.11-alpha" {
		t.Fatalf("lost releases on 304: %+v", s)
	}
	if !s.LastCheck.Equal(f.now) {
		t.Fatal("304 did not move LastCheck")
	}
}

func TestNotDueIsNoRequest(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	f.c.Check(context.Background(), true)
	hits := f.hits.Load()
	f.now = f.now.Add(23 * time.Hour)
	if err := f.c.Check(context.Background(), false); !errors.Is(err, ErrNotDue) {
		t.Fatalf("err = %v", err)
	}
	if f.hits.Load() != hits {
		t.Fatal("request made before due")
	}
}

func TestManualCheckRateLimited(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	f.c.Check(context.Background(), true)
	f.now = f.now.Add(30 * time.Second)
	if err := f.c.Check(context.Background(), true); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("err = %v", err)
	}
	f.now = f.now.Add(31 * time.Second)
	if err := f.c.Check(context.Background(), true); err != nil {
		t.Fatalf("after 61 s: %v", err)
	}
}

func TestFailuresBackOffAndKeepOldData(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	f.c.Check(context.Background(), true)
	f.code = 500
	var waits []time.Duration
	for i := 0; i < 8; i++ {
		f.now = f.now.Add(25 * time.Hour)
		if err := f.c.Check(context.Background(), false); err == nil {
			t.Fatal("want error")
		}
		waits = append(waits, f.c.Status().NextCheck.Sub(f.now))
	}
	want := []time.Duration{time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 16 * time.Hour, 24 * time.Hour, 24 * time.Hour, 24 * time.Hour}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("backoff %d = %v, want %v (all %v)", i, waits[i], want[i], waits)
		}
	}
	s := f.c.Status()
	if s.Latest == nil || s.LastError == "" || !s.Available {
		t.Fatalf("old data lost or error empty: %+v", s)
	}
	f.code = 200
	f.now = f.now.Add(25 * time.Hour)
	if err := f.c.Check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if s := f.c.Status(); s.LastError != "" || s.NextCheck.Sub(f.now) != 24*time.Hour {
		t.Fatalf("success did not reset: %+v", s)
	}
}

func TestRateLimitWaitsForReset(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	f.code = 403
	reset := f.now.Add(40 * time.Minute)
	f.hdr = http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {itoa(reset.Unix())}}
	if err := f.c.Check(context.Background(), true); err == nil {
		t.Fatal("want error")
	}
	if got := f.c.Status().NextCheck; got.Before(reset) {
		t.Fatalf("next check %v before reset %v", got, reset)
	}
}

func TestTimeoutAndMalformedJSON(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	sc := New(Config{Installed: "0.1.10-alpha", URL: slow.URL, Timeout: 100 * time.Millisecond, Logf: func(string, ...any) {}})
	if err := sc.Check(context.Background(), true); err == nil {
		t.Fatal("want timeout")
	}
	if sc.Status().LastError == "" || !sc.Status().NextCheck.After(time.Now()) {
		t.Fatal("timeout not recorded with a back-off")
	}

	f := newFixture(t, "0.1.10-alpha")
	f.body = `{"not":"a list"`
	f.now = f.now.Add(2 * time.Hour)
	f.etag = `"e2"`
	if err := f.c.Check(context.Background(), true); err == nil {
		t.Fatal("want parse error")
	}
	if f.c.Status().LastError == "" {
		t.Fatal("no error recorded")
	}
}

func TestStateSurvivesRestart(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	f.c.Check(context.Background(), true)
	f.c.Dismiss("0.1.11-alpha")
	again := New(f.c.cfg)
	s := again.Status()
	if s.Latest == nil || s.Dismissed != "0.1.11-alpha" || !s.LastCheck.Equal(f.now) {
		t.Fatalf("reloaded = %+v", s)
	}
	f.now = f.now.Add(time.Hour)
	hits := f.hits.Load()
	if err := again.Check(context.Background(), false); !errors.Is(err, ErrNotDue) || f.hits.Load() != hits {
		t.Fatalf("restart re-polled: %v", err)
	}
}

func TestCorruptStateIgnored(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	os.WriteFile(f.c.cfg.StatePath, []byte("{broken"), 0o644)
	c := New(f.c.cfg)
	if c.Status().Latest != nil {
		t.Fatal("bad state used")
	}
	if err := c.Check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
}

func TestDismissHidesUntilNewer(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	f.c.Check(context.Background(), true)
	f.c.Dismiss("0.1.11-alpha")
	if f.c.Status().Visible() {
		t.Fatal("still visible after dismiss")
	}
	f.body = strings.Replace(twoReleases, `[`, `[{"tag_name":"v0.1.12-alpha","prerelease":true,"draft":false,"html_url":"https://x/12","assets":[]},`, 1)
	f.etag = `"e3"`
	f.now = f.now.Add(25 * time.Hour)
	f.c.Check(context.Background(), false)
	if !f.c.Status().Visible() {
		t.Fatal("newer release stayed hidden")
	}
}

func TestSendsUserAgentAndAccept(t *testing.T) {
	var ua, acc string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, acc = r.Header.Get("User-Agent"), r.Header.Get("Accept")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	c := New(Config{Installed: "0.1.10-alpha", URL: srv.URL, StatePath: filepath.Join(t.TempDir(), "s.json"), Logf: func(string, ...any) {}})
	c.Check(context.Background(), true)
	if !strings.HasPrefix(ua, "push-hack/0.1.10-alpha") || !strings.Contains(acc, "github") {
		t.Fatalf("ua=%q accept=%q", ua, acc)
	}
}

func TestRunStopsOnCancelAndHonoursSwitch(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	f.c.cfg.AutoCheck = func() bool { return false }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.c.Run(ctx, 0); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
	if f.hits.Load() != 0 {
		t.Fatal("request made with auto-check off")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
