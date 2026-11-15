package livedevice

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math"
	mathrand "math/rand/v2"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Provider makes the engine side of devices. New runs off the audio path,
// when Live creates an instance of a class (a device loaded, a Set opened).
type Provider interface {
	New(c *Class) (Device, error)
}

// Device is one instance in Live, on the hack's side.
type Device interface {
	// Process runs one block on the device's realtime thread (SCHED_FIFO,
	// Live's own audio priority). It must not block, allocate much, log or
	// touch files: Live waits for it, at most half a block, then plays
	// silence.
	Process(b *Block)
	// State is what Live saves in the Set; SetState gets it back. Any text.
	State() (string, error)
	SetState(state string) error
	// Close frees the device. Process is never called again.
	Close()
}

// Statser is an optional extra for /api/devices.
type Statser interface {
	Stats() map[string]any
}

// Config of a Server.
type Config struct {
	// Name is the hack's id: the shared memory files are
	// /dev/shm/<Name>-dev-<n>.
	Name string
	// Socket is the control socket the plugin talks to (LiveDevice.json's
	// "socket").
	Socket string
	// Priority of the realtime threads; 0: 70, Live's own audio priority.
	Priority int
	Logf     func(format string, args ...any)
}

// Server runs a hack's devices.
type Server struct {
	cfg  Config
	prov Provider

	mu      sync.Mutex
	classes map[string]*Class
	list    map[int]*device
	next    int
	ln      net.Listener
}

type device struct {
	id    int
	class *Class
	dev   Device
	mem   []byte
	path  string
	quit  atomic.Bool
	done  chan struct{}

	blocks, notesIn, notesOut atomic.Uint64
	inPeak, outPeak           atomic.Uint32 // float32 bits: the last second's peak
}

// NewServer makes a server for these classes. Call Listen to start it.
func NewServer(cfg Config, p Provider, classes []*Class) *Server {
	if cfg.Priority == 0 {
		cfg.Priority = 70
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	// Device ids start at a random point, never at 1: a plugin in Live that
	// has not yet noticed the service restarted still calls with its old id,
	// and from 1 again that id could be another device's now (a state read
	// that way restored one module's patch into another's device, 2026-11-15).
	// Below 1<<30: the plugin keeps it in an int.
	s := &Server{cfg: cfg, prov: p, classes: map[string]*Class{}, list: map[int]*device{}, next: 1 + mathrand.IntN(1<<30)}
	for _, c := range classes {
		s.classes[c.UID] = c
	}
	return s
}

// Listen opens the control socket (mode 0666: Live runs as ableton) and
// serves it in the background.
func (s *Server) Listen() error {
	os.Remove(s.cfg.Socket)
	ln, err := net.Listen("unix", s.cfg.Socket)
	if err != nil {
		return err
	}
	os.Chmod(s.cfg.Socket, 0o666)
	s.ln = ln
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.conn(c)
		}
	}()
	s.cfg.Logf("livedevice: %s listening on %s", s.cfg.Name, s.cfg.Socket)
	return nil
}

// Close stops listening and closes every device.
func (s *Server) Close() {
	if s.ln != nil {
		s.ln.Close()
	}
	s.mu.Lock()
	var ids []int
	for id := range s.list {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.destroy(id)
	}
}

func (s *Server) create(uid string) (*device, error) {
	s.mu.Lock()
	c, ok := s.classes[uid]
	id := s.next
	s.next++
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown class %s", uid)
	}
	dev, err := s.prov.New(c)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/dev/shm/%s-dev-%d", s.cfg.Name, id)
	mem, err := shmCreate(path, ipcSize)
	if err != nil {
		dev.Close()
		return nil, err
	}
	b := &Block{mem: mem}
	*b.u32(offMagic) = ipcMagic
	*b.u32(offVersion) = ipcVersion
	d := &device{id: id, class: c, dev: dev, mem: mem, path: path, done: make(chan struct{})}
	s.mu.Lock()
	s.list[id] = d
	s.mu.Unlock()
	go s.run(d)
	s.cfg.Logf("livedevice: %d %s", id, c.Name)
	return d, nil
}

func (s *Server) destroy(id int) {
	s.mu.Lock()
	d := s.list[id]
	delete(s.list, id)
	s.mu.Unlock()
	if d == nil {
		return
	}
	d.quit.Store(true)
	futexWake((&Block{mem: d.mem}).u32(offReq))
	<-d.done
	d.dev.Close()
	shmFree(d.mem, d.path)
	s.cfg.Logf("livedevice: %d %s closed", id, d.class.Name)
}

func (s *Server) get(id int) *device {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list[id]
}

// run serves one device's blocks as the plugin asks for them.
func (s *Server) run(d *device) {
	defer close(d.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	setRealtime(s.cfg.Priority)
	b := &Block{mem: d.mem, class: d.class}
	req, done := b.u32(offReq), b.u32(offDone)
	alive := b.u32(offAlive)
	last := atomic.LoadUint32(req)
	for !d.quit.Load() {
		r := atomic.LoadUint32(req)
		if r == last {
			futexWait(req, r, 200*time.Millisecond)
			atomic.AddUint32(alive, 1) // the plugin's sign that we are here
			continue
		}
		last = r
		b.load()
		d.dev.Process(b)
		b.finish()
		d.blocks.Add(1)
		d.notesIn.Add(uint64(b.notesIn))
		d.notesOut.Add(uint64(b.notesOut))
		if d.class.Kind == Effect {
			d.peak(&d.inPeak, b.In)
		}
		d.peak(&d.outPeak, b.Out)
		atomic.StoreUint32(done, r)
		futexWake(done)
	}
}

// peak keeps the loudest sample of about the last second (344 blocks of
// 128): one atomic store per block.
func (d *device) peak(dst *atomic.Uint32, bufs [2][]float32) {
	var p float32
	for _, buf := range bufs {
		for _, v := range buf {
			p = max(p, float32(math.Abs(float64(v))))
		}
	}
	old := math.Float32frombits(dst.Load())
	if d.blocks.Load()%344 == 0 {
		old = 0
	}
	dst.Store(math.Float32bits(max(old, p)))
}

// Stats is one device as /api/devices lists it.
func (s *Server) Stats() []map[string]any {
	s.mu.Lock()
	var out []map[string]any
	for _, d := range s.list {
		m := map[string]any{"id": d.id, "name": d.class.Name, "kind": d.class.Kind, "ref": d.class.Ref,
			"blocks": d.blocks.Load(), "notes_in": d.notesIn.Load(), "notes_out": d.notesOut.Load(),
			"in_peak": math.Float32frombits(d.inPeak.Load()), "out_peak": math.Float32frombits(d.outPeak.Load()),
			"late": atomic.LoadUint32((&Block{mem: d.mem}).u32(offLate))}
		if st, ok := d.dev.(Statser); ok {
			for k, v := range st.Stats() {
				m[k] = v
			}
		}
		out = append(out, m)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(int) < out[j]["id"].(int) })
	return out
}

// Handler serves GET /api/devices (mount it where the hack likes).
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := s.Stats()
		if out == nil {
			out = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"devices": out})
	})
}

// conn: JSON lines from one plugin process. A connection that closes takes
// its devices with it (Live quit or crashed).
func (s *Server) conn(c net.Conn) {
	defer c.Close()
	mine := map[int]bool{}
	defer func() {
		for id := range mine {
			s.destroy(id)
		}
	}()
	r := bufio.NewReaderSize(c, 1<<20)
	enc := json.NewEncoder(c)
	fail := func(err string) { enc.Encode(map[string]any{"ok": false, "error": err}) }
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req struct {
			Op    string `json:"op"`
			UID   string `json:"uid"`
			ID    int    `json:"id"`
			State string `json:"state"`
		}
		if json.Unmarshal(line, &req) != nil {
			fail("bad request")
			continue
		}
		switch req.Op {
		case "create":
			d, err := s.create(req.UID)
			if err != nil {
				fail(err.Error())
				continue
			}
			mine[d.id] = true
			enc.Encode(map[string]any{"ok": true, "id": d.id, "shm": d.path})
		case "destroy":
			if mine[req.ID] {
				s.destroy(req.ID)
				delete(mine, req.ID)
			}
			enc.Encode(map[string]any{"ok": true})
		case "get_state", "set_state":
			// any connection may read a state (tests, tools); only the
			// plugin that made the device may change it
			d := s.get(req.ID)
			if d == nil || req.Op == "set_state" && !mine[req.ID] {
				fail("no such device")
				continue
			}
			if req.Op == "get_state" {
				st, err := d.dev.State()
				if err != nil {
					fail(err.Error())
					continue
				}
				enc.Encode(map[string]any{"ok": true, "state": st})
				continue
			}
			if err := d.dev.SetState(req.State); err != nil {
				fail(err.Error())
				continue
			}
			enc.Encode(map[string]any{"ok": true})
		default:
			fail("unknown op " + req.Op)
		}
	}
}
