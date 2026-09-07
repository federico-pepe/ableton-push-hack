# Push Manager

Web-based file browser for Ableton Push 3. Lets you browse, upload, download, rename and delete files on Push from any device on the same network (phone, laptop).

- **Port:** 7701
- **Access:** `http://push.local:7701` (on Push hotspot or local network)
- **Binary:** Go, linux/amd64, ~8–15MB RSS, `Nice=19`, `MemoryMax=64M`
- **Source:** `hacks/push-manager/src/`

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Embedded single-page UI |
| `GET` | `/api/roots` | List configured allowed root directories |
| `GET` | `/api/list?path=<dir>` | Directory listing (JSON array of `Entry`) |
| `GET` | `/api/download?path=<path>` | Download file; if path is a directory, streams a `.zip` |
| `POST` | `/api/upload?path=<dir>` | Upload file(s) into directory. Multipart form. Optional `relativePath` field preserves folder structure for directory uploads. |
| `DELETE` | `/api/delete?path=<path>` | Delete file or empty directory |
| `DELETE` | `/api/delete?path=<path>&recursive=true` | Delete file or directory tree (`os.RemoveAll`) |
| `POST` | `/api/rename` | Rename/move. JSON body: `{"old": "/abs/path", "new": "/abs/path"}` |
| `POST` | `/api/copy` | Copy file or directory tree. JSON body: `{"src": "/abs/path", "dst": "/abs/path"}`. Both paths must be within allowed roots. Destination must not already exist. Chowns result to match destination directory owner. |
| `POST` | `/api/unmount?path=<path>` | Unmount a removable drive. Path must be directly under `/run/media/`. Calls `syscall.Unmount` then removes the udev lock file (`/tmp/.automount-<name>`) so the drive can be re-mounted on next plug. |
| `GET` | `/api/stats` | System stats: CPU %, memory, disk, IP addresses, battery, uptime, hotspot password |
| `GET` | `/api/display/status` | Display hook status: `{connected, mode, frame_seq, width, height}` |
| `POST` | `/api/display/mode` | Set display mode. JSON body: `{"mode": 0\|1\|2}` (0=passthrough, 1=bar overlay, 2=custom). Entering mode 2 clears framebuf to black — no stale pixels from a previous session. |
| `POST` | `/api/display/image` | Upload image to display. Multipart form field `image` (PNG or JPEG). Scales to 960×160. Does **not** auto-set mode — caller must set mode=2 explicitly before sending frames. |
| `GET` | `/api/display/screenshot` | PNG of the current framebuf (960×160). Reads shm back (`shmReadFrame`) and encodes via `bgr565ToImage`+`png.Encode`. Only reflects **push-manager-owned** frames (Shadow UI, OSD, image upload) — the native Ableton UI is never copied into shm in passthrough, so it can't be captured. `X-Display-Mode` response header (`0`/`1`/`2`) lets the client warn when not in takeover (frame is stale). `Content-Disposition: attachment; filename="push-screenshot.png"`. Returns 503 if the hook isn't connected. |
| `GET` | `/api/assets/<path>` | Proxy Push's own PNG assets from `/opt/push3/products/push3/assets/Images/`. Cached 24h. |
| `GET` | `/api/status` | Health check + config summary |
| `GET` | `/api/midi/events?n=<N>` | Return last N MIDI events from ring buffer (default 50). JSON: `{connected, total, write_idx, events[]}` |
| `GET` | `/api/midi/stream` | SSE stream of live MIDI events. Sends `event: connected` on open, then `data: {json}` per event. |
| `POST` | `/api/midi/filter` | Set MIDI intercept. JSON body: `{"enabled": true\|false}`. Writes to `midiflt` shm file read by push_hook.so. When enabled, the `snd_seq_event_input` hook inside Push3's process neutralizes Note On/Off, CC, Poly Pressure, Program Change, Channel Pressure, and Pitch Bend events (types 6–13) before RtMidi reads them — Live sees no input. SysEx and Active Sensing always pass through. |
| `GET` | `/api/midi/filter/status` | Returns `{"enabled": true\|false}` — current MIDI intercept state. |
| `GET` | `/api/midi/ports` | Returns array of ALSA sequencer ports: `[{client, port, name, active, writable}]`. Parses `/proc/asound/seq/clients`. Default lists **readable** ports (for input subscription); `?writable=1` lists **writable** ports (for remap output destinations). |
| `POST` | `/api/midi/subscribe` | Switch subscription target. Body: `{"client": 16, "port": 0}`. Closes current seq fd to interrupt blocked read; goroutine restarts with new target. |
| `GET` | `/api/midi/chords` | List registered hardware chord bindings: `[{name, ccs, description}]`. |
| `POST` | `/api/midi/led` | Set a button or pad LED colour. Body: `{"type":"cc","channel":0,"cc":102,"value":127}` or `{"type":"note","channel":0,"note":36,"velocity":127}`. `channel` is 0-indexed. `value`/`velocity` is a Push colour palette index (0=off). Uses ALSA seq direct delivery to 16:0 — no rawmidi, no subscription needed. Stateless — does not update LED toggle state. |
| `GET` | `/api/midi/led/states` | Returns current LED toggle state: `{"states":{"102":127,"20":0,...}}`. Keys are CC numbers as strings; values are last sent palette index. Only CCs that have been toggled appear. Active only when MIDI intercept is enabled. |
| `DELETE` | `/api/midi/led/states` | Turns off all LEDs tracked by the toggle system (sends CC value=0) and clears the state map. Returns `{"cleared": N}`. |
| `GET` | `/api/midi/palette` | Query the full 128-entry LED RGB color palette via SysEx (command `0x04`). Blocks ~1–2s while querying hardware. Returns JSON array of `{index, r, g, b, w, hex}`. `r/g/b/w` are 8-bit (0–255) packed as two 7-bit SysEx bytes; `w` is white-balance 0–1024; `hex` is `#RRGGBB`. SysEx responses also appear in the MIDI monitor decoded as `Palette[idx] R=… G=… B=…`. Requires MIDI out to be initialized (push-manager must have subscribed). |
| `GET` | `/api/midi/mapping` | List MIDI remap rules + config: `{mappings:{"<src_type>:<ch>:<num>":MidiMapping}, enabled, require_intercept, out_client, out_port}`. |
| `POST` | `/api/midi/mapping` | Upsert one remap rule. Body: `{src_type,src_ch,src_num,relative,out_type,out_ch,out_num,out_min,out_max,name?}` (`src_type`/`out_type` = `cc`\|`note`). Keyed by `<src_type>:<src_ch>:<src_num>`. Persisted to `midi.json`. |
| `DELETE` | `/api/midi/mapping[?key=cc:0:20]` | Delete one rule by key, or all rules if no `key`. |
| `POST` | `/api/midi/mapping/config` | Set remap config. Body: `{enabled, require_intercept, out_client, out_port}`. `enabled` gates all remapping; `require_intercept` restricts it to when MIDI intercept is ON; `out_client`/`out_port` is the destination writable port (from `/api/midi/ports?writable=1`). |
| `GET` | `/api/presets` | Browser Bridge preset index. Query params `filter` (category: `Instruments`/`Audio Effects`/`MIDI Effects`/`Drums`, empty=all) and `q` (case-insensitive name substring). Returns `{count, total, presets:[{name,path,category,is_rack}]}`. Filesystem-scanned (`src/presets.go`); never touches Live. |
| `POST` | `/api/presets/refresh` | Rescan the preset index (after installing Packs). Returns `{ok, count}`. |
| `POST` | `/api/live/load` | Load a preset onto Live's selected track. Body `{"name":"…","category":"Instruments"}`. Sends `load:<root>:<name>` to the PushHackBrowser Remote Script over `127.0.0.1:7704`. Returns `{ok}` or `{ok:false, error}`. Requires the Browser Bridge hack installed + activated in Live. |
| `GET` | `/api/live/tempo` | Returns the current Live song tempo. Sends `get_tempo` to the PushHackBrowser Remote Script and returns `{ok:true, bpm:124.0}`. Requires Browser Bridge activated in Live. Returns `{ok:false, error}` if the Remote Script is unreachable. |
| `GET` | `/api/live/playing` | Returns `{ok:true, playing:true\|false}` — whether Live's transport is currently running. Sends `get_playing` to PushHackBrowser. |
| `POST` | `/api/live/play` | Starts Live's transport. Sends `play` to PushHackBrowser (fire-and-forget). Returns `{ok:true}`. |
| `POST` | `/api/live/stop` | Stops Live's transport. Sends `stop` to PushHackBrowser (fire-and-forget). Returns `{ok:true}`. |
| `GET` | `/api/presets/facets` | Distinct facet values for the Browser filter UI: `{categories, devices, sources, tags}`. |
| `GET` | `/api/hacks/installed` | Every deployed hack, read live off each `/data/push-hack/hacks/<id>/hack.json`: `[{id, name, port, web_ui?, enabled}]` sorted by `id`. `enabled` is computed, not read from `hack.json` — it mirrors Push Hack Catalog's own boot-autostart check (see below), not "currently running". The web UI polls it every 10s to hide a feature whose dependency is missing (the Browser tab needs `browser-bridge`) and to build the menu-bar links — see [Menu bar links](#menu-bar-links-for-other-hacks). |
| `GET` | `/api/ui/tabs` | The one ordered list behind both navigations: `{max: 8, entries: [{id, label, source, has_shadow, shadow, shadow_available, requires, has_web, web, web_label, web_path, port, port_conflict}]}`, plus `port_conflicts` — `{"<port>": [ids]}` for every port claimed by more than one installed hack. Built-in Shadow panels plus every installed hack declaring `web_ui` or `shadow_ui`, with the user's saved order and switches applied. |
| `POST` | `/api/ui/tabs` | Save that list. Body is the new order: `[{id, shadow, web}]`. Persisted to `<hackdir>/ui_tabs.json` and applied to a running Shadow UI immediately (panels for tabs that survive the change keep their state). Returns the same shape as `GET`. |
| `POST` | `/api/presets/meta` | Set per-preset metadata. Body `{"path":"…","favourite":true,"tags":["warm"]}` — `favourite` and `tags` are independent (omit to leave unchanged). Persisted to `<hackdir>/preset_meta.json`; shared with the on-device Shadow UI Favourites filter. Returns `{ok, favourite, tags}`. `GET /api/presets` accepts `filter,q,fav,tag,device,source,rack` query params. |

### Entry JSON shape

```json
{
  "name": "MySong.als",
  "path": "/data/Music/Ableton/MySong.als",
  "is_dir": false,
  "size": 204800,
  "mod_time": "2026-05-20T18:00:00Z",
  "extension": ".als"
}
```

### RootEntry JSON shape

```json
{
  "path": "/run/media/MY-DRIVE-sda1",
  "name": "MY-DRIVE-sda1",
  "exists": true,
  "removable": true
}
```

`removable: true` is set for entries discovered under `/run/media` — these are auto-mounted USB drives. Regular configured roots have `removable` omitted/false.

### Stats JSON shape

```json
{
  "cpu_percent": 12.3,
  "top_procs": [
    { "name": "Ableton Index", "cpu": 26.1 },
    { "name": "Live",          "cpu": 8.5  },
    { "name": "Push3",         "cpu": 7.3  },
    { "name": "push-manager",  "cpu": 0.1  }
  ],
  "memory": { "total": 7964778496, "used": 2147483648, "free": 5817294848 },
  "disk":   { "total": 215485054976, "used": 12884901888, "free": 202600153088 },
  "ip_addresses": ["wlan0: 192.168.1.67"],
  "uptime_seconds": 9240.5,
  "hotspot_password": "mypassword"
}
```

CPU is sampled over a single 250ms window: two `/proc/stat` readings for overall CPU%, plus two `/proc/<pid>/stat` readings per watched process. `top_procs` lists Ableton Index, Live, Push3, and push-manager sorted in that fixed order; a process is omitted if its PID is not found. `hotspot_password` is read from the first non-empty `hotspot_password` key found in any `PushPreferences.json` matching `/data/.config/Ableton/Live */PushPreferences.json`; omitted if not found.

**Battery:** The `battery` field is present in the struct and the UI handles it, but **Push 3 does not expose battery state through any standard Linux interface** — `/sys/class/power_supply/` is empty. Battery is managed by the XMOS firmware via a custom MIDI SysEx protocol, accessible only from Push's internal Python environment. The field will always be omitted in practice.

---

## Security model

- **Path traversal prevention:** every path validated against `allowed_roots` via `filepath.Rel()`. Any path resolving outside the roots is rejected.
- **No auth:** trusted-network model — Push hotspot or local LAN only. Do not expose to internet.
- **Read-only mode:** set `"read_only": true` in `hack.json` to disable all write operations.
- **Root protection:** cannot delete or rename an allowed root directory itself.
- **Atomic uploads:** temp file + `os.Rename()` prevents partial writes.
- **File ownership:** push-manager runs as root. All created/uploaded/copied files and directories are `chown`-ed to match the destination parent directory's owner (`ownerOf()` reads uid/gid via `syscall.Stat_t`). This ensures files copied to `/data/Music/` are owned by `ableton:users` and are visible in Push's Live UI.
- **Unmount scope:** `/api/unmount` only accepts paths directly under `/run/media/` — one level deep, no traversal.

---

## Configuration (`hack.json`)

```json
{
  "id": "push-manager",
  "name": "Push Manager",
  "version": "1.0.0",
  "port": 7701,
  "binary": "push-manager",
  "enabled": true,
  "allowed_roots": ["${USER_DATA}/Music", "/run/media"],
  "settings": {
    "max_upload_size_mb": 512,
    "show_hidden_files": false,
    "read_only": false
  }
}
```

`${USER_DATA}` is resolved at deploy time by `install.sh` to the detected user data directory (typically `/data`).

**`/run/media` is a special root.** Instead of exposing `/run/media` itself as a browsable directory, `handleRoots` enumerates its subdirectories and emits each mounted USB drive as its own root card. A device ID check (`syscall.Stat_t.Dev`) filters out leftover empty directories from previously-unmounted drives — only subdirectories on a different filesystem than `/run/media` (i.e. genuinely mounted drives) are included. Swap partition mounts (names starting with `swap`) are also excluded.

---

## UI Features

### Navigation
- Home screen shows root directory cards
- Breadcrumb trail in sticky header
- Browser back/forward button works (History API `pushState`/`popstate`)
- "‹ Back" button in header
- "SYSTEM" button → stats page

### Menu bar links for other hacks

Any hack installed on the device can put its own link in Push Manager's menu
bar. There is no API to call and no code to add here — the hack declares
`web_ui` next to its `port` in its own `hack.json`:

```jsonc
{
  "id": "automation",
  "port": 7703,
  "web_ui": { "label": "Automation", "path": "/" }
}
```

Push Manager links it as `http://<this-host>:<port><path>`, opening in a new
tab. This is the same field Push Hack Catalog reads for its "Open" links, so
a hack that already works there needs no change. Full field reference:
[catalog/schema.md](../../catalog/schema.md#navigation-hooks-hackjsons-web_ui-and-shadow_ui).

Notes:

- A hack with no `web_ui` (a Remote Script, `push-display`, Push Manager
  itself) gets no link. That is the intended way to opt out.
- Links come from `GET /api/ui/tabs`, polled every 10 seconds, so one appears
  or disappears within 10s of an install or removal — no restart.
- The user can turn any link off and reorder them (see below).
- Push Hack Catalog always has a link. It is a core hack on a fixed port, so
  the built-in **Catalog** entry carries the link itself rather than waiting
  for the installed copy to declare `web_ui` — an older catalog build, or one
  installed under a different id, still gets one. It is an ordinary entry
  otherwise: the user can switch it off or move it.
- **One link per port.** Two entries pointing at the same port would be two
  links to the same server and at most one of them can be right, so the
  earlier one in the user's order wins — a rule they can change by
  reordering. See port conflicts below.
- The link only points at the hack's UI. It does not embed it, and Push
  Manager never checks whether the hack is actually running.

### Tab settings (Display → Tabs)

One ordered list drives both navigations: the Shadow UI's tab strip on
Push's screen, and the web menu bar above. Each entry has two switches —
**Shadow** and **Web** — and a switch is only shown for a navigation the
entry can actually appear in (a built-in panel is Shadow-only; a hack that
declares just `web_ui` is Web-only).

Reordering is a pair of ↑ / ↓ buttons rather than drag-and-drop: this page
gets used on a phone, next to the hardware.

- Order and switches persist to `<hackdir>/ui_tabs.json` and apply to a
  running Shadow UI the moment you hit Save — no restart, and a tab that
  survives the change keeps its state (a browser cursor, a fetched catalog).
- Push 3 has 8 top buttons, so only the first 8 switched-on Shadow tabs are
  drawn. Anything past that is greyed out and labelled, rather than silently
  doing nothing.
- An entry whose dependency is missing (the Browser tab needs
  `browser-bridge`) shows as unavailable and is skipped on the hardware.
- A hack the config has never seen — one just installed — is appended at the
  end with both switches on, so new things show up rather than going missing.
- Turning every Shadow tab off leaves the last frame on screen. The Shadow UI
  is still reachable; give it at least one tab to get a usable screen back.
- A hack **disabled** through Push Hack Catalog (service stopped, its
  boot-autostart link removed, files kept) drops out of the list entirely —
  no dead menu-bar link, no Shadow tab that never answers. Re-enabling it
  brings the entry back on the next poll with its saved order/switches
  untouched; it doesn't reset to "just installed".

#### Port conflicts

Only one process can bind a port, so two installed hacks declaring the same
`port` in their `hack.json` means at least one of them is not running — and
anything pointing at that port (a menu-bar link, a `shadow_ui` tab) reaches
the wrong hack, or nothing.

push-manager does not try to guess which one won. It reports the clash, in
three places:

- A red banner at the top of this page naming the port and every hack
  claiming it, since that is where you can act on it.
- A note on each affected row.
- One line in `push-manager.log` at startup — a hack that silently never
  answers because a sibling took its port is otherwise miserable to
  diagnose.

The scan covers every installed hack, including ones with no navigation
entry of their own. Built-in entries are excluded: they only *point* at a
port, they bind nothing, so the built-in **Catalog** entry sharing 7702 with
the catalog hack is the normal case, not a clash. Nothing is probed over the
network — the `hack.json` files on disk say enough to tell you what to fix,
and a liveness poller would be a running cost for a problem you fix once.

The fix is to change the `port` in one hack's `hack.json`, or remove that
hack. Assigned ports are listed in `CLAUDE.md`.

### Shadow UI tabs from other hacks

A hack can own a Shadow UI tab without any Go code landing in push-manager.
It declares `shadow_ui` in its `hack.json`:

```jsonc
{
  "id": "automation",
  "port": 7703,
  "shadow_ui": { "label": "AUTO", "path": "/api/shadow" }
}
```

push-manager then treats `http://127.0.0.1:<port><path>` as the tab. It is a
dumb terminal: the hack owns every piece of state (cursor, sub-views, what a
button does), push-manager owns the pixels.

**GET** that path, roughly every 300ms while the tab is on screen. Answer
with JSON — every field optional:

```jsonc
{
  "title":   "LFO 1",                    // breadcrumb line
  "status":  "",                         // overrides title when set (errors, progress)
  "rows":    ["rate  1/4", "depth  64"], // the list body
  "cursor":  0,                          // which row is highlighted
  "buttons": ["Start", "Stop"],          // up to 8 under-screen labels, "" = unused
  "hint":    "jog to move"               // right-hand nav hint
}
```

**POST** to the same path on every press, with the raw control:

```json
{"cc": 20, "value": 127}
```

CC numbers are the standard Push 3 map (`docs/push3-button-map.md`, also in
`core/push3`) — under-screen buttons are CC 20–27, the jog wheel is CC 14
with value 127 = clockwise / 1 = counter-clockwise. push-manager re-polls
immediately after a POST, so a press shows its result on the next frame.

Notes:

- **Everything in `rows`, `title`, `hint` and `buttons` must be ASCII.** The
  on-device font has no glyph past it — an em-dash or an accent draws as an
  empty box. See the "Drawing text" rule in `CLAUDE.md`.
- push-manager scrolls the list to keep your `cursor` visible. You do not get
  told the window size; move the cursor and the window follows.
- A soft-button with a non-empty label is lit; an empty one is dark.
- If the hack does not answer, the tab shows "hack not responding" rather
  than disappearing.
- This is deliberately **not** a pixel protocol. A PNG per frame would cost an
  encode plus a decode 30 times a second on a device whose whole point is not
  stealing CPU from Live.
- A `shadow_ui` tab does not touch the display shm, so it is not a
  "display-owning hack" — no push-display dependency. It does need
  push-manager running, since push-manager is what draws it.

### Root cards
- Regular roots use `Sidebar_Folder.png`
- USB drives (removable roots) use `Sidebar_Computer.png`
- USB drive cards show an ⏏ eject button that calls `POST /api/unmount` — card disappears immediately on success
- After ejecting, physically unplug and replug the drive; it auto-mounts and reappears as a new root card

### Files & folders
- **Icons** from Push's own assets (`/api/assets/Browser/`):
  - Generic folders → `Sidebar_Folder.png`
  - Folders with "project" in name → `Sidebar_CurrentProject.png`
  - `.als` files → `Set.png`
  - `.asd` files → `DefaultSet.png`
  - Audio files (`.wav`, `.aif`, `.flac`, etc.) → `Audio.png`
- **Folder zip download** — tap ⤓ to download entire folder as `.zip` (streamed, no temp file)
- **File download** — tap ↓ or the filename
- **Delete** — × button; folders show "delete all contents" warning, uses `?recursive=true`
- **Rename** — ✎ button; bottom sheet with text input, Enter confirms, Esc cancels
- **Copy** — ⧉ button on folders and files; clipboard-style UX:
  1. Tap ⧉ → item copied to in-memory clipboard, sticky paste bar appears at bottom
  2. Navigate to destination directory
  3. Tap "Paste here" in paste bar → `POST /api/copy` with `{src, dst}`
  4. Tap ✕ in paste bar to cancel
  - Destination path = current directory + source item name; fails if destination already exists

### Upload
- Single "Upload ↑" button → dropdown: "Files" or "Folder"
- **Files:** standard multi-file picker
- **Folder:** `webkitdirectory` input; preserves full directory structure via `webkitRelativePath` → `UploadWithRelPath()` server-side creates intermediate directories
- FileList captured as `Array.from()` before input is cleared (prevents live-collection invalidation bug)

### Sort
Sort bar above file list: **Name** / **Date** / **Size** / **Type**

| Sort | Direction | Behaviour |
|------|-----------|-----------|
| Name | A→Z / Z→A | Dirs first, then files, alphabetical |
| Date | Newest / Oldest | Flat list (dirs + files mixed), by `mod_time` |
| Size | Small / Large | Flat list, by byte count |
| Type | A→Z / Z→A | Dirs first, then files grouped by extension |

Click active button to toggle direction. "Date" defaults to newest-first.

> **Note:** Only modification time (`mod_time`) is available on Linux via Go stdlib. For audio files recorded on Push this equals the recording timestamp; for project files it equals last-save time.

### Browser tab
Preset browser backed by the filesystem index (`presets.go`), no Live access for browsing.
- **Search** — debounced text box; matches preset name OR any tag.
- **Filters** — category, device, source (Core Library / pack / User Library), and a ★ Favourites toggle. Facet values come from `GET /api/presets/facets`. The category dropdown also offers each category's racks as a dedicated pick (**Instrument Racks**, **Audio Effect Racks**, **MIDI Effect Racks** — value `<cat>|rack`, sent as `filter=<cat>&rack=rack`); Drum Racks live under **Drums**.
- **Tags** — free-form; click `+ tag` on a row to add, `✕` on a chip to remove (`POST /api/presets/meta`). Click a tag chip in the filter row to filter by it.
- **Favourites** — ★ toggles per preset; persisted to `<hackdir>/preset_meta.json` and shared with the on-device Shadow UI Favourites filter.
- **Load** — `POST /api/live/load {name,category}` → PushHackBrowser instantiates it onto Live's selected track (fire-and-forget; toast confirms send).
- Rows capped at 300 (`BR_CAP`) with a "showing N of M" count.

### Stats page (System)
Auto-refreshes every 3 seconds. Shows:
- CPU % (250ms sample from `/proc/stat`)
- Memory used/total (from `/proc/meminfo`)
- Storage used/total (from `syscall.Statfs` on first allowed root)
- Network IPs (non-loopback IPv4 from `net.Interfaces()`)
- Hotspot password (from `PushPreferences.json`) — shown inline next to Network row in monospace
- Uptime (from `/proc/uptime`)

> **Battery not shown:** Push 3's battery state is only accessible via the XMOS firmware's custom MIDI SysEx protocol — `/sys/class/power_supply/` is empty. The UI handles the `battery` field gracefully (hidden when absent).

### Display page
Accessed via "Display" button in header. Tab-based layout:

**Control tab:**
- Status indicator (dot): green = hook connected, red = disconnected
- Mode pills: **Off** (passthrough), **Debug** (orange bar overlay), **Image** (static upload), **Video** (stream video from browser in real time)
- Live preview canvas (960×160, CSS aspect-ratio 6:1)
- Image upload section (drag-drop or tap; only visible in Image mode): upload any PNG/JPEG, scaled to 960×160. Caller must set mode=2 explicitly before uploading — the endpoint no longer auto-sets mode.
- When entering Image, Video, or Draw mode (mode=2): server always clears framebuf to black first — no stale pixels from a previous session.
- Display mode is reset to 0 (passthrough) by push-manager on every shm connect/reconnect — prevents stale mode=2 surviving push-manager restarts.
- **📷 Screenshot** button (in the Preview label row): `GET /api/display/screenshot` → inline PNG preview + **↓ Download PNG** button. Captures whatever push-manager currently owns on the display (Shadow UI, OSD, uploaded image). If the display isn't in takeover (`X-Display-Mode` ≠ 2), a toast warns the frame may be stale — the native Ableton UI can't be captured this way.

**Video mode:**
- UI-only mode: sets shm mode=2 (same as Image), but shows video controls instead of an upload drop zone
- Browser reads a local video file (MP4, WebM, MOV); file never touches Push
- Each video frame is drawn to an off-DOM canvas via `createImageBitmap()` (forces GPU texture flush), then POSTed as JPEG to `/api/display/image` at ~15fps
- `requestVideoFrameCallback` used when available (Chrome 83+), falls back to `requestAnimationFrame`
- GIF animation is not supported — browser canvas `drawImage` does not reliably capture animated GIF frames across browsers
- `visibilitychange` and `pagehide` events stop streaming when the tab is hidden or closed

**Draw tab:**
- Draw directly in the browser; strokes stream to Push display in real-time (~10fps)
- Tools: 6 color swatches (orange, white, green, blue, yellow, pink), 3 brush sizes, eraser, clear
- Streaming: dirty canvas → JPEG @ 0.7 quality → `POST /api/display/image` every 100ms
- `● LIVE` status indicator turns orange while streaming
- Canvas is 960×160 internally (CSS-scaled to fill width); touch events supported for mobile/tablet drawing
- Auto-activates takeover mode (mode=2) on first stroke; caller sets mode explicitly before streaming

### Theme
Dark theme matching Push's own UI aesthetic. White PNG icons from Push's assets work natively. `Tips-Intro.png` used as decorative hero background on home screen. `logo_high_resolution.png` in sticky header (falls back to text if Push not connected).

### MIDI Monitor tab
Accessed via "MIDI" button in header. Subscribes to the ALSA sequencer as a second sink alongside the Push3 app — all MIDI events are delivered to both subscribers simultaneously (non-exclusive).

- **Live stream** via SSE (`/api/midi/stream`); EventSource opened on tab enter, closed on tab leave
- **Event log**: timestamp (ms) · direction badge (IN=green) · hex bytes · decoded string
- **Counters**: IN / OUT total
- **Filters** (checked = hidden): Active Sensing (default hidden), SysEx (default hidden), CC (default shown), Note (default shown), Chan Pressure (default hidden) — client-side display filters, no API call
- **Input port dropdown** (`GET /api/midi/ports` → `POST /api/midi/subscribe`): selects the ALSA source (web UI only — not selectable on-device, see below).
- **Clear** button — client-side only, does not reset ring buffer
- **Ring buffer**: 256-slot Go slice, ~7KB — last 256 events always available at `/api/midi/events`
- **Decodes**: Note On/Off, CC, Poly Pressure, Program Change, Channel Pressure, Pitch Bend, SysEx, Active Sensing

**Intercept toggle** (orange tinted, in MIDI tab header area): calls `POST /api/midi/filter`. Writes to the `midiflt` shm file. When enabled, the `snd_seq_event_input` hook inside Push3's process neutralizes incoming MIDI events before RtMidi processes them — Live sees no pad/button/encoder input. Events still appear in the MIDI monitor because our ALSA seq subscription is independent. Event neutralization: the `type` byte is overwritten with `0` (`SND_SEQ_EVENT_NONE`) rather than dropped — this avoids blocking or timing disruption to Push3's MIDI loop. SysEx (`0x82`) and Active Sensing (`0x28`) always pass through unchanged.

`midiflt` shm file: `/data/push-hack/hacks/push-display/midiflt`, 16 bytes:
```
offset 0: uint32 magic  (0x4D464C54 "MFLT")
offset 4: uint8  enabled (0=passthrough, 1=intercept active)
offset 5: uint8[11] reserved
```

**On-device MIDI monitor (Shadow UI).** The Shadow UI MIDI panel (shown when intercept is ON) has two views. The main view has the Intercept/Forward toggles plus a **MONITOR** soft-button (Bot3). MONITOR opens a full-screen live event log read from the same 256-slot ring buffer. In the sub-view the under-screen soft-buttons toggle the same filter categories as the web UI (SENS / SYSEX / CC / NOTE on buttons 1-4, CHPRES on button 5 — LED green = shown, red = hidden). Re-press the MIDI tab to return to the main view. The input port is **not** selectable on-device: subscribing away from the Push port would sever the Shadow UI's own MIDI feed, so port selection is web-UI-only.

**Port selector** (dropdown in MIDI tab status bar): populated by `GET /api/midi/ports`. Selecting a port calls `POST /api/midi/subscribe` which closes the current seq fd and triggers a reconnect to the chosen port. Useful when external MIDI devices are connected to the USB-A port at boot — ALSA client numbering shifts and the Push built-in port may be at a different client number. Use the selector to re-target it without restarting.

Known ALSA client numbers (may shift with connected devices at boot):
- `16:0` — Push 3 built-in grid/pads/buttons/encoders (default)
- `20:0` — Push 3 User Port
- `128:0` — USB-A MIDI keyboard / device (if connected)
- `129:0` / `130:0` — MIDI I/O ports on the back panel

**Implementation** (`src/midi.go`): subscribes to ALSA sequencer directly via raw ioctls (no cgo, pure Go):
1. `open("/dev/snd/seq")`
2. `CLIENT_ID` ioctl → own client ID
3. `CREATE_PORT` ioctl — must set `portInfo[addr.client] = ownClientID` or kernel returns `EPERM`
4. `SUBSCRIBE_PORT` ioctl — sender = target `client:port`, dest = own port
5. Blocking `read()` loop decodes 28-byte `snd_seq_event` structs; SysEx events are variable-length (flag bit `0x04`)

The goroutine blocks on kernel `read()` — zero CPU between events. Subscription target is mutable: `handleMidiSubscribe` updates `midiTargetClient`/`midiTargetPort`, sets `midiSeqFd = -1`, then closes the fd — interrupted read causes goroutine to return and restart with new target.

**Boot-settle defer (USB-A safety).** `initMidiOut()` and `startMidiReader()` — the only `/dev/snd` access in push-manager — are launched from a goroutine that first calls `waitForBootSettle()`, blocking until system uptime ≥ `bootSettleSecs` (30s). Reason: push-manager starts early at boot (init.d S20); opening `/dev/snd` during the ~3–15s window when `snd-usb-audio` probes a USB-Audio device plugged into the USB-A port (e.g. a MIDI keyboard) aborts that device's enumeration and wedges the USB-A hub port (`1-1.2`) until a power cycle. This reproduces only on a **cold power-on** (a warm reboot keeps the hub powered). Deferring `/dev/snd` past the enumeration window fixes it. The HTTP server starts immediately; MIDI/LED/Shadow-UI features come online ~30s after a cold boot. `waitForBootSettle()` is a no-op when push-manager is restarted on an already-running system (uptime already past the threshold). See `CLAUDE.md` § "USB-A port wedge".

### MIDI Mapping (remap)

Re-maps any Push control (button, pad, or knob) to a user-chosen MIDI CC/Note and
sends the transformed value to a **user-selected writable output port** (dropdown
populated by `/api/midi/ports?writable=1`). Implemented in `src/remap.go`;
`applyRemap()` is called from `processFixedEvent` for every incoming CC/Note event.
Sending reuses the existing output fd (`sendSeqCCTo`/`sendSeqNoteTo`) — **no new
ALSA port is created**.

- **Source** defined by **Learn** (frontend captures the next `dir:'IN'` event off
  the SSE stream and fills the fields) or **manual** entry (type + channel + number).
- **Output** = type (CC/Note), channel, number, and a value range `[min,max]`.
  Absolute sources (buttons/pads) **scale** the incoming velocity into `[min,max]`
  (fixed-127 buttons → `max`; release → `min` / NoteOff). **Relative** encoders
  (CC 71–79, 14 — auto-detected during Learn) accumulate signed deltas
  (`decodeRel`) into an absolute value clamped to the range.
- **Two toggles:** `enabled` (master), and `require_intercept` (when set, remap
  fires only while MIDI intercept is ON — otherwise it fires regardless of intercept).
- The original Push→Live message is **not** suppressed by remap; toggle intercept
  yourself if you need the original blocked. If the chosen output port is one Live
  reads from and intercept is OFF, Live receives both original + remapped.
- Persisted in `midi.json` (`midiPersistData.Mappings` + remap config fields).

> **Encoder direction:** relative decode assumes two's-complement (`v<64` → `+v`,
> else `v-128`). Verify rotation direction on hardware; flip the sign in
> `decodeRel` if inverted.

---

## OSD (On-Screen Display)

Push Manager includes an OSD subsystem that briefly displays text on the Push 3 screen in response to hardware chord actions, then restores the previous display mode.

### How it works

1. `startOSD()` launches a background goroutine (`osdWorker`) on startup.
2. Any code sends an `OSDRequest{Text, Duration}` to the buffered channel `osdCh`.
3. `osdWorker` saves the current display mode, renders the text frame, writes it to `framebuf`, and sets mode=2 (takeover).
4. After `Duration`, the goroutine restores the saved mode.
5. A new request before the timer fires cancels the pending restore — only the latest timer triggers the restore. The saved mode is the one that was active before the _first_ OSD in a burst.

### Text rendering

Uses `golang.org/x/image/font/basicfont.Face7x13` (7×13 px, pure Go, no CGo). Each glyph is rendered at 1× then scaled 2× (nearest-neighbour pixel doubling) for a readable 14×26px effective size on Push's 3.5" display. Text is white on black, horizontally and vertically centered on the 960×160 canvas.

### Current chord triggers

| Chord | OSD text |
|-------|----------|
| CC 49 + CC 30 (held simultaneously) | `MIDI Intercept ON` / `MIDI Intercept OFF` |

### Adding OSD to new chord actions

```go
select {
case osdCh <- OSDRequest{Text: "My Action Fired", Duration: 2 * time.Second}:
default:
    log.Printf("osd: channel full, dropping")
}
```

**Graceful fallback:** if `framebuf` shm is not connected (push-display not deployed), `shmWritePixels` returns an error — the OSD is silently skipped and the display is unaffected. No crash risk.

---

## LED control

Set any Push 3 button or pad LED colour from Go or the web UI.

### Write path

Only **ALSA sequencer → port 16:0** reaches Push hardware. Confirmed dead ends:
- `midiC0D0` rawmidi write → silently dropped (Push3 app holds subdevice 0 exclusively; writes to subdevice 1 don't reach XMOS)
- `amidi -p hw:0,0,1` → silently dropped for the same reason

The correct path is the same as `RtMidi Output Client` (client 130): open `/dev/snd/seq`, create a port with `CAP_READ|CAP_SUBS_READ`, write `snd_seq_event` with `queue=QUEUE_DIRECT(253)` and `dest={client=16, port=0}`. No explicit subscription needed for direct-addressed events.

### Implementation (`midi.go`)

`initMidiOut()` opens a **separate** persistent `core/alsaseq.Client` (not shared with the reader, which gets closed on subscription changes), stored in `midiOut`. The output port is named **Push Manager**; the receive port is named **Push Manager In**.

```go
// Light up first top-left button (CC 102) with palette index 127 (red)
sendSeqCC(0, 102, 127)

// Turn it off
sendSeqCC(0, 102, 0)

// Light pad 36 (bottom-left) white
sendSeqNote(0, 36, 127)
```

### Colour palette

Push uses a 128-entry indexed colour palette, not direct RGB. **Both pad Notes (velocity) and CC buttons (value) use the same palette table — MIDI value = direct palette index.** Full verified table in [`../../docs/push3-led-colors.md`](../../docs/push3-led-colors.md).

Key values:

| Index | Hex | Name |
|-------|-----|------|
| 0 | `#000000` | off |
| 1 | `#FF4032` | red |
| 7 | `#FADC3B` | yellow |
| 11 | `#34C216` | green |
| 16 | `#31ADFF` | sky |
| 17 | `#3663FC` | blue |
| 23 | `#972BFF` | violet |
| 26 | `#FF2BD4` | pink |
| 120 | `#FFFFFF` | white (pure) |
| 122 | `#CCCCCC` | white_btn (Ableton standard) |
| 127 | `#FF0000` | pure red |

Retrieve the live palette from hardware at any time:
```
GET /api/midi/palette   → [{index, r, g, b, w, hex}, …]  (128 entries, ~1s)
```

The **MIDI tab → LED MODE CONFIG → palette ▾** button opens an inline 128-swatch picker (14 columns, matches Live's color picker column count). Hover shows index + hex. Click sets the Color input.

### API

```
POST /api/midi/led
{"type": "cc",   "channel": 0, "cc": 102,  "value":    127}
{"type": "note", "channel": 0, "note": 36, "velocity": 127}
```

`channel` is 0-indexed (0 = MIDI channel 1). All integers 0–127. Stateless — does not update the toggle state map.

### Button LED toggle state machine

When **MIDI intercept is ON**, every CC button press automatically toggles its LED:
- First press → LED on (CC value 127 sent to hardware)
- Second press → LED off (CC value 0 sent)
- State tracked in `ledToggleState map[uint8]uint8` (CC → last sent value)

When **MIDI intercept is OFF**, this logic is suppressed — Push firmware manages its own LED state and overriding it would cause flicker or conflicts.

Excluded from toggle logic: encoder rotation CCs (70–79) and tempo encoder (14) — these send value=127 for rotation, not button press, and have no LEDs.

Each toggle emits an `LED` event to the MIDI ring buffer (visible in `/api/midi/stream` SSE with `"dir":"LED"`).

```
GET    /api/midi/led/states   → {"states": {"102": 127, "20": 0, ...}}
DELETE /api/midi/led/states   → {"cleared": N}   (turns off all active LEDs, clears map)
```

**On intercept ON:** the chord action calls `clearAllLEDs(CCSettings)` as a goroutine. Sends CC value=0 to every known button CC (~40 buttons) except Settings (CC 30). Also sends Note velocity=0 to all 64 pad notes (36–99), clearing the pad grid LEDs. Finally explicitly sends CC value=127 to Settings — lighting it as a persistent visual anchor indicating intercept is active. `ledToggleState` reset to `{CCSettings: 127}`. Push firmware does not restore LEDs while intercept is active — our code owns them until intercept is turned OFF.

---

## Startup splash (Approach C)

On every Push 3 boot, "Push Hack loaded..." appears on the display via a two-stage handoff:

### Stage A — C hook (instant, ~0ms after push3 starts)

`push_hook.c` constructor writes pre-rendered BGR565 pixels (`splash_pixels` from `src/splash_data.h`) to `framebuf`, sets `mode=2` (takeover), and resets `frame_seq=0`. A background thread (`splash_fallback_thread`) sleeps 10 seconds and resets to `mode=0` if push-manager never connects.

The splash frame is generated by `gen_splash/main.go` using the same rendering pipeline as `renderOSDFrame` (basicfont 7×13 @ 2×, white on black, centered 960×160). Regenerate with `make splash` in `hacks/push-display/`.

### Stage B — push-manager (takes over ~1–2s after push3 starts)

On the first successful shm connection, `scheduleStartupSplash()` (guarded by `sync.Once`) writes a fresh "Push Hack loaded..." frame, keeps `mode=2` for 3 seconds, then restores `mode=0`.

On connection, if `frame_seq==0` (fresh hook): startup splash fires and overwrites the hook's static frame.  
If `frame_seq>0` (push-manager reconnecting after restart): startup splash still fires once, then passthrough.

### Fallback

If push-manager never starts (push-display deployed but push-manager not running), the C fallback timer resets `mode=0` after 10 seconds, so Push's own UI always returns.

### Regenerating the splash text

```bash
cd hacks/push-display
# Edit gen_splash/main.go — change the `text` constant
make splash   # rebuilds gen_splash tool + regenerates src/splash_data.h
make          # rebuild push_hook.so with new splash
hacks/push-display/deploy.sh  # deploy + restart Push3
```

---

## Ableton OS updates

**Uninstall the hack before running an Ableton OS update** (`./scripts/uninstall.sh`), then reinstall
after. The push-display `LD_PRELOAD` hook interposes the same USB/libusb path Push3 uses to flash
co-processor firmware during an update, and the collision freezes the device mid-update. An
LD_PRELOAD interposition cannot be removed from a running process, so there is no in-process
workaround — the hack must not be active when the update runs. See the README for the full rationale.

## Live log marker (support detection)

`startLiveLogMarker()` (launched from `main()`, in `src/live_log.go`) lets Ableton support confirm
push-hack is installed by reading nothing but Live's standard `Log.txt`. It polls `/proc` for the
Live process (`findWatchedPIDs`); when a **new** Live instance appears it waits an 8s grace — Live
truncates `Log.txt` on launch, so appending too early loses the line — then appends one native-format
entry to the newest `/data/.config/Ableton/Live */Log.txt`:

```
2026-06-30T14:51:02.000000: info: push-hack loaded: automation v0.1.0, push-display v0.1.0, push-manager v0.1.0
```

The hack list + versions come from scanning `/data/push-hack/hacks/*/hack.json`. The marker re-writes
on every Live restart (keyed off the Live PID) and is **independent of push-display** — it works even
when only push-manager is installed. Support can grep `Log.txt` for `push-hack loaded`.

## Known limitations

- Empty directories are not created during folder upload (browser `webkitdirectory` only sends files)
- `webkitdirectory` not supported on older iOS (< 15.0)
- Directory download zip is streamed with no `Content-Length` — browser can't show download progress
- Folder delete is irreversible (no trash); confirmation sheet required
- Birth time (file creation date) not available via Go stdlib on Linux; `mod_time` used instead
- Display hook requires push-display deployed and Push3 restarted; status dot shows red if hook not connected
- Draw tab streams JPEG (lossy); very fine detail may not reproduce exactly — use Image upload for pixel-precise content
- Display image upload accepts any resolution; server scales to 960×160 nearest-neighbor
- Video streaming (Video mode) requires a browser that supports `requestVideoFrameCallback` (Chrome 83+) or falls back to `requestAnimationFrame`. GIF animation is not supported — browser canvas `drawImage` does not reliably capture animated GIF frames.
- MIDI intercept requires push_hook.so to be loaded (push-display deployed and Push3 restarted). Intercept state persists in the `midiflt` shm file across push-manager restarts.
- **ALSA client numbering shifts at boot** if external MIDI devices (USB-A keyboard, MIDI I/O) are connected before Push3 starts. push-manager auto-detects Push 3 by name ("Ableton Push 3 Live Port") on each connection attempt, so a shifted client number is handled transparently. The port selector dropdown (`GET /api/midi/ports`, `POST /api/midi/subscribe`) can still be used to override manually; once overridden, auto-detect is disabled for that session.
