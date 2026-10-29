# Adding a new hack

This page shows how to make a new optional hack. Each optional hack lives
in its own GitHub repository (for example `push-hack-<id>`), and users
install it through Push Hack Catalog. The `hacks/` folder of this repo
holds only the three core hacks: push-manager, push-display, and
push-catalog.

For the `hack.json` and `service.initd` field reference, see
[architecture.md's "Hack structure"](architecture.md#hack-structure-hackshack-id).
For the list of published hacks, see
[catalog/catalog.json](../catalog/catalog.json).

## Create the repository

1. Create a new repository, for example `push-hack-my-hack`, with a `src/`
   folder.

2. Create `hack.json` in the repository root:

   ```json
   {
     "id": "my-hack",
     "name": "My Hack",
     "version": "0.1.0",
     "binary": "my-hack",
     "port": 7711
   }
   ```

3. Create a `Makefile` in the repository root:

   ```makefile
   all:
   	cd src && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../my-hack .
   ```

4. Write the service in `src/`.

For a no-binary hack (a shell script or udev rules), set `"binary": ""` and
provide a `service.initd` template instead.

## Test the hack on the device

1. Build the binary with `make`.
2. Copy the binary and `hack.json` to `/data/push-hack/hacks/my-hack/` on
   the Push.
3. Start the binary over SSH, in the foreground.

A small `scripts/deploy.sh` that does these three steps saves time.

For local testing, put any free port 7711 or higher in `hack.json`. The
range 7701-7710 is reserved for the framework: 7701 is push-manager, and
7702 is push-catalog. When a user installs the hack through
`push-catalog`, the catalog assigns a port and overwrites this value.

## A hack that uses Push's screen, buttons and pads

If your hack takes over Push's whole surface (as Stacks does), make
it an app of Push Hack Home, push-manager's switcher. Users open Home with
Shift + Help (or by holding Help) and pick your hack there, and press
Help to go back to Live.
Do not give your hack a button combination of its own: every hack sees
every press, so two combinations can collide. Do not use Help in your
hack either: push-manager takes it while your hack owns the surface.

1. Add a `surface` hook to `hack.json`:

   ```json
   "surface": { "label": "MYHACK", "color": "violet" }
   ```

   `label` is ASCII, at most 7 letters. `color` is a Push palette name.
2. Set `pmclient.Client.App` to your hack's id, and call `RunSurface`. Draw
   and react to presses only between `OnFocus` and `OnBlur`. Send frames
   with `Push` (it uses `PushFrame` and falls back to `PushImage` on an older push-manager).
3. Send your card's two status lines with `SurfaceStatus` when they change.
4. If your hack has its own "back to Live" button, call
   `SurfaceLeave("live")`.

push-manager switches the display and the MIDI intercept for you. Pass the
buttons that Live should keep (such as Play and Record) to `RunSurface`.
If your hack stops while it owns the surface, push-manager shows Home and
says so. See push-manager's README, "Push Hack Home", for the details.

## Publish the hack

Add a release workflow, cut a release, and add an entry to
`catalog/catalog.json`. See [`catalog/PUBLISHING.md`](../catalog/PUBLISHING.md)
for each step.

## Core shared library

Before you hand-roll ALSA MIDI, HTTP boilerplate, or SSE for a new hack,
look in `core/`. It is a shared Go module with these pieces:

- Push 3 constants (`core/push3`).
- Image drawing (`core/gfx`) and on-screen widgets (`core/gfx/widgets`).
- The display shm codec (`core/display`).
- HTTP middleware (`core/httpx`).
- Configuration loading (`core/hackcfg`).
- SSE broadcasting (`core/sse`).
- An HTTP client for the display and tempo API of push-manager (`core/pmclient`).
- The ALSA sequencer layer (`core/alsaseq`). It opens a port, sends and
  receives MIDI, and lists devices, with no direct ioctl calls.

See [`core/README.md`](../core/README.md).

Pin `core` to the newest `core/vX.Y.Z` tag in `src/go.mod`. Do not use a
`replace` line:

```
require github.com/federico-pepe/ableton-push-hack/core v0.3.0
```

To get a newer `core` version later, run this command in `src/`:

```bash
go get github.com/federico-pepe/ableton-push-hack/core@vX.Y.Z && go mod tidy
```

## Talking to Live

A hack that needs Live (tempo, clips, devices, the selection) uses the PushHack Remote Script. It ships with Push Hack. Connect to `127.0.0.1:7704`, send `hello`, then ask for data or subscribe to topics. Do not ship your own Remote Script. See [pushhack-remote-script.md](pushhack-remote-script.md).
