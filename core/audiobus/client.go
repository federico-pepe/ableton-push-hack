package audiobus

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	redialEvery  = time.Second
	writeTimeout = time.Second // a stalled mixer does not stall the hack longer
	spin         = 100 * time.Microsecond
)

// Client sends a hack's stereo audio to the mixer. It connects on its own and
// reconnects when the loopback service restarts or the card changes format.
// While there is no connection, Write sleeps one block and drops the audio, so
// the hack's render loop keeps its pace.
type Client struct {
	name   string
	socket string
	want   atomic.Int32
	rate   atomic.Int32 // last known card rate

	mu   sync.Mutex
	conn *connState // nil when not connected
	gen  atomic.Uint64
	stop chan struct{}
	wg   sync.WaitGroup
}

type connState struct {
	format Format
	pair   int
	dead   atomic.Bool

	mu     sync.RWMutex // Write holds R while it touches rg; teardown holds W
	rg     *ring
	closed bool
}

// put writes buf unless the ring is gone or full.
func (cs *connState) put(buf []float32) (done, gone bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if cs.closed {
		return false, true
	}
	return cs.rg.write(buf), false
}

func (cs *connState) teardown() {
	cs.dead.Store(true)
	cs.mu.Lock()
	if !cs.closed {
		cs.closed = true
		cs.rg.close()
	}
	cs.mu.Unlock()
}

// Open starts a client. pair is the wanted 0-based first channel, or -1 to
// take the saved or a free pair. It never blocks and never fails: it keeps
// trying to reach the mixer.
func Open(socket, name string, pair int) *Client {
	c := &Client{name: name, socket: socket, stop: make(chan struct{})}
	c.want.Store(int32(pair))
	c.wg.Add(1)
	go c.loop()
	return c
}

// SetPair asks for another pair; it takes effect on a fresh connection.
func (c *Client) SetPair(pair int) {
	c.want.Store(int32(pair))
	c.mu.Lock()
	if c.conn != nil {
		c.conn.dead.Store(true)
	}
	c.mu.Unlock()
}

// Connected says whether the mixer is taking audio now.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil && !c.conn.dead.Load()
}

// Format is what the card runs at, from the last connection. Zero until the
// first one.
func (c *Client) Format() Format {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return Format{}
	}
	return c.conn.format
}

// Pair is the pair the mixer gave us (-1 before the first connection).
func (c *Client) Pair() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return -1
	}
	return c.conn.pair
}

// Generation grows with every (re)connection. When it changes, read Format
// again: rate and block size may be new.
func (c *Client) Generation() uint64 { return c.gen.Load() }

// Close disconnects and stops redialling.
func (c *Client) Close() {
	close(c.stop)
	c.mu.Lock()
	if c.conn != nil {
		c.conn.dead.Store(true)
	}
	c.mu.Unlock()
	c.wg.Wait()
}

// Write queues one block of interleaved L,R float32. It waits while the ring
// is full; that wait is what paces the caller to the card. With no mixer it
// sleeps for the length of the block instead, so the pace stays the same.
func (c *Client) Write(buf []float32) error {
	c.mu.Lock()
	cs := c.conn
	c.mu.Unlock()
	if cs == nil || cs.dead.Load() {
		rate := int(c.rate.Load())
		if rate <= 0 {
			rate = 44100
		}
		time.Sleep(time.Duration(len(buf)/2) * time.Second / time.Duration(rate))
		return ErrNotConnected
	}
	deadline := time.Now().Add(writeTimeout)
	for {
		done, gone := cs.put(buf)
		if done {
			return nil
		}
		if gone || cs.dead.Load() {
			return ErrNotConnected
		}
		if time.Now().After(deadline) {
			return ErrStalled
		}
		time.Sleep(spin)
	}
}

// Errors from Write. Both mean "audio was dropped"; neither needs handling.
var (
	ErrNotConnected = errors.New("audiobus: not connected")
	ErrStalled      = errors.New("audiobus: mixer not reading")
)

func (c *Client) loop() {
	defer c.wg.Done()
	for {
		cs, conn := c.dial()
		if cs != nil {
			c.mu.Lock()
			c.conn = cs
			c.mu.Unlock()
			c.rate.Store(int32(cs.format.Rate))
			c.gen.Add(1)
			// The server never sends again; EOF means it dropped us.
			go func() {
				_, _ = bufio.NewReader(conn).Peek(1)
				cs.dead.Store(true)
			}()
			for !cs.dead.Load() {
				select {
				case <-c.stop:
					cs.dead.Store(true)
				case <-time.After(50 * time.Millisecond):
				}
			}
			conn.Close()
			c.mu.Lock()
			c.conn = nil
			c.mu.Unlock()
			cs.teardown()
		}
		select {
		case <-c.stop:
			return
		case <-time.After(redialEvery):
		}
	}
}

func (c *Client) dial() (*connState, net.Conn) {
	conn, err := net.DialTimeout("unix", c.socket, time.Second)
	if err != nil {
		return nil, nil
	}
	b, _ := json.Marshal(request{V: 1, Name: c.name, Pair: int(c.want.Load())})
	if _, err := conn.Write(append(b, '\n')); err != nil {
		conn.Close()
		return nil, nil
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	var rep reply
	if err != nil || json.Unmarshal(line, &rep) != nil || !rep.OK {
		conn.Close()
		return nil, nil
	}
	rg, err := openRing(rep.Ring)
	if err != nil {
		conn.Close()
		return nil, nil
	}
	return &connState{format: rep.Format, pair: rep.Pair, rg: rg}, conn
}
