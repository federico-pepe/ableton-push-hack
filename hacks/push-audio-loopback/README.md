# push-audio-loopback

Gives Live on Push 3 a selectable virtual audio device ("Push Hack
Virtual Audio") backed by the standard ALSA Loopback driver
(`snd-aloop`), so a separate process can feed audio into Live (or read
audio out of it) without touching the real, exclusively-locked XMOS
hardware PCM device.

Background and the full investigation (why Push 3's real audio device
can't just be opened by a second process, why Link Audio and an
LD_PRELOAD hook into `Live` were both tried and how this ended up being
the working path) live in `push-tethered-app`'s
`plans/2026-08-27-schwung-on-ableton-push-fluffy-feather.md`.

## What's here

- `aloop-rename.patch` — a small patch against the stock
  `sound/drivers/aloop.c` (from Ableton's GPL source release for Push 3
  firmware v2.4.2) that renames the card from generic "Loopback" to
  "Push Hack Virtual Audio", so it's identifiable in Live's device
  pickers. Functionally identical to stock `snd-aloop` otherwise.
- `src/main.go` — the installable service (`push-audio-loopback`). Loads
  the kernel module on boot and keeps checking it stays loaded. See
  "Persistent install" below.
- `src/loopback_feed/loopback_feed.c` / `Makefile` — a small low-jitter test-tone
  writer for the resulting Loopback device. A generic tool like
  `speaker-test` works but produces audible glitches: it shares the
  Loopback ring buffer with Live's own real-time audio engine with no
  common clock, and Live's side runs very tight periods (128 frames).
  `loopback_feed` matches Live's negotiated `hw_params` (channels/rate),
  uses blocking `snd_pcm_writei` so ALSA's own backpressure paces the
  writes, and takes `SCHED_FIFO` priority (root only) to avoid being
  preempted for long stretches.

## Building the kernel module

Needs the GPL source Ableton published for Push 3 firmware v2.4.2 —
already used elsewhere in this repo at
`resources/push-assets/push3-242-gpl-sources.tgz` (gitignored; obtain
your own copy via Ableton's GPL source request process if you don't
have it).

```bash
GPL_TGZ=resources/push-assets/push3-242-gpl-sources.tgz
KVER=linux-push3-5.15.48+gitAUTOINC+4ec79de9ce-r0   # match your device's `uname -r`

mkdir -p /tmp/push3-ksrc && cd /tmp/push3-ksrc
tar -xzf "$OLDPWD/$GPL_TGZ" \
  "sources/x86_64-oe-linux/$KVER/kernel.config" \
  "sources/x86_64-oe-linux/$KVER/$KVER-patched.tar.xz"
cd "sources/x86_64-oe-linux/$KVER"
mkdir ksrc && tar -xJf "$KVER-patched.tar.xz" -C ksrc
cp kernel.config ksrc/kernel-source/.config

# Apply the rename patch
patch -p1 -d ksrc/kernel-source < /path/to/hacks/push-audio-loopback/aloop-rename.patch

# Build in a matching environment (native x86_64, CONFIG_MODVERSIONS is
# off on this kernel so an exact toolchain match isn't required — only
# the .config/version string, checked below, needs to match)
docker run --rm --platform linux/amd64 -v "$PWD/ksrc/kernel-source":/work -w /work debian:bullseye \
  sh -c "apt-get update -qq && apt-get install -qq -y build-essential bc bison flex libssl-dev libelf-dev kmod rsync >/dev/null && \
         make olddefconfig ARCH=x86_64 && \
         make modules_prepare ARCH=x86_64 && \
         make M=sound/drivers ARCH=x86_64 modules"

# Sanity check before deploying: vermagic must match the target device's
# `uname -r` line exactly, or insmod will reject it (or need --force,
# which you should not do without understanding why it mismatched).
strings ksrc/kernel-source/sound/drivers/snd-aloop.ko | grep ^vermagic=
# compare against, e.g.:
ssh root@push.local 'strings /lib/modules/$(uname -r)/kernel/sound/usb/snd-usb-audio.ko | grep ^vermagic='
```

## Deploying

```bash
scp ksrc/kernel-source/sound/drivers/snd-aloop.ko root@push.local:/tmp/
ssh root@push.local '
  rmmod snd_aloop 2>/dev/null   # if a previous (e.g. stock-named) load is present
  insmod /tmp/snd-aloop.ko id=PHVAudio timer_source=A3.0.0
  cat /proc/asound/cards
'
```

`id=PHVAudio` sets the short bracketed card ID (`[PHVAudio]` in
`/proc/asound/cards`); it has to be a plain alnum token, unlike the
patch's freeform `shortname`/`longname` strings. `rmmod`/reboot fully
reverts a manual `insmod` like the one above. For a load that survives
reboot, see "Persistent install" below.

`timer_source=A3.0.0` points Loopback at Push 3's own USB audio card
(`hw:0`, ID `A3`, PCM device 0, subdevice 0) instead of its own default
jiffies clock. Use it every time you load this module. Without it, audio
through this card glitches at Live's default 128-frame buffer — see
`docs/push3-dsp-hosting.md` for why.

Select "Push Hack Virtual Audio" as an input or output device on a
track in Live's own audio preferences (visible directly on Push3's
screen) to actually route audio through it. The two ALSA sub-devices it
creates (`hw:PHVAudio,0` and `hw:PHVAudio,1`) are cross-wired: audio
written to one device's playback side arrives on the *other* device's
capture side (confirmed empirically, not just from driver reading) —
e.g. if Live is capturing from device 0, feed test audio into
`hw:PHVAudio,1,0`, matching Live's exact negotiated `channels`/`rate`
(check `/proc/asound/card1/pcm0c/sub0/hw_params` for what Live actually
opened it with) or the paired open will fail with `EINVAL`.

## Building `loopback_feed`

```bash
cd hacks/push-audio-loopback
make        # cross-builds via Docker debian:bullseye, needs libasound2-dev
```

```bash
scp loopback_feed root@push.local:/tmp/
ssh root@push.local '/tmp/loopback_feed hw:PHVAudio,1,0 32 44100 440 10'
#                                        ^device          ^ch ^rate ^Hz ^secs (0=infinite)
```

## Persistent install

`push-audio-loopback` (a Go binary, built by `make` alongside
`loopback_feed`) is this hack's real installable service. It loads the
module for you on every boot and checks it stays loaded. It does not
need any manual `insmod` step once installed.

It needs one `.ko` file per Push kernel version, placed at
`ko/<uname -r>/snd-aloop.ko` next to the binary (e.g.
`/data/push-hack/hacks/push-audio-loopback/ko/linux-push3-5.15.48+gitAUTOINC+4ec79de9ce-r0/snd-aloop.ko`).
Build the `.ko` with the recipe above, then copy it there:

```bash
ssh root@push.local 'mkdir -p /data/push-hack/hacks/push-audio-loopback/ko/$(uname -r)'
scp ksrc/kernel-source/sound/drivers/snd-aloop.ko \
  root@push.local:/data/push-hack/hacks/push-audio-loopback/ko/$(ssh root@push.local uname -r)/
```

Deploy the service itself with the framework installer:

```bash
./scripts/install.sh --hack push-audio-loopback --build
```

If the running kernel has no matching `.ko` file, or the `.ko`'s
`vermagic` does not match the running kernel, the service logs one clear
line and does not load anything — it will not force a mismatched module.
This happens after a Push firmware update changes the kernel; build and
copy a new `.ko` for the new kernel to fix it.
