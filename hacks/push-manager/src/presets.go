// presets.go — filesystem-scanned preset + sample database for the Browser Bridge.
//
// Browsing, searching and filtering must never touch Live (the Live API is
// single-threaded on the audio engine). push-manager scans files on disk
// directly: fast, in-process, and safe during performance. Only LOAD goes
// through the PushHackBrowser Remote Script (see live_bridge.go).
//
// Preset scan roots (Live 12, version-robust globs):
//   <Core Library>/Devices  — device presets, category = subfolder
//   <Core Library>/Racks    — Instrument/Drum/Audio Effect/MIDI Effect Racks
//   Factory Packs           — installed packs
//   User Library/{Presets,Devices} — user-installed content
//
// Sample scan roots:
//   <Core Library>/Samples  — Core Library loops, one shots, multisamples
//   Factory Packs           — per-pack Samples dirs
//   User Library/Samples    — user audio files

package main

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const abletonRoot = "/data/Music/Ableton"

// PresetCategory is a coarse grouping used for the Shadow UI filter.
type PresetCategory string

const (
	CatInstruments PresetCategory = "Instruments"
	CatAudioFX     PresetCategory = "Audio Effects"
	CatMidiFX      PresetCategory = "MIDI Effects"
	CatDrums       PresetCategory = "Drums"
	CatOther       PresetCategory = "Other"
)

// browserRoot maps a category to the Live browser root attribute used to scope
// the load-time lookup in PushHackBrowser. Empty = no scoping (search all).
func (c PresetCategory) browserRoot() string {
	switch c {
	case CatInstruments:
		return "instruments"
	case CatAudioFX:
		return "audio_effects"
	case CatMidiFX:
		return "midi_effects"
	case CatDrums:
		return "drums"
	}
	return ""
}

// PresetEntry is one browsable item — either an Ableton preset (.adv/.adg) or
// an audio sample (.wav/.aif/.aiff). Device/Source are derived facets (indexed);
// Favourite/Tags are merged from the metadata store at query time.
type PresetEntry struct {
	Name      string         `json:"name"`
	Path      string         `json:"path"`
	Category  PresetCategory `json:"category"`
	IsRack    bool           `json:"is_rack"`            // .adg rack vs .adv device preset
	EntryType string         `json:"type"`               // "preset" or "sample"
	SampleSub string         `json:"sample_subtype,omitempty"` // "Loops", "One Shots", "Multisamples"
	Device    string         `json:"device,omitempty"`
	Source    string         `json:"source,omitempty"`
	Favourite bool           `json:"favourite"`
	Tags      []string       `json:"tags,omitempty"`
}

// deriveDevice extracts the source device folder for Core Library device presets
// (…/Devices/<category>/<Device>/…) or the rack family (…/Racks/<Family>/…).
func deriveDevice(p string) string {
	parts := strings.Split(p, string(filepath.Separator))
	for i, seg := range parts {
		if (seg == "Devices" || seg == "Racks") && i+2 < len(parts) {
			// Devices/<category>/<Device>/…  or  Racks/<Family>/<sub>/…
			return parts[i+2]
		}
	}
	return ""
}

// deriveSource classifies where a preset comes from, for the Source facet.
func deriveSource(p string) string {
	switch {
	case strings.Contains(p, "Core Library"):
		return "Core Library"
	case strings.Contains(p, "/Factory Packs/"):
		// Factory Packs/<Pack name>/…
		rest := p[strings.Index(p, "/Factory Packs/")+len("/Factory Packs/"):]
		if i := strings.IndexByte(rest, filepath.Separator); i > 0 {
			return rest[:i]
		}
		return "Factory Pack"
	case strings.Contains(p, "/User Library/"):
		return "User Library"
	}
	return "Other"
}

var (
	presetMu        sync.RWMutex
	presetIndex     []PresetEntry
	presetBuiltAt   time.Time
	presetIndexPath string // set by main() to <hackdir>/presets.json
)

// categorizeByPath derives a coarse category from path segments. Order matters:
// the most specific keyword wins (Drum before Instrument, etc.).
func categorizeByPath(p string) PresetCategory {
	lp := strings.ToLower(p)
	switch {
	case strings.Contains(lp, "/drum rack") || strings.Contains(lp, "/drums/"):
		return CatDrums
	case strings.Contains(lp, "midi effect"):
		return CatMidiFX
	case strings.Contains(lp, "audio effect"):
		return CatAudioFX
	case strings.Contains(lp, "/instrument"): // Instruments, Instrument Racks
		return CatInstruments
	}
	return CatOther
}

// presetScanRoots returns the directories to scan for presets (.adv/.adg).
func presetScanRoots() []string {
	var roots []string
	for _, sub := range []string{"Devices", "Racks"} {
		matches, _ := filepath.Glob(filepath.Join(abletonRoot, "*Core Library*", sub))
		roots = append(roots, matches...)
	}
	roots = append(roots,
		filepath.Join(abletonRoot, "Factory Packs"),
		filepath.Join(abletonRoot, "User Library", "Presets"),
		filepath.Join(abletonRoot, "User Library", "Devices"),
	)
	return roots
}

// sampleScanRoots returns the directories to scan for audio samples.
func sampleScanRoots() []string {
	var roots []string
	matches, _ := filepath.Glob(filepath.Join(abletonRoot, "*Core Library*", "Samples"))
	roots = append(roots, matches...)
	roots = append(roots,
		filepath.Join(abletonRoot, "Factory Packs"),
		filepath.Join(abletonRoot, "User Library", "Samples"),
	)
	return roots
}

// deriveSampleSubtype returns the sample sub-category from the path.
func deriveSampleSubtype(p string) string {
	switch {
	case strings.Contains(p, "/One Shots/"):
		return "One Shots"
	case strings.Contains(p, "/Multisamples/"):
		return "Multisamples"
	case strings.Contains(p, "/Loops/"):
		return "Loops"
	}
	return ""
}

// buildPresetIndex scans the roots and replaces the in-memory index. Runs in Go
// (off Live's audio thread); safe to call at startup and on explicit refresh.
func buildPresetIndex() {
	start := time.Now()
	var entries []PresetEntry
	seen := map[string]bool{}

	// ── Presets (.adv / .adg) ─────────────────────────────────────────────────
	for _, root := range presetScanRoots() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".adv" && ext != ".adg" {
				return nil
			}
			if seen[p] {
				return nil
			}
			seen[p] = true
			entries = append(entries, PresetEntry{
				Name:      strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)),
				Path:      p,
				Category:  categorizeByPath(p),
				IsRack:    ext == ".adg",
				EntryType: "preset",
				Device:    deriveDevice(p),
				Source:    deriveSource(p),
			})
			return nil
		})
	}

	// ── Samples (.wav / .aif / .aiff) ─────────────────────────────────────────
	audioExts := map[string]bool{".wav": true, ".aif": true, ".aiff": true}
	for _, root := range sampleScanRoots() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr
			}
			ext := strings.ToLower(filepath.Ext(p))
			if !audioExts[ext] {
				return nil
			}
			if seen[p] {
				return nil
			}
			seen[p] = true
			entries = append(entries, PresetEntry{
				Name:      filepath.Base(p), // keep extension — matches Live browser display
				Path:      p,
				Category:  categorizeByPath(p),
				EntryType: "sample",
				SampleSub: deriveSampleSubtype(p),
				Source:    deriveSource(p),
			})
			return nil
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].EntryType != entries[j].EntryType {
			return entries[i].EntryType < entries[j].EntryType // presets before samples
		}
		if entries[i].Category != entries[j].Category {
			return entries[i].Category < entries[j].Category
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	presetMu.Lock()
	presetIndex = entries
	presetBuiltAt = time.Now()
	presetMu.Unlock()

	log.Printf("presets: indexed %d entries in %v", len(entries), time.Since(start))
	savePresetIndex()
}

// RefreshPresets rebuilds the index (rescan). Call in a goroutine.
func RefreshPresets() { buildPresetIndex() }

// PresetFilter is the set of facets the browser can filter by. Zero value = all.
type PresetFilter struct {
	Category      PresetCategory
	Q             string // case-insensitive substring on name OR any tag
	FavOnly       bool
	Tag           string
	Device        string
	Source        string
	RackOnly      bool   // .adg racks only (auto-excludes samples)
	PresetOnly    bool   // .adv device presets only (auto-excludes samples)
	TypeFilter    string // "preset", "sample", "" = all
	SubtypeFilter string // "Loops", "One Shots", "Multisamples", "" = all (samples only)
}

// QueryPresets returns indexed entries matching the filter, with favourite/tags
// merged in from the metadata store.
func QueryPresets(f PresetFilter) []PresetEntry {
	q := strings.ToLower(strings.TrimSpace(f.Q))
	presetMu.RLock()
	defer presetMu.RUnlock()
	out := make([]PresetEntry, 0, len(presetIndex))
	for _, e := range presetIndex {
		if f.TypeFilter != "" && e.EntryType != f.TypeFilter {
			continue
		}
		if f.SubtypeFilter != "" && e.SampleSub != f.SubtypeFilter {
			continue
		}
		if f.Category != "" && e.Category != f.Category {
			continue
		}
		if f.Device != "" && e.Device != f.Device {
			continue
		}
		if f.Source != "" && e.Source != f.Source {
			continue
		}
		// RackOnly / PresetOnly apply to presets only; both implicitly exclude samples.
		if (f.RackOnly || f.PresetOnly) && e.EntryType != "preset" {
			continue
		}
		if f.RackOnly && !e.IsRack {
			continue
		}
		if f.PresetOnly && e.IsRack {
			continue
		}
		meta := getPresetMeta(e.Path)
		if f.FavOnly && !meta.Favourite {
			continue
		}
		if f.Tag != "" && !containsStr(meta.Tags, f.Tag) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Name), q) && !anyTagContains(meta.Tags, q) {
			continue
		}
		e.Favourite = meta.Favourite
		e.Tags = meta.Tags
		out = append(out, e)
	}
	return out
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func anyTagContains(tags []string, q string) bool {
	for _, t := range tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

// presetFacets returns distinct values for the web filter UI.
func presetFacets() map[string]interface{} {
	presetMu.RLock()
	devSet, srcSet := map[string]bool{}, map[string]bool{}
	devByCat := map[PresetCategory]map[string]bool{}
	subtypeSet := map[string]bool{}
	for _, e := range presetIndex {
		if e.Source != "" {
			srcSet[e.Source] = true
		}
		if e.EntryType == "preset" {
			if e.Device != "" {
				devSet[e.Device] = true
				if _, ok := devByCat[e.Category]; !ok {
					devByCat[e.Category] = map[string]bool{}
				}
				devByCat[e.Category][e.Device] = true
			}
		} else if e.EntryType == "sample" && e.SampleSub != "" {
			subtypeSet[e.SampleSub] = true
		}
	}
	presetMu.RUnlock()

	tagSet := map[string]bool{}
	presetMetaMu.RLock()
	for _, m := range presetMetaMap {
		for _, t := range m.Tags {
			tagSet[t] = true
		}
	}
	presetMetaMu.RUnlock()

	devByCatSorted := map[string][]string{}
	for cat, m := range devByCat {
		devByCatSorted[string(cat)] = sortedKeys(m)
	}

	// Sources: Core Library first, rest sorted.
	allSrcs := sortedKeys(srcSet)
	srcs := []string{}
	for _, s := range allSrcs {
		if s == "Core Library" {
			srcs = append([]string{s}, srcs...)
		} else {
			srcs = append(srcs, s)
		}
	}

	return map[string]interface{}{
		"categories":          []PresetCategory{CatInstruments, CatAudioFX, CatMidiFX, CatDrums, CatOther},
		"devices":             sortedKeys(devSet),
		"devices_by_category": devByCatSorted,
		"sources":             srcs,
		"sample_subtypes":     sortedKeys(subtypeSet),
		"tags":                sortedKeys(tagSet),
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── Metadata store (favourites + tags) ──────────────────────────────────────

// PresetMeta holds user-assigned metadata for a preset, keyed by file path.
type PresetMeta struct {
	Favourite bool     `json:"favourite"`
	Tags      []string `json:"tags,omitempty"`
}

var (
	presetMetaMu    sync.RWMutex
	presetMetaMap   = map[string]PresetMeta{}
	presetMetaPath  string // set by main() to <hackdir>/preset_meta.json
)

func getPresetMeta(path string) PresetMeta {
	presetMetaMu.RLock()
	defer presetMetaMu.RUnlock()
	return presetMetaMap[path]
}

// isFavourite reports whether a preset path is starred (used by the Shadow UI).
func isFavourite(path string) bool { return getPresetMeta(path).Favourite }

// setPresetMeta updates favourite and/or tags for a path (nil = leave unchanged)
// and persists. Returns the merged metadata.
func setPresetMeta(path string, fav *bool, tags *[]string) PresetMeta {
	presetMetaMu.Lock()
	m := presetMetaMap[path]
	if fav != nil {
		m.Favourite = *fav
	}
	if tags != nil {
		m.Tags = *tags
	}
	if !m.Favourite && len(m.Tags) == 0 {
		delete(presetMetaMap, path) // keep the file tidy — no empty records
	} else {
		presetMetaMap[path] = m
	}
	presetMetaMu.Unlock()
	savePresetMeta()
	return m
}

func savePresetMeta() {
	if presetMetaPath == "" {
		return
	}
	presetMetaMu.RLock()
	data, err := json.Marshal(presetMetaMap)
	presetMetaMu.RUnlock()
	if err != nil {
		return
	}
	tmp := presetMetaPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Printf("presets: save meta: %v", err)
		return
	}
	_ = os.Rename(tmp, presetMetaPath)
}

func loadPresetMeta() {
	if presetMetaPath == "" {
		return
	}
	data, err := os.ReadFile(presetMetaPath)
	if err != nil {
		return
	}
	m := map[string]PresetMeta{}
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	presetMetaMu.Lock()
	presetMetaMap = m
	presetMetaMu.Unlock()
	log.Printf("presets: loaded metadata for %d presets", len(m))
}

// presetCount returns the number of indexed entries.
func presetCount() int {
	presetMu.RLock()
	defer presetMu.RUnlock()
	return len(presetIndex)
}

// ── Persistence (warm start) ────────────────────────────────────────────────

func savePresetIndex() {
	if presetIndexPath == "" {
		return
	}
	presetMu.RLock()
	data, err := json.Marshal(presetIndex)
	presetMu.RUnlock()
	if err != nil {
		return
	}
	tmp := presetIndexPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Printf("presets: save: %v", err)
		return
	}
	_ = os.Rename(tmp, presetIndexPath)
}

// ── HTTP ────────────────────────────────────────────────────────────────────

// GET /api/presets?filter=&q=&fav=&tag=&device=&source=&rack=&type=&subtype=
func handlePresets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := PresetFilter{
		Category:      PresetCategory(q.Get("filter")),
		Q:             q.Get("q"),
		FavOnly:       q.Get("fav") == "1" || q.Get("fav") == "true",
		Tag:           q.Get("tag"),
		Device:        q.Get("device"),
		Source:        q.Get("source"),
		RackOnly:      q.Get("rack") == "rack",
		PresetOnly:    q.Get("rack") == "preset",
		TypeFilter:    q.Get("type"),    // "preset", "sample", "" = all
		SubtypeFilter: q.Get("subtype"), // "Loops", "One Shots", "Multisamples"
	}
	results := QueryPresets(f)
	jsonResponse(w, map[string]interface{}{
		"count":   len(results),
		"total":   presetCount(),
		"presets": results,
	})
}

// GET /api/presets/facets — distinct values for the filter UI.
func handlePresetFacets(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, presetFacets())
}

// POST /api/presets/meta {path, favourite?:bool, tags?:[]string} — set tags/favourite.
func handlePresetMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path      string    `json:"path"`
		Favourite *bool     `json:"favourite"`
		Tags      *[]string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	m := setPresetMeta(body.Path, body.Favourite, body.Tags)
	jsonResponse(w, map[string]interface{}{"ok": true, "favourite": m.Favourite, "tags": m.Tags})
}

// POST /api/presets/refresh — rescan the index and return the fresh count.
func handlePresetsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	RefreshPresets()
	jsonResponse(w, map[string]interface{}{"ok": true, "count": presetCount()})
}

// loadPresetIndex loads a previously saved index for an instant warm start.
// A fresh buildPresetIndex() should still run in the background afterwards.
func loadPresetIndex() {
	if presetIndexPath == "" {
		return
	}
	data, err := os.ReadFile(presetIndexPath)
	if err != nil {
		return
	}
	var entries []PresetEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}
	presetMu.Lock()
	presetIndex = entries
	presetMu.Unlock()
	log.Printf("presets: warm-loaded %d entries from cache", len(entries))
}
