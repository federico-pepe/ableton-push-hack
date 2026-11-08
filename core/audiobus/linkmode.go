package audiobus

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	managerURL    = "http://127.0.0.1:7701"
	loopbackID    = "push-audio-loopback"
	probeInterval = 3 * time.Second
	probeTimeout  = 800 * time.Millisecond
)

// LinkProbe tells a hack whether push-audio-loopback sends audio to Live over
// Link ("link") or through the card ("card"). Port is dynamic, so it asks
// push-manager first, then reads the service's /api/status.
// Link never blocks: it returns the cached answer and refreshes in the
// background when the answer is older than probeInterval.
// Unknown (service down, old service with no "mode") counts as not Link.
type LinkProbe struct {
	manager string
	client  *http.Client

	link    atomic.Bool
	last    atomic.Int64 // unix nano of last refresh start
	running atomic.Bool

	mu   sync.Mutex
	port int
}

// NewLinkProbe returns a probe that asks push-manager on 127.0.0.1:7701.
func NewLinkProbe() *LinkProbe {
	return &LinkProbe{manager: managerURL, client: &http.Client{Timeout: probeTimeout}}
}

// Link reports whether the service runs in Link mode.
func (p *LinkProbe) Link() bool {
	if time.Since(time.Unix(0, p.last.Load())) > probeInterval && p.running.CompareAndSwap(false, true) {
		p.last.Store(time.Now().UnixNano())
		go func() {
			defer p.running.Store(false)
			p.link.Store(p.fetch())
		}()
	}
	return p.link.Load()
}

func (p *LinkProbe) fetch() bool {
	p.mu.Lock()
	port := p.port
	p.mu.Unlock()
	if port == 0 {
		port = p.lookupPort()
		if port == 0 {
			return false
		}
		p.mu.Lock()
		p.port = port
		p.mu.Unlock()
	}
	var st struct {
		Mode string `json:"mode"`
	}
	if !p.getJSON("http://127.0.0.1:"+strconv.Itoa(port)+"/api/status", &st) {
		// Service may have a new port after a reinstall: look it up again.
		p.mu.Lock()
		p.port = 0
		p.mu.Unlock()
		return false
	}
	return st.Mode == "link"
}

func (p *LinkProbe) lookupPort() int {
	var hacks []struct {
		ID   string `json:"id"`
		Port int    `json:"port"`
	}
	if !p.getJSON(p.manager+"/api/hacks/installed", &hacks) {
		return 0
	}
	for _, h := range hacks {
		if h.ID == loopbackID {
			return h.Port
		}
	}
	return 0
}

func (p *LinkProbe) getJSON(url string, v any) bool {
	resp, err := p.client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(v) == nil
}
