# Plugin Folders Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Group the plugin list into nested folders in both the Shadow UI PLUGINS tab and the web Browser's Plugins view, with folders editable from the web Browser.

**Architecture:** A user file on Push, `/data/push-hack/plugin-folders.json`, maps folders to plugin names. A new pure package `plugintree` turns Live's flat plugin list plus that file into a tree. push-manager serves the tree and edit routes; both UIs show it. A plugin that is in no folder falls back to a folder named after its vendor. The PushHack Remote Script does not change.

**Tech Stack:** Go (push-manager `main` package and a new `plugintree` package), vanilla JS in `ui/app.js`, Python 3 for the generator script.

**Spec:** the conversation of 2026-11-01 (decisions: JSON file on Push; folders for all Airwindopedia categories; web Browser can edit). Related: `docs/vst3-on-push3.md` and `hacks/push-manager/README.md` (PLUGINS tab).

## Global Constraints

- Branch `feat/plugin-folders`. **No `git commit`, no push** unless the user asks. Leave work uncommitted and say it is ready. (User rule, `~/.claude/CLAUDE.md`.)
- Text that reaches the Push screen is **printable ASCII only** (`basicfont.Face7x13`). Folder names are validated as printable ASCII, 1 to 40 characters. Plugin names come from Live and are drawn through `text.Truncate` as today.
- Config path: `/data/push-hack/plugin-folders.json`, in a package variable so tests can point at a temp path. Never write outside `/data/push-hack/`.
- Do not change the PushHack Remote Script or the `/api/live/plugins` reply. Do not bump `SCRIPT_VERSION`.
- Rebuild and `git add` `hacks/push-manager/push-manager` in the same change (CLAUDE.md "Committed pre-built binaries").
- Keep `CLAUDE.md`, `docs/`, `hacks/push-manager/README.md`, and `CHANGELOG.md` (`## [Unreleased]`) in sync. Docs use the `simple-english` skill; code comments use the `caveman` skill.
- Main-package tests cannot build on macOS (`coredisplay.USB` undefined). Logic with tests goes in `plugintree` (runs on the Mac). Main-package code is checked with `GOOS=linux GOARCH=amd64 go vet ./...`, plus the Docker run in Task 6.

## Review Focus

- A plugin listed in the file but not scanned: skipped, no empty row, no error.
- A folder that ends up empty (all its plugins missing): hidden, with its parents if they become empty.
- Missing, empty or invalid `plugin-folders.json`: the list still shows, grouped by vendor. An invalid file shows a status note and never blocks loading.
- One plugin in several folders: shown in each; loading from any loads the same plugin.
- Plugins with the vendor `airwindows` that are in no folder: they go into the top-level folder named `Airwindows` (case-insensitive match) when it exists, not into a second folder.
- Two writers (two browser tabs) editing at once: the file stays valid JSON (atomic write, one lock).
- A folder name with a non-ASCII character or `/`: the edit route returns 400 and the file is unchanged.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `hacks/push-manager/src/plugintree/tree.go` (new) | Config types, `Load`, `Save`, `Build`, `Add`, `Remove`, `Node.Find`, name validation. No Linux dependency. |
| `hacks/push-manager/src/plugintree/tree_test.go` (new) | Tests for all of the above. |
| `hacks/push-manager/src/plugin_folders_http.go` (new) | HTTP handlers and the lock around the file. |
| `hacks/push-manager/src/plugin_folders_http_test.go` (new) | Handler tests (run in Docker, Task 6). |
| `hacks/push-manager/src/plugins_panel.go` | Folder navigation in the Shadow UI. |
| `hacks/push-manager/src/plugins_panel_test.go` | Navigation tests. |
| `hacks/push-manager/src/main.go` | Register routes (near line 576). |
| `hacks/push-manager/src/ui/app.js`, `ui/index.html` | Web Browser folder view and editing. |
| `scripts/VST/make-plugin-folders.py` (new) | Make the file from `Airwindopedia.txt`. |
| `resources/VST/Airwindows/plugin-folders.json` (new) | Generated file, all Airwindopedia categories. |
| `Makefile` | Add `./plugintree` to `test-manager`. |
| Docs | `hacks/push-manager/README.md`, `docs/api-reference.md`, `docs/vst3-on-push3.md`, `CHANGELOG.md`, `CLAUDE.md` if a rule changes. |

## File format

```json
{
  "folders": [
    {"name": "Airwindows", "folders": [
      {"name": "Reverb", "plugins": ["Reverb", "MatrixVerb", "kPlateA"]},
      {"name": "Tape",   "plugins": ["Tape", "ToTape9"]}
    ]},
    {"name": "Synths", "plugins": ["Surge XT", "Dexed"]}
  ]
}
```

Rules, in this order:

1. Folders and plugins keep the order of the file. The author controls it (Airwindopedia lists the best effects first).
2. A plugin name must match a scanned plugin name exactly. Names that match nothing are ignored.
3. A plugin placed in at least one folder is not shown again in a vendor folder.
4. An unplaced plugin goes into a folder named after its vendor (`Other` if the vendor is empty). If a top-level folder with the same name (case-insensitive) exists, the plugin goes there, after that folder's own entries. Otherwise a new top-level folder is added at the end. Vendor folders are sorted by name, plugins inside them by name (case-insensitive).
5. A folder with no plugins in it or below it is removed.

## API

| Route | Body | Reply |
|-------|------|-------|
| `GET /api/live/plugin-tree` | none | `{"ok":true,"tree":{"name":"","folders":[...],"plugins":[...]},"note":""}`. A plugin leaf is the same object as in `/api/live/plugins`. `note` is non-empty when the file is invalid. |
| `GET /api/live/plugin-folders` | none | `{"ok":true,"config":{"folders":[...]}}` |
| `PUT /api/live/plugin-folders` | `{"folders":[...]}` | `{"ok":true}` or 400 with `{"ok":false,"error":"..."}` |
| `POST /api/live/plugin-folders/add` | `{"plugin":"Surge XT","path":["Synths","Lead"]}` | `{"ok":true}`; creates missing folders; idempotent |
| `POST /api/live/plugin-folders/remove` | `{"plugin":"Surge XT","path":["Synths"]}` | `{"ok":true}`; idempotent; keeps the (now empty) folder in the file |

---

### Task 1: `plugintree` package

**Files:**
- Create: `hacks/push-manager/src/plugintree/tree.go`
- Test: `hacks/push-manager/src/plugintree/tree_test.go`
- Modify: `Makefile` (line 7: add `./plugintree`)

**Interfaces:**
- Produces (package `plugintree`):
  - `type Plugin struct { Vendor string \`json:"vendor"\`; Name string \`json:"name"\`; Presets []string \`json:"presets"\` }` (same JSON as `bridgePlugin`)
  - `type Folder struct { Name string \`json:"name"\`; Plugins []string \`json:"plugins,omitempty"\`; Folders []Folder \`json:"folders,omitempty"\` }`
  - `type Config struct { Folders []Folder \`json:"folders"\` }`
  - `type Node struct { Name string \`json:"name"\`; Folders []*Node \`json:"folders"\`; Plugins []Plugin \`json:"plugins"\` }`
  - `func Load(path string) (Config, error)` (missing file: empty Config, nil error)
  - `func Save(path string, c Config) error` (write temp file in the same folder, `os.Rename`)
  - `func Build(plugins []Plugin, c Config) *Node` (the root has an empty `Name`)
  - `func (n *Node) Find(path []string) *Node` (nil if absent)
  - `func (n *Node) Count() int` (plugins in this node and below)
  - `func (c *Config) Add(path []string, plugin string) error`
  - `func (c *Config) Remove(path []string, plugin string)`
  - `func ValidName(s string) bool` (printable ASCII 0x20-0x7E, no `/`, trimmed length 1 to 40)
  - `func (c Config) Validate() error` (every folder name `ValidName`, depth at most 8)

- [ ] **Step 1: Write the failing tests** in `tree_test.go`, one test per rule above:
  - `TestBuildKeepsFileOrder`, `TestBuildSkipsUnscanned`, `TestBuildHidesEmptyFolders`
  - `TestBuildPlacedPluginNotInVendorFolder`, `TestBuildVendorFallbackSortedAndOther`
  - `TestBuildVendorMergesIntoTopLevelFolder` (vendor `airwindows`, top-level `Airwindows`: no second folder)
  - `TestBuildPluginInTwoFolders`
  - `TestLoadMissingFile` (empty Config, nil), `TestLoadInvalidJSON` (error, empty Config)
  - `TestSaveAtomic` (no `.tmp` left; the file reads back equal)
  - `TestAddCreatesPath`, `TestAddIdempotent`, `TestAddRejectsBadName` (`"a/b"`, `"café"`, `""`, 41 characters), `TestRemoveKeepsEmptyFolder`
  - `TestFindAndCount`
- [ ] **Step 2: Run** `cd hacks/push-manager/src && go test ./plugintree`. Expected: FAIL (package does not compile).
- [ ] **Step 3: Implement** the interface above in `tree.go`. `Build` follows the five rules; make a plain recursive function over `Folder`, with a set of scanned names and a set of placed names.
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Add `./plugintree` to `make test-manager`**, run `make test-manager`. Expected: PASS.

### Task 2: HTTP routes

**Files:**
- Create: `hacks/push-manager/src/plugin_folders_http.go`, `plugin_folders_http_test.go`
- Modify: `hacks/push-manager/src/main.go` (register next to `/api/live/plugins`, line 576), `plugins_panel.go` (remove the local `bridgePlugin` type: use `plugintree.Plugin`, or convert; keep `live_bridge.go` JSON unchanged)

**Interfaces:**
- Consumes: Task 1 package.
- Produces:
  - `var pluginFoldersFile = "/data/push-hack/plugin-folders.json"`
  - `var pluginFoldersMu sync.Mutex` (guards every read-modify-write of the file)
  - `func loadPluginTree() (*plugintree.Node, string, error)`: calls `livePlugins()`, `plugintree.Load`, `plugintree.Build`; the string is the note ("plugin-folders.json is not valid: ..." or "")
  - handlers `handleLivePluginTree`, `handleLivePluginFolders`, `handleLivePluginFoldersAdd`, `handleLivePluginFoldersRemove` (names as in the API table)

- [ ] **Step 1: Write failing tests** (`plugin_folders_http_test.go`, temp file via `pluginFoldersFile`): `TestFoldersAddThenGet`, `TestFoldersAddBadNameIs400FileUnchanged`, `TestFoldersPutInvalidIs400`, `TestFoldersConcurrentAddsKeepValidJSON` (20 goroutines), `TestLoadPluginTreeInvalidFileGivesNoteAndVendorTree`. Stub `livePlugins` through a package variable `var livePluginsFn = livePlugins`.
- [ ] **Step 2: Implement** the handlers. Every write: lock, `Load`, change, `Validate`, `Save`, unlock. Reply shape from the API table; HTTP 400 for validation errors, 405 for a wrong method.
- [ ] **Step 3: Check** `cd hacks/push-manager/src && GOOS=linux GOARCH=amd64 go vet ./...`. Expected: no output.
- [ ] **Step 4:** the handler tests run in Task 6.

### Task 3: Shadow UI folder navigation

**Files:**
- Modify: `hacks/push-manager/src/plugins_panel.go`, `plugins_panel_test.go`

**Interfaces:**
- Consumes: `loadPluginTree()` (Task 2), `plugintree.Node`.
- Produces: `PluginsPanel` fields `root *plugintree.Node`, `trail []string` (folder names from the root to the current folder), `saved []navPos` (cursor and scroll per level, `type navPos struct{ cursor, scroll int }`), `note string`. Row order in a folder: sub-folders first, then plugins.

Behaviour (decide here; the implementer does not choose):
- Folder row: `Sidebar_Folder.png` icon, text `Name  N` (N is `Count()`). Plugin row: `Browser_VST3Effect.png` icon. ASCII only.
- Press, D-pad right or jog press on a folder: push `navPos`, append to `trail`, cursor 0. On a plugin: as today (open presets, or load if it has none). Soft button Load on a folder: opens it, does not load.
- Back (left, jog-left, bottom button 2): presets level goes back to the folder as today; a folder level goes up one and restores the saved cursor and scroll. At the root, Back does nothing.
- Breadcrumb: `Plugins` at the root, else `Plugins / A / B`. When it is too long for the screen, keep the end (`text.Truncate` from the left is not available: cut the middle with `...`, ASCII).
- `refresh()` rebuilds the tree. If `trail` no longer exists, reset to the root. A non-empty `note` shows as the status until the next successful refresh.
- Remove the `rowCount()` use of `p.plugins`: it uses the current folder's rows.

- [ ] **Step 1: Write failing tests** in `plugins_panel_test.go` (keep the two existing ones working; adapt `TestPluginsPanelNavigation` to a root with only plugins): `TestPluginsPanelOpenFolderAndBack` (cursor and scroll restored), `TestPluginsPanelFoldersListedBeforePlugins`, `TestPluginsPanelBackAtRootDoesNothing`, `TestPluginsPanelLoadButtonOnFolderOpens`, `TestPluginsPanelBreadcrumbASCII` (only bytes 0x20-0x7E), `TestPluginsPanelRefreshResetsMissingTrail`.
- [ ] **Step 2: Implement** the panel changes as specified.
- [ ] **Step 3: Check** `GOOS=linux GOARCH=amd64 go vet ./...`. The tests run in Task 6.

### Task 4: Web Browser folder view and editing

**Files:**
- Modify: `hacks/push-manager/src/ui/app.js` (the "Plugins (VST3)" block, lines 1917-1970), `ui/index.html` (`#pl-box`, line 499)

**Interfaces:**
- Consumes: `GET /api/live/plugin-tree`, `GET /api/live/plugins`, add and remove routes.
- Produces: JS state `PL.tree`, `PL.path` (array of names), functions `plOpen(name)`, `plUp(depth)`, `plAddToFolder(i)`, `plRemoveFromFolder(i)`. `PL.list` (flat) stays for search and for `plLoad(i, ...)`.

Behaviour:
- No search text: show the breadcrumb (`Plugins / A / B`, each part clickable), then folder rows (name and count), then plugin rows as today (preset select, Load, Load preset).
- With search text: flat list over all plugins as today, regardless of the folder. A result row shows its vendor.
- Each plugin row gets a **Folder...** button. It asks (`prompt`) for a path such as `Airwindows/Reverb`, splits on `/`, and calls the add route. A 400 shows the server's error in a toast. After success reload the tree.
- Inside a folder, each plugin row also gets **Remove**. It calls the remove route with the current `PL.path`.
- The note from the tree reply, when set, shows in `#pl-note`.
- All text built into HTML goes through `esc(...)` (the existing helper).

- [ ] **Step 1:** Edit `plReload()` to fetch both endpoints (`Promise.all`); keep the existing error text.
- [ ] **Step 2:** Replace `plRender()` as described. Reuse `plLoad`.
- [ ] **Step 3: Check by hand** (no JS test harness in the repo): `node --check hacks/push-manager/src/ui/app.js` prints nothing. Then Task 6 for the real page.

### Task 5: Generator and the Airwindows file

**Files:**
- Create: `scripts/VST/make-plugin-folders.py`, `resources/VST/Airwindows/plugin-folders.json`

**Interfaces:**
- Produces: `make-plugin-folders.py --airwindopedia PATH --plugins-dir DIR --out FILE [--root-name Airwindows]`.

Behaviour: read the category lines of `Airwindopedia.txt` (lines of the form `Category: Name, Name, ...`) that follow the heading `# Categories`, up to the first line that starts with `#`. One line has a period where a comma belongs (`XBandpass. XHighpass`): split on that too. Match each name to a folder name in `--plugins-dir` (for example `plugins/LinuxVST/src`) ignoring spaces and case (`Hard Vacuum` is `HardVacuum`). Names with no match are listed on stderr, not written. Write one top-level folder `Airwindows` with one sub-folder per category, in the order of the file. Plugin order inside a category is the order in `Airwindopedia.txt`.

- [ ] **Step 1: Write the failing test** in `scripts/VST/test_make_plugin_folders.py` (`unittest`) with a 3-line fake Airwindopedia and a fake plugin dir: assert names with spaces match, unmatched names are reported, order is kept. Run `python3 -m unittest scripts/VST/test_make_plugin_folders.py`. Expected: FAIL.
- [ ] **Step 2: Implement** the script. Expected after: PASS.
- [ ] **Step 3: Generate** `resources/VST/Airwindows/plugin-folders.json` from `~/Developer/airwindows` (`--airwindopedia ~/Developer/airwindows/Airwindopedia.txt --plugins-dir ~/Developer/airwindows/plugins/LinuxVST/src`). Check that it parses and that the 7 categories used for the install (Ambience, Amp Sims, Distortion, Effects, Lo-Fi, Reverb, Tape) hold 161 unique names.

### Task 6: Verify, device test, docs, binary

**Files:**
- Modify: `hacks/push-manager/README.md` (PLUGINS tab and Browser Plugins view), `docs/api-reference.md` (new routes), `docs/vst3-on-push3.md` (new "Folders" section, how to install the Airwindows file), `CHANGELOG.md`, `hacks/push-manager/push-manager` (rebuilt)

- [ ] **Step 1: Run the main-package tests on Linux.** `cd hacks/push-manager/src && docker run --rm --platform linux/amd64 -v "$PWD/../../..:/repo" -w /repo/hacks/push-manager/src golang:1.25 go test -run 'Plugin' .` (`go.mod` says Go 1.25.0). Expected: PASS. If the container cannot build the display code, report it and say which tests did not run.
- [ ] **Step 2: Build the binary:** `cd hacks/push-manager && PATH=$PATH:/usr/local/go/bin make`. Then `git add push-manager`.
- [ ] **Step 3: Deploy to the Push** (`./scripts/install.sh --hack push-manager`) and copy `resources/VST/Airwindows/plugin-folders.json` to `/data/push-hack/plugin-folders.json`.
- [ ] **Step 4: Ask the user to check on the device** and give answer options: Shadow UI shows folders first (yes / plugins still flat / crash); open a folder, Back restores the position (yes / no); web Browser shows the folder tree, Folder... and Remove work (yes / no / error text). Wait for the answer. Do not commit before it.
- [ ] **Step 5: Write the docs** (simple-english) and the `CHANGELOG.md` entry (Added: plugin folders in the PLUGINS tab and the web Browser; `plugin-folders.json`; the new routes).
- [ ] **Step 6:** Leave everything uncommitted. Tell the user it is ready.

## Out of scope

Drag and drop; renaming or deleting folders in the web UI (edit the file); a folder level in Push's own browser; folders in the PushHack Remote Script; syncing the file across Pushes.

## Rulings made while executing (2026-11-01)

- **No commits.** The user's rule is to leave work uncommitted, so the plan's commit steps were not run.
- **Category block.** The plan said "before the first line that starts with `# `". The real file puts the block after `# Categories`, so the generator starts there and stops at the first `#` line.
- **Invalid file and edits.** The edit routes return 422 and leave the file alone when the file on disk is not valid (the plan did not say). `PUT` still replaces a bad file, so the user can repair it from the web.
- **`bridgePlugin`** is now an alias of `plugintree.Plugin`, so `live_bridge.go` JSON and its tests did not change.
- **Generated file stays local.** `resources/VST/` is in `.gitignore`, so `resources/VST/Airwindows/plugin-folders.json` is not tracked. The docs tell users to run the generator.
- **Deploy.** Only the `push-manager` binary and the folders file were copied (not the full `install.sh`, which also rewrites `/etc/init.d` and the Remote Script). The old binary is on the Push as `push-manager.prev-folders`.
