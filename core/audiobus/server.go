// Package audiobus lets many hacks play through the one loopback card.
//
// ALSA lets only one process write to a loopback stream, so the loopback
// service owns it: it runs a Server, mixes every client into the card, and
// writes one block per period. A hack runs a Client and writes stereo float32
// blocks. See docs/audio-bus.md.
package audiobus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Default places. A tmpfs, so rings never touch flash.
const (
	DefaultDir  = "/dev/shm/push-hack-audio"
	socketName  = "bus.sock"
	pairsName   = "pairs.json"
	ringPeriods = 2 // ring holds at least this many periods
)

// Format is what the card runs at. Clients render at this rate and block size.
type Format struct {
	Rate     int `json:"rate"`
	Period   int `json:"period"`
	Channels int `json:"channels"` // card channels, not 2
}

func (f Format) valid() bool { return f.Rate > 0 && f.Period > 0 && f.Channels >= 2 }

// request is the client's first line. Pair is the 0-based first channel of
// the stereo pair it wants, or -1 for "my saved pair, or any free one".
type request struct {
	V    int    `json:"v"`
	Name string `json:"name"`
	Pair int    `json:"pair"`
}

type reply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Ring  string `json:"ring,omitempty"`
	Pair  int    `json:"pair"`
	Format
}

// session is one connected client, as the mixer sees it.
type session struct {
	name  string
	pair  int
	rg    *ring
	path  string
	conn  net.Conn
	peak  atomic.Uint32 // float32 bits, peak of the last block
	under atomic.Uint64 // blocks that came up short, after the first sound
	live  atomic.Bool   // has delivered at least one frame
}

// ClientInfo is a read-only view of one client, for UIs.
type ClientInfo struct {
	Name      string  `json:"name"`
	Pair      int     `json:"pair"`
	Peak      float32 `json:"peak"`
	Underruns uint64  `json:"underruns"`
}

// Server owns the mixing side. Safe for concurrent use; Mix is lock-free.
type Server struct {
	// PairsPath is where name->pair is saved. Set it before Listen, to a
	// folder that survives a reboot. Empty: dir/pairs.json (tmpfs).
	PairsPath string

	dir string

	mu       sync.Mutex // guards pairs, listener, closed, sessions writes
	pairs    map[string]int
	ln       net.Listener
	closed   bool
	format   atomic.Pointer[Format]     // Mix reads it without the lock
	sessions atomic.Pointer[[]*session] // copy on write; Mix only loads it
}

// NewServer makes a server whose socket and rings live in dir.
func NewServer(dir string) *Server {
	s := &Server{dir: dir, pairs: map[string]int{}}
	empty := []*session{}
	s.sessions.Store(&empty)
	return s
}

// SocketPath is where clients connect.
func SocketPath(dir string) string { return filepath.Join(dir, socketName) }

// Listen starts accepting clients in the background.
func (s *Server) Listen() error {
	if err := os.MkdirAll(s.dir, 0o777); err != nil {
		return err
	}
	_ = os.Chmod(s.dir, 0o777)
	s.loadPairs()
	path := SocketPath(s.dir)
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	_ = os.Chmod(path, 0o777)
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return nil
}

// SetFormat tells the server what the card runs at. A change drops every
// client: their rings have the wrong size. Clients reconnect on their own.
func (s *Server) SetFormat(f Format) {
	old := s.format.Swap(&f)
	if old == nil || *old != f {
		s.dropAll()
	}
}

// Close stops the server and drops all clients.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	ln := s.ln
	s.mu.Unlock()
	if ln != nil {
		ln.Close()
	}
	s.dropAll()
}

func (s *Server) dropAll() {
	s.mu.Lock()
	old := *s.sessions.Load()
	empty := []*session{}
	s.sessions.Store(&empty)
	s.mu.Unlock()
	for _, ss := range old {
		s.release(ss)
	}
}

// release closes one session's connection; its ring is freed once Mix has
// stopped seeing it (it is already out of the list).
func (s *Server) release(ss *session) {
	ss.conn.Close()
	// Mix may still be reading this ring for one more block: unmap late.
	go func(rg *ring, path string) {
		syncGrace()
		rg.close()
		os.Remove(path)
	}(ss.rg, ss.path)
}

func (s *Server) serve(c net.Conn) {
	br := bufio.NewReader(c)
	line, err := br.ReadBytes('\n')
	if err != nil {
		c.Close()
		return
	}
	var rq request
	if json.Unmarshal(line, &rq) != nil || rq.V != 1 || rq.Name == "" {
		writeReply(c, reply{Error: "bad request"})
		c.Close()
		return
	}
	ss, rep := s.add(rq, c)
	writeReply(c, rep)
	if ss == nil {
		c.Close()
		return
	}
	log.Printf("audiobus: %q connected on channels %d-%d", ss.name, ss.pair+1, ss.pair+2)
	// The client sends nothing more; EOF means it is gone.
	_, _ = br.Peek(1)
	s.remove(ss)
	log.Printf("audiobus: %q left", ss.name)
}

func writeReply(c net.Conn, r reply) {
	b, _ := json.Marshal(r)
	_, _ = c.Write(append(b, '\n'))
}

func (s *Server) add(rq request, c net.Conn) (*session, reply) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var f Format
	if p := s.format.Load(); p != nil {
		f = *p
	}
	if s.closed || !f.valid() {
		return nil, reply{Error: "card not ready"}
	}
	cur := *s.sessions.Load()
	pair := s.choosePair(rq, f.Channels, cur)
	if pair < 0 {
		return nil, reply{Error: "no free channel pair"}
	}
	capFrames := pow2(f.Period * ringPeriods)
	path := filepath.Join(s.dir, fmt.Sprintf("%s-%d.ring", sanitize(rq.Name), nextID()))
	rg, err := createRing(path, capFrames)
	if err != nil {
		return nil, reply{Error: err.Error()}
	}
	ss := &session{name: rq.Name, pair: pair, rg: rg, path: path, conn: c}
	next := append(append([]*session{}, cur...), ss)
	s.sessions.Store(&next)
	s.pairs[rq.Name] = pair
	s.savePairs()
	return ss, reply{OK: true, Ring: path, Pair: pair, Format: f}
}

func (s *Server) remove(ss *session) {
	s.mu.Lock()
	cur := *s.sessions.Load()
	next := make([]*session, 0, len(cur))
	found := false
	for _, x := range cur {
		if x == ss {
			found = true
			continue
		}
		next = append(next, x)
	}
	s.sessions.Store(&next)
	s.mu.Unlock()
	if found {
		s.release(ss)
	}
}

// choosePair: asked pair if valid, else the saved one, else the first pair no
// client uses. -1 if the card has none left. Caller holds s.mu.
func (s *Server) choosePair(rq request, channels int, cur []*session) int {
	valid := func(p int) bool { return p >= 0 && p%2 == 0 && p+1 < channels }
	if valid(rq.Pair) {
		return rq.Pair
	}
	taken := map[int]bool{}
	for _, x := range cur {
		taken[x.pair] = true
	}
	if p, ok := s.pairs[rq.Name]; ok && valid(p) && !taken[p] {
		return p
	}
	for p := 0; p+1 < channels; p += 2 {
		if !taken[p] {
			return p
		}
	}
	return -1
}

// Mix zeroes out (Period*Channels interleaved float32) and adds every client
// into its pair. It takes no lock and allocates nothing. Call it once per
// period from the thread that writes the card.
func (s *Server) Mix(out []float32) {
	for i := range out {
		out[i] = 0
	}
	f := s.format.Load()
	if f == nil || !f.valid() || len(out) < f.Period*f.Channels {
		return
	}
	period, ch := f.Period, f.Channels
	for _, ss := range *s.sessions.Load() {
		got, peak := ss.rg.addTo(out[ss.pair:], ch, period)
		ss.peak.Store(float32bits(peak))
		if got > 0 {
			ss.live.Store(true)
		}
		if got < period && ss.live.Load() {
			ss.under.Add(1)
		}
	}
}

// Clients lists who is connected.
func (s *Server) Clients() []ClientInfo {
	cur := *s.sessions.Load()
	out := make([]ClientInfo, len(cur))
	for i, ss := range cur {
		out[i] = ClientInfo{ss.name, ss.pair, float32frombits(ss.peak.Load()), ss.under.Load()}
	}
	return out
}

func (s *Server) loadPairs() {
	b, err := os.ReadFile(s.pairsPath())
	if err != nil {
		return
	}
	m := map[string]int{}
	if json.Unmarshal(b, &m) == nil {
		s.mu.Lock()
		s.pairs = m
		s.mu.Unlock()
	}
}

func (s *Server) pairsPath() string {
	if s.PairsPath != "" {
		return s.PairsPath
	}
	return filepath.Join(s.dir, pairsName)
}

// savePairs writes the name->pair map. Caller holds s.mu.
func (s *Server) savePairs() {
	b, err := json.MarshalIndent(s.pairs, "", "  ")
	if err != nil {
		return
	}
	p := s.pairsPath()
	if os.WriteFile(p+".tmp", b, 0o666) == nil {
		_ = os.Rename(p+".tmp", p)
	}
}

var idSeq atomic.Uint64

func nextID() uint64 { return idSeq.Add(1) }

// sanitize keeps a client name safe as part of a file name.
func sanitize(name string) string {
	b := []byte(name)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			b[i] = '_'
		}
	}
	return string(b)
}

func float32bits(f float32) uint32     { return math.Float32bits(f) }
func float32frombits(u uint32) float32 { return math.Float32frombits(u) }

// syncGrace waits until Mix cannot still hold a dropped session. Mix runs once
// per period (a few ms) and never blocks on a client.
func syncGrace() { time.Sleep(500 * time.Millisecond) }
