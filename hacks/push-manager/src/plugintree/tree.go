// Package plugintree groups Live's flat plugin list into nested folders.
//
// The folders come from a user file (plugin-folders.json). A plugin in no
// folder falls back to a folder named after its vendor. No Linux code here,
// so it tests on any computer.
package plugintree

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxNameLen = 40
	maxDepth   = 8
	otherName  = "Other"
)

// Plugin is one scanned plugin. Same JSON as the PushHack list_plugins reply.
type Plugin struct {
	Vendor  string   `json:"vendor"`
	Name    string   `json:"name"`
	Presets []string `json:"presets"`
}

// Folder is one folder in the user file.
type Folder struct {
	Name    string   `json:"name"`
	Plugins []string `json:"plugins,omitempty"`
	Folders []Folder `json:"folders,omitempty"`
}

// Config is the whole user file.
type Config struct {
	Folders []Folder `json:"folders"`
}

// Node is one folder of the built tree. The root has an empty Name.
type Node struct {
	Name    string   `json:"name"`
	Folders []*Node  `json:"folders"`
	Plugins []Plugin `json:"plugins"`
}

// ValidName: printable ASCII only (the panel font has nothing else), no "/",
// no edge spaces, 1 to 40 bytes.
func ValidName(s string) bool {
	if s == "" || len(s) > maxNameLen || s != strings.TrimSpace(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e || s[i] == '/' {
			return false
		}
	}
	return true
}

// Validate checks every folder name and the depth.
func (c Config) Validate() error { return validateFolders(c.Folders, 1) }

func validateFolders(fs []Folder, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("folders nested deeper than %d", maxDepth)
	}
	for _, f := range fs {
		if !ValidName(f.Name) {
			return fmt.Errorf("bad folder name %+q", f.Name)
		}
		if err := validateFolders(f.Folders, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// Load reads the file. A missing file is an empty Config, not an error. An
// invalid file returns an empty Config and the error.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Save writes the file through a temp file and a rename, so a reader never
// sees half a file.
func Save(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".plugin-folders-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Count is the number of plugins in this node and below.
func (n *Node) Count() int {
	c := len(n.Plugins)
	for _, f := range n.Folders {
		c += f.Count()
	}
	return c
}

// Find follows folder names from this node. Nil if a name is missing.
func (n *Node) Find(path []string) *Node {
	cur := n
	for _, name := range path {
		var next *Node
		for _, f := range cur.Folders {
			if f.Name == name {
				next = f
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

// Build makes the tree: file folders first (file order), then the vendor
// fallback for plugins no folder holds. Empty folders are dropped.
func Build(plugins []Plugin, c Config) *Node {
	byName := map[string]Plugin{}
	for _, p := range plugins {
		if _, dup := byName[p.Name]; !dup {
			byName[p.Name] = p
		}
	}
	placed := map[string]bool{}
	root := newNode("")

	var walk func(parent *Node, fs []Folder)
	walk = func(parent *Node, fs []Folder) {
		for _, f := range fs {
			n := newNode(f.Name)
			for _, name := range f.Plugins {
				if p, ok := byName[name]; ok {
					n.Plugins = append(n.Plugins, p)
					placed[name] = true
				}
			}
			walk(n, f.Folders)
			if n.Count() > 0 {
				parent.Folders = append(parent.Folders, n)
			}
		}
	}
	walk(root, c.Folders)

	byVendor := map[string][]Plugin{}
	for _, p := range plugins {
		if placed[p.Name] || byName[p.Name].Name == "" {
			continue
		}
		placed[p.Name] = true // a duplicate scan entry is shown once
		v := p.Vendor
		if v == "" {
			v = otherName
		}
		byVendor[v] = append(byVendor[v], p)
	}
	vendors := make([]string, 0, len(byVendor))
	for v := range byVendor {
		vendors = append(vendors, v)
	}
	sort.Slice(vendors, func(i, j int) bool { return lessFold(vendors[i], vendors[j]) })
	for _, v := range vendors {
		ps := byVendor[v]
		sort.SliceStable(ps, func(i, j int) bool { return lessFold(ps[i].Name, ps[j].Name) })
		target := (*Node)(nil)
		for _, f := range root.Folders {
			if strings.EqualFold(f.Name, v) {
				target = f
				break
			}
		}
		if target == nil {
			target = newNode(v)
			root.Folders = append(root.Folders, target)
		}
		target.Plugins = append(target.Plugins, ps...)
	}
	return root
}

func newNode(name string) *Node {
	return &Node{Name: name, Folders: []*Node{}, Plugins: []Plugin{}}
}

func lessFold(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
}

// Add puts plugin in the folder at path, making missing folders. Idempotent.
func (c *Config) Add(path []string, plugin string) error {
	if len(path) == 0 || plugin == "" {
		return errors.New("path and plugin are required")
	}
	if len(path) > maxDepth {
		return fmt.Errorf("folders nested deeper than %d", maxDepth)
	}
	for _, name := range path {
		if !ValidName(name) {
			return fmt.Errorf("bad folder name %+q", name)
		}
	}
	list := &c.Folders
	var f *Folder
	for _, name := range path {
		f = findOrAdd(list, name)
		list = &f.Folders
	}
	for _, p := range f.Plugins {
		if p == plugin {
			return nil
		}
	}
	f.Plugins = append(f.Plugins, plugin)
	return nil
}

func findOrAdd(list *[]Folder, name string) *Folder {
	for i := range *list {
		if (*list)[i].Name == name {
			return &(*list)[i]
		}
	}
	*list = append(*list, Folder{Name: name})
	return &(*list)[len(*list)-1]
}

// Remove takes plugin out of the folder at path. The folder stays in the
// file. A missing folder or plugin is not an error.
func (c *Config) Remove(path []string, plugin string) {
	list := c.Folders
	var f *Folder
	for _, name := range path {
		f = nil
		for i := range list {
			if list[i].Name == name {
				f = &list[i]
				break
			}
		}
		if f == nil {
			return
		}
		list = f.Folders
	}
	if f == nil {
		return
	}
	out := f.Plugins[:0]
	for _, p := range f.Plugins {
		if p != plugin {
			out = append(out, p)
		}
	}
	f.Plugins = out
}
