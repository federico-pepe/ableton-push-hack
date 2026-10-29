package main

// display_mirror.go — the screen in a browser, whatever is on it (Live's own
// UI included).
//
//	GET /api/display/mirror        a page that draws the screen into a canvas
//	GET /api/display/mirror/frame  one PNG of the newest frame
//
// Source is the hook's mirror file (core/display Mirror), not framebuf:
// framebuf only holds what push-manager wrote, so in passthrough it is stale.
//
// The page fetches one PNG at a time instead of reading an MJPEG stream: in
// Chrome the stream showed the newest frame late (about one screen change
// behind) on a screen that changes in bursts.
//
// Cost model: nothing runs with no viewer. Each request reads the hook's
// newest frame and encodes it at most once per hook frame, whoever asks.

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"strconv"
	"sync"
	"time"

	coredisplay "github.com/federico-pepe/ableton-push-hack/core/display"
)

// Lossless: JPEG's chroma subsampling blurs coloured text and edges, and a
// flat UI compresses well. BestSpeed keeps the encode cheap.
var mirrorPNG = png.Encoder{CompressionLevel: png.BestSpeed, BufferPool: &pngPool{}}

// pngPool keeps the encoder's working buffer between frames (no garbage per frame).
type pngPool struct{ b *png.EncoderBuffer }

func (p *pngPool) Get() *png.EncoderBuffer  { b := p.b; p.b = nil; return b }
func (p *pngPool) Put(b *png.EncoderBuffer) { p.b = b }

var mirrorSrc = &coredisplay.Mirror{}

// mirrorMinGap is the least time between two reads of the hook's frame: the
// page gets at most 15 pictures a second. A poll inside the gap is answered
// from the cache, with no copy and no encode.
const mirrorMinGap = 66 * time.Millisecond

// mirrorScale: whole-number nearest-neighbour upscale done on the Push (1 = off).
const mirrorScale = 2

// upscale copies src into dst, every pixel becoming scale x scale pixels.
func upscale(dst, src *image.RGBA, scale int) {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	for y := 0; y < h; y++ {
		in := src.Pix[y*src.Stride : y*src.Stride+w*4]
		first := dst.Pix[y*scale*dst.Stride : y*scale*dst.Stride+w*scale*4]
		for x := 0; x < w; x++ {
			for k := 0; k < scale; k++ {
				copy(first[(x*scale+k)*4:], in[x*4:x*4+4])
			}
		}
		for k := 1; k < scale; k++ {
			copy(dst.Pix[(y*scale+k)*dst.Stride:], first)
		}
	}
}

// snap is the scratch and cache behind /api/display/mirror/frame.
// hook is the last hook frame number looked at; ver is the hook number at
// which the picture last changed. The hook sends the same picture many times
// a second, so ver is what a viewer waits on, not hook.
var snap struct {
	sync.Mutex
	raw, prev []byte // prev holds the pixels behind png
	img, big  *image.RGBA
	buf       bytes.Buffer
	hook, ver uint32
	at        time.Time // last read of the hook's frame
	png       []byte
}

// mirrorSnapshot returns the newest picture as a PNG and its version. It
// reads and encodes only when the hook has sent a frame since the last call,
// and encodes only when the pixels differ.
func mirrorSnapshot() (pic []byte, ver uint32, ok bool) {
	snap.Lock()
	defer snap.Unlock()
	if snap.raw == nil {
		snap.raw = make([]byte, coredisplay.FrameBytes)
		snap.prev = make([]byte, coredisplay.FrameBytes)
		snap.img = image.NewRGBA(image.Rect(0, 0, coredisplay.VisW, coredisplay.VisH))
		snap.big = image.NewRGBA(image.Rect(0, 0, coredisplay.VisW*mirrorScale, coredisplay.VisH*mirrorScale))
	}
	if snap.png != nil && time.Since(snap.at) < mirrorMinGap {
		return snap.png, snap.ver, true
	}
	if cur := mirrorSrc.Seq(); cur != 0 && snap.png != nil && cur == snap.hook {
		return snap.png, snap.ver, true // no new hook frame: no copy, no encode
	}
	seq, ok := mirrorSrc.Read(snap.raw)
	if !ok {
		return nil, 0, false
	}
	snap.hook = seq
	snap.at = time.Now()
	if snap.png != nil && bytes.Equal(snap.raw, snap.prev) {
		return snap.png, snap.ver, true // same picture sent again
	}
	coredisplay.DecodeRGBA(snap.raw, snap.img)
	out := snap.img
	if mirrorScale > 1 {
		upscale(snap.big, snap.img, mirrorScale)
		out = snap.big
	}
	snap.buf.Reset()
	if err := mirrorPNG.Encode(&snap.buf, out); err != nil {
		return nil, 0, false
	}
	snap.png = append([]byte(nil), snap.buf.Bytes()...)
	snap.raw, snap.prev = snap.prev, snap.raw
	snap.ver = seq
	return snap.png, snap.ver, true
}

// mirrorAvailable maps the hook's mirror file and answers 503 if it is not
// there: push_hook.so older than this feature, or Push3 not restarted since
// it was installed.
func mirrorAvailable(w http.ResponseWriter) bool {
	mirrorSrc.Ensure()
	if mirrorSrc.Connected() {
		return true
	}
	http.Error(w, "display mirror not available (needs the newer push_hook.so, loaded by a Push3 restart)", http.StatusServiceUnavailable)
	return false
}

// GET /api/display/mirror
func handleDisplayMirror(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if !mirrorAvailable(w) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(mirrorViewHTML)) //nolint:errcheck
}

// GET /api/display/mirror/frame[?since=<seq>]
// One PNG of the newest frame, its version in X-Mirror-Seq. With since equal
// to the newest version it answers 204 and sends nothing.
func handleDisplayMirrorFrame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if !mirrorAvailable(w) {
		return
	}
	pic, ver, ok := mirrorSnapshot()
	if !ok {
		http.Error(w, "no mirror frame yet", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Mirror-Seq", strconv.FormatUint(uint64(ver), 10))
	if since, err := strconv.ParseUint(r.URL.Query().Get("since"), 10, 32); err == nil && uint32(since) == ver {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(pic) //nolint:errcheck
}

const mirrorViewHTML = `<!doctype html><meta charset=utf-8><title>Push mirror</title>
<style>html,body{margin:0;background:#111;height:100%}canvas{width:960px;max-width:100%;image-rendering:auto;display:block}</style>
<canvas id=c width=1920 height=320></canvas>
<script>
const ctx=document.getElementById('c').getContext('2d');let since=0;
const wait=ms=>new Promise(r=>setTimeout(r,ms));
(async function loop(){for(;;){try{
 const r=await fetch('/api/display/mirror/frame?since='+since,{cache:'no-store'});
 if(r.status===200){since=r.headers.get('X-Mirror-Seq');ctx.drawImage(await createImageBitmap(await r.blob()),0,0);}
 else await wait(r.status===204?16:500);
}catch(e){await wait(500);}}})();
</script>`
