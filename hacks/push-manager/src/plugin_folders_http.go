// plugin_folders_http.go — plugin folders: the tree for both UIs, and the
// routes that edit /data/push-hack/plugin-folders.json.
//
// Grouping logic lives in package plugintree. This file only reads and writes
// the file, under one lock, and talks HTTP.
package main

import (
	"encoding/json"
	"net/http"
	"sync"

	"push-manager/plugintree"
)

// Tests point these at a temp path and a fake plugin list.
var (
	pluginFoldersFile = "/data/push-hack/plugin-folders.json"
	livePluginsFn     = livePlugins
)

// One lock for every read-modify-write of the file. Two browser tabs cannot
// interleave and corrupt it.
var pluginFoldersMu sync.Mutex

// loadPluginTree asks Live for its plugins and groups them. The note is
// non-empty when the folders file is invalid: the tree is then grouped by
// vendor only, so the list still works.
func loadPluginTree() (*plugintree.Node, string, error) {
	plugins, err := livePluginsFn()
	if err != nil {
		return nil, "", err
	}
	pluginFoldersMu.Lock()
	cfg, cerr := plugintree.Load(pluginFoldersFile)
	pluginFoldersMu.Unlock()
	note := ""
	if cerr != nil {
		// The note is drawn on the Push screen: ASCII only.
		note = asciiOnly("plugin-folders.json is not valid: " + cerr.Error())
	}
	return plugintree.Build(plugins, cfg), note, nil
}

func folderReply(w http.ResponseWriter, code int, v map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func folderFail(w http.ResponseWriter, code int, msg string) {
	folderReply(w, code, map[string]interface{}{"ok": false, "error": msg})
}

// GET /api/live/plugin-tree — the plugins grouped into folders.
func handleLivePluginTree(w http.ResponseWriter, r *http.Request) {
	tree, note, err := loadPluginTree()
	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "tree": tree, "note": note})
}

// GET  /api/live/plugin-folders — the file as it is.
// PUT  /api/live/plugin-folders — replace the whole file.
func handleLivePluginFolders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		pluginFoldersMu.Lock()
		cfg, err := plugintree.Load(pluginFoldersFile)
		pluginFoldersMu.Unlock()
		if err != nil {
			folderFail(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if cfg.Folders == nil {
			cfg.Folders = []plugintree.Folder{}
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "config": cfg})
	case http.MethodPut:
		var cfg plugintree.Config
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&cfg); err != nil {
			folderFail(w, http.StatusBadRequest, "bad JSON: "+err.Error())
			return
		}
		if err := cfg.Validate(); err != nil {
			folderFail(w, http.StatusBadRequest, err.Error())
			return
		}
		pluginFoldersMu.Lock()
		err := plugintree.Save(pluginFoldersFile, cfg)
		pluginFoldersMu.Unlock()
		if err != nil {
			folderFail(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, map[string]interface{}{"ok": true})
	default:
		http.Error(w, "GET or PUT required", http.StatusMethodNotAllowed)
	}
}

type folderEdit struct {
	Plugin string   `json:"plugin"`
	Path   []string `json:"path"`
}

func handleLivePluginFoldersAdd(w http.ResponseWriter, r *http.Request) {
	editPluginFolders(w, r, func(c *plugintree.Config, e folderEdit) error {
		return c.Add(e.Path, e.Plugin)
	})
}

func handleLivePluginFoldersRemove(w http.ResponseWriter, r *http.Request) {
	editPluginFolders(w, r, func(c *plugintree.Config, e folderEdit) error {
		c.Remove(e.Path, e.Plugin)
		return nil
	})
}

// editPluginFolders: lock, load, change, validate, save. A file that is not
// valid is not overwritten by an edit: the user fixes it first.
func editPluginFolders(w http.ResponseWriter, r *http.Request, change func(*plugintree.Config, folderEdit) error) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var e folderEdit
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&e); err != nil || e.Plugin == "" {
		folderFail(w, http.StatusBadRequest, "plugin and path required")
		return
	}
	pluginFoldersMu.Lock()
	defer pluginFoldersMu.Unlock()
	cfg, err := plugintree.Load(pluginFoldersFile)
	if err != nil {
		folderFail(w, http.StatusUnprocessableEntity, "plugin-folders.json is not valid: "+err.Error())
		return
	}
	if err := change(&cfg, e); err != nil {
		folderFail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := cfg.Validate(); err != nil {
		folderFail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := plugintree.Save(pluginFoldersFile, cfg); err != nil {
		folderFail(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
}
