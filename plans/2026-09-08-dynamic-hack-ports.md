# Dynamic port assignment for catalog-installed hacks

## Context

Every hack used to declare a static `port` in its `hack.json`. Two catalog
hacks could accidentally pick the same number, so push-manager grew a
`portConflicts()` safeguard (`hacks/push-manager/src/ui_tabs.go`) that
detected clashes and showed a banner/log warning rather than silently
breaking links. This change removes the class of bug instead of just
detecting it: push-catalog now assigns each hack's port itself at install
time, deterministically avoiding whatever is already claimed, so hack
authors no longer coordinate port numbers by hand.

Decisions:
- **Always auto-assign**, ignoring whatever port (if any) a hack's own
  `hack.json` declares — the `port` field is removed from the schema hack
  authors ship. push-catalog is the sole writer of the installed copy's
  `port`.
- **Removed** `portConflicts()`/the banner entirely — dynamic assignment
  makes the clash class of bug structurally impossible for
  catalog-installed hacks, so the safeguard was dead weight.
- **7701–7710 is reserved for the framework as a block**, not just the two
  ports in use. Rationale: Browser Bridge's Remote Script listens on a
  hardcoded `127.0.0.1:7704` (`hacks/push-manager/src/live_bridge.go:20`)
  that never goes through `hack.json`/`install_service` (`binary: ""`, no
  service) — so it cannot show up in `allocate_port`'s used-port scan the
  way a normal hack's port would. Carving out one magic number (7704) in
  the allocator would work but is fragile and silent — the next
  framework-internal fixed port added later could just as easily collide.
  Reserving the whole 7701–7710 block instead means the allocator never
  needs to know about Browser Bridge or any future fixed-port special case:
  it simply never hands out anything below 7711.
  - 7701 = push-manager, 7702 = push-catalog, 7704 = Browser Bridge's fixed
    socket. 7703/7705–7710 are unused headroom, held for future
    framework-level fixed ports.
- **Catalog hacks are assigned dynamically starting at 7711** by
  push-catalog, to every hack that declares `web_ui` or `shadow_ui` (not
  merely "has a `binary`" — found live: `push-audio-loopback` has a binary
  but runs no HTTP server at all, just loads a kernel module, and declares
  neither hook. An earlier version of this gate assigned it a port anyway;
  harmless in practice since nothing reads it without a nav hook, but
  wrong, and fixed before merge).

## How ports reach a running hack

A hack binary starts as `$BIN -config $CFG` where `$CFG` is the *installed*
`hack.json` (`hacks/push-catalog/push-catalog.sh`'s `install_service`); it
reads its port out of that file at startup (`core/hackcfg/config.go`).
push-manager's nav/tabs and push-catalog's own `/api/catalog` "Open" link
both re-read the same installed `hack.json` live, every poll — nothing
caches a port elsewhere. So whichever port ends up written into the
installed `hacks/<id>/hack.json` before the service first starts is
authoritative everywhere, with zero changes needed to any hack binary, to
push-manager's nav code, or to push-catalog's catalog listing. That makes
`install_one` the single insertion point.

## Changes made

### 1. `hacks/push-catalog/push-catalog.sh` — allocate + inject the port

This is the **canonical** script (`hacks/push-catalog/src/push-catalog.sh`
is a generated copy — `make embed`/`make build` regenerates it from the
root file; never edit the `src/` copy directly).

In `install_one()`:
- Before the tarball overwrites anything, capture the hack's *previous*
  port if this is a re-install/update (`dir/hack.json` already exists).
- After extraction and the `hack.json` sanity check, and before the
  `chown -R "$owner" "$dir"` step (so the rewritten file gets the same
  ownership fix-up as everything else tar just wrote as root), a
  `needs_port` gate checks `binary` is non-empty **and** `web_ui` or
  `shadow_ui` is present; only then does a new `allocate_port` function
  rewrite the extracted `hack.json`'s `port` field to its result via
  `as_root`. A hack with no binary (Remote Script) or a binary but no nav
  hook (e.g. `push-audio-loopback`, which only loads a kernel module and
  runs no HTTP server) is left with no `port` field at all.
- `allocate_port(id, hacks_dir, prev_port)`: a `python3` one-liner (same
  style as `q`/`rq`) globs `hacks_dir/*/hack.json` (excluding `id`'s own
  directory), collects every `port` value found into a used-set, then
  returns `prev_port` if it's set, `>= 7711`, and not in the used-set
  (keeps a reinstalled/updated hack's port stable — no dead bookmarks);
  otherwise returns the smallest free integer `>= 7711`.
- Added self-test coverage (`self_test()`, sections "3d"/"3e") for
  `allocate_port` and the `needs_port` gate, offline, no root needed.

### 2. Menu-bar links / Display → Tabs — no code change, by design

The existing link-building code (`hacks_nav.go`'s `web_ui`/`shadow_ui`
decode, `ui_tabs.go`'s menu/tab assembly) already reads `port` straight off
each installed `hack.json`, fresh, on every 10s poll. Since `install_one`
now writes the real assigned port into that same file before the service
ever starts, a hack's menu link and Shadow UI tab pick up the correct
dynamic port automatically, with zero changes to either file.

### 3. `hacks/push-manager/src/ui_tabs.go` — removed the conflict safeguard

Deleted `portConflicts()`, `warnPortConflicts()`, `annotatePortConflicts()`,
the `PortConflict` field on `uiEntry`, and the `port_conflicts` key from
`uiTabsResponse()`. Removed the call site in `main.go` and the
`TestPortConflicts` test in `ui_tabs_test.go`. Rebuilt and committed the
`push-manager` binary.

### 4. Docs — dropped the manual port registry, describe dynamic assignment

Updated `catalog/schema.md`, `CLAUDE.md`, `README.md`, `MANUAL.md`,
`hacks/push-manager/README.md`, and `docs/architecture.md` to remove the
`port` field from example `hack.json` snippets, drop the "pick a free port
from CLAUDE.md" / port-conflict-banner language, and describe the
7701–7710 reserved block + 7711+ dynamic assignment instead.
`docs/api-reference.md` had no port-related content to update.

Added a `CHANGELOG.md` entry under `## [Unreleased]`.

## Verification

- `bash hacks/push-catalog/push-catalog.sh --self-test` (run from
  `hacks/push-catalog/`, against the canonical script) — passes, including
  the new `allocate_port` assertions.
- `cd hacks/push-manager && PATH=$PATH:/usr/local/go/bin make` — builds
  clean. `go test` cannot run off-device (`coredisplay` is Linux-only, a
  pre-existing limitation unrelated to this change) — verified instead with
  `GOOS=linux GOARCH=amd64 go vet ./...`, which passes.
- On-device (real Push, `push.local`): reinstalled `push-braids` (a
  pre-existing hack with a stale port `7707`, below the new `7711` floor)
  — correctly reassigned to `7711`. Installed `screensaver` fresh —
  correctly assigned the next free port, `7712`. Reinstalled `push-braids`
  again — kept `7711`, confirming update-stability. `push-manager`'s
  `GET /api/ui/tabs` reflected both new ports, carried no `port_conflicts`
  key, and both `http://push.local:7711/` and `:7712/` responded `200`.
</content>
