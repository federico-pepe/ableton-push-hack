// abusrec: a test client for the audio bus inputs. It declares one input and
// records what the service sends to it into a WAV file. Use it to check a
// route on a Push without a hack that records.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/audiobus"
)

func main() {
	name := flag.String("name", "rec", "client name")
	input := flag.String("input", "in", "input name")
	secs := flag.Int("secs", 10, "seconds to record")
	out := flag.String("out", "rec.wav", "WAV file")
	dir := flag.String("dir", audiobus.DefaultDir, "bus folder")
	flag.Parse()

	c := audiobus.OpenWithInputs(audiobus.SocketPath(*dir), *name, -1, []string{*input})
	defer c.Close()
	for !c.Connected() || len(c.Inputs()) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	f := c.Format()
	log.Printf("connected: %d Hz, period %d, inputs %v", f.Rate, f.Period, c.Inputs())

	file, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	if err := writeHeader(file, f.Rate, 0); err != nil {
		log.Fatal(err)
	}
	if _, err := file.Seek(44, 0); err != nil { // WriteAt does not move the position
		log.Fatal(err)
	}

	buf := make([]float32, f.Period*2)
	pcm := make([]byte, f.Period*4)
	block := time.Duration(f.Period) * time.Second / time.Duration(f.Rate)
	start := time.Now()
	next := start
	var frames, real, silent int
	for time.Since(start) < time.Duration(*secs)*time.Second {
		next = next.Add(block)
		time.Sleep(time.Until(next))
		n, err := c.Read(0, buf)
		if err != nil {
			log.Printf("read: %v", err)
		}
		real += n
		if n < f.Period {
			silent++
		}
		for i, v := range buf {
			if v > 1 {
				v = 1
			} else if v < -1 {
				v = -1
			}
			binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(v*32767)))
		}
		if _, err := file.Write(pcm); err != nil {
			log.Fatal(err)
		}
		frames += f.Period
	}
	if err := writeHeader(file, f.Rate, frames); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %d frames to %s: %d frames of audio, %d periods short\n", frames, *out, real, silent)
}

// writeHeader writes (or rewrites) a 16-bit stereo PCM header at the start of f.
func writeHeader(f *os.File, rate, frames int) error {
	data := uint32(frames * 4)
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], 36+data)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 2)
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*4))
	binary.LittleEndian.PutUint16(h[32:], 4)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], data)
	_, err := f.WriteAt(h, 0)
	return err
}
