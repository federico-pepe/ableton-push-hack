# Catalog format

For the big picture — the store as an index of hacks living in their owners'
repos — see [`ARCHITECTURE.md`](ARCHITECTURE.md). For the step-by-step of
publishing a hack (building, releasing, opening the catalog PR), see
[`PUBLISHING.md`](PUBLISHING.md). This file is the field reference for one
`catalog.json` entry and its repo's `release.json`.

The catalog is one file: `catalog.json`. Installing a hack means the store
reads this, fetches the entry's `release.json` from the owner's repo (for the
current `version` + `download_url`), downloads and extracts that tarball into
`/data/push-hack/hacks/<id>/`, and registers an init.d service using the
`hack.json` the tarball itself contains. Nothing else — the store hosts no
binaries and pins no hashes; it trusts the owner's repo the same way `go get`
or a Homebrew tap does.

## Catalog entry schema (`catalog.json`)

```jsonc
{
  "id": "my-hack",                 // unique, kebab-case, == hack.json id
  "name": "My Hack",
  "description": "One line, shown in the list.",
  "author": "your-name",
  "homepage": "https://github.com/you/my-hack",   // optional
  "github_repo": "you/my-hack",       // owner/repo the store fetches release.json from
  "default_branch": "main",           // branch release.json lives on
  "asset_name": "my-hack.tar.gz",     // filename of the release asset (informational; download_url from release.json is authoritative)
  "requires": ["some-other-hack"]     // optional: other *catalog* hack ids that must be installed first
}
```

No `version`, `url`, or `sha256` lives in the catalog — those are always
fetched fresh from the hack's own `release.json`, so cutting a new release
never requires a catalog PR.

**Don't list `push-manager`, `push-display`, or `push-catalog` in `requires`.**
They're the framework's own base install, not catalog entries — the daemon
can't fetch them (no `github_repo` for them in the catalog) and every hack
already assumes they're present. `requires` is for genuine catalog-to-catalog
dependencies only (e.g. a hack that needs another community hack also
installed). On `install`, the daemon checks each `requires` entry: if it's a
catalog hack and isn't installed yet, it's installed automatically first; if
it names one of the three base hacks or anything else not in the catalog, the
daemon just logs a warning (it has nothing it can fetch) rather than failing
the install.

## Per-hack `release.json` (lives at the repo root, `default_branch`)

```json
{
  "version": "0.1.0",
  "download_url": "https://github.com/you/my-hack/releases/download/v0.1.0/my-hack.tar.gz",
  "released_at": "2026-08-28T13:39:00Z"
}
```

`released_at` is optional (ISO 8601 UTC) — when present, the catalog's web
UI shows it as that hack's "last updated" date. Omit it and the UI just shows
`?`.

The store fetches this via
`https://raw.githubusercontent.com/<github_repo>/<default_branch>/release.json`.
A release workflow (see `PUBLISHING.md`) keeps it in sync with each tag.
`GET /api/catalog` re-fetches every hack's `release.json` live on every
request to source `version`/`released_at` for the listing — catalog entries
never carry those fields themselves.

## The release tarball

The asset at `download_url` is a `.tar.gz` whose single top-level entry is
`<id>/`, extracted directly into `/data/push-hack/hacks/`:

```
my-hack.tar.gz
└── my-hack/
    ├── hack.json      # the standard framework hack.json — id, name, version,
    │                  # binary, enabled, ... same shape the framework's
    │                  # own hacks use. The store does not invent a new format.
    │                  # Don't declare `port`: the store assigns it at install
    │                  # time (see "Ports are assigned by the store" below)
    │                  # and overwrites whatever's there.
    ├── my-hack        # the linux/amd64 binary, executable bit preserved by tar
    └── ...             # any other files the hack needs (remote-script/, etc.)
```

## Non-service hacks (`hack.json`'s `install_path`)

Most hacks are a `linux/amd64` binary run as an init.d service. A hack with
`"binary": ""` has nothing to exec — the store's `install_service` step
becomes a no-op, same as the framework's own `install.sh` convention for
no-binary hacks. Two more optional `hack.json` fields cover the one real
case that doesn't fit the binary+service model at all: an Ableton Live
Remote Script, which must land in Live's own User Library, not
`hacks/<id>/`, and needs no running service since it lives inside Live.

```jsonc
{
  "binary": "",
  "install_path": "/data/Music/Ableton/User Library/Remote Scripts/MyScript",
  "post_install": "Enable MyScript in a free control-surface slot (Input/Output = None) and restart Live."
}
```

- `install_path` — absolute path the store copies the tarball's
  `remote-script/` directory to (convention: that's the only source
  directory it knows to copy). Present only on hacks that need it; absent
  by default.
- `post_install` — a one-time-setup message surfaced to the user (catalog
  daemon's log output, shown in the web/on-device install log) after a
  successful install. The store never drives Live's own UI itself — no
  `post_install` *action*, only a hint pointing at what remains manual.
- `push-catalog remove` reads `install_path` back off the still-installed
  `hack.json` before deleting `hacks/<id>/`, and removes it too — the
  daemon owns the full lifecycle of anything it put on disk.

## Navigation hooks (`hack.json`'s `web_ui` and `shadow_ui`)

A hack can put itself in either of push-manager's two navigations. Both
hooks are one object next to the hack's own `port`, and both are optional:

```jsonc
{
  "web_ui":    { "label": "My Hack", "path": "/" },
  "shadow_ui": { "label": "MINE",    "path": "/api/shadow" }
}
```

| Field | Meaning |
|-------|---------|
| `label` | Link or tab text. Keep it short. A `shadow_ui` label is drawn in a 120px column on Push's screen, and **must be ASCII** — the on-device font has no glyph past ASCII. |
| `path` | Path on the hack's own port. Defaults to `/` if empty. |

The port is not repeated in either object; the hack's own `port` field is
used — the one the store assigns at install time, not one the author
declares (see "Ports are assigned by the store" below).

### `web_ui` — a link in the menu bar

Two readers, one declaration:

- **Push Hack Catalog** puts an "Open" link on the installed hack's card.
  `GET /api/catalog` (given `hacks_dir`, which `cmd_catalog` always passes)
  reads it straight off the *installed* copy's `hack.json` alongside
  `installed_version`, so the card needs no second round-trip.
- **Push Manager** puts a link in its own menu bar, built from
  `GET /api/ui/tabs` and polled every 10s. A hack installed or removed
  through the catalog shows up or drops out within 10s, no restart.

The URL is `http://<device-host>:<port><path>`, opened in a new tab.

### `shadow_ui` — a tab on Push's screen

Push Manager adds a tab to the Shadow UI's top strip and renders it by
polling the hack. The hack serves a small JSON view and receives every
button press back as a `{"cc", "value"}` POST on the same path — it owns
all the state, push-manager just draws. The full request/response contract
is in [push-manager's README](../hacks/push-manager/README.md#shadow-ui-tabs-from-other-hacks).

A hack with a `shadow_ui` tab does **not** need to draw pixels or touch the
display shm, so it is not a "display-owning hack" — no push-display
dependency, no `/api/display/*` calls. It does need push-manager running,
since push-manager is what draws the tab.

### The user's own switches

Both hooks are a *request*, not a guarantee. Push Manager's Display →
Tabs settings page lists every entry with two switches — one per
navigation — and lets the user reorder them; the order and switches
persist in push-manager's `ui_tabs.json`. A hack that has just been
installed starts with both switches on. Push 3 has 8 top buttons, so only
the first 8 switched-on Shadow tabs are drawn.

Omit a hook entirely for a hack that has no such UI (a Remote Script, or
push-display). That is how a hack opts out — it then never appears in that
navigation or in the settings list for it.

### Ports are assigned by the store

Both hooks build their URL from the hack's own `port` — but a hack no
longer declares that port itself. `push-catalog install` writes it into
the installed `hack.json` before the service ever starts, picking the
lowest free integer `>= 7711` (scanning every other installed hack's
`hack.json` for what's already claimed), so two catalog hacks can never
collide. 7701–7710 is reserved for the framework itself (push-manager,
push-catalog, and a couple of fixed internal ports — see `CLAUDE.md`) and
is never handed out. A reinstalled/updated hack keeps the port it already
had rather than getting reshuffled.

Push Manager's own entry has no `web_ui`: you are already looking at it, and
its Catalog entry is built in rather than read from push-catalog's
`hack.json`, so the link survives a catalog installed under a different id.

### A disabled hack drops out entirely

Neither reader probes whether a hack is actually *running* — no liveness
poll. It does check whether a hack is *disabled* (stopped, boot-autostart
removed, files kept — Push Hack Catalog's `POST /api/disable`): a disabled
hack's `web_ui`/`shadow_ui` entry is left out of both navigations
altogether, rather than showing a link or tab that reaches nothing.
Re-enabling it (`POST /api/enable`) brings the entry back on the next 10s
poll, in whatever order/switch state it had before — nothing in
`ui_tabs.json` is touched by disable/enable.
