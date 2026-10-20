# Push Hack self-update

**Status: plan only. No code written. Seven decisions need the owner (section 9). Four device facts are unverified (section 8).**

## 1. Goal

Push Hack tells the user when a newer Push Hack release exists. The user can install it from the Push screen or from the web app. The unit keeps working while it happens.

Requirements, from the owner:

1. Compare the installed version with the newest GitHub release. Include `-alpha` and `-beta`.
2. Check only when the internet works.
3. Do not poll often.
4. Show a yellow bar at the bottom of the shadow UI when an update exists.
5. Show the same notice in the Push Manager web app.
6. Show version and update controls in the web "System" tab.
7. Let Push Hack update itself after the user confirms with a button press. No frozen UI, no frozen unit.

## 2. Is it doable?

Yes. Three facts make it simpler than it looks, and two facts make it harder.

Easier:

- Push Manager is a separate root process from Push3 and Live. Restarting it does not touch audio or Live. The unit does not freeze.
- Linux lets a process rename a new file over its own running binary. The old process keeps its old inode. Only a restart loads the new one.
- Push Catalog already downloads and installs releases from GitHub. The download and `tar` part is a known pattern on this device.

Harder:

- **No version exists on the device today.** The repo version lives only in git tags (latest `v0.1.9-alpha`). `hack.json` says `0.1.0` and is unrelated. No binary knows which release it came from. This must be built first (section 4.1).
- **No release artifact exists.** There is no CI, no GitHub release workflow, and no tarball. `install.sh` pushes files from a Mac over SSH. The device has nothing to download (section 4.2).
- The updater lives inside the thing it updates (section 5).

## 3. Architecture

```
GitHub releases API ──(HTTPS, 24 h, ETag)──> update checker (push-manager, new update.go)
                                                   │ state: /data/push-hack/update/state.json
                       ┌───────────────────────────┼──────────────────────────┐
                 shadow UI bar               web banner + System tab     /api/update/*
                 (confirm = button)          (confirm = dialog)
                                                   │ apply
                                    download -> verify -> stage
                                                   │
                              detached helper (setsid): swap, restart, health check, roll back
```

All new code goes in `hacks/push-manager/src/`. Nothing goes in `core/`, so no `core/vX.Y.Z` tag and no consumer bump.

## 4. Prerequisites

### 4.1 One version, stamped into the binary

- Add a `VERSION` file at the repo root (`0.1.10-alpha`). It is the single source of truth. The git tag is `v` + that string.
- `hacks/push-manager/Makefile` and `hacks/push-catalog/Makefile` pass `-ldflags "-X main.buildVersion=$(cat ../../VERSION)"`.
- Add a `-version` flag to both binaries. The updater uses it to test a staged binary.
- Committed binaries must be rebuilt at release time, after `VERSION` changes (existing rule in CLAUDE.md). A binary built between releases reports the last release. That is acceptable. Dev builds append `-dev`.
- `/api/status` (main.go:427) adds `"hack_version"`. Keep `config.Version` for the hack's own `hack.json` field.

### 4.2 A release artifact (built: `.github/workflows/release.yml`, `scripts/package-release.sh`, `scripts/releasetool/`)

New `scripts/release.sh <version>`:

1. Check the tree is clean and `VERSION` matches.
2. Build push-manager and push-catalog. Build push-display if its source changed.
3. Make `push-hack-<version>.tar.gz` with the binaries, `push_hook.so`, and a `manifest.json`.
4. Write the loose `manifest.json` with the tarball's SHA-256. No signature (D2).
5. Print the `gh release create` command. The owner runs it. The script does not publish by itself.

`manifest.json` fields: `version`, `needs_reinstall` (bool, true when a release changes init.d scripts, udev rules, or the `/etc/init.d/push3` patch), `hook_changed` (bool), `shm_protocol` (int), `notes_url`.

Releases must be marked prerelease for `-alpha` / `-beta` tags.

## 5. The self-update problem

The updater runs inside push-manager. Push-manager must replace itself and restart. Order matters.

**Apply sequence**

1. User confirms. Push-manager checks: a newer release is known, disk has room, no update is running.
2. Download the tarball to `/data/push-hack/update/staging/`. Verify SHA-256 (and signature if D2 = yes). Extract.
3. Run `staging/push-manager -version`. It must print the target version. This proves the binary executes on this device. Abort on any failure; nothing is changed yet.
4. Save the old binaries as `*.prev`. Rename the new files into place (atomic, same filesystem). The running process is not disturbed.
5. Start a **detached helper** and exit control to it. The helper is the new `push-manager -apply-update` mode, started with `setsid` so the init script's stop cannot kill it. It writes `update/pending.json` (`target`, `started_at`).
6. The helper restarts push-catalog (port 7702), then push-manager, through the init.d scripts. Push-manager restart takes 1 to 3 seconds.
7. The helper polls `GET http://127.0.0.1:7701/api/status` for up to 30 s. It waits for `hack_version == target`.
   - Match: delete `pending.json`, keep `*.prev` until the next update.
   - No match: copy `*.prev` back, restart again, write `update/last_error`. The UI shows "Update failed, rolled back".
8. On start, push-manager reads `pending.json` or `last_error` and shows "Updated to vX" or "Update failed" once (screen toast and web banner).

**What the user sees.** Shadow UI shows "Updating..." in step 2 to 5. During step 6 the shadow UI is gone for 1 to 3 s, then it returns. The web page loses its connection and reconnects (needs a retry loop in `app.js`). Live, audio, MIDI to Live: untouched.

**Side effect to tell the user.** On start push-manager runs `clearStaleSurface()` (release.go). Any app that holds the surface (Stacks, Riffs, Home) is sent back to Live. The confirm prompt says so.

**What is not updated automatically**

- `push_hook.so`: the file is replaced, but the running Push3 keeps the old hook until Push3 restarts. Restarting Push3 would end the Live session, so the updater never does it. The UI says "hook updated, restart Push to apply" when `hook_changed` is true. Push-manager (new) and the loaded hook (old) must stay compatible, so `manifest.shm_protocol` is checked against the loaded hook's protocol; if they differ, the update is refused with "reboot required" (D6).
- init.d scripts, udev rules, `/etc/init.d/push3`: not touched. If `needs_reinstall` is true, the UI says "run install.sh from a computer" and offers no button.
- `hack.json` of push-manager: never overwritten. It holds the user's settings and install-time paths.
- Community hacks: Push Catalog already updates them. Out of scope.

## 6. Checker design

- **Source:** `GET https://api.github.com/repos/federico-pepe/ableton-push-hack/releases`. Use the list endpoint. `/releases/latest` skips pre-releases and would never see `-alpha`.
- **Compare:** own semver compare. `0.1.10-alpha` > `0.1.9-alpha`. Order: `alpha` < `beta` < `rc` < no suffix. Table-driven tests.
- **Filter:** skip drafts. Include pre-releases by default (setting `update.include_prerelease`, default true).
- **When:** first check 60 s after `waitForBootSettle` plus up to 10 min of random jitter. Then every 24 h. The last-check time is saved in `state.json`, so restarts do not re-poll.
- **Cheap:** send `If-None-Match` with the saved ETag. A 304 reply costs nothing against GitHub's 60 requests per hour per IP.
- **Offline:** no separate ping. The request itself is the test (5 s dial timeout, 10 s total). On failure: log once per state change, retry after 1 h, then 2 h, up to 24 h. Never show an error bar for a failed check.
- **Manual:** "Check now" in the System tab. Rate limit: one per 60 s.
- **Snooze:** "Dismiss" hides the bar for that version until a newer one appears.
- **Switch:** `update.auto_check` (default true). Off means zero network traffic.
- **State file:** `/data/push-hack/update/state.json`: `last_check`, `etag`, `latest`, `dismissed`, `last_error`.
- **Resource use:** one goroutine, one small HTTP request a day. Fits the "do not hog CPU or memory" rule.

## 7. UI

### Shadow UI (screen)

- Yellow bar, one line, ASCII only (core rule: `basicfont.Face7x13` has no other glyphs). Text: `Update v0.1.10-alpha available - press <button> to install`.
- Drawn in `renderFrame()` after the panel, on every panel, so it needs no change per panel.
- Confirm flow: first press arms the bar ("Press again to install, any other key cancels"). The second press within 5 s starts the update. A single stray press never starts an install on a live performance tool.
- Geometry (D4): the screen already has a top tab strip and a bottom soft-button strip. The yellow bar needs its own row. Options: (a) shrink the content area by one row, (b) draw over the hint area of the bottom strip. Needs a mockup against `suiH` / `suiContentBot` before coding.
- While Home or an app owns the surface, push-manager does not draw the shadow UI. The bar does not show there unless D4 says otherwise.

### Web app

- Yellow bar fixed at the page bottom on every tab, with "Update" and "Dismiss".
- System tab (`#btn-stats`, `showStats()`): new card with installed version, latest version, channel switch (include pre-releases), last checked, "Check now", "Update now" (confirm dialog), progress line, release notes link, last error.
- After "Update now": poll `/api/status` every second until the version changes, then reload.

### API (documented in `docs/api-reference.md`)

| Route | Method | Purpose |
|---|---|---|
| `/api/update/status` | GET | installed, latest, available, state, progress, last_error |
| `/api/update/check` | POST | check now (rate limited) |
| `/api/update/apply` | POST | body `{"version":"..."}`. Must equal the known latest. No URL accepted from the client. |
| `/api/update/dismiss` | POST | snooze the current latest |

## 8. Device facts (spike S0, run 2026-10-20 on the owner's unit)

Spike S0, read-only over SSH plus one throwaway Go binary. Report results to the owner before step 1.

| # | Question | Why it matters |
|---|---|---|
| S1 | Does a Go binary on the device complete a TLS handshake with `api.github.com`? Are CA certs present (`/etc/ssl/certs`)? The update-guard plan noted there is no `curl`, and Push Catalog uses shell tools. | If no CA bundle, the checker needs an embedded root pool (`x/crypto` or embedded PEM). |
| S2 | How does the push-manager init script stop and start? Does `stop` kill the whole process group or only the pidfile PID? Is the unit root? | Decides if `setsid` is enough to protect the helper, and whether the helper can call init.d. |
| S3 | Do the two binaries (11 MB and 6 MB) and the hook fit in `/data` staging with margin? Is `/data` the same filesystem as the install dir (so `rename` is atomic)? | Atomic swap needs same filesystem. |
| S4 | Does the shadow UI come back cleanly after a push-manager restart, and what does the screen show during the gap in passthrough vs takeover? | Defines the real "no freeze" claim. |

## 9. Decisions (owner, 2026-10-20)

- **D1.** Yes. Publish GitHub Releases with a tarball from `scripts/release.sh`.
- **D2 (dropped 2026-10-20).** The owner first said yes to ed25519 signing, then dropped it to match the other repos and the catalog: trust is HTTPS and the GitHub repo. The release carries SHA-256 hashes only, to catch a broken download. Known cost: a compromised GitHub account could publish a malicious release that units install as root. A key pair made for this sits in `~/.push-hack-release/` and is not used.
- **D3 (as built, step 6).** UPDATE is the 8th under-screen button (CC27), because top button 8 is already tab 8. The prompt has OK on button 7 and CANCEL on button 8, so a double tap on the opening button cancels. Original note: use a button above or below the screen. No double press. Flow: bar shows an "Update" button, then a modal "OK / Cancel". While the modal is open, push-manager takes those buttons, so no other hack can act on them. Open point for the mockup step: pick the exact CCs so they do not clash with the active panel's own soft buttons (CC20-27, `SoftBotStrip`), and check a hack that does not own the surface but still hears the press.
- **D4.** Bottom bar. Shown only when an update is available. Hidden while Home or an app owns the surface.
- **D5.** Call GitHub only. Never call ableton.com. `update.auto_check` setting turns it off (zero traffic).
- **D6.** Refuse the update when `shm_protocol` differs from the loaded hook; say "restart Push first".
- **D7.** Scope is only what this repo ships: push-manager, push-catalog, push-display (`push_hook.so`). Never catalog hacks. Updater writes only under `/data/push-hack/hacks/<id>/`. It never writes to `/etc`. What touches `/etc` today (all by `install.sh` / `deploy.sh`): `/etc/init.d/push-hack-*` scripts and their `rcN.d` links, the `LD_PRELOAD` line in `/etc/init.d/push3` (push-display), and udev rules `99-push-hack-*.rules` (some catalog hacks). The init scripts start the binary by fixed path, so a binary swap needs no `/etc` change. A release that changes any of those sets `needs_reinstall` in its manifest; the UI then says "run install.sh from a computer" and offers no button.

**Other limits to accept**

- Devices running a version before this feature never auto-update. They need one `install.sh` run.
- The apply route is open to anyone on the LAN, like the rest of push-manager. It can only install the verified latest release, not arbitrary files.
- First real test needs a published release newer than the installed one. Plan: ship `0.1.10-alpha` with the checker; later publish `0.1.11-alpha` to test the update itself.

## 10. Implementation steps

Each step ends with `go vet`, tests, and no commit (owner commits). Hardware steps wait for on-device confirmation from the owner.

1. **S0 spike** (section 8). Stop and report.
2. **Version stamping** (4.1). Tests: `-version` output.
3. **`semver.go` + tests.** Pure function, table tests including alpha/beta/rc and `0.1.9` vs `0.1.10`.
4. **`update.go` checker.** `httptest` fake GitHub: 200, 304, 403 rate limit, timeout, malformed JSON, drafts, pre-release filter, ETag, backoff. State file round trip.
5. **API routes + System tab card + web banner.** Check from the web only. Device check: owner confirms the card shows right versions and "last checked".
6. **Shadow UI bar** after D3/D4 mockup. Device check: bar shows, dismiss works, ASCII only, no change in frame rate.
7. **`scripts/release.sh` + manifest.** Dry run produces a tarball and checksums.
8. **Apply path.** Download, verify, stage, `-version` test. Unit tests with a temp dir and a fake init script. Test failure points: bad checksum, truncated download, wrong version in binary, disk full, power cut between rename steps (leaves `.prev`).
9. **Helper (`-apply-update`), restart, health check, rollback.** Test rollback by staging a binary that exits at start. Device check with the owner: real update `0.1.10` to `0.1.11`, plus a forced rollback, plus a pull-the-plug test during step 4 and step 6.
10. **Docs and changelog:** `CLAUDE.md` (new rule on `VERSION`, release script, updater), `README.md`, `docs/architecture.md`, `docs/api-reference.md`, `docs/scripts.md` (release.sh), `hacks/push-manager/README.md`, `CHANGELOG.md` `[Unreleased]`. Rebuild and add `hacks/push-manager/push-manager` and `hacks/push-catalog/push-catalog` in the same commit as source. Tell the installer repo owner only if `install.sh` changes (it should not).

## 11. Risks

| Risk | Handling |
|---|---|
| Update fails half way, unit left without push-manager | `.prev` copies, health check, rollback, atomic renames. Tested by forced failure and power cut. |
| Helper killed by init stop | `setsid`; S2 verifies. |
| Wrong binary arch or missing loader | Static build (`CGO_ENABLED=0`, already required); `-version` run before swap. |
| GitHub rate limit or outage | ETag, 24 h cadence, silent backoff. |
| Compromised release | D2 signature. |
| Update pressed by accident | Two-press confirm, never automatic. |
| Hook and manager out of step | D6. |
| Update during a gig | Update is only user-triggered. Prompt states the 1 to 3 s screen gap and that apps return to Live. |

## 12. Spike S0 results (2026-10-20)

| # | Result |
|---|---|
| S1 | **Pass.** A static Go binary got `200 OK` from `api.github.com` in 0.34 s. CA bundle is at `/etc/ssl/certs`, clock is right, DNS works. Public repo returns `[]`: **no GitHub release exists yet**. Rate limit showed 54 of 60 left for this IP, so the network is shared. The ETag plan stays. No `curl`, but `wget`, `openssl`, `python3` exist. |
| S2 | **Pass with one fix.** push-manager and push-catalog run as root, session and group 548. The init script `stop` kills only the pidfile PID, so a `setsid` helper survives. **But `restart` is `stop; sleep 1; start` with no wait for exit.** If the old process needs more than 1 s to exit, the new one fails on port 7701. The helper must wait for the old PID to exit, then call `start`, not `restart`. Push Catalog's script already waits (up to 5 s). |
| S3 | **Pass.** Install dir and `/data` are the same ext4 filesystem (`/dev/nvme0n1p5`, 141 GB free). Atomic `rename` works. Staging goes in `/data/push-hack/update/`. `push_hook.so` is root-owned, push-manager is root, so it can replace it. Binaries are owned by `ableton:users`; keep that owner after a swap. |
| S4 | **Not run.** It needs a push-manager restart on your unit and you watching the screen. Pending your OK. |

Extra: `hack.json` is mode 600 and holds user settings. The updater must not touch it (already in section 5).

**S4 timing (measured, 2 restarts):** old process exits in about 50 ms after SIGTERM. New process answers `GET /api/status` about 100 ms after `start`. The process gap is well under 1 s. The health-check route is `/api/status` (the field `version` there is `hack.json`'s, so add `hack_version`). Screen behaviour, owner-confirmed on device 2026-10-20: in Live view nothing visible. With the shadow UI open the screen goes black for the gap, then shows Live. The restart sent the surface to Live (`clearStaleSurface`) but left the LEDs as the shadow UI had them. That bug was fixed and confirmed on device the same day (`release.go`, `main.go`: LED repaint at start). So the restart path of the updater needs no new work for this. Apps holding the surface return to Live; the confirm prompt says so.

## 13. Progress (2026-10-20 to 21)

Done and confirmed on device: LED repaint on restart fix (branch `fix/led-repaint-on-restart`), step 2 version stamping, step 3 semver, step 4 checker, step 5 web bar, System card and routes, step 6 Shadow UI bar and prompt. Steps 7 (CI release, no signing), 8 and 9 (apply, helper, rollback) are built. Confirmed on device 2026-10-20: a failed update (new push-manager exits at start) rolled back in about 18 s, and a good update (0.1.10-alpha to 0.1.11-alpha) worked. Found on device and fixed: the helper must run from the old binary, not the new one; a dead helper must roll back; the new service dying must fail within 2 s; the Shadow UI must re-read the result file. Not yet tested: a real GitHub release, a hook change, power loss during an update.

Bench left on the device for testing (remove it first; it also leaves the unit on a bench-built 0.1.11-alpha): `/data/push-hack/update/settings.json` `url` key and `/data/push-hack/update/bench/` with a `python3 -m http.server 8099`. Remove before a real release.

## 14. Correction (2026-10-20)

This plan assumed the next release was `0.1.10-alpha`, from reading `git tag` in text order. The tags go to `v0.1.14-alpha` (sort with `git tag --sort=-v:refname`). `VERSION` is now `0.1.15-alpha`. Version numbers in this plan (0.1.10, 0.1.11, 0.1.12) are examples and bench names only. The first real test is: install the `0.1.15-alpha` build with `install.sh`, tag `v0.1.16-alpha`, press UPDATE.
