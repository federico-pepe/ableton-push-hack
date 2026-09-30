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

`push-hack-arrangement`'s `scripts/deploy.sh` is a working example of these
three steps.

For local testing, put any free port 7711 or higher in `hack.json`. The
range 7701-7710 is reserved for the framework: 7701 is push-manager, and
7702 is push-catalog. When a user installs the hack through
`push-catalog`, the catalog assigns a port and overwrites this value.

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
require github.com/federico-pepe/ableton-push-hack/core v0.2.0
```

To get a newer `core` version later, run this command in `src/`:

```bash
go get github.com/federico-pepe/ableton-push-hack/core@vX.Y.Z && go mod tidy
```
