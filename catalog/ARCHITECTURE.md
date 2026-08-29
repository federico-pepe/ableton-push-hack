# Store architecture

## The model: an index, not a host

Push Hack Catalog does **not** host hacks. It is a **catalog** (`catalog/catalog.json`)
that points at hacks living in **their authors' own repositories**. Each repo
publishes its own GitHub Releases and keeps a `release.json` at its root
pointing at the current one. Same shape as a Homebrew tap or a Go module: the
store aggregates pointers and always resolves the latest release live; owners
keep their code, their releases, and full control over both.

```
┌─ owner A repo ──────────┐        ┌─ owner B repo ──────────┐
│ hack source              │        │ hack source              │
│ release.json (main)      │        │ release.json (main)      │
│   → {version, download_url}       │   → {version, download_url}
│ Release: my-hack.tar.gz  │        │ Release: bar.tar.gz      │
└──────────────────┬──────┘        └─────────────────┬────────┘
                    │                                 │
                    ▼                                 ▼
          ┌─ store repo: catalog/catalog.json ────────────────┐
          │  entry(foo) → owner A github_repo                    │
          │  entry(bar) → owner B github_repo                    │
          └───────────────────────────────────────────────────┘
                              │  push-catalog daemon (on the Push)
                              ▼
        fetch catalog → pick hack → fetch that repo's release.json
        → download the tarball it points at → extract as a framework hack
```

Removing a bad hack is just deleting its catalog entry — the store owns no
binaries and no releases. Cutting a new version of a hack never touches the
catalog at all: the store always re-fetches `release.json` live, so authors
publish updates entirely within their own repo.

## What the store expects from a hack (the contract)

To be installable, a hack repo must provide exactly three things:

1. **A `release.json` at the repo root**, on `default_branch`, giving the
   current `version` and `download_url` of the release tarball. Kept in sync
   by a release workflow (see [`PUBLISHING.md`](PUBLISHING.md)) — never hand
   edited.
2. **A release tarball** (`<id>.tar.gz`) whose single top-level dir (`<id>/`)
   contains a standard framework `hack.json` plus the `linux/amd64` binary
   (Push 3 is Intel) and any other files the hack needs. The store never
   compiles anything — it only extracts what's published.
3. **A catalog entry** in `catalog/catalog.json` naming the repo
   (`github_repo`, `default_branch`, `asset_name`) — see
   [`schema.md`](schema.md) for the field reference.

## Repo structure

### An owner's hack repo (per author)
```
<owner>/<repo>/
  hack.json           # framework metadata (bundled into the release tarball)
  Makefile             # builds GOOS=linux GOARCH=amd64
  README.md
  src/ …               # source
  release.json          # {version, download_url} — updated by the release workflow
  .github/workflows/release.yml   # tag push -> build, tar, GitHub Release, update release.json
  └─ GitHub Release "v<version>"
       asset: <id>.tar.gz
```
A hack may also live inside a framework fork's `hacks/` dir during
development, but publishing requires its own repo with its own releases — the
store only ever points at a `github_repo`.

### The store repo (this one)
```
catalog/
  catalog.json         # THE catalog — one entry per added hack, nothing else
  schema.md             # entry + release.json field reference
  PUBLISHING.md          # step-by-step: build, release, open the catalog PR
  ARCHITECTURE.md        # this file
```
`catalog.json` is the whole store: it hosts no binaries and no version
numbers — every entry is a live pointer at an owner's `release.json`.

## Install flow (what the on-device daemon does)

1. Fetch `settings.registry` → `catalog.json`.
2. User picks a hack → read its entry (`github_repo`, `default_branch`).
3. Fetch `https://raw.githubusercontent.com/<github_repo>/<default_branch>/release.json`
   → `{version, download_url}`.
4. Download the tarball at `download_url`, extract straight into
   `/data/push-hack/hacks/` (its own `<id>/` top-level dir lands correctly).
5. Read the extracted `hack.json`, register the init.d service, start it.

There is deliberately **no integrity pin** (no sha256, no signing) — the
trust boundary is "this repo is on GitHub, served over HTTPS, and its
catalog entry was reviewed at PR time," the same trust model as installing
any open-source release binary directly. If Push Hack Catalog ever opens to
unreviewed third-party taps, this is the layer to revisit.

## Adding your hack to the store

See [`PUBLISHING.md`](PUBLISHING.md) for the full walkthrough. In short: tag
a release in your own repo (a GitHub Action builds it, tars it, and updates
your `release.json` automatically), then open a PR here adding one entry to
`catalog.json` pointing at your repo. A maintainer reviews and merges — no
sha256, no re-review needed for future versions, since the store always
resolves your latest release live.

**Multiple taps (later):** the store can point at more than one
`catalog.json`, so authors can host their own catalogs instead of PR-ing a
central one — fully decentralized, the endgame of this model.
