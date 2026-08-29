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
