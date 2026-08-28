# Store architecture

## The model: an index, not a host

Push Store does **not** host hacks. It is a **catalog** (`registry/index.json`)
that points at hacks living in **their authors' own repositories**, each binary
published as a release asset and **pinned by sha256**. Same shape as a Homebrew
tap: the store aggregates pointers; owners keep their code and releases.

```
┌─ owner A repo ──────────┐        ┌─ owner B repo ──────────┐
│ hacks/foo/  (source)    │        │ hacks/bar/  (source)    │
│ Release: foo (amd64)    │        │ Release: bar (amd64)    │
│   └─ sha256 ─────┐      │        │   └─ sha256 ────┐       │
└──────────────────┼──────┘        └─────────────────┼───────┘
                   │                                 │
                   ▼                                 ▼
          ┌─ store repo: registry/index.json ─────────────────┐
          │  entry(foo) → owner A release URL + sha256         │
          │  entry(bar) → owner B release URL + sha256         │
          └───────────────────────────────────────────────────┘
                              │  push-store daemon (on the Push)
                              ▼
        fetch index → pick hack → download owner's asset
        → verify sha256 → install as a framework hack
```

Removing a bad hack is just deleting its entry — the store owns no binaries.

## What the store expects from a hack (the contract)

To be installable, a hack must provide exactly three things:

1. **A framework `hack.json`** — the standard Push Hack metadata (`id`, `name`,
   `version`, `binary`, and optionally `port`, `allowed_roots`, `settings`).
   The store embeds this verbatim in the registry entry and writes it to the
   device on install.
2. **A prebuilt `linux/amd64` binary** (Push 3 is Intel), published as a
   downloadable release asset. The store never compiles anything.
3. **A registry entry** in `index.json` that carries the `hack.json` (as `hack`)
   and lists every file the hack needs (`assets`) — the binary (`exec: true`)
   plus any data files — each with a `url` and a **`sha256`**.

That is the entire "output" the store consumes: **`hack.json` + one or more
sha-pinned assets, described by one registry entry.** Field-by-field reference:
[`schema.md`](schema.md).

## Repo structure

### An owner's hack repo (per author)
```
<owner>/<repo>/
  hacks/<id>/
    hack.json          # framework metadata (embedded in the store entry)
    Makefile           # builds GOOS=linux GOARCH=amd64
    README.md
    src/ …             # source
  └─ GitHub Release "<id>-<version>"
       asset: <id>     # the amd64 binary the store downloads
```
A hack may also live inside a framework fork's `hacks/` dir (as in this repo) —
what matters is that a **release asset** exists for the store to point at.

### The store repo (this one)
```
registry/
  index.json           # THE catalog — one entry per added hack, nothing else
  schema.md            # entry field reference + how to publish
  ARCHITECTURE.md      # this file
```
`index.json` is the whole store. It hosts no binaries: every `assets[].url`
points at an owner's release, integrity-pinned by `assets[].sha256`.

## Install flow (what the on-device daemon does)

1. Fetch `settings.registry` → `index.json`.
2. User picks a hack → read its entry.
3. For each asset: download `url`, compute `sha256sum`, **reject on mismatch**.
4. Write `hack.json`, place the binary, register the init.d service, start it.

The `sha256` pin is the trust boundary — it guarantees the byte-for-byte binary
a reviewer approved is exactly what installs, even though it is fetched from a
third-party release. Note this proves **integrity**, not **authenticity**:
whoever can edit `index.json` can change the sha to match a different binary, so
the store is only as trustworthy as the registry itself. Signing the index (so a
compromised host or tap can't forge it) is the next layer, added before opening
the catalog to arbitrary publishers.

## Adding your hack to the store

1. Build for `linux/amd64`; publish the binary as a Release on **your** repo.
2. `sha256sum <binary>`.
3. Open a PR adding one entry to `index.json` ([`schema.md`](schema.md)),
   pointing at your release `url` + that `sha256`.
4. A maintainer reviews and merges. It is now browsable and installable from
   every Push running the store.

**Updating:** cut a new release, then PR the new `url` + `sha256` and a bumped
`version`. **Multiple taps (later):** the store can point at more than one
`index.json`, so authors can host their own catalogs instead of PR-ing a central
one — fully decentralized, the endgame of this model.
