// Covers the plugin-folders routes: edits reach the file, bad names are
// refused without touching it, and many writers leave valid JSON.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"push-manager/plugintree"
)

func useTempFolders(t *testing.T) string {
	t.Helper()
	oldFile, oldFn := pluginFoldersFile, livePluginsFn
	pluginFoldersFile = filepath.Join(t.TempDir(), "plugin-folders.json")
	livePluginsFn = func() ([]bridgePlugin, error) {
		return []bridgePlugin{{Vendor: "Acme", Name: "A"}, {Vendor: "Acme", Name: "B"}}, nil
	}
	t.Cleanup(func() { pluginFoldersFile, livePluginsFn = oldFile, oldFn })
	return pluginFoldersFile
}

func post(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	return rec
}

func TestFoldersAddThenGet(t *testing.T) {
	useTempFolders(t)
	if rec := post(handleLivePluginFoldersAdd, `{"plugin":"A","path":["Fx","Verb"]}`); rec.Code != 200 {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	rec := httptest.NewRecorder()
	handleLivePluginTree(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var r struct {
		Tree plugintree.Node `json:"tree"`
		Note string          `json:"note"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if n := (&r.Tree).Find([]string{"Fx", "Verb"}); n == nil || len(n.Plugins) != 1 || n.Plugins[0].Name != "A" {
		t.Fatalf("tree: %s", rec.Body)
	}
	if acme := (&r.Tree).Find([]string{"Acme"}); acme == nil || len(acme.Plugins) != 1 || acme.Plugins[0].Name != "B" {
		t.Fatalf("vendor fallback for the unplaced plugin: %s", rec.Body)
	}
}

func TestFoldersAddBadNameIs400FileUnchanged(t *testing.T) {
	path := useTempFolders(t)
	post(handleLivePluginFoldersAdd, `{"plugin":"A","path":["Fx"]}`)
	before, _ := os.ReadFile(path)
	for _, body := range []string{
		`{"plugin":"A","path":["a/b"]}`,
		`{"plugin":"A","path":["café"]}`,
		`{"plugin":"A","path":[]}`,
		`{"path":["Fx"]}`,
		`not json`,
	} {
		if rec := post(handleLivePluginFoldersAdd, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s -> %d, want 400", body, rec.Code)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("file changed after refused edits")
	}
}

func TestFoldersPutInvalidIs400(t *testing.T) {
	path := useTempFolders(t)
	rec := httptest.NewRecorder()
	handleLivePluginFolders(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"folders":[{"name":"bad/name"}]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d", rec.Code)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("file written for an invalid config")
	}
}

func TestFoldersConcurrentAddsKeepValidJSON(t *testing.T) {
	path := useTempFolders(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]any{"plugin": "P" + string(rune('a'+i)), "path": []string{"F"}})
			post(handleLivePluginFoldersAdd, string(body))
		}(i)
	}
	wg.Wait()
	cfg, err := plugintree.Load(path)
	if err != nil || len(cfg.Folders) != 1 || len(cfg.Folders[0].Plugins) != 20 {
		t.Fatalf("cfg %+v err %v", cfg, err)
	}
}

func TestLoadPluginTreeInvalidFileGivesNoteAndVendorTree(t *testing.T) {
	path := useTempFolders(t)
	os.WriteFile(path, []byte("{broken"), 0o644)
	tree, note, err := loadPluginTree()
	if err != nil || note == "" {
		t.Fatalf("note %q err %v", note, err)
	}
	if acme := tree.Find([]string{"Acme"}); acme == nil || len(acme.Plugins) != 2 {
		t.Fatalf("tree must fall back to vendors: %+v", tree)
	}
}

func TestFoldersEditRefusedWhenFileInvalid(t *testing.T) {
	path := useTempFolders(t)
	os.WriteFile(path, []byte("{broken"), 0o644)
	if rec := post(handleLivePluginFoldersAdd, `{"plugin":"A","path":["Fx"]}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code %d, want 422", rec.Code)
	}
	if b, _ := os.ReadFile(path); string(b) != "{broken" {
		t.Fatal("invalid file was overwritten by an edit")
	}
}
