// Package update asks GitHub whether a newer Push Hack release exists.
//
// It only looks. Nothing here downloads or installs. State lives in one JSON
// file so a restart does not ask again. A check is one small request a day,
// with an ETag, so a "no change" answer costs nothing against GitHub's limit.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"push-manager/semver"
)

const (
	// DefaultURL lists releases, pre-releases too. /releases/latest would hide
	// -alpha and -beta.
	DefaultURL = "https://api.github.com/repos/federico-pepe/ableton-push-hack/releases?per_page=20"

	checkEvery     = 24 * time.Hour
	firstBackoff   = time.Hour
	manualMinGap   = 60 * time.Second
	maxBody        = 4 << 20
	defaultTimeout = 10 * time.Second
)

var (
	// ErrNotDue means the next scheduled check has not come yet.
	ErrNotDue = errors.New("update: next check not due yet")
	// ErrTooSoon means a manual check came less than a minute after the last try.
	ErrTooSoon = errors.New("update: checked less than a minute ago")
)

// Asset is one file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// Release is one usable GitHub release (not a draft, tag is a version).
type Release struct {
	Tag        string  `json:"tag"`
	Version    string  `json:"version"` // Tag without "v"
	Prerelease bool    `json:"prerelease"`
	URL        string  `json:"url"` // release page
	Assets     []Asset `json:"assets"`
}

type state struct {
	LastCheck   time.Time `json:"last_check"`
	LastTry     time.Time `json:"last_try"`
	NextCheck   time.Time `json:"next_check"`
	ETag        string    `json:"etag"`
	Failures    int       `json:"failures"`
	LastError   string    `json:"last_error"`
	Dismissed   string    `json:"dismissed"`
	Releases    []Release `json:"releases"`
	StateFormat int       `json:"format"`
}

// Config sets up a Checker. Only Installed and StatePath are needed; the rest
// have defaults.
type Config struct {
	Installed         string // this binary's release, e.g. "0.1.10-alpha"; "dev" never updates
	URL               string
	StatePath         string
	Timeout           time.Duration
	Now               func() time.Time
	AutoCheck         func() bool // nil = on
	IncludePrerelease func() bool // nil = include
	Logf              func(format string, args ...any)
}

// Checker holds the state and runs checks. Safe for concurrent use.
type Checker struct {
	cfg     Config
	client  *http.Client
	mu      sync.Mutex
	st      state
	lastLog string
}

// New loads saved state (a missing or broken file is ignored).
func New(cfg Config) *Checker {
	if cfg.URL == "" {
		cfg.URL = DefaultURL
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	c := &Checker{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
	if b, err := os.ReadFile(cfg.StatePath); err == nil {
		var st state
		if json.Unmarshal(b, &st) == nil {
			c.st = st
		}
	}
	return c
}

// Status is a snapshot for the screen and the web page.
type Status struct {
	Installed string
	Latest    *Release // newest release by the pre-release switch; nil if none known
	Available bool     // Latest is newer than Installed
	Dismissed string
	LastCheck time.Time
	NextCheck time.Time
	LastError string
	Releases  []Release
}

// Visible reports whether the bar should show: an update exists and the
// user has not dismissed that version.
func (s Status) Visible() bool {
	return s.Available && s.Latest != nil && s.Latest.Version != s.Dismissed
}

// Status returns the current picture. It never makes a request.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{
		Installed: c.cfg.Installed,
		Dismissed: c.st.Dismissed,
		LastCheck: c.st.LastCheck,
		NextCheck: c.st.NextCheck,
		LastError: c.st.LastError,
		Releases:  append([]Release(nil), c.st.Releases...),
	}
	include := c.cfg.IncludePrerelease == nil || c.cfg.IncludePrerelease()
	for i := range c.st.Releases { // sorted newest first
		r := c.st.Releases[i]
		if r.Prerelease && !include {
			continue
		}
		s.Latest = &r
		break
	}
	if s.Latest != nil {
		cur, cerr := semver.Parse(c.cfg.Installed)
		lat, lerr := semver.Parse(s.Latest.Version)
		s.Available = cerr == nil && lerr == nil && lat.Compare(cur) > 0
	}
	return s
}

// Dismiss hides the bar for this version. A newer release shows it again.
func (c *Checker) Dismiss(version string) {
	c.mu.Lock()
	c.st.Dismissed = version
	c.saveLocked()
	c.mu.Unlock()
}

// Check asks GitHub. force is a manual check: it skips the schedule but not
// the one-minute gap. Without force it returns ErrNotDue before the next
// scheduled time. A failed request is recorded, backed off, and returned.
func (c *Checker) Check(ctx context.Context, force bool) error {
	c.mu.Lock()
	now := c.cfg.Now()
	if force {
		if !c.st.LastTry.IsZero() && now.Sub(c.st.LastTry) < manualMinGap {
			c.mu.Unlock()
			return ErrTooSoon
		}
	} else if now.Before(c.st.NextCheck) {
		c.mu.Unlock()
		return ErrNotDue
	}
	etag := c.st.ETag
	c.st.LastTry = now
	c.mu.Unlock()

	rels, newETag, notModified, retryAt, err := c.fetch(ctx, etag)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.st.Failures++
		wait := checkEvery
		if c.st.Failures <= 6 { // 1 h, 2 h, ... 32 h would pass the cap
			wait = min(firstBackoff<<(c.st.Failures-1), checkEvery)
		}
		next := now.Add(wait)
		if retryAt.After(next) {
			next = retryAt
		}
		c.st.NextCheck = next
		c.st.LastError = err.Error()
		c.saveLocked()
		c.logOnce("update check failed: " + err.Error())
		return err
	}
	if c.st.LastError != "" {
		c.logOnce("update check works again")
	}
	c.st.Failures = 0
	c.st.LastError = ""
	c.st.LastCheck = now
	c.st.NextCheck = now.Add(checkEvery)
	if !notModified {
		c.st.Releases = rels
		c.st.ETag = newETag
	}
	c.saveLocked()
	return nil
}

type ghRelease struct {
	Tag        string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
	HTMLURL    string `json:"html_url"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (c *Checker) fetch(ctx context.Context, etag string) (rels []Release, newETag string, notModified bool, retryAt time.Time, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.URL, nil)
	if err != nil {
		return nil, "", false, time.Time{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "push-hack/"+c.cfg.Installed)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", false, time.Time{}, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil, "", true, time.Time{}, nil
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		if resp.Header.Get("X-Ratelimit-Remaining") == "0" {
			if n, perr := strconv.ParseInt(resp.Header.Get("X-Ratelimit-Reset"), 10, 64); perr == nil {
				retryAt = time.Unix(n, 0)
			}
		}
		return nil, "", false, retryAt, fmt.Errorf("github answered %s", resp.Status)
	case resp.StatusCode != http.StatusOK:
		return nil, "", false, time.Time{}, fmt.Errorf("github answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, "", false, time.Time{}, err
	}
	var raw []ghRelease
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, "", false, time.Time{}, fmt.Errorf("bad release list: %v", err)
	}
	return parseReleases(raw), resp.Header.Get("ETag"), false, time.Time{}, nil
}

// parseReleases drops drafts and tags that are not versions, and sorts the
// rest newest first.
func parseReleases(raw []ghRelease) []Release {
	type item struct {
		r Release
		v semver.Version
	}
	var items []item
	for _, g := range raw {
		if g.Draft {
			continue
		}
		v, err := semver.Parse(g.Tag)
		if err != nil {
			continue
		}
		r := Release{Tag: g.Tag, Version: v.String(), Prerelease: g.Prerelease || v.Prerelease(), URL: g.HTMLURL}
		for _, a := range g.Assets {
			r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
		}
		items = append(items, item{r, v})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].v.Compare(items[j].v) > 0 })
	out := make([]Release, len(items))
	for i, it := range items {
		out[i] = it.r
	}
	return out
}

// Run checks on schedule until ctx ends. The first look comes after
// startDelay. It re-reads AutoCheck each round, so the switch takes effect
// without a restart. A failed or skipped round does no harm.
func (c *Checker) Run(ctx context.Context, startDelay time.Duration) {
	wait := startDelay
	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		wait = time.Minute // poll the switch and schedule once a minute
		if c.cfg.AutoCheck != nil && !c.cfg.AutoCheck() {
			continue
		}
		_ = c.Check(ctx, false)
	}
}

// logOnce logs a message only when it differs from the last one, so a long
// offline spell does not fill the log.
func (c *Checker) logOnce(msg string) {
	if msg == c.lastLog {
		return
	}
	c.lastLog = msg
	c.cfg.Logf("%s", msg)
}

// saveLocked writes the state file through a temp file and a rename. A write
// error is logged and otherwise ignored: the checker still works from memory.
func (c *Checker) saveLocked() {
	if c.cfg.StatePath == "" {
		return
	}
	c.st.StateFormat = 1
	b, err := json.MarshalIndent(c.st, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(c.cfg.StatePath), 0o755)
	}
	if err == nil {
		tmp := c.cfg.StatePath + ".tmp"
		if err = os.WriteFile(tmp, b, 0o644); err == nil {
			err = os.Rename(tmp, c.cfg.StatePath)
		}
	}
	if err != nil {
		c.logOnce("update state not saved: " + err.Error())
	}
}
