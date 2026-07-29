package main

// engine.go — MIDI CC automation engine.
// Manages up to 8 lanes, each with a breakpoint curve, loop timing, and CC target.
// Plays curves at 50Hz, sending CC values to Push 3 via ALSA seq.
// Supports linear and Catmull-Rom smooth interpolation.
// Polls push-manager /api/live/tempo + /api/live/playing every 2s.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── Data model ────────────────────────────────────────────────────────────

type CurvePoint struct {
	Phase float64 `json:"phase"` // 0.0–1.0: position within loop
	Value float64 `json:"value"` // 0.0–1.0: maps to CC 0–127
}

type AutoLane struct {
	ID       int          `json:"id"`
	Label    string       `json:"label"`
	CC       uint8        `json:"cc"`
	Channel  uint8        `json:"channel"`
	Points   []CurvePoint `json:"points"`  // sorted by Phase; at least 1
	Enabled  bool         `json:"enabled"`
	Smooth   bool    `json:"smooth"`    // true = Catmull-Rom, false = linear
	SyncMode bool    `json:"sync_mode"` // true = use Live BPM; false = FreeBPM/FreeSecs
	Beats    float64 `json:"beats"`     // loop length in beats (sync mode)
	FreeBPM  float64 `json:"free_bpm"`  // BPM when free mode
	FreeSecs float64 `json:"free_secs"` // loop length in seconds (free mode)
}

type AutoState struct {
	Lanes         []*AutoLane `json:"lanes"`
	Running       bool        `json:"running"`
	TransportSync bool        `json:"transport_sync"` // follow Live play/stop
}

// ── Globals ───────────────────────────────────────────────────────────────

var (
	autoMu       sync.RWMutex
	autoState    = AutoState{}
	autoNextID   int
	autoConfigPath string

	// autoPhases is owned exclusively by the engine goroutine — no lock needed
	autoPhases []float64

	liveBPMMu sync.RWMutex
	liveBPM   = 120.0

	// SSE broadcast: engine notifies all connected SSE clients
	autoStreamMu      sync.Mutex
	autoStreamClients []chan autoStreamPayload

	resetPhasesRequested uint32 // atomic; set by MIDI stop, cleared by engine tick

	pushManagerBase string // set from main.go before startAutoEngine
)

type autoStreamPayload struct {
	Running       bool      `json:"running"`
	TransportSync bool      `json:"transport_sync"`
	Phases        []float64 `json:"phases"`
	LiveBPM       float64   `json:"live_bpm"`
}

// ── Persistence ───────────────────────────────────────────────────────────

func loadAutoConfig() {
	if autoConfigPath == "" {
		return
	}
	data, err := os.ReadFile(autoConfigPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("auto: load config: %v", err)
		}
		return
	}
	var s AutoState
	if err := json.Unmarshal(data, &s); err != nil {
		log.Printf("auto: parse config: %v", err)
		return
	}
	if s.Lanes == nil {
		s.Lanes = []*AutoLane{}
	}
	autoMu.Lock()
	autoState = s
	autoNextID = 0
	for _, l := range autoState.Lanes {
		if l.ID >= autoNextID {
			autoNextID = l.ID + 1
		}
		sanitizeLane(l)
	}
	autoMu.Unlock()
	log.Printf("auto: loaded %d lanes", len(s.Lanes))
}

func saveAutoConfig() {
	if autoConfigPath == "" {
		return
	}
	autoMu.RLock()
	data, err := json.MarshalIndent(autoState, "", "  ")
	autoMu.RUnlock()
	if err != nil {
		log.Printf("auto: marshal config: %v", err)
		return
	}
	tmp := autoConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Printf("auto: write config tmp: %v", err)
		return
	}
	if err := os.Rename(tmp, autoConfigPath); err != nil {
		log.Printf("auto: rename config: %v", err)
	}
}

// sanitizeLane fills in defaults and ensures at least one curve point.
func sanitizeLane(l *AutoLane) {
	if l.Label == "" {
		l.Label = fmt.Sprintf("Lane %d", l.ID)
	}
	if l.FreeBPM <= 0 {
		l.FreeBPM = 120
	}
	if l.FreeSecs <= 0 {
		l.FreeSecs = 4
	}
	if l.Beats <= 0 {
		l.Beats = 4
	}
	if len(l.Points) == 0 {
		l.Points = []CurvePoint{{Phase: 0.0, Value: 0.5}}
	}
	sortPoints(l)
}

func sortPoints(l *AutoLane) {
	pts := l.Points
	for i := 1; i < len(pts); i++ {
		for j := i; j > 0 && pts[j].Phase < pts[j-1].Phase; j-- {
			pts[j], pts[j-1] = pts[j-1], pts[j]
		}
	}
}

// ── Interpolation ─────────────────────────────────────────────────────────

// interpolateLane returns the interpolated value [0,1] for the given phase [0,1).
func interpolateLane(lane *AutoLane, phase float64) float64 {
	pts := lane.Points
	n := len(pts)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return pts[0].Value
	}

	// Find segment index: pts[segIdx].Phase <= phase < pts[nextIdx].Phase
	segIdx := n - 1 // default: wrap-around (last point → first)
	for i := 0; i < n-1; i++ {
		if phase >= pts[i].Phase && phase < pts[i+1].Phase {
			segIdx = i
			break
		}
	}
	nextIdx := (segIdx + 1) % n

	// Local t in [0,1) within this segment
	phaseStart := pts[segIdx].Phase
	var phaseEnd float64
	if segIdx == n-1 {
		phaseEnd = 1.0 // wrap-around segment ends at loop boundary
	} else {
		phaseEnd = pts[nextIdx].Phase
	}
	segLen := phaseEnd - phaseStart
	var t float64
	if segLen > 0 {
		t = (phase - phaseStart) / segLen
	}

	v1 := pts[segIdx].Value
	v2 := pts[nextIdx].Value

	if !lane.Smooth {
		return v1 + t*(v2-v1)
	}

	// Catmull-Rom with circular indexing — uniform parameterization (value-only).
	// P0 = point before P1, P3 = point after P2.
	p0Idx := (segIdx - 1 + n) % n
	p3Idx := (segIdx + 2) % n
	v0 := pts[p0Idx].Value
	v3 := pts[p3Idx].Value

	t2 := t * t
	t3 := t2 * t
	val := 0.5 * ((2*v1) + (-v0+v2)*t + (2*v0-5*v1+4*v2-v3)*t2 + (-v0+3*v1-3*v2+v3)*t3)
	return math.Max(0, math.Min(1, val))
}

// ── BPM + transport poller ────────────────────────────────────────────────

func startBPMPoller(base string) {
	pushManagerBase = base
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		ticker := time.NewTicker(5 * time.Second) // slower fallback poll
		defer ticker.Stop()
		for range ticker.C {
			// Only use HTTP BPM if MIDI clock is stale (> 5s since last tick)
			clockMu.Lock()
			total := clockTotal
			var lastNs int64
			if total > 0 {
				lastNs = clockRing[(total-1)%24]
			}
			clockMu.Unlock()
			if total > 0 && time.Now().UnixNano()-lastNs < 5e9 {
				continue // MIDI clock active, skip HTTP poll
			}
			pollTempo(client, base)
		}
	}()
}

func pollTempo(client *http.Client, base string) {
	resp, err := client.Get(base + "/api/live/tempo")
	if err != nil {
		return
	}
	var body struct {
		OK  bool    `json:"ok"`
		BPM float64 `json:"bpm"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if body.OK && body.BPM > 0 {
		liveBPMMu.Lock()
		liveBPM = body.BPM
		liveBPMMu.Unlock()
	}
}

// ── Playback engine ───────────────────────────────────────────────────────

func startAutoEngine() {
	go func() {
		const tickDuration = 0.020 // 20ms = 50Hz
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		sseTimer := time.NewTicker(50 * time.Millisecond)
		defer sseTimer.Stop()

		for {
			select {
			case <-ticker.C:
				engineTick(tickDuration)
			case <-sseTimer.C:
				broadcastSSE()
			}
		}
	}()
}

func engineTick(dt float64) {
	// Reset phases if MIDI Stop was received
	if atomic.LoadUint32(&resetPhasesRequested) == 1 {
		atomic.StoreUint32(&resetPhasesRequested, 0)
		for i := range autoPhases {
			autoPhases[i] = 0
		}
	}

	autoMu.RLock()
	running := autoState.Running
	lanes := autoState.Lanes
	autoMu.RUnlock()

	// Sync phase slice length to lane count
	if len(autoPhases) != len(lanes) {
		newPhases := make([]float64, len(lanes))
		copy(newPhases, autoPhases)
		autoPhases = newPhases
	}

	if !running {
		return
	}

	liveBPMMu.RLock()
	bpm := liveBPM
	liveBPMMu.RUnlock()

	for i, lane := range lanes {
		if !lane.Enabled {
			continue
		}

		// Advance phase
		var loopDurationSecs float64
		if lane.SyncMode {
			if bpm <= 0 {
				bpm = 120
			}
			loopDurationSecs = lane.Beats * 60.0 / bpm
		} else {
			loopDurationSecs = lane.FreeSecs
			if loopDurationSecs <= 0 {
				loopDurationSecs = 4
			}
		}
		autoPhases[i] += dt / loopDurationSecs
		if autoPhases[i] >= 1.0 {
			autoPhases[i] -= math.Floor(autoPhases[i])
		}

		// Interpolate curve and send CC
		val := interpolateLane(lane, autoPhases[i])
		ccVal := int32(math.Round(val * 127))
		if err := sendSeqCC(lane.Channel, lane.CC, ccVal); err != nil {
			// Log occasionally but don't spam
		}
	}
}

func broadcastSSE() {
	autoMu.RLock()
	running := autoState.Running
	tsync := autoState.TransportSync
	autoMu.RUnlock()

	liveBPMMu.RLock()
	bpm := liveBPM
	liveBPMMu.RUnlock()

	// Copy phases snapshot
	phases := make([]float64, len(autoPhases))
	copy(phases, autoPhases)

	payload := autoStreamPayload{
		Running:       running,
		TransportSync: tsync,
		Phases:        phases,
		LiveBPM:       bpm,
	}

	autoStreamMu.Lock()
	active := make([]chan autoStreamPayload, 0, len(autoStreamClients))
	for _, ch := range autoStreamClients {
		select {
		case ch <- payload:
			active = append(active, ch)
		default:
			// Slow or disconnected client — drop
		}
	}
	autoStreamClients = active
	autoStreamMu.Unlock()
}

func registerSSEClient() chan autoStreamPayload {
	ch := make(chan autoStreamPayload, 4)
	autoStreamMu.Lock()
	autoStreamClients = append(autoStreamClients, ch)
	autoStreamMu.Unlock()
	return ch
}

func unregisterSSEClient(ch chan autoStreamPayload) {
	autoStreamMu.Lock()
	out := autoStreamClients[:0]
	for _, c := range autoStreamClients {
		if c != ch {
			out = append(out, c)
		}
	}
	autoStreamClients = out
	autoStreamMu.Unlock()
}

// ── HTTP handlers ─────────────────────────────────────────────────────────

// GET /api/auto/state
func handleAutoState(w http.ResponseWriter, r *http.Request) {
	autoMu.RLock()
	running := autoState.Running
	lanes := autoState.Lanes
	tsync := autoState.TransportSync
	autoMu.RUnlock()

	liveBPMMu.RLock()
	bpm := liveBPM
	liveBPMMu.RUnlock()

	if lanes == nil {
		lanes = []*AutoLane{}
	}
	jsonResponse(w, map[string]interface{}{
		"running":        running,
		"transport_sync": tsync,
		"lanes":          lanes,
		"live_bpm":       bpm,
	})
}

// POST /api/auto/transport_sync — {"enabled": true/false}
func handleAutoTransportSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	autoMu.Lock()
	autoState.TransportSync = body.Enabled
	autoMu.Unlock()
	saveAutoConfig()
	jsonResponse(w, map[string]interface{}{"ok": true, "transport_sync": body.Enabled})
}

// POST /api/auto/play
func handleAutoPlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	autoMu.Lock()
	autoState.Running = true
	autoMu.Unlock()
	saveAutoConfig()
	jsonResponse(w, map[string]interface{}{"ok": true, "running": true})
}

// POST /api/auto/stop
func handleAutoStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	autoMu.Lock()
	autoState.Running = false
	autoMu.Unlock()
	saveAutoConfig()
	jsonResponse(w, map[string]interface{}{"ok": true, "running": false})
}

// POST /api/auto/lane — create a new lane
func handleAutoLaneCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	autoMu.Lock()
	if len(autoState.Lanes) >= 8 {
		autoMu.Unlock()
		http.Error(w, "max 8 lanes", http.StatusConflict)
		return
	}

	var lane AutoLane
	if err := json.NewDecoder(r.Body).Decode(&lane); err != nil && err != io.EOF {
		autoMu.Unlock()
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	lane.ID = autoNextID
	autoNextID++
	lane.Enabled = true
	sanitizeLane(&lane)
	autoState.Lanes = append(autoState.Lanes, &lane)
	autoPhases = append(autoPhases, 0)
	autoMu.Unlock()

	saveAutoConfig()
	jsonResponse(w, map[string]interface{}{"ok": true, "lane": lane})
}

// handleAutoLaneByID dispatches PUT, DELETE, and POST /reset for /api/auto/lane/{id}[/reset]
func handleAutoLaneByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/auto/lane/")
	parts := strings.SplitN(path, "/", 2)
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "invalid lane id", http.StatusBadRequest)
		return
	}

	if len(parts) == 2 && parts[1] == "reset" {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		autoMu.RLock()
		idx := laneIndex(id)
		autoMu.RUnlock()
		if idx < 0 {
			http.Error(w, "lane not found", http.StatusNotFound)
			return
		}
		if idx < len(autoPhases) {
			autoPhases[idx] = 0
		}
		jsonResponse(w, map[string]interface{}{"ok": true})
		return
	}

	switch r.Method {
	case http.MethodPut:
		handleAutoLaneUpdate(w, r, id)
	case http.MethodDelete:
		handleAutoLaneDelete(w, r, id)
	default:
		http.Error(w, "PUT or DELETE required", http.StatusMethodNotAllowed)
	}
}

func handleAutoLaneUpdate(w http.ResponseWriter, r *http.Request, id int) {
	var patch AutoLane
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	autoMu.Lock()
	idx := laneIndex(id)
	if idx < 0 {
		autoMu.Unlock()
		http.Error(w, "lane not found", http.StatusNotFound)
		return
	}
	lane := autoState.Lanes[idx]
	patch.ID = id // preserve ID
	if patch.Label != "" {
		lane.Label = patch.Label
	}
	lane.CC = patch.CC
	lane.Channel = patch.Channel
	lane.Enabled = patch.Enabled
	lane.Smooth = patch.Smooth
	lane.SyncMode = patch.SyncMode
	if patch.Beats > 0 {
		lane.Beats = patch.Beats
	}
	if patch.FreeBPM > 0 {
		lane.FreeBPM = patch.FreeBPM
	}
	if patch.FreeSecs > 0 {
		lane.FreeSecs = patch.FreeSecs
	}
	if len(patch.Points) > 0 {
		lane.Points = patch.Points
		sortPoints(lane)
	}
	result := *lane
	autoMu.Unlock()

	saveAutoConfig()
	jsonResponse(w, map[string]interface{}{"ok": true, "lane": result})
}

func handleAutoLaneDelete(w http.ResponseWriter, r *http.Request, id int) {
	autoMu.Lock()
	idx := laneIndex(id)
	if idx < 0 {
		autoMu.Unlock()
		http.Error(w, "lane not found", http.StatusNotFound)
		return
	}
	autoState.Lanes = append(autoState.Lanes[:idx], autoState.Lanes[idx+1:]...)
	if idx < len(autoPhases) {
		autoPhases = append(autoPhases[:idx], autoPhases[idx+1:]...)
	}
	autoMu.Unlock()

	saveAutoConfig()
	jsonResponse(w, map[string]interface{}{"ok": true})
}

// GET /api/auto/stream — SSE stream with phase positions at 20Hz
func handleAutoStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := registerSSEClient()
	defer unregisterSSEClient(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			data, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────

// laneIndex returns the slice index of the lane with the given ID, or -1.
// Caller must hold autoMu.
func laneIndex(id int) int {
	for i, l := range autoState.Lanes {
		if l.ID == id {
			return i
		}
	}
	return -1
}
