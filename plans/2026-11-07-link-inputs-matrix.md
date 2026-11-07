# Plan: Link inputs and a routing matrix for the hacks

Status: built and merged (2026-11-07), not released. Steps 1 to 6 are done: the Link source, the play-out rule, the bus inputs (`core/v0.5.5`), the routes and the matrix on the page.
A 131 Hz tone from a track of Live was recorded through a route for 40 seconds, with no dropout after the first 525 ms.
Not done: the tests for restart of Live and of the service with routes, a hack to hack route, the CPU and delay numbers, and a hack that uses an input (step 7: left to the authors of hacks).
This plan follows `plans/2026-11-05-link-audio-for-hacks.md`, which covers audio from a hack to Live.

## Goal

A hack must be able to receive audio from any Link Audio channel. A channel can be a track or the Main output of Live, or the channel of another hack.
Example: record a track of Live into a hack.

The web page of push-audio-loopback gets a matrix. Its rows are the Link channels. Its columns are the inputs of the hacks. The user connects a row to a column with one click.

## What we know

The spike (2026-11-05) tested the receive side with a small peer on a Push 3.

| Test | Result |
|---|---|
| Channels listed | Six channels from Live: four tracks, one return, Main. The names are the names of the tracks. |
| Format | Stereo, 44100 Hz, `int16`, about 345 buffers per second. |
| Gaps in the buffer counter | 0, over more than 60000 buffers per channel. |
| Longest wait between two buffers | 7.3 to 8.1 ms. |
| Signal | The peak level was not zero while Live played. |
| Playout | Not tested. We counted the buffers but we did not play or record them. |

Link works like this:
- A source is made for one channel (`LinkAudioSource(link, channelId, callback)`). Link calls the callback on its own thread with each buffer.
- A buffer has 1 or 2 channels. It carries its sample rate, a counter and a beat time.
- Live sends a channel only while a source listens to it.
- Live chooses its own inputs. Our service cannot connect a channel to a track of Live.

The matrix therefore controls only what the hacks receive. It cannot control what Live receives. The page tells the user to pick the PushHack channels in the input chooser of Live.

## Design

### 1. Bus protocol (in `core/audiobus`)

Today the bus carries audio in one direction: from a hack to the service. It gets a second direction.

- A hack declares its inputs when it connects. The first message gets a new field, for example `"inputs":["Sample in"]`. Each input is stereo.
- The service makes one ring for each input, in the same folder as the output rings. The service writes it. The hack reads it.
- The reply gets a new field with the path of each input ring. An old service does not send it. A hack then knows that it has no inputs.
- An old hack does not send the field. A new service ignores inputs for it. Both directions stay compatible.
- The client gets `Inputs()` and `Read(input, buf)`. `Read` never blocks. It returns one period of audio, or silence when the ring is empty.

This is a change in `core/`. It needs a new `core/vX.Y.Z` tag and a new pin in the service and in each hack that uses it.

### 2. Link sources (shim and service)

- The shim lists the channels with their IDs, peer names and channel names.
- The shim makes and removes a source for a channel ID. The callback runs on a Link thread. It converts `int16` to `float32`, turns mono into stereo, and writes into a small ring in C++. It does no work that blocks.
- The sample rate of a buffer must be 44100 Hz in this version. For any other rate the route plays silence and the page shows "rate not supported". Resampling is a later step.
- The service reads these rings once for each period, with the timer that it already has. It writes one period into the input ring of the hack.

### 3. Jitter buffer

Link delivers buffers when it can. The hack needs one steady stream. A small buffer in the service joins the two.

- The target fill is 4 periods (about 12 ms). The user can change it.
- Playout starts when the fill reaches the target.
- When the fill is more than the target plus 2 periods, the service drops the oldest period.
- When the buffer is empty, the service plays silence and counts an underrun.
- The clock of the service and the clock of Live drift apart slowly. This costs one drop or one silence every few minutes. This version accepts that. A later version can resample with a small adaptive ratio.

The delay of a route is the Link latency of Live, plus the jitter target, plus the ring of the hack. We measure the real value.

### 4. Routes and storage

- A route connects one channel to one input. An input has one source. A channel can feed many inputs.
- The service saves routes in `link-routes.json`, next to `hack.json`. A route stores the channel ID, the peer name and the channel name.
- At start, the service finds a route by ID first, then by names.
- A route whose channel is gone stays in the file. The page shows it as "missing". The audio is silent. It connects again when the channel comes back.
- There is no route by default. A new hack input is silent until the user connects it. Any device on the Wi-Fi can offer a Link channel, so nothing connects by itself.

### 5. Web page

The matrix shows only in Link mode.

- Rows: the Link channels, grouped by peer. The Push (Live) comes first. Then the hacks of this service, then other devices.
- Columns: the inputs of the hacks that are connected.
- A cell is a toggle. Selecting a cell for an input removes the previous route of that input.
- Each route shows a level meter, the fill of the jitter buffer and the count of underruns.
- A note says: "To send audio to Live, pick the channel of the hack in the input of an audio track."

API:
- `GET /api/matrix` gives the channels, the inputs and the routes.
- `POST /api/route` with `{"hack","input","channel_id"}` makes a route. A `null` channel removes it. It accepts `application/json` only, like `POST /api/mode`.

## Steps

1. **Shim.** List channels. Make and remove a source. Write a test binary that records one channel of Live to a WAV file. Run it on the Push.
   Gate: a 1 kHz sine from a Live track records with no break that the eye sees in a plot.
2. **Jitter buffer.** Write the policy in Go, with unit tests that use fake timings: normal flow, a late burst, a long gap, a clock that runs fast and a clock that runs slow.
3. **Bus protocol.** Add inputs to `core/audiobus`, with tests. Add a small test client (`abusrec`) that records an input to a WAV file. Tag a new `core/vX.Y.Z`. Bump the pin in push-audio-loopback.
4. **Service.** Add routes, storage and the two API calls. Switching the mode or restarting the service must restore the routes.
5. **Web page.** Add the matrix.
6. **Tests on the Push.** See the next section.
7. **First hack.** A separate plan chooses the hack that gets an input, for example a sampler in MachineModule.
8. **Documents.** See the last section.

## Tests and pass criteria

| Test | Pass |
|---|---|
| Record a 1 kHz sine from a Live track for 10 minutes | Fewer than one dropout in 10 minutes. If the drift gives more, add the adaptive resampling before release. |
| Record Main while the Live set plays | The WAV file matches what Live plays. |
| Route from the channel of a hack to the input of another hack | Works. |
| Restart of the service | The routes come back with no action by the user. |
| Restart of Live | The routes come back when Live is back. The missing state shows in between. |
| A channel disappears and appears | The page shows "missing", then connects again. |
| Round trip: a click in Live, through a hack input and output, back to Live | The delay is written down for Link latency 30, 20 and 10 ms. |
| Sample rate of Live at 48000 Hz | The page shows "rate not supported". Nothing crashes. |
| CPU of the service with 4 routes | Written down. |
| A device on the Wi-Fi offers a channel | It shows in the matrix. Nothing connects until the user does it. |

## Risks and unknowns

- **Drift.** The jitter buffer drops or repeats audio now and then. The size of this effect is not known. Test 1 measures it.
- **Receive timing.** The spike counted buffers. It did not play them. The beat time in each buffer is not used in this version.
- **Rate.** Live on the Push runs at 44100 Hz. A user who changes the rate breaks the inputs until we add resampling.
- **Cost in Live.** Live sends a channel only while a source listens. Each route adds network and CPU work to Live. We measure it.
- **Feedback.** A hack can record a track that plays the hack. Live and the hack then form a loop. The page does not detect this. The docs name it.
- **Release order.** The `core/` tag must come first. Then the service. Then the hacks. A hack with inputs needs the new service.
- **License.** Same as in the first plan. The service binary is GPL v2 or later.
- **The drift of hacks after a restart.** This is a known issue of the first plan. Inputs make it matter more, because a recording hack must stay in time with Live.

## Out of scope

- Connecting a channel to an input of Live. Live decides this.
- Sample rate conversion, except as a later step.
- Mono channels as separate routes. A mono channel plays on both sides.
- Any hack that uses an input. That is the next plan.
- Card mode. Inputs exist in Link mode only.

## Documents to update when the code lands

- `docs/audio-bus.md`: the input direction, the new fields and the jitter buffer.
- `docs/api-reference.md` or the README of push-audio-loopback: `/api/matrix` and `/api/route`.
- `docs/adding-a-hack.md`: how a hack declares and reads an input.
- `core/README.md`: the new `Client` methods.
- `CHANGELOG.md`, under `## [Unreleased]`.
