# Adding a new hack

This page walks through the steps to add a new hack to this repo. For the
port rules, see [CLAUDE.md's "Adding a New Hack"](../CLAUDE.md#adding-a-new-hack).
For the `hack.json` and `service.initd` field reference, see
[architecture.md's "Hack structure"](architecture.md#hack-structure-hackshack-id).

1. Create the hack folder:

   ```bash
   mkdir -p hacks/my-hack/src
   ```

2. Create `hacks/my-hack/hack.json`:

   ```json
   {
     "id": "my-hack",
     "name": "My Hack",
     "binary": "my-hack",
     "enabled": true,
     "allowed_roots": [],
     "settings": {}
   }
   ```

3. Create `hacks/my-hack/Makefile`:

   ```makefile
   all:
   	cd src && GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../my-hack .
   ```

4. Write the service in `hacks/my-hack/src/`. Then deploy it:

   ```bash
   ./scripts/install.sh --hack my-hack
   ```

The framework generates a sysvinit init.d script and registers it with
`update-rc.d`. The hack survives reboots.

For a no-binary hack (a shell script or udev rules), set `"binary": ""` and
provide a `service.initd` template instead.

Unlike `push-catalog install`, `./scripts/install.sh` does not assign a
port for you. For local testing, add `"port": <n>` to
`hack.json`, with any free value 7711 or higher (7701-7710 is reserved for
the framework: 7701 is push-manager, 7702 is push-catalog). After you
publish the hack through `push-catalog`, it assigns a port automatically on
every install and overwrites this value. You can drop the field from what
you publish.

Prefer not to build or deploy the hack yourself? [`push-catalog`](../hacks/push-catalog/)
is an on-device installer. It lets you browse and install community-published
hacks from your phone. See [`catalog/PUBLISHING.md`](../catalog/PUBLISHING.md)
for how to publish your own hack into it.

## Core shared library

Before you hand-roll ALSA MIDI, HTTP boilerplate, or SSE for a new hack,
check `core/`. It is a shared Go module with the pieces push-manager,
automation, and keyboard-visualizer all reuse: Push 3 constants
(`core/push3`), image drawing (`core/gfx`), the display shm codec
(`core/display`), HTTP middleware (`core/httpx`), config loading
(`core/hackcfg`), SSE broadcasting (`core/sse`), an HTTP client for
push-manager's display and tempo API (`core/pmclient`), and the ALSA
sequencer layer (`core/alsaseq`). `core/alsaseq` opens a port and sends or
receives MIDI and enumerates devices, with no direct ioctl calls. See
[`core/README.md`](../core/README.md).

Pull it in from `hacks/my-hack/src/go.mod`:

```
require github.com/federico-pepe/ableton-push-hack/core v0.0.0
replace github.com/federico-pepe/ableton-push-hack/core => ../../../core
```
