# Audio bus: many hacks, one loopback card

Status: released, working on a Push 3. Link Audio mode and inputs: see the sections below. The server and client are in `core/audiobus`
(`core/v0.5.3` and later). The mixer runs inside push-audio-loopback.
Braids, MachineModule and MonoModule are clients.

## The problem

The "Push Hack Virtual Audio" card is an ALSA loopback. Live reads from device 0, sub-device 0.
A hack writes to device 1, sub-device 0 (`hw:PHVAudio,1,0`). ALSA lets only one process write to a stream.

Two audio hacks cannot run together. The second hack gets this error: `snd_pcm_open(hw:PHVAudio,1,0): Device or resource busy`.
Today you must stop Braids to use MachineModule.

The other sub-devices of the card do not help. Live reads only sub-device 0, so audio on another sub-device never reaches Live.

## The idea

One process owns the write side of the card: the mixer. It lives in push-audio-loopback, the hack that already owns the card.
Each audio hack becomes a client of the mixer. A client sends its audio to the mixer. It does not open ALSA.

Each period, the mixer adds the audio of all clients and writes one block to the card.
The mixer knows no hack by name. It mixes whoever is connected. A user can install any set of audio hacks.

## Parts

| Part | Where | Job |
|---|---|---|
| `Server` | `core/audiobus` | Accepts clients and mixes them. `Mix` has no lock and no allocation. |
| `Client` | `core/audiobus` | A hack calls `Open`, then `Write` for each block. It reconnects by itself. |
| `HWParams` | `core/alsapcm` | Reads the format that Live negotiated. It replaces the copies in Braids and MachineModule. |
| mixer | push-audio-loopback | Opens the card, sets the format, calls `Mix` and writes the result. It also serves a status page and `/api/status`. |
| `abustone` | `core/audiobus/cmd` | A test client that plays a sine wave. |
| `abusrec` | `core/audiobus/cmd` | A test client that records an input to a WAV file. |

## Data path

A client writes stereo `float32` frames into a ring buffer. The ring is a file in `/dev/shm/push-hack-audio`.
The client and the mixer both map this file into memory. The client is the only writer. The mixer is the only reader.

The ring holds two periods, rounded up to a power of two. For 256 frames, the ring holds 512 frames.
A client waits when the ring is full. This wait paces the render loop of the hack to the card.

The latency is at most the size of the ring: 512 frames, about 12 ms at 44.1 kHz.

The mixer reads one period from each ring and adds the frames to the channel pair of the client. The mixer uses `float32`.
It clips once, when it converts the sum to 16-bit for the card. Two loud clients do not clip each other before the sum.

If a ring has too few frames, the mixer plays silence for the missing part and counts an underrun.
The mixer never waits for a client.

## Control path

A client connects to the Unix socket `/dev/shm/push-hack-audio/bus.sock` and sends one JSON line:

```json
{"v":1,"name":"MachineModule","pair":-1}
```

`pair` is the first channel of the stereo pair, counted from 0. The value -1 means "my saved pair, or a free one".

The mixer makes the ring and answers with one JSON line:

```json
{"ok":true,"ring":"/dev/shm/push-hack-audio/MachineModule-1.ring","pair":2,"rate":44100,"period":256,"channels":32}
```

The connection stays open. Nothing more is sent. When the connection closes, the mixer removes the client.
When the mixer closes the connection, the client knows that the mixer is gone.

## Channel pairs

The mixer chooses the pair for each client, in this order:

1. The pair that the client asked for, if the card has it.
2. The pair that the mixer saved for this name.
3. The first pair that no client uses.

The mixer saves the pair of each name in a file. A hack keeps its pair after a restart.
Two clients can use the same pair. The mixer then adds them. This is the choice of the user.

A hack shows the pair in its own web page, as MachineModule does. The hack sends the choice to the mixer with `SetPair`.
In Live, the user enables the pair on the input of the card.

## Link Audio mode

Since 2026-11-07 the service can send the audio of the hacks to Live over Ableton Link Audio instead of the loopback card.
Link is the default. The web page of push-audio-loopback has a switch with two values: **Virtual Audio Interface** and **Link**.
The choice is saved in `output-mode.json` next to `hack.json`.

In Link mode:
- The service joins Link as the peer `PushHack`. Each hack that connects to the bus gets its own Link channel, named after the hack.
- The hacks do not change. They use the same bus and the same `Open`, `Format` and `Write`. The format is fixed: stereo, 44100 Hz, 128 frames, 16 channels (8 pairs).
- The service reads the pair of each client (`Server.Mix`) and writes it to the sink of that client. Nothing is mixed across hacks.
- Live keeps the sound card of the Push as its output. In Live, turn on Link and Link Audio. Give an audio track the input type `PushHack` and pick the channel of the hack.
- The page shows `Link: N peers`, as Live does.

Rules for the stamps. Live ignores buffers that carry its own timestamps. Stamps taken from the clock for each buffer give glitches.
The service stamps every buffer with a sample counter. The counter starts at "now plus 40 ms" and restarts only when it drifts more than 10 ms.
Never set the Link tempo or the transport.

Results on a Push 3 (2026-11-07): MachineModule, MonoModule and Braids play clean, alone and together. A restart of the service brings the audio back with no action in Live.
Link keeps working with Wi-Fi off, and with Live at 48000, 88200 and 96000 Hz. The service uses about 3% of one core with two hacks.
The worst block time is 83 to 211 µs of 2902 µs. The stamp counter restarts about once every 7 minutes.

Known issue: after a restart of the service, the hacks can drift against the clock of Live. The engine of a hack stops while the bus is not connected, but the MIDI clock goes on.
Press play in Live. A fix in the hacks (keep rendering while disconnected) is not done yet.

The design and the tests are in `plans/2026-11-05-link-audio-for-hacks.md`.

## Inputs: the other direction

A hack can also receive audio. It declares inputs when it connects, and the service writes audio into one ring for each input.
The source of an input is a Link channel: a track of Live, the Main output, or the channel of another hack. The user connects them in a matrix on the web page.

Client side (`core/audiobus`, `core/v0.5.5` and later):

```go
c := audiobus.OpenWithInputs(sock, "MyHack", -1, []string{"Sample in"})
// ...once connected:
n, err := c.Read(0, buf) // buf: one period of interleaved L,R float32
```

- `Inputs()` lists the inputs that the server gave. With an older server it is empty, and `Read` returns `ErrNoInput`.
- `Read` never blocks. It fills `buf` with the audio that waits and the rest with silence. It returns the number of real frames.
- Call `Read` once for each period. The service writes one period for each tick.

Wire format. The first line gets an optional field, `"inputs":["Sample in"]` (at most 8 names, each different, each at most 64 bytes).
The reply gets `"in_rings":[...]`, one path for each input in the same order. An old client sends no inputs. An old server sends no `in_rings`.
An input ring holds 8 periods. The service writes it (`Input.Write`), the hack reads it.

Service side. The service subscribes to the channel (`LinkAudioSource`), and the rule in `jitter.go` makes one steady stream:
play starts when the ring holds 4 periods, audio above 6 periods is dropped, and an empty ring gives silence and a new fill.
The source accepts only 44100 Hz. Another rate shows "rate not supported" on the page.
Routes are saved in `link-routes.json`. A route comes back when its hack or its channel comes back. There is no route by default.

Result (2026-11-07): a 131 Hz tone from a track of Live, recorded through a route by `abusrec` for 40 seconds.
The only silence was the first 525 ms (connect and fill). After that there was no dropout. The plan is `plans/2026-11-07-link-inputs-matrix.md`.

`abusrec` (`core/audiobus/cmd/abusrec`) records one input to a WAV file. Use it to test a route on a Push.

## Failure behavior

| Event | Result |
|---|---|
| A client crashes | The connection closes. The mixer removes the client. The other clients are not affected. |
| The mixer is not running | `Write` sleeps for the length of the block and drops the audio. The hack keeps its pace. The client retries each second. |
| The mixer restarts | The clients reconnect in about one second. |
| Live changes the format of the card | The mixer drops all clients. They reconnect with the new rate and period. A hack reads `Format` when `Generation` changes. |
| The card is not ready | The mixer refuses clients with the error "card not ready". |
| A client is late | The mixer plays silence for that client and counts an underrun. |

## Results

First test (Push 3, 44.1 kHz, period 256, 32 channels): the mixer ran with two `abustone`
clients on pairs 1-2 and 3-4 for 25 seconds.

| Measure | Result |
|---|---|
| Blocks written | 4313 (172 per second, the rate of the card) |
| Write errors | 0 |
| Underruns | 0 for both clients |
| Time for `Mix`, worst case | 110 to 190 µs (a block lasts 5800 µs) |
| CPU of the mixer | 6.7% in one sample of `top` |

Second test: the mixer ran inside the push-audio-loopback service, with Braids and
MachineModule as clients on pairs 1-2 and 5-6. Both hacks were audible in Live at the same time.
The worst `Mix` time was 220 µs. There were no write errors and no underruns.

These numbers come from short runs. There is no long-run test yet.

Other results from the tests:

- When the service restarts, the hacks fall back to the card for a moment and grab the device.
  The new mixer then logs "Device or resource busy" for about two seconds. The hacks see the mixer
  again, leave the card, and the mixer opens it. No restart of a hack is needed.
- A client that changes its pair reconnects. The sound continues with no underruns.
- Create tracks in Live must not change the pair that the user chose. Live offers only the pairs
  that the user enabled. Both hacks used to switch to the one offered pair. They now keep the
  user's pair and name the offered pairs in the error.

## Known limits

- Dropped clients keep their ring mapped for 500 ms, because `Mix` can still read it. A fixed delay is simple but not elegant.
- The bus folder is in `/dev/shm`, open to all users (mode 0777). Every process on the Push can send audio. Hacks already run as root on a single-user device.
- The sample rate of a client must follow the card. A hack that renders at a fixed rate needs a resampler.
- There is no live change of pair for a running client, except `SetPair`, which reconnects.
- The engine of a hack can send denormal numbers (for example 1e-43) in a silent tail. The mixer handles them, but they cost CPU on some processors.

## Plan

| Step | State |
|---|---|
| 1. Review this document | Done |
| 2. Add `core/audiobus` and `ReadHWParams` to `core/`, tag a new `core/vX.Y.Z` | Done. Released in `core/v0.5.3`. |
| 3. Move the mixer into the push-audio-loopback service, build with cgo | Done. The release workflow builds in Docker. Released as push-audio-loopback 0.2.0-alpha, pinned to `core/v0.5.3`. |
| 4. Status API and page for the mixer | Done (`/api/status`, status page). |
| 5. MachineModule and MonoModule as clients | Done. Both released as 0.2.0-alpha. |
| 6. Braids as a client | Done. Released as push-braids 0.4.0-alpha. |
| 7. Keep the direct `hw:` path in each hack as a fallback | Done. Remove it later. |
| 8. Link Audio output mode in push-audio-loopback (default) | Done on branch, see "Link Audio mode". |
| 9. Inputs for hacks (`OpenWithInputs`, `Client.Read`), routes and a matrix on the web page | Done on branch, see "Inputs". `core/v0.5.5`. |
| 10. Hacks keep rendering while the bus is not connected | Not done. Needed to stop the drift after a restart of the service. |

Release order: merge the `core/` change and tag it, then bump the `core` pin in the three
repos, then release push-audio-loopback first (the mixer), then the two hacks.

## Decisions

| Question | Decision |
|---|---|
| Is 12 ms of extra latency acceptable? | Yes. |
| Sample format | `float32`, one conversion to 16-bit at the end. |
| Where is the document? | In this repository, as `docs/audio-bus.md`. |
