# Audio bus: many hacks, one loopback card

Status: prototype. The code is in `core/audiobus` and in the `mixer` program of
push-audio-loopback (branch `audiobus-prototype`). Nothing here is released.

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
| mixer | push-audio-loopback | Opens the card, sets the format, calls `Mix` and writes the result. |
| `abustone` | `core/audiobus/cmd` | A test client that plays a sine wave. |

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

## Failure behavior

| Event | Result |
|---|---|
| A client crashes | The connection closes. The mixer removes the client. The other clients are not affected. |
| The mixer is not running | `Write` sleeps for the length of the block and drops the audio. The hack keeps its pace. The client retries each second. |
| The mixer restarts | The clients reconnect in about one second. |
| Live changes the format of the card | The mixer drops all clients. They reconnect with the new rate and period. A hack reads `Format` when `Generation` changes. |
| The card is not ready | The mixer refuses clients with the error "card not ready". |
| A client is late | The mixer plays silence for that client and counts an underrun. |

## Prototype results

Test on a Push 3 (44.1 kHz, period 256, 32 channels). The mixer ran with two `abustone` clients on pairs 1-2 and 3-4. The test ran for 25 seconds.

| Measure | Result |
|---|---|
| Blocks written | 4313 (172 per second, the rate of the card) |
| Write errors | 0 |
| Underruns | 0 for both clients |
| Time for `Mix`, worst case | 110 to 190 µs (a block lasts 5800 µs) |
| CPU of the mixer | 6.7% in one sample of `top` |
| CPU of one client | 0.0% in the same sample |

These numbers come from one short run. They do not include real hacks, a long run, or the sound in Live.

## What the prototype does not do

- The mixer is a separate program. The released push-audio-loopback is still pure Go.
- The mixer needs cgo and libasound to write to the card. The release workflow of push-audio-loopback builds with `CGO_ENABLED=0`. It must change.
- Braids and MachineModule do not use the bus yet.
- A hack cannot ask for a new pair while it runs, except through `SetPair`, which reconnects. There is no live pair change.
- Dropped clients keep their ring mapped for 500 ms, because `Mix` can still read it. A fixed delay is simple but not elegant.
- The bus folder is in `/dev/shm`, open to all users (mode 0777). Every process on the Push can send audio. Hacks already run as root on a single-user device.
- The sample rate of a client must follow the card. A hack that renders at a fixed rate needs a resampler.

## Plan

1. Review this document.
2. Add `core/audiobus` and the `HWParams` change to `core/`. Tag a new `core/vX.Y.Z`.
3. Move the mixer into the main program of push-audio-loopback. Change the release workflow to use cgo. Keep the kernel module loader.
4. Add a small status API to the mixer: the clients, their pairs, their peaks and their underruns. Show it in the web page of push-audio-loopback.
5. Move MachineModule to the client. It is the smaller of the two hacks.
6. Move Braids to the client.
7. Keep the direct `hw:` path in each hack as a fallback, for users who do not have the new push-audio-loopback. Remove it later.

## Decisions

| Question | Decision |
|---|---|
| Is 12 ms of extra latency acceptable? | Yes. |
| Sample format | `float32`, one conversion to 16-bit at the end. |
| Where is the document? | In this repository, as `docs/audio-bus.md`. |
