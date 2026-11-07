package audiobus

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// Input is one audio input of a client. The service writes audio into it and
// the hack reads it with Client.Read. It is the other direction of the bus:
// a hack can record a channel of Live, or a channel of another hack.
type Input struct {
	Client string // the client that owns the input
	Name   string // the name the client gave it

	rg   *ring
	path string
	peak atomic.Uint32 // float32 bits, peak of the last block written
	full atomic.Uint64 // blocks refused because the hack was not reading
}

// Free is how many frames Write can take now.
func (in *Input) Free() int { return in.rg.free() }

// Fill is how many frames wait for the hack to read them.
func (in *Input) Fill() int { return in.rg.avail() }

// Peak is the peak of the last block that Write took, 0..1.
func (in *Input) Peak() float32 { return float32frombits(in.peak.Load()) }

// Refused counts the blocks that Write refused because the ring was full.
func (in *Input) Refused() uint64 { return in.full.Load() }

// Write adds one block of interleaved L,R, or nothing. It never blocks and
// allocates nothing. Call it from one goroutine only.
func (in *Input) Write(buf []float32) bool {
	if !in.rg.write(buf) {
		in.full.Add(1)
		return false
	}
	var pk float32
	for _, v := range buf {
		if v < 0 {
			v = -v
		}
		if v > pk {
			pk = v
		}
	}
	in.peak.Store(float32bits(pk))
	return true
}

// Inputs lists the inputs of every connected client. The slice is a copy. An
// Input stays valid for about half a second after its client leaves, as the
// rings of the mixer do, so a caller that works through a list in one period
// is safe.
func (s *Server) Inputs() []*Input {
	var out []*Input
	for _, ss := range *s.sessions.Load() {
		out = append(out, ss.ins...)
	}
	return out
}

// checkInputs says why a list of input names is not allowed, or "".
func checkInputs(names []string) string {
	if len(names) > maxInputs {
		return fmt.Sprintf("at most %d inputs", maxInputs)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" || len(n) > maxInputName || seen[n] {
			return "bad input name"
		}
		seen[n] = true
	}
	return ""
}

// makeInputs creates one ring for each input the client asked for. Caller
// holds s.mu. On error it removes what it made.
func (s *Server) makeInputs(rq request, f Format, mainPath string) ([]*Input, []string, error) {
	if len(rq.Inputs) == 0 {
		return nil, nil, nil
	}
	capFrames := pow2(f.Period * inputPeriods)
	base := mainPath[:len(mainPath)-len(filepath.Ext(mainPath))]
	var ins []*Input
	var paths []string
	for i, name := range rq.Inputs {
		path := fmt.Sprintf("%s-in%d.ring", base, i)
		rg, err := createRing(path, capFrames)
		if err != nil {
			for _, in := range ins {
				in.rg.close()
				os.Remove(in.path)
			}
			return nil, nil, err
		}
		ins = append(ins, &Input{Client: rq.Name, Name: name, rg: rg, path: path})
		paths = append(paths, path)
	}
	return ins, paths, nil
}
