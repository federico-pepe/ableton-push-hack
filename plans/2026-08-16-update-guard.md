# Update Guard — protect push-hack across Ableton OS updates

**Status: Phase 0 (discovery) complete. Implementation NOT started — halted by decision, see "Decision" below.**

## Context

Push 3 on AbletonOS v3.21 updates itself via a **SWUpdate A/B-slot daemon** (`/usr/bin/swupdate`, started by `/etc/rc5.d/S70swupdate`, fronted by `/opt/push3/UpdateDBusService`). With push-hack installed this causes two distinct failures, documented at `docs/push3-internals.md:184` and `:186`:

1. **Device freezes mid-update.** `push_hook.so` is LD_PRELOAD-injected into `Push3` and interposes `libusb_bulk_transfer`. During an update Push3 flashes the co-processor firmware over that same libusb path; the collision hangs the device (observed at `SWUpdateStatus::run (6%)`, recoverable only by hard power-off). A previously attempted *in-process* kill-switch failed — an LD_PRELOAD interposition cannot be removed from a live process.
2. **Services vanish after the update.** The slot flip boots a pristine vendor root, so `/etc/init.d/push-hack-*`, the `/etc/rc5.d/S99push-hack-*` symlinks and the `/etc/init.d/push3` LD_PRELOAD patch are all gone. `/data/push-hack/` (binaries, configs) survives.

Today the only mitigation is manual: uninstall before updating, reinstall after (`README.md:25`, `MANUAL.md:61`).

**Original hypothesis:** the failed kill-switch was *in-process*; push-manager is a separate root process and could stop push-display (un-patching `/etc/init.d/push3` and restarting Push3) before the flash begins — a fix the earlier attempt could not reach. Intended UX: detect the incoming update, warn full-screen, disarm on hardware confirmation.

---

## Phase 0 — Device discovery: RESULTS (2026-08-16)

Probed against the live device (AbletonOS `abletonos-x86_64-intel-v3.21`, serial 37589789) over read-only SSH.

### Confirmed — design foundations hold

| # | Question | Result |
|---|---|---|
| **D8** | Does push-manager run as root? | ✅ **Yes** — `root 779 push-manager`. An out-of-process disarm is privileged enough to work. |
| **D5** | Per-partition counters for the inactive slot? | ✅ **Yes** — `nvme0n1p4` in `/proc/diskstats` shows **0 writes, 0 sectors written, ever**. Nothing but an update ever touches it, so a write delta is a zero-false-positive signal. |
| **D3** | swupdate invocation | `/usr/bin/swupdate -v -p /opt/push3/reboot.sh -e stable secondary` (pid 822, root). `/proc/822/fd` not readable as `ableton`. |
| — | Update duration | `::run` advances ~**1% per second**, ~**100 s** total. The documented freeze at 6% is ~6 s in. |
| — | OS version source | `/etc/os-release` → `VERSION_ID=abletonos-x86_64-intel-v3.21`. |

### The blocker — D2 fails the go/no-go

Measured from a real update sequence in `/data/logs/Push3.log` (2026-01-09):

```
09:23:06.007  "Response was 200"                                    ← last update-check poll
              …10 minutes of COMPLETE SILENCE (user deciding)…
09:33:16.770  "Start writing software update"                       ← earliest possible signal
09:33:16.933  "Software update state changed 'SWUpdateStatus::start' (0%)"
09:33:16.948  "Software update state changed 'SWUpdateStatus::run' (0%)"
09:33:22.902  "…'SWUpdateStatus::run' (6%)"                         ← documented freeze point
```

- `"Start writing software update"` → `::start` = **163 ms**
- `::start` → `::run` = **15 ms**

**163 ms is not enough to render a warning frame, and nowhere near enough for a 1.5 s hold-to-confirm.** The chosen confirm-gated UX cannot exist on this trigger. The plan's own go/no-go clause ("if D2's window is too short… do not fake protection that isn't there") is triggered.

### Why there is no earlier local signal

- **The 10-minute pre-update window is entirely silent in `Push3.log`.** No download-started, no download-progress, no update-available event. The download is handled inside `UpdateDBusService`/`swupdate`, which do not log there.
- **`PushWebServiceCli.log` is a dead end** — it logs only local HTTP transfers to/from a browser on the LAN (`/data/settings/.web_service.id`, `/opt/push3/.../html/*`). It is the on-device web server, not the update downloader.
- **The update-check poll is not a signal.** `"Start reading software update state"` / `"Getting next version URL …"` / `"Response was 200"` fire at every boot and repeatedly afterwards, regardless of whether an update exists.

### The one viable early signal — deliberately NOT pursued

Push3 polls a public Ableton endpoint whose response cleanly states whether an update exists:

```
GET https://hardware-updates.ableton.com/api/v1/update/push3-12-public-beta/2.4.5b6/
{"id":464,"version":"2.4.5b11","created":"2026-08-12T13:49:48",
 "product":"push3-12-public-beta","mandatory":false,
 "updatefiles":[{"path":"push3-12-public-beta/2.4.5b11/update.swu",
                 "url":"https://cdn-hardware-updates.ableton.com/.../update.swu"}]}
```

(Verified 2026-08-16: this device is on `2.4.5b6`; `2.4.5b11` is available.) Channel and current version are recoverable from the poll URL Push3 itself logs. push-manager is Go, so `net/http` would cover it with no new dependencies — there is no `curl` on the device.

This would give **days** of warning instead of 163 ms, and the confirm-gated disarm would work exactly as designed.

**Decision (2026-08-16): rejected — we are not building on Ableton's update API.** It makes push-manager phone home from a process the user did not expect to do so, and couples the hack to an endpoint we do not control. Recorded here so the option is not rediscovered from scratch later.

### Other probe notes

- **`Push3.log` is a binary file** (contains null bytes — `grep` reports "binary file matches"). Any future log scanner must handle that, not assume clean text.
- Log format is `<ISO8601>=info { "message": "…" }`; `launcher.log` wraps Push3's stdout with escaped newlines, so the same strings appear in both.
- `launcher.log` rotates by date (`launcher.log.YYYY-MM-DD`); `Push3.log` does not rotate and spans months.
- **Not probed** (became moot once D2 failed): D7 (exec of the initd script from Go), D10 (does a push3 restart also restart Live).

---

## Decision

**Halted after Phase 0.** Reactive auto-detection is not viable (163 ms), and the only trigger that would support the approved UX — polling Ableton's update API — was rejected on the grounds above.

Nothing was implemented. No files were created or modified beyond this plan document.

## What remains viable, if this is picked up later

Everything below is independent of detection and was already validated as sound during planning. None of it requires network access or log parsing.

1. **Manual disarm path.** A `Shift + User` hardware chord and a web-UI "Prepare for OS update" button, both invoking the same flow: `exec` `/etc/init.d/push-hack-push-display stop` (which strips the `# push-hack: push-display` marker from `/etc/init.d/push3` and restarts Push3), then verify the marker is gone. Never re-implement the `sed` — that contract lives in `hacks/push-display/service.initd` and the two would drift.
2. **Breadcrumb on `/data`.** `install-manifest.json` written at push-manager startup (always-current hack inventory, independent of any update event) plus a plain-text `READ-ME-AFTER-OS-UPDATE.txt`. After the slot flip push-manager is dead and its web UI unreachable, so the only surfaces left are SSH and Push's own file browser. Reuse `installedHacksSummary()` ([live_log.go:104](../hacks/push-manager/src/live_log.go#L104)) by splitting out a structured `installedHacks()`.
3. **`scripts/install.sh --repair`.** The recovery is *already implemented* — `install_hack_service()` does the whole `/etc/init.d/` + `update-rc.d` + start dance, and for push-display re-applies the LD_PRELOAD patch idempotently. `--repair` only needs to reach it while skipping the build and binary-copy path. `check_connection()` already auto-clears the post-update SSH host-key change ([lib/common.sh:104](../lib/common.sh#L104)). Seconds, no SCP, no build.
4. **A too-late marker.** On seeing `"Start writing software update"` in `Push3.log`: write the breadcrumb, log loudly, take **no** action. Restarting Push3 mid-flash kills the process writing co-processor firmware — worse than a freeze a power-cycle recovers from.

Item 3 alone removes most of the pain of the symlink wipe and is testable today without any OS update.

## Known limits (unchanged, and worth keeping in the docs)

1. **Full on-device self-heal after the slot flip is impossible.** Restoring `/etc` needs root, and after the flip nothing of ours runs — no cron, no init hook. Host-side `--repair` is the ceiling.
2. **Acting during `::run` cannot help and can hurt.**
3. **Any warning needs a working display**, which needs the hook — the thing being guarded. If Push3 has already wedged, no warning appears.
4. **Disarm costs a Push3 restart.** There is no cheaper disarm: neutralising the hook in place was already tried and the update still froze.
