# browser-bridge decoupling — catalog support for non-service hacks

**Status: implemented.** `push-catalog` gained `install_path`/`post_install`
support (see `catalog/schema.md`'s "Non-service hacks" section), and
`browser-bridge` moved to its own repo,
[federico-pepe/push-hack-browser-bridge](https://github.com/federico-pepe/push-hack-browser-bridge),
installable via the catalog like Automation and Keyboard Visualizer.

## Context

`push-catalog`'s install model (`catalog/ARCHITECTURE.md`) assumes every
hack is a `linux/amd64` binary + `hack.json`, extracted into
`/data/push-hack/hacks/<id>/` and run as an init.d service
(`install_service()` in `push-catalog.sh`). `keyboard-visualizer` and
`automation` both fit that shape and have been split into their own repos,
installable through the catalog.

`browser-bridge` does not fit it:

- It's a **Python MIDI Remote Script** (`PushHackBrowser`), not a compiled
  binary — no `binary` field in the usual sense, nothing to `exec`.
- It installs into **Live's own `User Library/Remote Scripts/` folder**, not
  `/data/push-hack/hacks/<id>/`.
- It needs a **one-time manual activation step** in Live's own Preferences
  (select `PushHackBrowser` in a control-surface slot, Input/Output = None,
  restart Live) — nothing push-catalog does today can drive that, and
  nothing should try to (it's a Live UI action, not a filesystem op).
- There's no service to start/stop/restart — `install_service()`'s whole
  purpose doesn't apply.

So decoupling `browser-bridge` today would mean either forcing it through a
model that doesn't fit, or growing push-catalog's contract first. This plan
is about the second option.

## What push-catalog would need

1. **A way for a tarball to say "I'm not a service."** Today
   `install_service()` treats an empty `hack.json.binary` as "no binary —
   nothing to run" and just stops (see `hacks/push-manager/README.md`'s
   note on no-binary hacks, and `service.initd` templates for udev-rule-only
   hacks in the existing framework). push-catalog's own `install_one()`
   would need the same escape hatch: skip `install_service()` cleanly when
   `binary` is empty, same as `install.sh` already does for the framework's
   own no-binary hacks. This part is probably a small, mostly-mechanical
   change.

2. **A non-default install path.** Schwung's `release.json` already has
   precedent for this — an optional `install_path` field the host copies
   the release payload to, instead of assuming its own modules directory
   (see the `install_path` handling in `schwung`'s own `store_utils.mjs`,
   referenced when this repo's catalog model was designed). push-catalog
   would need something equivalent: an `install_path` (or a fixed,
   documented convention like "Remote Scripts payloads always land in
   `<remote-script/>` inside the tarball, installed into `/data/Music/
   Ableton/User Library/Remote Scripts/<name>/`") so `install_one()` can
   route the extracted files somewhere other than `hacks/<id>/`.

3. **A `post_install` hint, not a `post_install` action.** Also borrowed
   from the Schwung precedent (`release.json`'s optional `post_install`
   string, surfaced to the user rather than executed). push-catalog should
   **not** try to drive Live's Preferences UI — that's real Live automation
   with real risk if it goes wrong mid-set. Instead: extraction succeeds,
   and the daemon surfaces a clear one-time-setup message (in the web UI's
   log pane and/or the on-device `CATALOG` tab) pointing at what the user
   still has to do by hand, mirroring what `hacks/browser-bridge/README.md`
   already documents for the manual-install path today.

4. **`catalog/schema.md` and `ARCHITECTURE.md` updates** once the shape is
   real — right now both documents describe only the binary+service
   contract.

## Resolved decisions

- **`install_path` stays generic.** Any hack can declare it, not scoped to
  a named "Remote Script" kind. Less special-casing, and nothing today
  suggests a second kind won't show up.
- **`push-catalog remove` must clean up whatever it installed**, including
  a non-`hacks/<id>/` `install_path`. Catalog owns the full lifecycle of
  anything it puts on disk — install without a matching remove path is a
  half-managed hack.
- **Core stays minimal and mandatory; everything else is optional via the
  catalog.** Only push-display, push-manager, and push-catalog are "core" —
  installed directly via `install.sh`, never through the catalog.
  `browser-bridge` is **not** core by this definition (Automation and
  Keyboard Visualizer already left the monorepo the same way) — it moves
  out and becomes a catalog-installable optional hack, following this
  plan's `install_path`/no-service/`post_install` work. `install.sh` keeps
  it as a fallback path for anyone bootstrapping without network access to
  GitHub releases, same as the other three.

## New: dynamic navigation for installed hacks with a web UI

Raised while reviewing this plan: today a user who installs Keyboard
Visualizer or Automation via the catalog gets a working service on its own
port, but nothing links to it — they have to know the port and type the
URL by hand. Push Manager's header should surface a menu entry for any
installed hack that has a web UI, pointing at that hack's own URL.

**Where it lives:** in Push Manager, not a separate service. Reasoning:

- Push Manager is the one hack in the mandatory core triad that already
  serves a web UI (port 7701) and already scans `/data/push-hack/hacks/*/
  hack.json` for a different purpose (`live_log.go`'s "list all deployed
  hacks" pass) — the read path for a nav list already exists in spirit.
- A separate "landing page" service would itself need to be either core
  (scope creep on the "3 core hacks" decision just made above) or optional
  (then it's sometimes missing, which defeats the point of a page whose
  whole job is "always show me what's installed").
- Push-catalog already exposes `/api/installed`, so Push Manager doesn't
  even need to re-derive install state — it can hit that endpoint (catalog
  panel already talks to it at `127.0.0.1:7702`, same pattern), or read
  `hack.json` directly like `live_log.go` does, no new coupling either way.

**Proposed shape:**

- Extend `hack.json` with an optional `web_ui` object, e.g.
  `"web_ui": {"label": "Keyboard Visualizer", "path": "/"}` — port is
  already a top-level `hack.json` field, so the link is just
  `http://<device-host>:<port><path>`. Absent `web_ui` = no nav entry
  (covers push-display, which has none).
- Push Manager scans installed `hack.json` files (own hacks dir, same as
  `live_log.go`) at startup and re-scans on an interval or right after a
  catalog install/remove action, and renders the header's nav dynamically
  from whatever it finds — no hardcoded hack list.
- `catalog/schema.md` gains the `web_ui` field once shape is agreed.

**Resolved:**
- Refresh trigger: **polling on an interval**, matching
  `catalog_panel.go`'s existing 10s self-heal poll. Consistent with the
  rest of the catalog integration; not correctness-critical enough to
  justify a push-catalog → push-manager signal path.
- `web_ui` field shape for v1: **label + path only**. Opens as a normal
  link/new tab. No icon, no iframe — other hacks' UIs aren't designed to
  be embedded, and iframing risks CSS/JS collisions for no benefit.

- Nav entries link to `http://push.local:<port>/`, same host, different
  port — confirmed working, cross-port link already tested.

All open questions for this section resolved. Ready to implement.

## Non-goals for this plan

- Not proposing push-catalog gain the ability to click through Live's own
  UI. Manual activation stays manual; the goal is only to get the *files*
  installed via the catalog, with a clear pointer to the manual step that
  remains.
