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
    │                  # port, binary, enabled, ... same shape the framework's
    │                  # own hacks use. The store does not invent a new format.
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

## Web UI navigation (`hack.json`'s `web_ui`)

A hack with its own web UI can declare it so Push Manager's header can link
to it, instead of the user having to know the port and type the URL by hand:

```jsonc
{
  "port": 7703,
  "web_ui": { "label": "My Hack", "path": "/" }
}
```

Push Manager scans installed hacks' `hack.json` for this field and renders
a header nav entry per hit, linking to `http://<device-host>:<port><path>`
in a new tab. Omit `web_ui` entirely for a hack with no UI of its own (e.g.
a Remote Script, or push-display).
