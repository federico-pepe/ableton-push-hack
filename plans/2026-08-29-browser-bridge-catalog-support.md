# browser-bridge decoupling — catalog support for non-service hacks

**Status: not started. Written down after decoupling `automation` surfaced
the gap; `browser-bridge` deliberately left in the monorepo for now.**

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

## Open questions to resolve before implementing

- Does `install_path`/no-service support stay generic (any hack can declare
  it) or is it scoped narrowly to "Remote Script" as a named, special-cased
  kind? Generic is more flexible; narrow is less surface to get wrong for a
  contract that (as of this writing) has exactly one real use case.
- Should `push-catalog remove` know how to clean up a non-`hacks/<id>/`
  install path too? `browser-bridge`'s own uninstall today is manual
  (`scripts/uninstall.sh` doesn't touch Live's Remote Scripts folder either
  — check current behavior before assuming).
- Whether `browser-bridge` moving out of the monorepo at all is even
  desired: it's one of the "core three or four" framework hacks most users
  get via `install.sh` directly, not really a community/optional hack in
  the spirit of the catalog model. Worth a deliberate decision, not just
  "we did it for automation and keyboard-visualizer so let's do it here
  too."

## Non-goals for this plan

- Not proposing push-catalog gain the ability to click through Live's own
  UI. Manual activation stays manual; the goal is only to get the *files*
  installed via the catalog, with a clear pointer to the manual step that
  remains.
