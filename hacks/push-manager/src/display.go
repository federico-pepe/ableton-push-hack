package main

// display.go — shared-memory bridge between push-manager and push_hook.so
//
// The hook creates /data/push-hack/hacks/push-display/framebuf on first load.
// Push-manager mmaps it to read mode / write custom pixel frames.
//
// Shared memory layout (must match push_hook.c):
//   offset  0: uint32 magic      (0x50555348 "PUSH")
//   offset  4: uint32 version    (1)
//   offset  8: uint32 mode       (0=passthrough, 1=bar, 2=takeover)
//   offset 12: uint32 frame_seq  (push-manager increments on each image write)
//   offset 16: [655360]byte      raw BGR565 pixels, no XOR, row-major

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"log"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Display geometry (960×160, stride 1024, frame sent twice) lives in
// core/push3 as the shared source of truth — see push3.VisW/VisH/Stride/
// FrameBytes/TotalBytes.
const (
	dispVisW   = push3.VisW
	dispH      = push3.VisH
	dispStride = push3.Stride
	dispFrameB = push3.FrameBytes // 327680 bytes per single frame
	dispBytes  = push3.TotalBytes // 655360 bytes total (sent twice)

	shmFile    = "/data/push-hack/hacks/push-display/framebuf"
	shmMagic   = uint32(0x50555348)
	shmTotalSz = 16 + dispBytes

	// mode values — mirror push_hook.c
	ModePassthrough = uint32(0)
	ModeBar         = uint32(1)
	ModeTakeover    = uint32(2)

	// shm byte offsets
	offMagic    = 0
	offVersion  = 4
	offMode     = 8
	offFrameSeq = 12
	offPixels   = 16
)

var (
	shmBuf         []byte    // mmap'd bytes; nil if unavailable
	shmMu          sync.Mutex
	shmLastAttempt time.Time

	startupSplashOnce sync.Once
)

// tryOpenShm attempts to mmap the framebuf file.
// Called from initDisplayShm (startup) and lazily on status requests.
// Caller must hold shmMu.
func tryOpenShm() {
	f, err := os.OpenFile(shmFile, os.O_RDWR, 0664)
	if err != nil {
		return // hook not running yet — silent
	}
	defer f.Close()

	data, err := syscall.Mmap(int(f.Fd()), 0, shmTotalSz,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		log.Printf("display shm: mmap: %v", err)
		return
	}

	magic := binary.LittleEndian.Uint32(data[offMagic:])
	if magic != shmMagic {
		log.Printf("display shm: bad magic 0x%08X (hook may not be loaded yet)", magic)
		syscall.Munmap(data) //nolint
		return
	}

	shmBuf = data
	seq := binary.LittleEndian.Uint32(data[offFrameSeq:])
	if seq > 0 {
		// Push-manager was running before this connect; reset any stale takeover.
		if binary.LittleEndian.Uint32(data[offMode:]) == ModeTakeover {
			binary.LittleEndian.PutUint32(data[offMode:], ModePassthrough)
			log.Printf("display shm reconnected: cleared stale mode=2 (seq=%d)", seq)
		} else {
			log.Printf("display shm reconnected: seq=%d", seq)
		}
	} else {
		// frame_seq==0: fresh hook load — hook wrote startup splash, mode=2.
		log.Printf("display shm connected: fresh hook (seq=0), startup splash pending")
	}
	// Startup splash fires once per push-manager process (sync.Once).
	go scheduleStartupSplash()
}

func initDisplayShm() {
	shmMu.Lock()
	defer shmMu.Unlock()
	shmLastAttempt = time.Now()
	tryOpenShm()
}

// ensureShm reconnects if disconnected, rate-limited to once per 5 s.
func ensureShm() {
	shmMu.Lock()
	defer shmMu.Unlock()
	if shmBuf != nil {
		return
	}
	if time.Since(shmLastAttempt) < 5*time.Second {
		return
	}
	shmLastAttempt = time.Now()
	tryOpenShm()
}

func shmGetMode() uint32 {
	shmMu.Lock()
	defer shmMu.Unlock()
	if shmBuf == nil {
		return ModePassthrough
	}
	return binary.LittleEndian.Uint32(shmBuf[offMode:])
}

// shmReadFrame copies the current framebuf (first of the two duplicated halves)
// and the current mode out of shm. Returns ok=false if the hook isn't connected.
func shmReadFrame() (px []byte, mode uint32, ok bool) {
	shmMu.Lock()
	defer shmMu.Unlock()
	if shmBuf == nil {
		return nil, ModePassthrough, false
	}
	px = make([]byte, dispFrameB)
	copy(px, shmBuf[offPixels:offPixels+dispFrameB])
	mode = binary.LittleEndian.Uint32(shmBuf[offMode:])
	return px, mode, true
}

func shmSetMode(mode uint32) error {
	shmMu.Lock()
	defer shmMu.Unlock()
	if shmBuf == nil {
		return fmt.Errorf("display hook not connected (is push-display deployed and Push3 running?)")
	}
	binary.LittleEndian.PutUint32(shmBuf[offMode:], mode)
	return nil
}

func shmWritePixels(pixels []byte) error {
	if len(pixels) != dispBytes {
		return fmt.Errorf("expected %d bytes, got %d", dispBytes, len(pixels))
	}
	shmMu.Lock()
	defer shmMu.Unlock()
	if shmBuf == nil {
		return fmt.Errorf("display hook not connected")
	}
	copy(shmBuf[offPixels:], pixels)
	seq := binary.LittleEndian.Uint32(shmBuf[offFrameSeq:]) + 1
	binary.LittleEndian.PutUint32(shmBuf[offFrameSeq:], seq)
	return nil
}

// imageToBGR565 scales img to 960×160 (Push 2/3 visible area), writes into
// a 1024-pixel-stride framebuffer (64 padding pixels per row), and duplicates
// the result into both halves of the 655360-byte shm buffer (Push 3 sends
// the same frame twice per display update).
// Returns raw BGR565 little-endian bytes, no XOR — hook applies XOR.
func imageToBGR565(img image.Image) []byte {
	b := img.Bounds()
	srcW := b.Max.X - b.Min.X
	srcH := b.Max.Y - b.Min.Y
	pixels := make([]byte, dispBytes)

	for y := 0; y < dispH; y++ {
		for x := 0; x < dispVisW; x++ {
			sx := b.Min.X + x*srcW/dispVisW
			sy := b.Min.Y + y*srcH/dispH
			r16, g16, b16, _ := img.At(sx, sy).RGBA() // 0–65535
			b5 := uint16(b16 >> 11)
			g6 := uint16(g16 >> 10)
			r5 := uint16(r16 >> 11)
			v := (b5 << 11) | (g6 << 5) | r5
			off := (y*dispStride + x) * 2
			lo, hi := byte(v), byte(v>>8)
			pixels[off] = lo
			pixels[off+1] = hi
			// duplicate into second frame (Push 3 sends frame twice)
			pixels[dispFrameB+off] = lo
			pixels[dispFrameB+off+1] = hi
		}
	}
	return pixels
}

// bgr565ToImage is the reverse of imageToBGR565: it decodes one framebuf frame
// (dispFrameB bytes, 1024-pixel stride) into a 960×160 NRGBA image, skipping the
// 64-pixel-per-row stride padding. Input is raw BGR565 little-endian, no XOR.
func bgr565ToImage(px []byte) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, dispVisW, dispH))
	for y := 0; y < dispH; y++ {
		for x := 0; x < dispVisW; x++ {
			off := (y*dispStride + x) * 2
			v := uint16(px[off]) | uint16(px[off+1])<<8
			b5 := (v >> 11) & 0x1f
			g6 := (v >> 5) & 0x3f
			r5 := v & 0x1f
			img.SetNRGBA(x, y, color.NRGBA{
				R: byte(r5<<3 | r5>>2),
				G: byte(g6<<2 | g6>>4),
				B: byte(b5<<3 | b5>>2),
				A: 255,
			})
		}
	}
	return img
}

// ── HTTP handlers ─────────────────────────────────────────────────────────

// testPattern returns a BGR565 frame with vertical colour bars at known pixel
// positions within the 960-pixel visible width. Both frame halves populated.
func testPattern() []byte {
	pixels := make([]byte, dispBytes)
	cols := []struct {
		x    int
		r, g, b uint8
	}{
		{0,   255, 255, 255}, // white  — left edge
		{120, 255, 0,   0},   // red
		{240, 0,   255, 0},   // green
		{360, 0,   255, 255}, // cyan
		{480, 0,   0,   255}, // blue
		{600, 255, 0,   255}, // magenta
		{720, 255, 255, 0},   // yellow
		{840, 255, 128, 0},   // orange
		{959, 255, 255, 255}, // white  — right edge
	}
	for _, c := range cols {
		if c.x >= dispVisW {
			continue
		}
		b5 := uint16(c.b >> 3)
		g6 := uint16(c.g >> 2)
		r5 := uint16(c.r >> 3)
		v := (b5 << 11) | (g6 << 5) | r5
		lo, hi := byte(v), byte(v>>8)
		for y := 0; y < dispH; y++ {
			off := (y*dispStride + c.x) * 2
			pixels[off] = lo
			pixels[off+1] = hi
			pixels[dispFrameB+off] = lo
			pixels[dispFrameB+off+1] = hi
		}
	}
	return pixels
}

// POST /api/display/testpattern — paint calibration bars, auto-enable takeover
func handleDisplayTestPattern(w http.ResponseWriter, r *http.Request) {
	pixels := testPattern()
	if err := shmWritePixels(pixels); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err := shmSetMode(ModeTakeover); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"ok":   true,
		"desc": "white@0 red@240 green@480 cyan@640 blue@800 magenta@960 yellow@1120 white@1279",
	})
}

// GET /api/display/status
func handleDisplayStatus(w http.ResponseWriter, r *http.Request) {
	ensureShm()
	shmMu.Lock()
	connected := shmBuf != nil
	mode := uint32(ModePassthrough)
	frameSeq := uint32(0)
	if connected {
		mode = binary.LittleEndian.Uint32(shmBuf[offMode:])
		frameSeq = binary.LittleEndian.Uint32(shmBuf[offFrameSeq:])
	}
	shmMu.Unlock()

	jsonResponse(w, map[string]interface{}{
		"connected": connected,
		"mode":      mode,
		"frame_seq": frameSeq,
		"width":     dispVisW,
		"height":    dispH,
	})
}

// GET /api/display/screenshot
// Encodes the current framebuf as a PNG. Only reflects what push-manager last
// rendered (Shadow UI, OSD, test pattern, uploaded image) — in passthrough the
// framebuf is stale, so the X-Display-Mode header lets the client warn.
func handleDisplayScreenshot(w http.ResponseWriter, r *http.Request) {
	ensureShm()
	px, mode, ok := shmReadFrame()
	if !ok {
		http.Error(w, "display hook not connected (is push-display deployed and Push3 running?)", http.StatusServiceUnavailable)
		return
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, bgr565ToImage(px)); err != nil {
		http.Error(w, "encode png: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", `attachment; filename="push-screenshot.png"`)
	w.Header().Set("X-Display-Mode", fmt.Sprintf("%d", mode))
	w.Write(buf.Bytes()) //nolint:errcheck
}

// POST /api/display/mode  body: {"mode": 0|1|2}
func handleDisplayMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode uint32 `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if body.Mode > 2 {
		http.Error(w, "mode must be 0 (off), 1 (bar) or 2 (custom)", http.StatusBadRequest)
		return
	}
	// When entering takeover mode (2) with no content yet, clear framebuf to
	// black so stale pixels from a previous session don't show.
	if body.Mode == ModeTakeover && shmGetMode() != ModeTakeover {
		black := make([]byte, dispFrameB*2) // two frame halves, all zeros = black
		if err := shmWritePixels(black); err != nil {
			log.Printf("display: clear to black: %v", err)
		}
	}
	if err := shmSetMode(body.Mode); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	jsonResponse(w, map[string]interface{}{"mode": body.Mode})
}

// POST /api/display/image  multipart field "image" = PNG or JPEG
// Scales image to 1280×256, converts to BGR565, writes to shm.
// Also auto-sets mode to 2 (takeover) if currently in passthrough/bar.
func handleDisplayImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
		return
	}
	f, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "image field required", http.StatusBadRequest)
		return
	}
	defer f.Close()

	img, format, err := image.Decode(f)
	if err != nil {
		http.Error(w, "decode image: "+err.Error(), http.StatusBadRequest)
		return
	}

	pixels := imageToBGR565(img)
	if err := shmWritePixels(pixels); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// NOTE: mode is NOT auto-set here. The caller is responsible for setting
	// mode=2 (takeover) before sending frames. This prevents streaming POSTs
	// from overriding an explicit mode=0 (Off) set by the user.
	jsonResponse(w, map[string]interface{}{
		"ok":     true,
		"format": format,
		"size":   fmt.Sprintf("%dx%d", dispVisW, dispH),
		"mode":   shmGetMode(),
	})
}

// ── Startup splash ────────────────────────────────────────────────────────
//
// Called (in a goroutine) on the first successful shm connection.
// Shows "Push Hack loaded..." for splashDuration then restores passthrough.
// sync.Once — fires at most once per push-manager process.
//
// Approach C handoff: push_hook.c constructor writes its own identical static
// frame at push3 start; push-manager connects ~1-2s later and overwrites,
// then restores mode=0 after splashDuration.

const splashDuration = 3 * time.Second

func scheduleStartupSplash() {
	startupSplashOnce.Do(func() {
		time.Sleep(200 * time.Millisecond)
		pixels := renderOSDFrame("Push Hack loaded...")
		if err := shmWritePixels(pixels); err != nil {
			log.Printf("startup splash: %v (push-display not loaded, skipping)", err)
			return
		}
		if err := shmSetMode(ModeTakeover); err != nil {
			log.Printf("startup splash: set mode: %v", err)
			return
		}
		log.Printf("startup splash: showing for %v", splashDuration)
		time.Sleep(splashDuration)
		shmMu.Lock()
		if shmBuf != nil && binary.LittleEndian.Uint32(shmBuf[offMode:]) == ModeTakeover {
			binary.LittleEndian.PutUint32(shmBuf[offMode:], ModePassthrough)
			log.Printf("startup splash: restored mode=passthrough")
		}
		shmMu.Unlock()
	})
}

// ── OSD (on-screen display) ───────────────────────────────────────────────
//
// Accepts OSDRequest via osdCh. Renders text centered on a black 960×160 frame
// at 2× scale (basicfont 7×13 → 14×26 effective), writes to framebuf, sets
// mode=2 (takeover), then restores the prior mode after Duration.
//
// Rapid requests cancel any pending restore — only the latest timer restores.
// If framebuf is not connected (push-display not deployed), logs and skips silently.

// OSDLine is one line of text in a multi-line OSD frame.
// Scale is a pixel scaling factor applied to basicfont.Face7x13 (7×13 px):
// Scale=1 → 7×13 per glyph, Scale=2 → 14×26, Scale=3 → 21×39.
type OSDLine struct {
	Text  string
	Scale int
}

// OSDRequest asks the OSD goroutine to show text for the given duration.
// If Lines is non-nil it takes precedence over Text (multi-line render).
// OnComplete, if set, is called after the OSD duration elapses and the prior
// display mode is restored — useful for sequencing work that must start only
// after OSD finishes (e.g. shadow UI, which also writes the framebuf).
type OSDRequest struct {
	Text       string
	Lines      []OSDLine
	Duration   time.Duration
	OnComplete func()
}

// osdCh is the send channel for OSD requests. Buffered to avoid blocking callers.
var osdCh = make(chan OSDRequest, 8)

// startOSD launches the OSD worker goroutine. Call once from main after initDisplayShm.
func startOSD() {
	go osdWorker()
}

func osdWorker() {
	var (
		mu        sync.Mutex
		gen       uint64
		savedMode uint32
		hasActive bool
	)

	for req := range osdCh {
		ensureShm()

		mu.Lock()
		gen++
		myGen := gen
		if !hasActive {
			savedMode = shmGetMode()
			hasActive = true
		}
		restoreMode := savedMode
		mu.Unlock()

		var pixels []byte
		if len(req.Lines) > 0 {
			pixels = renderOSDFrameLines(req.Lines)
		} else {
			pixels = renderOSDFrame(req.Text)
		}
		if err := shmWritePixels(pixels); err != nil {
			log.Printf("osd: shm not connected, skipping OSD (%v)", err)
			mu.Lock()
			hasActive = false
			mu.Unlock()
			continue
		}
		if err := shmSetMode(ModeTakeover); err != nil {
			log.Printf("osd: set mode: %v", err)
			mu.Lock()
			hasActive = false
			mu.Unlock()
			continue
		}

		dur := req.Duration
		onComplete := req.OnComplete
		go func() {
			time.Sleep(dur)
			mu.Lock()
			if gen != myGen {
				// superseded by a newer OSD request
				mu.Unlock()
				return
			}
			hasActive = false
			mu.Unlock()
			if err := shmSetMode(restoreMode); err != nil {
				log.Printf("osd: restore mode: %v", err)
			}
			if onComplete != nil {
				onComplete()
			}
		}()
	}
}

// renderOSDFrame draws text centered on a black 960×160 image at 2× scale.
// Uses basicfont.Face7x13 (7×13 px per glyph). Returns BGR565 frame bytes.
func renderOSDFrame(text string) []byte {
	const scale = 2
	face := basicfont.Face7x13

	// Measure bounding box of rendered text
	bounds, _ := font.BoundString(face, text)

	smallW := (-bounds.Min.X + bounds.Max.X).Round()
	smallH := (-bounds.Min.Y + bounds.Max.Y).Round()
	if smallW <= 0 || smallH <= 0 {
		// Empty text: return black frame
		return make([]byte, dispBytes)
	}

	// Render at 1× on a small NRGBA canvas
	small := image.NewNRGBA(image.Rect(0, 0, smallW, smallH))
	draw.Draw(small, small.Bounds(), image.Black, image.Point{}, draw.Src)
	d := font.Drawer{
		Dst:  small,
		Src:  image.White,
		Face: face,
		Dot:  fixed.Point26_6{X: -bounds.Min.X, Y: -bounds.Min.Y},
	}
	d.DrawString(text)

	// Compose 2× scaled canvas centered on 960×160
	scaledW := smallW * scale
	scaledH := smallH * scale
	startX := (dispVisW - scaledW) / 2
	startY := (dispH - scaledH) / 2

	canvas := image.NewNRGBA(image.Rect(0, 0, dispVisW, dispH))
	// background already black (zero)

	for sy := 0; sy < smallH; sy++ {
		for sx := 0; sx < smallW; sx++ {
			c := small.At(sx, sy)
			r, g, b, a := c.RGBA()
			if a == 0 {
				continue
			}
			pix := color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					px := startX + sx*scale + dx
					py := startY + sy*scale + dy
					if px >= 0 && px < dispVisW && py >= 0 && py < dispH {
						canvas.SetNRGBA(px, py, pix)
					}
				}
			}
		}
	}

	return imageToBGR565(canvas)
}

// renderOSDFrameLines renders multiple text lines stacked vertically and centered
// on a black 960×160 frame. Each OSDLine has its own Scale factor for font size.
// Lines are separated by a 6-pixel gap (after scaling).
func renderOSDFrameLines(lines []OSDLine) []byte {
	const lineGap = 6
	face := basicfont.Face7x13

	type rendered struct {
		img   *image.NRGBA
		scale int
		w, h  int
	}
	var rows []rendered
	totalH, maxW := 0, 0

	for _, l := range lines {
		if l.Text == "" || l.Scale <= 0 {
			continue
		}
		bounds, _ := font.BoundString(face, l.Text)
		sw := (-bounds.Min.X + bounds.Max.X).Round()
		sh := (-bounds.Min.Y + bounds.Max.Y).Round()
		if sw <= 0 || sh <= 0 {
			continue
		}
		img := image.NewNRGBA(image.Rect(0, 0, sw, sh))
		draw.Draw(img, img.Bounds(), image.Black, image.Point{}, draw.Src)
		d := &font.Drawer{
			Dst:  img,
			Src:  image.White,
			Face: face,
			Dot:  fixed.Point26_6{X: -bounds.Min.X, Y: -bounds.Min.Y},
		}
		d.DrawString(l.Text)
		scaledW := sw * l.Scale
		scaledH := sh * l.Scale
		rows = append(rows, rendered{img: img, scale: l.Scale, w: scaledW, h: scaledH})
		totalH += scaledH
		if scaledW > maxW {
			maxW = scaledW
		}
	}
	if len(rows) > 0 {
		totalH += lineGap * (len(rows) - 1)
	}
	if len(rows) == 0 {
		return make([]byte, dispBytes)
	}

	canvas := image.NewNRGBA(image.Rect(0, 0, dispVisW, dispH))
	curY := (dispH - totalH) / 2

	for _, r := range rows {
		startX := (dispVisW - r.w) / 2
		for sy := 0; sy < r.img.Bounds().Dy(); sy++ {
			for sx := 0; sx < r.img.Bounds().Dx(); sx++ {
				cr, cg, cb, ca := r.img.At(sx, sy).RGBA()
				if ca == 0 {
					continue
				}
				pix := color.NRGBA{R: uint8(cr >> 8), G: uint8(cg >> 8), B: uint8(cb >> 8), A: uint8(ca >> 8)}
				for dy := 0; dy < r.scale; dy++ {
					for dx := 0; dx < r.scale; dx++ {
						px := startX + sx*r.scale + dx
						py := curY + sy*r.scale + dy
						if px >= 0 && px < dispVisW && py >= 0 && py < dispH {
							canvas.SetNRGBA(px, py, pix)
						}
					}
				}
			}
		}
		curY += r.h + lineGap
	}
	return imageToBGR565(canvas)
}
