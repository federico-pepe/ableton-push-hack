# Plan: Link Audio instead of the Loopback card

Status: in progress. The spike works. Steps 1 and 3 are built on the branch `link-audio` of push-hack-audio-loopback (not committed).
The setting is on the web page of the service, as two buttons: "Virtual Audio Interface" and "Link". Step 2 (the change in `core/`) was not needed.
The first tests on a Push 3 (2026-11-07) pass. See "Results on the Push".

## Goal

Audio hacks (Braids, MachineModule, MonoModule) must send audio to Live without the Loopback card.
Live must keep the sound card of the Push (`A3`) as its output at all times.

Today a hack writes to the Loopback card, and Live reads it as an input.
A user who wants to hear a hack with processing in Live keeps the output on `A3`, so this works.
But the card needs a kernel module, a clock setting (`timer_source=A3.0.0`) and a mixer with 32 channels.
Link Audio can carry the same audio with one named channel for each hack.

An earlier idea was a bridge that plays the audio of Live on `A3` from a hack. Live now keeps `A3`, so the bridge is not needed and its plan was removed.

## Spike results (2026-11-05)

We built a small Link peer named "PushHack". It ran on a Push 3 with Live. The source code is not in this repository.

| Test | Result |
|---|---|
| Peer finds Live on the same Push | Works, over `wlan0`, with no setup |
| Receive from Live | 6 stereo channels, 44100 Hz, about 345 buffers per second |
| Gaps in the received buffers | 0, over more than 60000 buffers per channel |
| Longest wait between two buffers | 7.3 to 8.1 ms (the nominal value is 2.9 ms) |
| Tone sent to Live (sink "PushHack Tone") | Clean, with the Link Audio latency of Live at 30 ms and at 20 ms |
| Tempo | The peer takes the tempo of Live. It never sets it. |
| Echo of a Live channel, with the timestamps of Live | Silent, at every offset from +20 to -100 ms |
| Echo, stamped with the current time on every buffer | Audible but glitchy |
| Echo, stamped with a sample counter | Clean piano, at a Link Audio latency of 20 ms |

The last row is the important one. A Link sink must stamp its buffers with a sample counter, not with the current time.
The counter starts at "now plus 40 ms". It restarts when it drifts more than 10 ms from that target.
In the spike this restart happened about once every 3 seconds. No click was audible.

Build facts:
- The peer builds with `g++ -std=c++17` in the Docker image `gcc:12-bullseye`, with `-static-libstdc++ -static-libgcc`.
- The Push has no `/lib64`. A binary from this image looks for `/lib64/ld-linux-x86-64.so.2` and fails with "No such file".
  The loader is `/lib/ld-linux-x86-64.so.2`. The service binary must use this path (linker option `--dynamic-linker`).
  Step 1 checks how the service builds today.
- `scp` to the Push needs the `-O` option.
- `ps` and `pgrep` on the Push do not show a process that runs through the loader by its own name. Use `pidof ld-linux-x86-64.so.2`.

## Results on the Push (2026-11-07)

The service ran in Link mode with MachineModule and MonoModule. Live took the input "PushHack" for each hack.

| Test | Result |
|---|---|
| Build for the Push | Works. The Docker image has glibc 2.36 and the Push has 2.35. A static libstdc++ asked for `arc4random` (glibc 2.36), and the loader refused the binary. The shim now has its own `arc4random`. `make build` fails if a newer glibc symbol appears. |
| MachineModule alone | Clean. The user heard no glitch. |
| MachineModule and MonoModule together | Clean. |
| Braids | Installed on the Push, shows as a new Link channel, works. |
| Restart of Live | Works. Link is off by default in Live after a restart, so the user turns it on again. |
| Wi-Fi off on the Push | Link still works. |
| Sample rate of Live at 48000, 88200 and 96000 Hz | Kept working. The hacks send 44100 Hz. Pitch was not checked. |
| Stop and start of a hack | The channel closes, disappears in Live, and comes back on the same track when the hack starts. |
| CPU of the service | About 3% of one core (4 cores), with two hacks playing. |
| Restart of the service | The tracks in Live got audio again with no action by the user. The hacks reconnected in less than 1 second. |
| Restarts of the stamp counter | 3 for each channel in 21 minutes. The spike had about 1 for every 3 seconds. |
| Long run | 21 minutes with two hacks (490887 blocks, 0 write errors). The user stopped the test early and judged it solid. The planned 30 minutes were not done. |
| Time to process one block | 83 to 167 µs. A block lasts 2902 µs. |

The `step 2` change in `core/` was not needed. `Server.Mix` with 16 channels gives each hack its own pair, and the service reads the pair of each client.

## Known issue: hacks drift after a restart of the service

After a restart of the service, both hacks were out of time with each other and with the metronome of Live. Pressing play twice in Live fixed it.

We think the cause is in the hacks, not in Link. In `renderBus` of MachineModule, the loop ends when the bus connection drops.
The sound engine then stops. Its sequencer moves only while `Render` runs, but the MIDI clock of Live goes on. Each hack stops at a different time.
We did not test this in card mode. A restart of the service in card mode should show the same drift.

A possible fix, not done yet: keep calling `Render` while the bus is not connected. `Write` already drops the audio and sleeps one block then.
The change is small, in `src/bus.go` of MachineModule and MonoModule. It is a separate change in those two repositories.

A restart of the service happens only when the user changes the mode or updates the hack. The workaround is to press play in Live.

## Where the code lives

| Part | Place | Reason |
|---|---|---|
| C++ shim and Go wrapper for the Link peer | push-audio-loopback | The hack already builds with cgo in Docker. `core/` has no cgo. |
| Link output mode, sinks, stamping, pacing | push-audio-loopback | This service already owns the mixer and the status page. |
| A method that gives the service each client block without mixing | `core/audiobus` | Pure Go. The exact shape is set in Step 2. |
| MonoModule, MachineModule, Braids | No change | They use only the bus client (`Open`, `Format`, `Generation`, `Write`). |

We do not put Link in `core/`. Link is C++ and needs cgo. Its code is GPL v2 or later.
`core/` is MIT and many hacks use it. A GPL dependency in `core/` would reach all of them.

## Design

The service gets a setting with two values: **Loopback card** and **Link Audio**. The default is **Link Audio** (decided 2026-11-07, after the tests on the Push passed).

In Link mode:
1. The service joins Link as one peer, named "PushHack". It enables Link Audio.
2. A client connects to the bus socket, as it does today. The service makes one sink for it. The name of the sink is the name of the client.
3. The format is fixed: stereo, 44100 Hz, period 128. The client reads it when `Generation` changes, as it does today.
4. A timer in the service runs once for each period. It reads one period from the ring of each client and commits it to the sink of that client.
5. The ring still blocks a client that writes too fast. The timer of the service takes the place of the card clock.
6. When a client disconnects, the service removes its sink.
7. The service never sets the tempo or the transport.

The mixer for the card is not used in Link mode. Each hack has its own channel, so a pair number has no meaning.
The hacks hide the pair control when the format says Link mode. This is an optional change in the UI of the hacks.

When the user changes the setting, the service drops all clients. They reconnect, as they do after a format change in Live.

## Steps

1. **Shim.** Build the C++ shim and the Go wrapper. Make a test binary that sends two named sinks with different tones.
   Check how the service links today and use `/lib/ld-linux-x86-64.so.2`. Run the binary on the Push.
   Gate: Live plays both tones, each on its own channel.
2. **Core.** Add the pure-Go method for a block of each client. Add a test. Tag a new `core/vX.Y.Z` and bump the pin in the service.
3. **Service.** Add the Link output mode behind the setting. Show the mode, the sinks, the restarts of the stamp and the underruns on the status page.
4. **Hacks.** Run MonoModule alone, then MachineModule alone, then both together, then Braids. Make no change to them.
5. **Tests.** See the next section.
6. **Release.** Release push-audio-loopback as a new alpha version, with "Link Audio" as the default. The input matrix (see `plans/2026-11-07-link-inputs-matrix.md`) is built first.
7. **Documents.** See the last section.

## Tests and pass criteria

| Test | Pass |
|---|---|
| Two hacks together for 30 minutes | No audible click. No growth of the restarts of the stamp. |
| Link Audio latency of Live at 30, 20 and 10 ms | The lowest value that stays clean is written down. |
| Restart of the service | The tracks in Live get audio again with no action by the user. If the user must pick the inputs again, Link Audio does not become the default. |
| Restart of Live | The hacks send audio again with no action by the user. |
| Hack stops and starts | Only its channel appears and disappears. |
| Use with no network | Written down. The Push has only `wlan0` and `lo`. We need a way into the Push that does not use Wi-Fi to run this test. |
| CPU of the service | Less than the mixer in card mode (6.7% in one `top` sample), or a reason is written down. |
| Total delay of a played note | Measured and written down. |
| Sample rate | A hack that renders at another rate is heard at the right pitch. |

The tracks in Live must also work with the Push in a live session, so the offline test decides the default.

## Risks and unknowns

- **Inputs after a restart.** In the spike, Live seemed to drop the input after each restart of the peer. We did not test if Live reconnects by itself.
  The header of Link says that channel IDs persist. This is the largest risk for the user.
- **No network.** Link needs a network interface. The Loopback card does not. A Push on a stage may have no Wi-Fi.
- **Restarts of the stamp.** About one for every 3 seconds in the spike. Step 3 must make them rarer or prove that they are inaudible.
- **Format.** Link carries 16-bit audio. The bus uses `float32` and one conversion at the end. Quantization is a change to document.
- **Network side effect.** The peer joins the Link session of every other device on the network. It takes their tempo. The user can see this in other devices.
- **License.** The service binary becomes GPL v2 or later when it links Link. We must decide the license of the service, or ask `link-devs@ableton.com` for a proprietary license.
- **Live version.** Link Audio on the Push needs a Live version that supports it. The Push of the user supports it. Other users need a check.
- **Link source.** We use the SDK from `github.com/Ableton/link`. We did not read its FAQ (HTTP 403) or its release notes (HTTP 504). We pin a tag, not `master`.

## Out of scope

- Removing the Loopback card. The card stays as the second mode until the tests pass. A later plan decides when to remove it.
- Reading the physical inputs. A hack can receive any channel that Live sends over Link, and the spike proved this. The API for it is a later plan.
- Changes to MonoModule, MachineModule or Braids, except the optional pair control.
- Any change to `/boot/`, `/opt/` or `/etc/`.

## Documents to update when the code lands

- `docs/audio-bus.md`: the Link mode, the stamping rule and the fixed format.
- `docs/push3-internals.md`: the audio section (Live keeps `A3`, hacks use Link).
- `docs/push3-dsp-hosting.md`: the path from a hack to Live.
- README and `hack.json` of push-audio-loopback.
- `CHANGELOG.md`, under `## [Unreleased]`.
