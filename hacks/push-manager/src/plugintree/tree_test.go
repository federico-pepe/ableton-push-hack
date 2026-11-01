package plugintree

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func pl(vendor, name string) Plugin { return Plugin{Vendor: vendor, Name: name} }

func names(ps []Plugin) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func folderNames(ns []*Node) []string {
	out := []string{}
	for _, n := range ns {
		out = append(out, n.Name)
	}
	return out
}

func TestBuildKeepsFileOrder(t *testing.T) {
	ps := []Plugin{pl("a", "A"), pl("a", "B"), pl("a", "C")}
	c := Config{Folders: []Folder{
		{Name: "Z", Plugins: []string{"C", "A"}},
		{Name: "Y", Plugins: []string{"B"}},
	}}
	root := Build(ps, c)
	if got := folderNames(root.Folders); !reflect.DeepEqual(got, []string{"Z", "Y"}) {
		t.Fatalf("folder order %v", got)
	}
	if got := names(root.Folders[0].Plugins); !reflect.DeepEqual(got, []string{"C", "A"}) {
		t.Fatalf("plugin order %v", got)
	}
}

func TestBuildSkipsUnscanned(t *testing.T) {
	c := Config{Folders: []Folder{{Name: "F", Plugins: []string{"Ghost", "A"}}}}
	root := Build([]Plugin{pl("v", "A")}, c)
	if got := names(root.Folders[0].Plugins); !reflect.DeepEqual(got, []string{"A"}) {
		t.Fatalf("got %v", got)
	}
}

func TestBuildHidesEmptyFolders(t *testing.T) {
	c := Config{Folders: []Folder{
		{Name: "Outer", Folders: []Folder{{Name: "Inner", Plugins: []string{"Ghost"}}}},
		{Name: "Keep", Plugins: []string{"A"}},
	}}
	root := Build([]Plugin{pl("v", "A")}, c)
	if got := folderNames(root.Folders); !reflect.DeepEqual(got, []string{"Keep"}) {
		t.Fatalf("folders %v", got)
	}
}

func TestBuildPlacedPluginNotInVendorFolder(t *testing.T) {
	c := Config{Folders: []Folder{{Name: "F", Plugins: []string{"A"}}}}
	root := Build([]Plugin{pl("Acme", "A"), pl("Acme", "B")}, c)
	acme := root.Find([]string{"Acme"})
	if acme == nil || !reflect.DeepEqual(names(acme.Plugins), []string{"B"}) {
		t.Fatalf("vendor folder %+v", acme)
	}
}

func TestBuildVendorFallbackSortedAndOther(t *testing.T) {
	ps := []Plugin{pl("zed", "z1"), pl("", "o1"), pl("Acme", "b"), pl("Acme", "A")}
	root := Build(ps, Config{})
	if got := folderNames(root.Folders); !reflect.DeepEqual(got, []string{"Acme", "Other", "zed"}) {
		t.Fatalf("folders %v", got)
	}
	if got := names(root.Folders[0].Plugins); !reflect.DeepEqual(got, []string{"A", "b"}) {
		t.Fatalf("plugins %v", got)
	}
}

func TestBuildVendorMergesIntoTopLevelFolder(t *testing.T) {
	ps := []Plugin{pl("airwindows", "Reverb"), pl("airwindows", "Extra")}
	c := Config{Folders: []Folder{{Name: "Airwindows", Folders: []Folder{{Name: "Reverb", Plugins: []string{"Reverb"}}}}}}
	root := Build(ps, c)
	if got := folderNames(root.Folders); !reflect.DeepEqual(got, []string{"Airwindows"}) {
		t.Fatalf("top-level folders %v", got)
	}
	aw := root.Folders[0]
	if !reflect.DeepEqual(names(aw.Plugins), []string{"Extra"}) || len(aw.Folders) != 1 {
		t.Fatalf("airwindows node %+v", aw)
	}
}

func TestBuildPluginInTwoFolders(t *testing.T) {
	c := Config{Folders: []Folder{
		{Name: "One", Plugins: []string{"A"}},
		{Name: "Two", Plugins: []string{"A"}},
	}}
	root := Build([]Plugin{pl("v", "A")}, c)
	if len(root.Folders) != 2 || root.Count() != 2 {
		t.Fatalf("tree %+v count %d", root, root.Count())
	}
}

func TestLoadMissingFile(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(c.Folders) != 0 {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.json")
	os.WriteFile(p, []byte("{not json"), 0o644)
	c, err := Load(p)
	if err == nil || len(c.Folders) != 0 {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}

func TestLoadInvalidNameIsError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.json")
	os.WriteFile(p, []byte(`{"folders":[{"name":"café","plugins":["A"]}]}`), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("non-ASCII folder name must be rejected")
	}
}

func TestSaveAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.json")
	want := Config{Folders: []Folder{{Name: "F", Plugins: []string{"A"}}}}
	if err := Save(p, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v err %v", got, err)
	}
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 1 {
		t.Fatalf("leftover files: %v", ents)
	}
}

func TestAddCreatesPath(t *testing.T) {
	var c Config
	if err := c.Add([]string{"A", "B"}, "P"); err != nil {
		t.Fatal(err)
	}
	if c.Folders[0].Name != "A" || c.Folders[0].Folders[0].Name != "B" ||
		!reflect.DeepEqual(c.Folders[0].Folders[0].Plugins, []string{"P"}) {
		t.Fatalf("%+v", c)
	}
}

func TestAddIdempotent(t *testing.T) {
	var c Config
	c.Add([]string{"A"}, "P")
	c.Add([]string{"A"}, "P")
	if len(c.Folders[0].Plugins) != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestAddRejectsBadName(t *testing.T) {
	for _, bad := range []string{"a/b", "café", "", " lead", strings.Repeat("x", 41)} {
		var c Config
		if err := c.Add([]string{bad}, "P"); err == nil {
			t.Errorf("name %q accepted", bad)
		}
		if len(c.Folders) != 0 {
			t.Errorf("name %q changed the config", bad)
		}
	}
	var c Config
	if err := c.Add(nil, "P"); err == nil {
		t.Error("empty path accepted")
	}
	if err := c.Add([]string{"A"}, ""); err == nil {
		t.Error("empty plugin accepted")
	}
}

func TestRemoveKeepsEmptyFolder(t *testing.T) {
	var c Config
	c.Add([]string{"A"}, "P")
	c.Remove([]string{"A"}, "P")
	c.Remove([]string{"Missing"}, "P") // no-op
	if len(c.Folders) != 1 || len(c.Folders[0].Plugins) != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestFindAndCount(t *testing.T) {
	c := Config{Folders: []Folder{{Name: "A", Folders: []Folder{{Name: "B", Plugins: []string{"X", "Y"}}}}}}
	root := Build([]Plugin{pl("v", "X"), pl("v", "Y")}, c)
	if root.Find(nil) != root {
		t.Fatal("empty path is the root")
	}
	if n := root.Find([]string{"A", "B"}); n == nil || n.Count() != 2 {
		t.Fatalf("find A/B: %+v", n)
	}
	if root.Find([]string{"A", "Nope"}) != nil {
		t.Fatal("missing path must be nil")
	}
	if root.Find([]string{"A"}).Count() != 2 {
		t.Fatal("Count must include sub-folders")
	}
}

func TestValidateDepth(t *testing.T) {
	f := Folder{Name: "x"}
	for i := 0; i < 9; i++ {
		f = Folder{Name: "x", Folders: []Folder{f}}
	}
	if err := (Config{Folders: []Folder{f}}).Validate(); err == nil {
		t.Fatal("depth 10 must be rejected")
	}
}
