package main

import (
	"archive/zip"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/federico-pepe/ableton-push-hack/core/httpx"
)

//go:embed ui/index.html ui/app.css ui/app.js
var embeddedUI embed.FS

// HackConfig mirrors hack.json structure
type HackConfig struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Port        int      `json:"port"`
	AllowedRoots []string `json:"allowed_roots"`
	Settings    struct {
		MaxUploadSizeMB int  `json:"max_upload_size_mb"`
		ShowHidden      bool `json:"show_hidden_files"`
		ReadOnly        bool `json:"read_only"`
	} `json:"settings"`
}

var (
	config     HackConfig
	fileOps    *FileOps
)

func loadConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&config); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Defaults
	if config.Port == 0 {
		config.Port = 7701
	}
	if config.Settings.MaxUploadSizeMB == 0 {
		config.Settings.MaxUploadSizeMB = 512
	}

	// Resolve ~ in roots
	for i, root := range config.AllowedRoots {
		if strings.HasPrefix(root, "~/") {
			home, _ := os.UserHomeDir()
			config.AllowedRoots[i] = filepath.Join(home, root[2:])
		}
		config.AllowedRoots[i] = filepath.Clean(config.AllowedRoots[i])
	}

	return nil
}

// ── Middleware ─────────────────────────────────────────────────────────────

func withLogging(next http.Handler) http.Handler { return httpx.WithLogging(next) }

func withCORS(next http.Handler) http.Handler {
	return httpx.WithCORS("GET, POST, DELETE, OPTIONS", next)
}

// ── Handlers ───────────────────────────────────────────────────────────────

// GET / — serve embedded UI (index.html + app.css + app.js).
func handleUI(w http.ResponseWriter, r *http.Request) {
	var file, ctype string
	switch r.URL.Path {
	case "/":
		file, ctype = "ui/index.html", "text/html; charset=utf-8"
	case "/app.css":
		file, ctype = "ui/app.css", "text/css; charset=utf-8"
	case "/app.js":
		file, ctype = "ui/app.js", "application/javascript; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	data, err := embeddedUI.ReadFile(file)
	if err != nil {
		http.Error(w, "UI not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

// GET /api/roots — list allowed root directories.
// /run/media is expanded: each mounted subdirectory becomes its own root entry.
func handleRoots(w http.ResponseWriter, r *http.Request) {
	type RootEntry struct {
		Path     string `json:"path"`
		Name     string `json:"name"`
		Exists   bool   `json:"exists"`
		Removable bool  `json:"removable,omitempty"`
	}

	roots := make([]RootEntry, 0, len(config.AllowedRoots))
	for _, root := range config.AllowedRoots {
		if root == "/run/media" {
			// Expand to individual mounted drives only.
			// Use device ID comparison: if subdir's Dev == parent's Dev,
			// it's a leftover empty dir on the same tmpfs (not a real mount).
			var parentStat syscall.Stat_t
			if err := syscall.Stat("/run/media", &parentStat); err != nil {
				continue
			}
			entries, err := os.ReadDir("/run/media")
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), "swap") {
					continue
				}
				p := filepath.Join("/run/media", e.Name())
				var childStat syscall.Stat_t
				if err := syscall.Stat(p, &childStat); err != nil {
					continue
				}
				// Only include if it's a different filesystem (actually mounted)
				if childStat.Dev == parentStat.Dev {
					continue
				}
				roots = append(roots, RootEntry{
					Path:      p,
					Name:      e.Name(),
					Exists:    true,
					Removable: true,
				})
			}
			continue
		}
		_, err := os.Stat(root)
		roots = append(roots, RootEntry{
			Path:   root,
			Name:   filepath.Base(root),
			Exists: err == nil,
		})
	}

	jsonResponse(w, roots)
}

// GET /api/list?path=<dir> — list directory contents
func handleList(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		// Default to first allowed root
		if len(config.AllowedRoots) > 0 {
			path = config.AllowedRoots[0]
		} else {
			http.Error(w, "no allowed roots configured", http.StatusBadRequest)
			return
		}
	}

	entries, err := fileOps.List(path)
	if err != nil {
		httpError(w, err)
		return
	}

	jsonResponse(w, entries)
}

// GET /api/download?path=<file> — download a file
// GET /api/download?path=<dir>  — download a directory as .zip
func handleDownload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}

	safe, err := fileOps.SafePath(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	info, err := os.Stat(safe)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if info.IsDir() {
		// Stream directory as zip
		name := filepath.Base(safe) + ".zip"
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))

		zw := zip.NewWriter(w)
		err = filepath.Walk(safe, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return err
			}
			rel, err := filepath.Rel(safe, p)
			if err != nil {
				return err
			}
			f, err := zw.Create(rel)
			if err != nil {
				return err
			}
			src, err := os.Open(p)
			if err != nil {
				return err
			}
			defer src.Close()
			_, err = io.Copy(f, src)
			return err
		})
		if err != nil {
			log.Printf("zip error for %s: %v", safe, err)
		}
		zw.Close()
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(safe)))
	http.ServeFile(w, r, safe)
}

// POST /api/upload?path=<dir> — upload file(s) into directory.
// Each file may include a "relativePath" form field to preserve folder structure
// (sent by the browser when using webkitdirectory or DataTransferItem traversal).
func handleUpload(w http.ResponseWriter, r *http.Request) {
	if config.Settings.ReadOnly {
		http.Error(w, "read-only mode", http.StatusForbidden)
		return
	}

	dir := r.URL.Query().Get("path")
	if dir == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}

	maxBytes := int64(config.Settings.MaxUploadSizeMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, fmt.Sprintf("parse form: %v", err), http.StatusBadRequest)
		return
	}

	var uploaded []string
	for _, fileHeaders := range r.MultipartForm.File {
		for _, fh := range fileHeaders {
			// "relativePath" field carries the folder-relative path when uploading directories
			relPath := ""
			if vals, ok := r.MultipartForm.Value["relativePath"]; ok && len(vals) > 0 {
				relPath = filepath.Clean(vals[0])
			}
			name, err := fileOps.UploadWithRelPath(dir, fh, relPath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			uploaded = append(uploaded, name)
		}
	}

	jsonResponse(w, map[string]interface{}{
		"uploaded": uploaded,
		"count":    len(uploaded),
	})
}

// DELETE /api/delete?path=<file>&recursive=true — delete file, empty dir, or full tree
func handleDelete(w http.ResponseWriter, r *http.Request) {
	if config.Settings.ReadOnly {
		http.Error(w, "read-only mode", http.StatusForbidden)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}

	var err error
	if r.URL.Query().Get("recursive") == "true" {
		err = fileOps.DeleteAll(path)
	} else {
		err = fileOps.Delete(path)
	}
	if err != nil {
		httpError(w, err)
		return
	}

	jsonResponse(w, map[string]string{"deleted": path})
}

// POST /api/copy — copy a file or directory tree
// JSON body: {"src": "/abs/src", "dst": "/abs/dst"}
func handleCopy(w http.ResponseWriter, r *http.Request) {
	if config.Settings.ReadOnly {
		http.Error(w, "read-only mode", http.StatusForbidden)
		return
	}
	var body struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Src == "" || body.Dst == "" {
		http.Error(w, "src and dst required", http.StatusBadRequest)
		return
	}
	if err := fileOps.Copy(body.Src, body.Dst); err != nil {
		httpError(w, err)
		return
	}
	jsonResponse(w, map[string]string{"copied": body.Dst})
}

// POST /api/unmount?path=<mount> — unmount a removable drive under /run/media
func handleUnmount(w http.ResponseWriter, r *http.Request) {
	path := filepath.Clean(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	// Only allow unmounting paths directly under /run/media
	if filepath.Dir(path) != "/run/media" {
		http.Error(w, "only /run/media/* paths allowed", http.StatusForbidden)
		return
	}
	if err := syscall.Unmount(path, 0); err != nil {
		http.Error(w, fmt.Sprintf("unmount failed: %v", err), http.StatusInternalServerError)
		return
	}
	// Clean up mount.sh's lock file so udev will auto-mount on next plug-in.
	// mount.sh writes /tmp/.automount-<basename> and skips mounting if it exists.
	name := filepath.Base(path)
	os.Remove("/tmp/.automount-" + name)
	jsonResponse(w, map[string]string{"unmounted": path})
}

// POST /api/rename — rename or move a file or directory
// JSON body: {"old": "/path/to/old", "new": "/path/to/new"}
func handleRename(w http.ResponseWriter, r *http.Request) {
	if config.Settings.ReadOnly {
		http.Error(w, "read-only mode", http.StatusForbidden)
		return
	}
	var body struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Old == "" || body.New == "" {
		http.Error(w, "old and new required", http.StatusBadRequest)
		return
	}
	if err := fileOps.Rename(body.Old, body.New); err != nil {
		httpError(w, err)
		return
	}
	jsonResponse(w, map[string]string{"renamed": body.New})
}

// GET /api/assets/<path> — serve Push's read-only image assets
// Proxies files from /opt/push3/products/push3/assets/Images/ with a 24h cache header.
const pushAssetsDir = "/opt/push3/products/push3/assets/Images"

func handleAssets(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/api/assets/")
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(pushAssetsDir, rel)
	// Re-validate: full path must still be under pushAssetsDir
	if !strings.HasPrefix(full, pushAssetsDir+"/") && full != pushAssetsDir {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, full)
}

// GET /api/stats — system statistics
func handleStats(w http.ResponseWriter, r *http.Request) {
	diskPath := "/"
	if len(config.AllowedRoots) > 0 {
		diskPath = config.AllowedRoots[0]
	}
	jsonResponse(w, collectStats(diskPath))
}

// GET /api/status — health check + config summary
func handleStatus(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"status":        "ok",
		"name":          config.Name,
		"version":       config.Version,
		"port":          config.Port,
		"read_only":     config.Settings.ReadOnly,
		"allowed_roots": config.AllowedRoots,
	})
}

// ── Helpers ────────────────────────────────────────────────────────────────

func jsonResponse(w http.ResponseWriter, v interface{}) { httpx.JSON(w, v) }

func httpError(w http.ResponseWriter, err error) { httpx.Error(w, err) }

// ── Main ───────────────────────────────────────────────────────────────────

func main() {
	configPath := flag.String("config", "hack.json", "path to hack.json config file")
	portOverride := flag.Int("port", 0, "override port from config")
	flag.Parse()

	// Load config
	if err := loadConfig(*configPath); err != nil {
		log.Fatalf("load config: %v", err)
	}

	if *portOverride != 0 {
		config.Port = *portOverride
	}

	// Init file operations with allowed roots
	fileOps = NewFileOps(config.AllowedRoots, config.Settings.ShowHidden)

	// Connect to push-display shared memory (non-fatal if hook not running)
	initDisplayShm()

	// Mark Live's own Log.txt once Live is up so support can confirm push-hack is
	// installed straight from the standard Live log. See live_log.go.
	go startLiveLogMarker()

	// Start OSD goroutine (uses display shm; safe if hook not loaded)
	startOSD()

	// Set MIDI config persistence path (same directory as hack.json)
	midiConfigPath = filepath.Join(filepath.Dir(*configPath), "midi.json")
	loadMidiPersist()

	// Register MIDI chord bindings (in-memory; no device access)
	initMidiChords()

	// Browser Bridge preset index — filesystem scan, off Live's audio thread.
	// Warm-load the cache for an instant UI, then rescan fresh in the background.
	presetIndexPath = filepath.Join(filepath.Dir(*configPath), "presets.json")
	presetMetaPath = filepath.Join(filepath.Dir(*configPath), "preset_meta.json")
	loadPresetMeta()
	loadPresetIndex()
	go buildPresetIndex()

	// Defer ALSA seq init (LED output fd + reader) past the early boot window.
	// Opening /dev/snd during the USB-A port's enumeration window after a COLD
	// boot can wedge that port — a USB-Audio device plugged into USB-A fails to
	// enumerate and the port stays dead until a power cycle. Run in a goroutine
	// so the HTTP server comes up immediately. No delay on a manual restart of
	// an already-running system (uptime already past the threshold).
	go func() {
		waitForBootSettle()
		initMidiOut()
		startMidiReader()
	}()

	// Routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleUI)
	mux.HandleFunc("/api/roots", handleRoots)
	mux.HandleFunc("/api/list", handleList)
	mux.HandleFunc("/api/download", handleDownload) // handles both files and dirs (zip)
	mux.HandleFunc("/api/upload", handleUpload)
	mux.HandleFunc("/api/delete", handleDelete)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/rename", handleRename)
	mux.HandleFunc("/api/copy", handleCopy)
	mux.HandleFunc("/api/unmount", handleUnmount)
	mux.HandleFunc("/api/stats", handleStats)
	mux.HandleFunc("/api/assets/", handleAssets)
	mux.HandleFunc("/api/display/status", handleDisplayStatus)
	mux.HandleFunc("/api/display/mode", handleDisplayMode)
	mux.HandleFunc("/api/display/image", handleDisplayImage)
	mux.HandleFunc("/api/display/screenshot", handleDisplayScreenshot)
	mux.HandleFunc("/api/midi/events", handleMidiEvents)
	mux.HandleFunc("/api/midi/stream", handleMidiStream)
	mux.HandleFunc("/api/midi/filter", handleMidiFilter)
	mux.HandleFunc("/api/midi/filter/status", handleMidiFilterStatus)
	mux.HandleFunc("/api/midi/ports", handleMidiPorts)
	mux.HandleFunc("/api/midi/subscribe", handleMidiSubscribe)
	mux.HandleFunc("/api/midi/chords", handleMidiChords)
	mux.HandleFunc("/api/midi/led", handleMidiLed)
	mux.HandleFunc("/api/midi/led/states", handleMidiLedStates)
	mux.HandleFunc("/api/midi/led/config", handleMidiLedConfig)
	mux.HandleFunc("/api/midi/forward", handleMidiForward)
	mux.HandleFunc("/api/midi/palette", handleMidiPalette)
	mux.HandleFunc("/api/midi/mapping", handleMidiMapping)
	mux.HandleFunc("/api/midi/mapping/config", handleMidiMappingConfig)
	mux.HandleFunc("/api/presets", handlePresets)
	mux.HandleFunc("/api/presets/refresh", handlePresetsRefresh)
	mux.HandleFunc("/api/presets/facets", handlePresetFacets)
	mux.HandleFunc("/api/presets/meta", handlePresetMeta)
	mux.HandleFunc("/api/live/load", handleLiveLoad)
	mux.HandleFunc("/api/live/tempo", handleLiveTempo)
	mux.HandleFunc("/api/live/playing", handleLivePlaying)
	mux.HandleFunc("/api/live/play", handleLivePlay)
	mux.HandleFunc("/api/live/stop", handleLiveStop)

	handler := withCORS(withLogging(mux))

	addr := fmt.Sprintf(":%d", config.Port)
	log.Printf("Push Manager %s starting on %s", config.Version, addr)
	log.Printf("Allowed roots: %s", strings.Join(config.AllowedRoots, ", "))

	srv := httpx.NewServer(addr, handler) // WriteTimeout is long (5min) for large downloads

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
