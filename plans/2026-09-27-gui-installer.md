# GUI installer for non-terminal users

## Context

`./scripts/install.sh` is terminal-only bash: scary ASCII disclaimer requiring
a typed "Yes", an SSH wizard that prints a pubkey and tells the user to paste
it into `http://push.local/ssh` by hand, then `read -r </dev/tty` prompts
through the rest of the flow. Fine for developers, a wall for a musician who
has never opened a terminal. Goal: a double-click app for Windows/Mac/Linux
that does the same job with no command line at all, while `install.sh`/
`uninstall.sh`/`discover.sh` stay exactly as they are for developers, CI, and
scripted use.

Two prior-art data points from research this session:
- `charlesvestal/schwung` + `charlesvestal/schwung-installer` — separate
  repos, installer is Electron.
- `push-tethered-app` (this org's own sibling project, `~/Developer`) already
  ships a working cross-platform GUI on **Wails v3** (`cmd/pushapp-ui`), with
  a CI recipe (`.github/workflows/build.yml`) that already solved the hard
  parts: webkitgtk version pin on Linux, `CGO_ENABLED=0` override needed on
  Windows, AppImage packaging, wails3 CLI caching.

Decision, from discussion with the user: new **separate repo**
(`push-hack-installer`), built with **Wails v3**, not Electron — reuse
push-tethered-app's already-proven Go+webview CI recipe instead of
reinventing it in Electron/Node, and keep this repo's CI light.

## Repo

New repo, e.g. `federico-pepe/push-hack-installer`. Not a folder inside
`ableton-push-hack` — own release cadence, own (heavier) CI matrix, own issue
tracker, so installer-app churn never has to go through this repo's
doc-sync/CHANGELOG discipline, and this repo's CI stays untouched.

No `require`/`replace` module coupling to `core/` (unlike push-manager/
automation/keyboard-visualizer). The installer only needs, at build or
release-fetch time:
- the three core hack binaries: `hacks/push-manager/push-manager`,
  `hacks/push-catalog/push-catalog`, `hacks/push-display/push_hook.so` (all
  already git-tracked pre-built binaries in this repo, per CLAUDE.md).
- `hacks/*/hack.json` for names/descriptions to show in the UI.
- the shell logic in `scripts/lib/common.sh` and `scripts/install.sh` as a
  **behavioral reference** to port to Go, not a runtime dependency.

Two sourcing options for the binaries, pick at implementation time:
1. Vendor a pinned copy at build time (simple, but binaries drift out of sync
   with a push-hack release unless a version bump step is added).
2. Fetch a specific push-hack GitHub Release's assets at install time (always
   current, needs network + a version pin in the installer's own config).
Recommend (1) for v1 — simplest, matches "pre-built binaries committed"
philosophy already used here — revisit (2) if release cadence mismatch
becomes a problem.

## Structure (mirrors push-tethered-app's `cmd/pushapp-ui`)

```
push-hack-installer/
  cmd/installer-ui/          # Wails v3 app: Go backend + webview frontend
    frontend/                # HTML/CSS/JS (or a light framework), matches
                              # push-tethered-app's frontend/ layout
  internal/
    sshinstall/              # ported install.sh logic: keygen, connect,
                              # SCP binaries, generate init.d script, verify
    pushdiscover/             # ported discover.sh: read-only device probe
  vendor-bin/                 # pinned push-hack binaries (see sourcing above)
  .github/workflows/build.yml # copy push-tethered-app's recipe: webkitgtk
                               # pin, Windows CGO_ENABLED override, wails3
                               # CLI cache, AppImage/dmg/exe packaging
```

## Porting the install logic (Go, not bash)

Read `scripts/install.sh` and `scripts/lib/common.sh` end to end before
porting — they encode real hazards that must survive translation:
- sysvinit (not systemd) detection and init.d script generation
- `root@push.local` needed for service install/`.so` copy, `ableton@` for
  the rest — the port must keep this split, not silently escalate everywhere
- the SSH pubkey paste step is fundamentally a manual step (Push's `/ssh`
  page requires a human to click "add") — the GUI can only improve it by
  auto-opening the system browser to `http://push.local/ssh` and polling for
  a successful SSH connection instead of a blocking terminal "press Enter"
- `discover.sh`'s mDNS fallback (`push.local` vs `--host <ip>`) — the GUI
  needs an equivalent "can't find Push, enter IP manually" fallback field

Use `golang.org/x/crypto/ssh` for the Go side instead of shelling out to
`ssh`/`scp` — keeps the installer self-contained on Windows, where `ssh.exe`
availability can't be assumed the way it can on Mac/Linux.

## UI flow

1. Welcome screen — replace the ASCII disclaimer with a proper dialog:
   what this installs, safety notes from CLAUDE.md's hard rules (never
   touches `/boot`, `/opt`), explicit consent button.
2. Connect — hostname field defaulting to `push.local`, "Can't connect?"
   reveals a manual IP field (mirrors `discover.sh --host`). Live connection
   status, not a blocking prompt.
3. SSH key setup — generate keypair if missing, show fingerprint, one button
   that opens `http://push.local/ssh` in the system browser, then the app
   polls in the background until the key is accepted (no "press Enter").
4. Hack selection — checkboxes for the three core hacks, descriptions pulled
   from each `hack.json`, sane defaults pre-checked.
5. Progress — per-hack install steps streamed to a log view (stop/restart
   service, copy binary, verify status), same steps `install.sh` performs.
6. Done screen — link to push-manager's web UI (`http://push.local:7701`),
   an uninstall button that runs the equivalent of `uninstall.sh`.

## Docs

- New repo gets its own README (install/build instructions, screenshot of
  the flow).
- This repo's README quick-start section gets a short pointer: "non-terminal
  users: download the installer app from `push-hack-installer`" above the
  existing bash instructions, which stay as the developer/CI path.
- No CLAUDE.md doc-sync burden here since the installer is a separate repo
  — its own CLAUDE.md (if any) governs itself.

## Verification

- Build `cmd/installer-ui` locally on Mac (available dev machine) first;
  confirm SSH keygen/connect/install/uninstall flow works end-to-end against
  a real Push 3 or `discover.sh`-style dry run.
- Borrow push-tethered-app's CI workflow, adjust for this repo's binaries,
  confirm Linux (webkitgtk deps), Windows (CGO override), Mac builds all
  succeed in GH Actions before first release.
- Manual test matrix: fresh Push (no SSH enabled yet) end to end on at least
  one of the three OSes before calling v1 done; the other two by CI build
  success + smoke test.
