# Push 3 Internals

Reference document for Ableton Push 3's OS and filesystem. Discovered via `./scripts/discover.sh` and direct SSH exploration. Update this file when new firmware is installed or new paths are discovered.

**Companion reference docs** (kept separate — this file links, does not duplicate):
- [`push3-button-map.md`](push3-button-map.md) — button / encoder / pad MIDI map (CC + Note numbers)
- [`push3-led-colors.md`](push3-led-colors.md) — 128-entry LED color palette (pads + buttons)
- [`push3-assets.md`](push3-assets.md) — all 663 UI image assets under `/opt/push3/.../Images/`

---

## System

| Field | Value |
|-------|-------|
| OS | AbletonOS `abletonos-x86_64-intel-v3.21` |
| Kernel | `5.15.48-intel-pk-preempt-rt` (real-time kernel) |
| Architecture | `x86_64` (Intel) |
| Hostname | `push` |
| Init system | sysvinit (runlevel 5) |
| Co-processor firmware | package `push3_fw_1.5.4` — XMOS `1.0 Build 91` (`1.0.91`), pmc `1.20`, apc `1.9`, xySensor `0.13`, imagery `0.7` |
| Discovered | 2026-07-25 |

> **Update note (2026-07-25):** device updated to AbletonOS `v3.21` (was `v3.20`). Kernel unchanged. Firmware bundle `push3_fw_1.5.4` (from `/data/logs/Push3.log`). New since v3.20: A/B dual-slot OS updates via **SWUpdate** (`swupdate` daemon + `S70swupdate`), and three Live↔Push3 **Unix-socket IPC channels** under `/data` (see Filesystem layout).

> **Update note (2026-08-25):** Ableton published GPL source for v2.4.2
> (`resources/push-assets/push3-242-gpl-sources.tgz`, gitignored — kernel +
> stock GPL package sources only, not the proprietary rootfs). **Headline
> result: a keyboard plugged into the USB-A port can already control the
> onboard Live, visibly reflected on Push's own screen, with zero new
> code** — Live's own shortcuts (`Ctrl+N`/`Ctrl+T`/`Ctrl+Shift+T`) executed
> live and Push3's app redrew to show it (see "Keyboard shortcuts control
> the onboard Live" below). The kernel config's USB Gadget options looked
> promising but were **checked live on the device and ruled out** — no
> gadget instance, no UDC exists at runtime (see "External-facing USB
> personality — gadget theory tested and killed" below); the external
> interface layout is most likely XMOS's own USB device presenting
> directly, not an SoC-composed gadget. Also confirmed: mouse/keyboard HID
> works end-to-end at the raw `/dev/input/eventN` level too (needs `root`
> or `input`-group membership to read it), and the three Live↔Push3
> `/data` IPC sockets are real and currently
> connected, but standalone-mode only (Push's own onboard Live↔Push3 app,
> not reachable from a tethered external computer). No custom Ableton
> driver code found in the kernel tree itself — the actual protocol logic
> lives in proprietary userspace, not GPL sources.

---

## SSH access

Two accounts:

| User | Usage |
|------|-------|
| `ableton` | Normal operations, file access, log reading |
| `root` | Service installation (`/etc/init.d/`), `update-rc.d`, system paths |

No `sudo` on the device. Scripts that need root use a separate `root@push.local` SSH session.

SSH keys are managed via a D-Bus service (`/opt/push3/SSHKeyDBusService`), which writes authorized keys to `/data/settings/ssh/`. SSH can be enabled/disabled from Push's own UI settings.

Connect: `ssh ableton@push.local` / `ssh root@push.local`

**Host key changes on OS update.** An Ableton OS update regenerates the device's SSH host key, so the next connection fails with `REMOTE HOST IDENTIFICATION HAS CHANGED` (client's cached key no longer matches). Fix manually with `ssh-keygen -R push.local`. The installer does this automatically — `check_connection()` in `lib/common.sh` detects the warning and clears the stale key via `clear_host_key()` before retrying.

---

## Filesystem layout

Single NVMe disk `nvme0n1` (238.5GB), 5 partitions — **A/B dual-slot** layout for atomic OS updates:

```
nvme0n1p1   /boot   vfat   35.7MB          — EFI boot partition
nvme0n1p2   [SWAP]         8GB             — swap
nvme0n1p3   /       ext4   10GB, ~51% used — ACTIVE root slot (READ-ONLY at /opt)
nvme0n1p4           ext4   10GB            — INACTIVE root slot (SWUpdate writes new OS here, then flips)
nvme0n1p5   /data   ext4   210.4GB, 7% used — ALL user/mutable data (writable by ableton)
/run            tmpfs                      — runtime files, PIDs (cleared on reboot)
/var/volatile   tmpfs                      — volatile var data
```

`/dev/root` (mounted at `/`) is the currently-active slot (`nvme0n1p3` on this device). SWUpdate installs the next OS image onto the *other* slot (`nvme0n1p4`) and flips the boot pointer on reboot — so a failed/interrupted update can roll back to the still-good slot. This is why `/data` and `/data/push-hack/` survive OS updates: only a root slot is rewritten.

### Key paths under `/data` (writable, 201GB partition)

```
/data/
├── Music/
│   └── Ableton/                  ← user projects, samples, presets (main content)
├── live-to-push-midi-ipc-channel ← Unix socket: Live → Push3 MIDI (v3.21+)
├── push-to-live-midi-ipc-channel ← Unix socket: Push3 → Live MIDI (v3.21+)
├── push-flip-api-ipc-channel     ← Unix socket: Push "flip" API (v3.21+)
├── logs/
│   ├── Push3.log                 ← Push3 controller app log
│   ├── PushWebServiceCli.log     ← web service log
│   └── launcher.log[.DATE]       ← launcher log (rotated)
├── settings/
│   ├── Options.txt               ← Live options/flags
│   ├── connman/                  ← network configuration
│   ├── secrets/                  ← device secrets
│   ├── ssh/                      ← SSH authorized_keys (managed by SSHKeyDBusService)
│   └── webservice.db             ← web service database
├── Crashes/                      ← crash reports (.zip)
├── AllCrashes/                   ← AllCrashes.zip archive
├── Crashpad/                     ← Crashpad crash database (root-readable)
├── nativeKONTROL/                ← third-party ClyphX Pro data
├── Scratch/                      ← root-owned scratch space
├── .config/
│   ├── Ableton/                  ← Ableton Live config
│   └── Cycling '74/              ← Max for Live config
└── .cache/
    ├── Ableton/                  ← Ableton cache
    ├── fontconfig/
    └── mesa_shader_cache/        ← GPU shader cache
```

**Live↔Push3 IPC channels (new in v3.21):** the three `*-ipc-channel` entries at the `/data` root are Unix domain sockets (`srwxr-xr-x`, owned `ableton:users`) that carry the *internal* Live↔Push3 message bus — MIDI in both directions plus a "flip" API. These are **application-internal** and do **not** replace the hardware ALSA sequencer path: pads/buttons/encoders still arrive on the kernel `Ableton Push 3 Live Port` (client 16:0) that push-manager subscribes to (see [MIDI protocol](#midi-protocol)). The hack does not touch these sockets. Alongside them, `/data` also holds many transient per-session sockets (`wc<hex>`, `<hex>`) — Push web-service working-copy channels, safe to ignore.

### Read-only paths (do not modify)

```
/opt/push3/     ← all Push3 binaries and firmware
/               ← root filesystem (aside from /data, /tmp, /run)
```

---

## Running processes

Relevant processes at boot (from `ps aux`):

| Process | User | Notes |
|---------|------|-------|
| `/opt/push3/Live` | ableton | Full Ableton Live instance (~580MB RSS, ~9% CPU) |
| `/opt/push3/Push3` | ableton | Push3 controller UI (~126MB RSS) |
| `/opt/push3/XPython3Exe push3-launcher.pyc` | ableton | Python launcher |
| `/opt/push3/PushWebServiceCli` | ableton | Internal web service, logs to `/data/logs/` |
| `/opt/push3/Ableton Index` | ableton | Content indexer (`SNl` — low priority) |
| `/opt/push3/SSHKeyDBusService` | root | D-Bus SSH key management |
| `/opt/push3/UpdateDBusService` | root | OTA update service (front-end, `--with-live-service`) |
| `/usr/bin/swupdate` | root | **SWUpdate A/B slot updater** — `swupdate -v -p /opt/push3/reboot.sh -e stable secondary`; writes new OS to inactive slot, reboots via `reboot.sh` |
| `/opt/push3/XCrashpadHandler` | ableton | Crash reporter → Sentry |
| `x-window-manager` | root | Minimal X window manager for display |
| `Xorg :0` | root | X server for Push display |
| `avahi-daemon` | avahi | mDNS → `push.local` hostname |

**Memory at rest:** ~861MB used of 7.4GB (8GB swap, unused). Plenty of headroom for hacks.  
**CPU at rest:** Live ~13%, Push3 ~5%, `Ableton Index` spikes ~56% while indexing (nice `SNl`, low priority — settles when done).

---

## Network

| Interface | Address | Notes |
|-----------|---------|-------|
| `wlan0` | `192.168.1.67` (DHCP) | Wi-Fi, connects to local network |
| `lo` | `127.0.0.1` | Loopback |

Push can also act as a Wi-Fi hotspot (configurable from Push display settings). When in hotspot mode the IP will be different — use `push.local` for mDNS-based access.

### Listening ports (stock)

| Port | Bind | Service |
|------|------|---------|
| 22 | `0.0.0.0` | SSH |
| 53 | `127.0.0.1` | DNS (avahi/connman local resolver) |

No web ports open by default — push-hack adds port 7701+.

---

## Init system

sysvinit, **runlevel 5**. Boot scripts in `/etc/rc5.d/` as `S##name` symlinks to `/etc/init.d/`.

Boot order (full, from `/etc/rc5.d/`, v3.21):
```
S01data-partition-connman-setup
S01networking
S02dbus-1
S04add-empty-grubenv.sh
S04set-ownership.sh
S05connman
S09sshd
S09xserver-nodm
S15mountnfs.sh
S20acpid
S20data-partition-resize.sh
S20hwclock.sh
S20sysctl-push3            ← kernel sysctl tuning for Push3 (init.d/sysctl-push3)
S20syslog
S21avahi-daemon
S21push3                   ← launches Push3 app (init.d/push3 — carries the LD_PRELOAD patch)
S70swupdate               ← starts SWUpdate A/B updater daemon
S99ntpd
S99report-aos-version.sh
S99rmnologin.sh
S99stop-bootlogd
```

**`update-rc.d`** available at `/usr/sbin/update-rc.d` — use this to register/deregister init.d services.

push-hack services are registered as `S99push-hack-<id>` (late in boot order, after network is up).

> **⚠️ Firmware updates wipe init.d symlinks.** After any Push firmware update, re-run `./scripts/install.sh` to re-register services. Binary and data under `/data/push-hack/` survive updates unchanged — only the `/etc/rc5.d/` symlinks and the `/etc/init.d/push3` LD_PRELOAD patch are lost.

> **⛔ Uninstall the hack before an Ableton OS update.** Push3 itself drives the update and flashes the co-processor firmware over USB — the same `libusb_bulk_transfer` path the push-display LD_PRELOAD hook interposes. An active hook collides and **freezes the device mid-update** (observed stuck at `SWUpdateStatus::run (6%)`, drowning in `MidiHub: Failed to push MIDI message to outgoing queue`; requires a hard power-off). There is **no in-process mitigation** — an LD_PRELOAD interposition cannot be removed from a running process, and by the time the update signal (`SWUpdateStatus::start`) appears, Push3 is already the hooked process performing the update (a detect-and-neutralize monitor was tried and confirmed insufficient: the hook was neutralized yet the update still froze). Run `./scripts/uninstall.sh` before updating, then reinstall after. Push3's update-state machine, for reference: `SWUpdateStatus::start` → `::run (N%)` → `::done` → `::success`/`::failure`, emitted to `/data/logs/Push3.log`. The underlying updater is the **`swupdate`** daemon (`S70swupdate`): it writes the new OS image to the inactive root slot (`nvme0n1p4`) and flips the boot pointer, while Push3 flashes the co-processor firmware over USB — it's the USB flash step that collides with the display hook.

---

## Available runtimes

| Runtime | Version | Path |
|---------|---------|------|
| Python 3 | 3.10.18 | `/usr/bin/python3` |
| Bash | 5.1.16 | `/bin/bash` |
| sh | → bash | `/bin/sh` |
| Go | not installed | — |
| Node.js | not installed | — |

Python 3 (`/opt/push3/XPython3Exe`) is used by Push's own launcher. The system `python3` is standard Python 3.10.

For push-hack services: cross-compile Go binaries on dev machine (`GOOS=linux GOARCH=amd64`), deploy static binary to Push. No runtime deps needed.

---

## Key binaries under `/opt/push3/`

Discovered from process list (read-only, do not modify):

| Binary | Role |
|--------|------|
| `Live` | Full Ableton Live |
| `Push3` | Push3 hardware controller |
| `XPython3Exe` | Python 3 runtime (custom) |
| `PushWebServiceCli` | Internal HTTP web service |
| `Ableton Index` | User library content indexer |
| `SSHKeyDBusService` | SSH key management via D-Bus |
| `UpdateDBusService` | OTA update handler |
| `XCrashpadHandler` | Crashpad crash reporter |

---

## UI Assets

Push ships **663 PNG images** (v3.21) under `/opt/push3/products/push3/assets/Images/` (read-only).
All icons are **white on transparent background** — designed for Push's own dark UI; use as-is on dark web UIs, invert with CSS on light backgrounds.

**Full grouped index → [`docs/push3-assets.md`](push3-assets.md)** — every file, grouped by prefix (root Images/ + `Browser/` subfolder). Not duplicated here.

### Serving assets from a hack

The push-manager's `/api/assets/<path>` endpoint proxies any file from `/opt/push3/products/push3/assets/Images/` with `Cache-Control: public, max-age=86400`. Other hacks can implement the same pattern.

---

## USB peripherals (USB-A port)

### External storage — works out of the box

Push already has `usb-storage` kernel module registered and udev rules for auto-mounting block devices. Ableton's own `/opt/push3/sense-usb-storage.sh` script fires on storage add events (field diagnostic tool that bundles logs onto USB drives), confirming this is designed-for hardware.

**Auto-mount flow:**
1. Plug in USB drive → udev `add` event fires
2. `/etc/udev/scripts/mount.sh` runs, mounts to `/run/media/<label>-<device>` (e.g. `TEST-sda1`)
3. Lock file written: `/tmp/.automount-<name>` — used as "already mounted" cache
4. On remove event, udev fires `remove`, `mount.sh` unmounts and deletes the lock file

**Supported filesystems:** ext4, vfat/FAT32, NTFS, HFS+ (all built into kernel).

**Unmount via software:**  
`syscall.Unmount(path, 0)` works but bypasses udev — the `remove` event never fires, so the lock file at `/tmp/.automount-<name>` is NOT cleaned up. On the next physical plug-in, `mount.sh` finds the lock file and skips mounting. Fix: explicitly `os.Remove("/tmp/.automount-" + name)` after `syscall.Unmount`.

**Detecting real mounts vs. leftover dirs:**  
After unmount, `/run/media/<name>` directory may persist as an empty dir on the `/run/media` tmpfs. Distinguish with a device ID check:
```go
var parentStat, childStat syscall.Stat_t
syscall.Stat("/run/media", &parentStat)
syscall.Stat(mountPath, &childStat)
if childStat.Dev == parentStat.Dev { /* not mounted, leftover dir */ }
```

**Swap partitions:** udev also mounts swap partitions (names like `swap1-nvme0n1p2`). Filter these in any listing code.

### Mouse/keyboard (HID) — kernel supports it, disabled by default

Confirmed from the kernel config shipped in Ableton's GPL source release for
v2.4.2 (`resources/push-assets/push3-242-gpl-sources.tgz`, gitignored —
see `sources/x86_64-oe-linux/linux-push3-*/kernel.config`):
`CONFIG_USB_HID=m`, `usbmouse`/`usbkbd`/`hid-generic` modules all present
(matches a prior on-device investigation: the `.ko` files exist under
`/lib/modules/5.15.48-intel-pk-preempt-rt/kernel/drivers/hid/usbhid/` but
are **not loaded at boot**). `CONFIG_INPUT_MOUSEDEV=y`,
`CONFIG_INPUT_EVDEV=y`, `CONFIG_INPUT_KEYBOARD=y` are all built in, so once
`usbhid` is loaded (`modprobe usbhid`, or hotplug if udev rules allow it —
unconfirmed) a plugged-in mouse/keyboard should appear under
`/dev/input/eventN` via the normal kernel input subsystem.

**Known limitation, unchanged by this finding:** Push3's own app runs
`--faceless` (see Push3 process architecture below) and reads all its own
hardware input from the XMOS co-processor, never from X11/evdev — so even
with `usbhid` loaded, a keyboard would not work in Push3's own UI. It would
work in a separate X11 client running on `:0` (`Xorg` is confirmed running
at boot — see Init system), since that's a normal X server with normal
input handling.

### Battery — XMOS firmware only, not via sysfs

`/sys/class/power_supply/` is **empty** on Push 3. The ACPI battery driver is present but has no devices bound. Battery state is managed by the XMOS co-processor and communicated to the Linux Compute Module via a **custom MIDI SysEx protocol** over the internal USB connection (`2982:1969`, ALSA device `Ableton Push 3`).

The Push3 Python battery component (`Push2/battery_component.pyc`) queries battery status via `_enquire_battery_status` SysEx calls. There is a `GET_POWER_STATE_MESSAGE_ID` constant in `Push2/sysex.pyc` for querying power supply connection state. No publicly documented SysEx command byte values are available.

Battery info is **not accessible** from outside the Push3 Python environment without reverse-engineering the custom SysEx protocol and sending raw MIDI messages.


---

## Push3 process architecture

`/opt/push3/Push3` is a **Qt5/QML application** with key flags:
- `--faceless` — Qt EglFS platform plugin: renders direct to DRM/KMS framebuffer, no X windows
- `--no-parent` — detached from init process tree

Started by `/etc/init.d/push3` → `XPython3Exe push3-launcher.pyc` → spawns `Push3` binary as `ableton` user.

Push3 bundles its own Qt5 (`/opt/push3/lib/libQt5*.so`) and **libusb-1.0** (`/opt/push3/lib/libusb-1.0.so`). It does NOT use system X11 for input — all hardware I/O goes through the XMOS co-processor via USB.

D-Bus system service: `com.ableton.push3` at `/com/ableton/push3`. Exposes methods including `renamePath(iss)` (rename files in Push's browser), `hasLive12License`, `screenRequest`, `sendFileMetadata`. No battery or hardware state exposed.

---

## XMOS co-processor — USB interface map

The XMOS chip (USB device `2982:1969`, internal hub port `1-1.4`) handles all hardware I/O. 7 USB interfaces:

| Interface | Class | Endpoints | Role |
|-----------|-------|-----------|------|
| 0 | `0xFF` vendor-specific | `ep_01` OUT, `ep_81` IN | **Display** — bulk transfer, pixel data |
| 1 | `0x01` Audio | — | Audio streaming |
| 2 | `0x01` Audio | — | Audio streaming |
| 3 | `0x01` Audio | — | Audio streaming |
| 4 | `0x01` Audio control | — | Audio control |
| 5 | `0x01` MIDI streaming | `ep_03` OUT, `ep_83` IN | **MIDI** — pads, buttons, encoders, SysEx |
| 6 | `0xFF` vendor-specific | `ep_04` OUT, `ep_84` IN | Hardware control (LEDs, battery?) |

### Display protocol (confirmed empirically — Push 2 and Push 3 identical)

Based on Ableton's published [Push 2 MIDI and Display Interface](https://github.com/Ableton/push-interface) spec. Push 3 uses same VID (`0x2982`), same endpoint layout, same display geometry.

- **Pixel format:** BGR565 — 16-bit little-endian, bits 15–11 = Blue, 10–5 = Green, 4–0 = Red (confirmed by empirical color tests)
- **Resolution:** **960×160 px** visible pixels, row stride = **1024 pixels** (64px padding per row). Same on Push 2 and Push 3.
- **Frame structure (per display update):**
  1. 16-byte header transfer: `{ 0xFF, 0xCC, 0xAA, 0x88, 0x00, ... }`
  2. Pixel data: **2 × 327,680-byte bulk transfers** (655,360 bytes total) — Push 3 sends each frame twice
  3. Multiple 16-byte sync transfers: `{ 0xF8, 0xF8, 0x13, 0x13, ... }` (5th byte varies)
- **Frame bytes:** `1024 stride × 160 rows × 2 bytes/pixel = 327,680 bytes per half`. Full frame = 655,360 bytes.
- **Signal shaping:** pixel data XOR'd byte-by-byte with repeating `{0xE7, 0xF3, 0xE7, 0xFF}`. This is a 1-byte phase rotation of the `0xFFE7F3E7` value cited in some docs — the pixel bulk transfer starts at phase 1. Empirically confirmed: black pixels (0x0000) → wire bytes `e7 f3 e7 ff ...`. To encode pixel `V` at byte offset `i` within the chunk: `wire[i] = V_byte[i] ^ {0xE7,0xF3,0xE7,0xFF}[i & 3]`.
- **Timing:** 60fps double-buffered; display goes blank if no frame within ~2s
- **Orange #FF5500 in BGR565:** `0x02BF` (LE bytes: `0xBF 0x02`). Wire bytes after XOR: `0x58 0xF1 0x58 0xFD` (at phase-0 offset).

### MIDI protocol

#### How Push3 routes MIDI (important)

**MIDI does NOT flow through libusb at the application level.** Despite the XMOS co-processor using USB endpoint `ep_03`/`ep_83` for MIDI, the Linux ALSA kernel MIDI driver handles that USB traffic internally. Push3's application code talks to ALSA, not raw libusb, for MIDI. The LD_PRELOAD libusb hook does **not** see any MIDI traffic.

#### ALSA devices

| Path | Device | Notes |
|------|--------|-------|
| `/dev/snd/midiC0D0` | ALSA rawmidi | 2 subdevices; subdevice 0 owned by Push3 app |
| `/dev/snd/seq` | ALSA sequencer | Broadcast to all subscribers — correct approach |

**ALSA rawmidi is exclusive per subdevice.** Push3's app (PID ~1188, using RtMidi) holds subdevice 0. An outside opener gets subdevice 1, which only receives hardware-broadcast messages (Active Sensing `0xFE` ~3×/sec) — not pad/button/encoder events.

#### ALSA sequencer topology

Observed on v3.21 (`/proc/asound/seq/clients`), stock, no external MIDI, push-hack not installed:

```
Client   0: System            (kernel)
Client  16: Ableton Push 3     (kernel hw — ALL Push ports live under this ONE client)
  Port 0 : Ableton Push 3 Live Port      (RWeX) — pads/buttons/encoders  ← subscribe here
  Port 1 : Ableton Push 3 User Port      (RWeX) — MIDI I/O routing
  Port 2 : Ableton Push 3 External Port  (RWeX) — external TRS/DIN MIDI   (NEW in v3.21)
Client 128: Ableton Live       (User)   — Live's own seq client; subscribes to 16:0
Client 129: RtMidi Input Client (User)
Client 130: RtMidi Output Client (User)
```

**⚠ Topology changed in v3.21.** Previously Live Port and User Port were separate kernel clients (`16` and `20`); they are now **three ports on a single client 16** (`16:0` Live, `16:1` User, `16:2` External — the External Port is new). Live is now its own named seq client `Ableton Live` (128) rather than the old anonymous "RtMidi Input". Interception is unchanged: subscribe to `16:0` for pads/buttons/encoders. `Midi Through` (old client 14) was not present in this capture.

When push-manager is running it appears as an additional client (`131+`) also subscribed to `16:0`; every subscriber receives client 16's events simultaneously — the sequencer broadcasts. This is the correct interception mechanism.

**⚠ Client numbering shifts at boot** if external MIDI devices are connected to the USB-A port or MIDI I/O ports before Push3 starts. Each additional device claims the next available client number, so the Push 3 built-in port that is normally at `16:0` may appear at `18:0` or higher. Typical numbering with a USB-A keyboard connected at boot:
- `16:0` — USB MIDI keyboard
- `18:0` or `20:0` — Push 3 Live Port

push-manager auto-detects Push 3 by scanning `/proc/asound/seq/clients` for "Ableton Push 3 Live Port" on each connection attempt (`detectPush3Port()`), so a shifted client is handled transparently — no manual intervention needed. The port selector dropdown (`GET /api/midi/ports`, `POST /api/midi/subscribe`) can still override the target; doing so disables auto-detect for that session. To restore stable numbering: disconnect external devices and reboot Push.

#### Subscribing via ALSA sequencer (no cgo, pure Go ioctls)

```
1. open("/dev/snd/seq")
2. ioctl CLIENT_ID  (0x80045301) → own client ID
3. ioctl CREATE_PORT (0xC0A85320) — portInfo[168 bytes]:
     portInfo[0] = ownClientID   ← REQUIRED: kernel checks info->addr.client == client->number → EPERM if wrong
     name = "push-hack-monitor"
     capability = capWrite | capSubsWrite
     type = portTypeMidi | portTypeApp
4. ioctl SUBSCRIBE_PORT (0x40505330) — subscribe[80 bytes]:
     sender client = 16, sender port = 0
     dest client = ownClientID, dest port = 0
5. read() loop — blocks until events arrive; decode 28-byte snd_seq_event structs
```

**snd_seq_event layout (28 bytes):**
```
offset  0: uint8  type       (6=NoteOn, 7=NoteOff, 8=PolyPress, 10=CC, 11=PgmChange, 12=ChanPress, 13=PitchBend, 40=ActiveSensing, 130=SysEx)
offset  1: uint8  flags      (bit 2 set = variable-length / SysEx)
offset  2: uint8  tag
offset  3: uint8  queue
offset  4: uint64 time       (8 bytes)
offset 12: uint16 src addr   (client, port)
offset 14: uint16 dst addr   (client, port)
offset 16: [12]uint8 data    (for fixed events: note[0]=ch, [1]=note, [2]=vel; CC: [0]=ch, [1]=param, [2]=value)
```

For SysEx (`flags & 0x04` set): the 28-byte header is followed by a variable-length block whose length is `data[4..7]` as a uint32 LE pointer-sized field; Go reads the extra bytes off the socket in a second `read()`.

#### SysEx manufacturer / known messages

SysEx manufacturer ID: `00 21 1D` (Ableton). Known constants from `Push2/sysex.pyc`:
- `GET_POWER_STATE_MESSAGE_ID` — query AC power connection
- `POWER_COMMAND_MESSAGE_ID` — power control
- `IDENTITY_RESPONSE_PRODUCT_ID_BYTES_PUSH3` — identity response

Push3 sends periodic SysEx heartbeats (~3–5/sec) including LED state and touch sensor data.

Full Push 2 MIDI implementation documented at https://github.com/Ableton/push-interface — Push 3 is a superset. For the exact Push 3 control→CC/Note assignments verified on hardware, see [`push3-button-map.md`](push3-button-map.md); for the LED color indices (same palette for pad Note-velocity and button CC-value), see [`push3-led-colors.md`](push3-led-colors.md).

### External-facing USB personality — gadget theory tested and killed (2026-08-25)

Ableton's v2.4.2 GPL source release (`resources/push-assets/push3-242-gpl-sources.tgz`,
gitignored, ~725MB Yocto/OpenEmbedded dump — kernel + stock GPL package
sources, not the proprietary rootfs) has this in the kernel config
(`sources/x86_64-oe-linux/linux-push3-*/kernel.config`):

```
CONFIG_USB_GADGET=m
CONFIG_USB_LIBCOMPOSITE=m
CONFIG_USB_CONFIGFS=m
CONFIG_USB_CONFIGFS_F_MIDI=y / F_UAC1=y / F_UAC2=y / F_HID=y / F_FS=y
```

Read on its own, that looks like evidence the SoC composes the external,
host-facing personality (display/MIDI/audio/`xPort`) as a Linux USB gadget.
**Checked live on the device (SSH, 2026-08-25) and this is wrong** —
worth recording the correction, not just deleting the wrong guess:

```
$ ls /sys/kernel/config/usb_gadget/
ls: cannot access '/sys/kernel/config/usb_gadget/': No such file or directory
$ ls /sys/class/udc/
ls: cannot access '/sys/class/udc/': No such file or directory
```

No gadget instance, no USB Device Controller registered at all — the
kernel merely has the *capability* compiled in as loadable modules; nothing
instantiates it on this unit. The `pci_ep` configfs group is mounted
instead (`mount | grep configfs` → `configfs on /sys/kernel/config`,
containing only `pci_ep/`, unrelated to USB gadget).

**What `lsusb -t`/`lsusb` show instead, from the SoC's own perspective
(it is host, not device, on this link):**

```
Bus 01.Port 1: Dev 4, Class=Hub, Driver=hub/4p         <- 0424:2534 (SMSC/Microchip hub)
    Port 4: Dev 5, 2982:1969 "Ableton Push 3"          <- the XMOS-driven device, 7 interfaces
```

`2982:1969` — the exact same VID:PID and 7-interface layout (vendor
display, 4-5 audio-class interfaces including MIDI streaming, vendor
`xPort`) that push-tethered-app's docs describe seeing from an *external*
host computer over the tether cable. **Leading hypothesis now:** there is
no SoC-generated gadget at all. The XMOS chip's own USB device is what
directly presents to whichever side is currently allowed to see it — the
SoC itself in standalone mode (as captured above), an external tethered
computer in controller mode — mediated by the hub (`0424:2534`) and
whatever the leftmost mode-switch button actually toggles at the hub/mux
level. Simpler than the gadget theory, and fits the "mutually exclusive,
not concurrent" behavior push-tethered-app's docs already established
without needing a separate explanation for it. Still not proven — would
need catching the hub's port assignments changing between standalone and
controller mode to confirm.

This also means the previous version of this section's claim — that
`xPort` might be a *relay* of the SoC-composed gadget's own interface 6 —
doesn't hold up either; simpler explanation now is `xPort` (host-facing
interface 6) *is* XMOS's own interface 6 directly ("Hardware control
(LEDs, battery?)" per the table above), not a relay of anything.

### Live↔Push3 IPC sockets (2026-08-25/26)

Three Unix-domain sockets under `/data`, all standalone-mode only — this is
local IPC between Push3's *onboard* Live and its *onboard* hardware-control
app, not reachable over a USB tether from an external computer:

```
/data/live-to-push-midi-ipc-channel
/data/push-to-live-midi-ipc-channel
/data/push-flip-api-ipc-channel
```

**Topology** (via `strace -p` on both endpoint processes as root — both
processes' `/proc/<pid>/fd` need root even though `ps` shows owner
`ableton`): `/opt/push3/Push3 --faceless` is the server for
`live-to-push-midi-ipc-channel`; `/opt/push3/Live` is the server for the
other two.

**These sockets do not carry MIDI.** `aconnect -l` shows Push3's own
`--faceless` app doing its real MIDI over the kernel ALSA sequencer —
`RtMidi Input/Output Client` wired straight to Push 3's own `Live Port`,
the exact same kernel MIDI port an externally-tethered host uses. Traced
`push-to-live-midi-ipc-channel` and `live-to-push-midi-ipc-channel` live
during real pad touches and playback: both stayed completely silent every
time.

**`push-flip-api-ipc-channel` is active, Live→Push3 only, framed but not
decoded.** Every message so far has the same envelope: a 9-byte header
(`01 00 00 00 00 00 00 <hi> <lo>`, big-endian 16-bit at the end) followed
by a second, variable-length payload in the same `sendmsg` call — and that
second field's length is exactly what the header's trailing 16-bit value
encodes (confirmed: header said `0x00A0` = 160, the payload was 160 bytes).
So the header is a **type + length prefix**, not a value of its own — an
earlier pass mistook the length field for audio-meter data and drew a
now-retracted L/R-stereo conclusion from it. The actual payload is
structured binary containing readable ASCII fragments (`sig `,
`{ablelive`, repeated within a single message) — looks like an internal
Ableton object/signature serialization, not raw sensor or meter data.
Traffic only appears during real activity (touches, transport running) and
stops the instant the transport stops; the payload itself is undecoded.

**Next step, if picked back up:** capture full raw messages to a file for
offline, tool-assisted parsing instead of reading truncated `strace`
output live over SSH — the payload is real structured data and deserves
that, but is a bigger reverse-engineering task than this session's ad hoc
probing was set up for.

**Unrelated quirk found along the way — not a push-hack bug.**
`/data/logs/Push3.log` has logged `MidiHub: Failed to push MIDI message to
outgoing queue` at a rock-steady **~800/minute (~13.3/sec)** ever since its
first occurrence, **2026-06-17T12:32:06**, with no drift — the "escalating"
read in an earlier version of this note was a misread of a cumulative
counter, not an actual increase. `Push Hack Automation`'s CC-sending toggle
(bound to Push's Play button) was a suspect, but its on/off state only
changed today from deliberate button presses; the error rate stayed flat
for weeks before that with nobody touching the device, ruling it out.
`MidiHub` is Live's own internal (proprietary) subsystem — no source
access, so this isn't something fixable from push-hack or
push-tethered-app. Left as a known device quirk, not investigated further.

### Mouse/keyboard, live test — confirmed working end-to-end (2026-08-25)

`modprobe usbhid` loads cleanly. A Keychron K2 keyboard plugged into the
USB-A port enumerates as 4 separate HID input devices (base keyboard,
consumer control, system control, an extra keyboard interface — standard
for a composite USB keyboard), on hub port `1-1.2` — the same internal hub
(`0424:2534`) XMOS sits on at port `1-1.4` (see the USB gadget section
below). `dmesg` shows a clean `usbkbd`/`hid-generic` probe, `hidraw0`/
`hidraw1` created.

**Nothing else was holding the device** — `fuser` on every `/dev/input/eventN`
it created came back empty, meaning Xorg (confirmed running at boot) is
not consuming it, at least not by default. Raw keystrokes are delivered
correctly: reading `/dev/input/event4` while typing captured real
`input_event` structs, decoded (`EV_KEY`, codes 30/31/32 = KEY_A/S/D,
press then release) matching keys actually typed.

**One real constraint found:** the `ableton` account (the normal,
non-root SSH account) is **not** in the `input` group
(`groups ableton` → `users disk audio messagebus realtime`, no `input`),
and `/dev/input/eventN` is `root:input 0660` — so reading it requires
either running as `root`, or a one-time provisioning step (add `ableton`
to the `input` group, or a udev rule relaxing the permission) before a
hack running as the normal user can read keyboard/mouse input itself.

**Conclusion: the kernel/driver path is fully proven.** A standalone hack
*can* read a real mouse/keyboard plugged into the USB-A port today, once
`usbhid` is loaded and the permission issue above is provisioned for. See
below, though — the more direct, no-code-needed path turned out to be
even more useful.

### Keyboard shortcuts control the onboard Live, reflected on Push's own screen — no hack needed (2026-08-25)

The single most useful finding of this whole session. Federico typed real
Ableton Live keyboard shortcuts (`Ctrl+N` = New Live Set, `Ctrl+T` = new
audio track, `Ctrl+Shift+T` = new MIDI track) on the keyboard plugged into
the USB-A port, **and watched them execute live, visible on Push 3's own
physical screen** — new set created, new tracks appearing.

**Mechanism, traced end to end:**

1. `usbhid` enumerates the keyboard (see above).
2. There's a delay before anything claims it — the first `fuser` check
   (run right after plugging in) showed nothing holding
   `/dev/input/eventN`. A later check, after Federico's shortcut test,
   found **`Xorg` (PID 625) now holding `event4`-`event7` open** — udev
   hotplug detection with some startup lag, not instant.
3. `/opt/push3/Live` (PID 889, the full bundled Ableton Live, already
   confirmed running earlier this session — standalone mode) is a normal
   X11 client on display `:0`. Once Xorg owns the keyboard, Live receives
   ordinary X key events and runs its own native shortcuts exactly as it
   would on a desktop — nothing Push-specific about this part at all.
4. Live's resulting session-state change (new set, new tracks) reaches
   **Push3's own onboard app** (`--faceless`, direct DRM/KMS,
   confirmed earlier to never read X11/evdev itself — that part still
   holds) via IPC, not via any screen-sharing/mirroring — almost
   certainly through `push-flip-api-ipc-channel` (see the IPC sockets
   section above), which is exactly the kind of state-sync channel this
   finding implies must exist. Push3's app then redraws its own native
   Push-style UI to reflect the new state, using its own DRM/KMS
   framebuffer — Federico is seeing Push's own rendering of Live's state,
   not a pixel mirror of Live's X11 window.

**Practical consequence — bigger than the "read raw evdev, draw our own
UI" plan this section originally scoped:** a keyboard plugged into Push 3
standalone can *already*, today, with zero new code, drive real,
visible-on-Push's-own-screen actions, just by using Live's existing
keyboard shortcut set. No need to reverse-engineer the display protocol
or write a custom input-reading hack to get *this* class of interaction —
it's Live's own shortcut surface, for free. Untested but very likely true
by the same mechanism: text entry (e.g. typing to rename a track) and
mouse input, since both are ordinary X11 input Live already handles like
any desktop app.

**Still true, unchanged:** Push3's own app doesn't read keyboard/mouse
*directly* — everything above flows through Live. A hack wanting keyboard
input Live doesn't already expose a shortcut for still needs the
`/dev/input/eventN` route from the previous section.

### Intercepting XMOS traffic — LD_PRELOAD hook (`hacks/push-display`)

**Status: IMPLEMENTED and deployed.**

Push3 links against `/opt/push3/lib/libusb-1.0.so`. `LD_PRELOAD` resolves symbols before rpath, so a hook library in `/data/push-hack/` can intercept all `libusb_bulk_transfer` / `libusb_submit_transfer` calls.

**Injection point:** `/etc/init.d/push3` has `export LD_PRELOAD=/data/push-hack/hacks/push-display/push_hook.so` added before the `start-stop-daemon` line. Environment propagates through Python launcher → Push3.

**Deployed files:**
- `hacks/push-display/src/push_hook.c` — the hook (C, cross-compiled linux/amd64 via Docker gcc:12-bullseye)
- `hacks/push-display/Makefile` — Docker-based build: `docker run --rm --platform linux/amd64 gcc:12-bullseye`
- `hacks/push-display/hack.json` — `"binary": ""` (no Go binary; service.initd patches init.d/push3 and deploys the .so)
- Deployed to: `/data/push-hack/hacks/push-display/push_hook.so`
- Log: `/data/push-hack/logs/push-hook.log`

**Shared memory control block** (`framebuf`):
- File: `/data/push-hack/hacks/push-display/framebuf` (655,376 bytes, world-writable `0666`)
- Created by the hook constructor on first load; push-manager mmaps it R/W for control
- Layout (must stay in sync between `push_hook.c` and `display.go`):

```c
typedef struct __attribute__((packed)) {
    uint32_t magic;               // 0x50555348 ("PUSH")
    uint32_t version;             // 1
    uint32_t mode;                // 0=passthrough, 1=bar overlay, 2=full takeover
    uint32_t frame_seq;           // incremented by push-manager on each frame write
    uint8_t  pixels[655360];      // raw BGR565, no XOR — hook applies XOR on copy
} push_display_shm_t;             // total: 655376 bytes
```

**Permissions note:** `start-stop-daemon` fires the constructor as root before exec-ing as `ableton` (uid=1000). Root creates `framebuf` as `root:root 644`; ableton can't mmap R/W. Fix: `fchmod(fd, 0666)` after ftruncate in `shm_init()`.

**Display modes:**
- `0` = passthrough — hook does nothing, Ableton Live UI shows normally
- `1` = bar overlay — paints orange `#FF5500` bar (20px high) at top of every frame
- `2` = full takeover — replaces entire frame with `shm->pixels[]` content; push-manager writes images here

**What a hook could also enable (not yet implemented):**
- Write MIDI OUT to `ep_03` → LED control (custom pad colors)
- Probe `ep_84` → battery, other hardware state

**Note:** Reading MIDI IN (`ep_83`) from the libusb hook does NOT work — the ALSA kernel MIDI driver owns that endpoint. MIDI interception must go through the ALSA sequencer (see MIDI section above). `hacks/push-manager` implements this via direct ALSA seq ioctls in `src/midi.go`.

#### MIDI event neutralization via `snd_seq_event_input` hook

`push_hook.so` also intercepts `snd_seq_event_input` from `libasound.so`. This function is called by RtMidi inside Push3's process each time a sequencer event arrives. When `midiflt->enabled`, the hook overwrites the event's `type` byte with `0` (`SND_SEQ_EVENT_NONE`) before returning to the caller — Live sees no input. The event is not dropped; overwriting `type` instead avoids blocking or timing disruption to Push3's MIDI processing loop. SysEx (`type 0x82`) and Active Sensing (`type 0x28`) always pass through unchanged.

Our ALSA sequencer subscription in `src/midi.go` is a separate subscriber at the kernel level — it receives events directly from client 16 before they reach any application, so all events appear in the MIDI monitor regardless of filter state.

**`midiflt` shared memory file:** `/data/push-hack/hacks/push-display/midiflt`

Created by push_hook.so constructor alongside `framebuf`. 16 bytes, permissions `0666`:

```c
typedef struct __attribute__((packed)) {
    uint32_t magic;    // 0x4D464C54 ("MFLT")
    uint8_t  enabled;  // 0=passthrough, 1=intercept active
    uint8_t  reserved[11];
} push_midiflt_shm_t;
```

`push-manager` mmaps this file R/W and exposes `POST /api/midi/filter` and `GET /api/midi/filter/status`. Intercept state persists in the file across push-manager restarts; it is only reset when push_hook.so is reloaded (Push3 restart).

**Reference implementation:** [schwung-spi](https://github.com/charlesvestal/schwung-spi) does this for Ableton Move (SPI instead of USB) — same shadow-buffer + callback pattern.

**Risk:** Requires Push3 restart. Hook bugs could crash Push3 / Live. Keep hook minimal and always call-through to real libusb.

**`usbmon` not available** — kernel module not present in `5.15.48-intel-pk-preempt-rt`. Cannot sniff traffic non-destructively.

---

## Notes for hack authors

- **Install to `/data/push-hack/`** — writable by `ableton`, survives firmware updates that only touch `/opt`
- **Never write to `/opt`** — read-only, would require remount and risks bricking on OTA update
- **Nice 19 + memory cap** — hacks must not compete with Live's real-time audio engine
- **Port range for hacks**: 7701+ (stock Push only uses 22 and local 53)
- **Log to `/data/push-hack/logs/`** — follows Ableton's own convention (`/data/logs/`)
- **Python 3.10 available** for lightweight scripting hacks that don't need a compiled binary
- **D-Bus session** runs as `ableton` user — potential IPC avenue for future hacks that interact with Live
- **`/data/settings/Options.txt`** — Live options file, same format as desktop Live's `Options.txt`; may be useful for future hacks
