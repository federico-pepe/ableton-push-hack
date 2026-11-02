// abustone: a test client for the audio bus. Plays a sine on its own pair.
// Use it to check the mixer on a Push without any hack running.
package main

import (
	"flag"
	"log"
	"math"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/audiobus"
)

func main() {
	name := flag.String("name", "tone", "client name")
	freq := flag.Float64("freq", 440, "sine frequency in Hz")
	amp := flag.Float64("amp", 0.1, "amplitude 0..1")
	pair := flag.Int("pair", -1, "first channel of the pair, 0-based; -1 = saved or free")
	dir := flag.String("dir", audiobus.DefaultDir, "bus folder")
	flag.Parse()

	c := audiobus.Open(audiobus.SocketPath(*dir), *name, *pair)
	defer c.Close()
	var (
		phase float64
		gen   uint64
		buf   []float32
		f     audiobus.Format
		last  = time.Now()
	)
	for {
		if !c.Connected() {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if g := c.Generation(); g != gen {
			gen, f = g, c.Format()
			buf = make([]float32, f.Period*2)
			log.Printf("connected: pair %d, %d Hz, period %d", c.Pair(), f.Rate, f.Period)
		}
		step := 2 * math.Pi * *freq / float64(f.Rate)
		for i := 0; i < f.Period; i++ {
			v := float32(*amp * math.Sin(phase))
			buf[i*2], buf[i*2+1] = v, v
			if phase += step; phase > 2*math.Pi {
				phase -= 2 * math.Pi
			}
		}
		_ = c.Write(buf)
		if time.Since(last) > 5*time.Second {
			last = time.Now()
			log.Printf("still playing on pair %d", c.Pair())
		}
	}
}
