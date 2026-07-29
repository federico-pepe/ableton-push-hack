# Push Display Hook (`push-display`)

`LD_PRELOAD` hook (C shared library, `push_hook.so`) injected into the **Push3 process only** (the
constructor checks `/proc/self/comm == Push3`). It gives push-manager control of the Push 3 display and can
neutralize MIDI into Live. Not a daemon — it lives inside Push3.

> **⚠️ Uninstall before an Ableton OS update.** Push3 flashes co-processor firmware over the same
> `libusb_bulk_transfer` path this hook interposes; an active hook **freezes the device mid-update**. There
> is no in-process kill-switch (an `LD_PRELOAD` interposition can't be removed from a running process). Run
> `./scripts/uninstall.sh` before updating, reinstall after. See the root `README.md` and
> `../../docs/push3-internals.md`.

## What it does

- **Display** — intercepts `libusb_bulk_transfer` to overlay or fully replace Push's display frames.
- **MIDI neutralization** — intercepts `snd_seq_event_input` from `libasound`; when `midiflt->enabled`,
  overwrites each event's `type` with `0` (`SND_SEQ_EVENT_NONE`) so Live sees no input. SysEx (`0x82`) and
  Active Sensing (`0x28`) always pass through.
- **8s boot grace** before activating; passes all calls through to the real libusb until then.

## Injection

`/etc/init.d/push3` gets `export LD_PRELOAD=/data/push-hack/hacks/push-display/push_hook.so` added before
its `start-stop-daemon` line (managed by install/uninstall). The env propagates Python launcher → Push3.

## Shared memory (control surface for push-manager)

Two world-writable (`0666`) files, created by the hook constructor on first load. push-manager mmaps them
R/W. **Layouts must stay in sync between `src/push_hook.c` and `../push-manager/src/display.go`.**

`framebuf` — `/data/push-hack/hacks/push-display/framebuf` (655,376 bytes):

```c
typedef struct __attribute__((packed)) {
    uint32_t magic;          // 0x50555348 ("PUSH")
    uint32_t version;        // 1
    uint32_t mode;           // 0=passthrough, 1=bar overlay, 2=full takeover
    uint32_t frame_seq;      // incremented by push-manager on each frame write
    uint8_t  pixels[655360]; // raw BGR565 (960×160, stride 1024); hook applies XOR on copy
} push_display_shm_t;        // total 655376 bytes
```

`midiflt` — `/data/push-hack/hacks/push-display/midiflt` (16 bytes):

```c
typedef struct __attribute__((packed)) {
    uint32_t magic;      // 0x4D464C54 ("MFLT")
    uint8_t  enabled;    // 0=passthrough, 1=intercept active
    uint8_t  reserved[11];
} push_midiflt_shm_t;
```

Display geometry, protocol, and the XOR shaping are documented in
[`../../docs/push3-internals.md`](../../docs/push3-internals.md).

## Build

Cross-compiles `push_hook.so` for linux/amd64 via Docker (no host toolchain needed):

```bash
cd hacks/push-display && make            # builds push_hook.so
make splash                              # regenerate src/splash_data.h from gen_splash/
```

## Deploy

```bash
./scripts/install.sh --hack push-display --build   # build + deploy via framework
hacks/push-display/deploy.sh                        # standalone re-deploy
```

`push_hook.so` is copied as `root`; the `service.initd` patches the `LD_PRELOAD` line into
`/etc/init.d/push3`. **Requires a Push3 restart** to take effect (hook loads when Push3 next starts).

Log: `/data/push-hack/logs/push-hook.log`.

## Risk

Hook bugs can crash Push3 / Live. Keep it minimal and always call through to the real libusb. Reference
implementation for the shadow-buffer + callback pattern:
[schwung-spi](https://github.com/charlesvestal/schwung-spi) (Ableton Move, SPI).
