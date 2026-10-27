# Scripts

This page lists the shell scripts in `scripts/` and `hacks/push-display/`. Use them to probe, deploy, and remove hacks on Push.

| Script | Purpose |
|--------|---------|
| `./scripts/discover.sh` | Probe Push OS — init system, paths, processes, ports |
| `./scripts/install.sh` | Build and deploy all hacks, or one hack, and register the service |
| `./scripts/uninstall.sh` | Remove all hacks and services |
| `hacks/push-display/deploy.sh` | Build and deploy `push_hook.so` on its own, without the rest of the framework |

## discover.sh

```bash
./scripts/discover.sh
./scripts/discover.sh --host 192.168.1.67
```

| Flag | Effect |
|------|--------|
| `--host <address>` | Connect to this address instead of `push.local` |
| `--user <name>` | SSH as this user |
| `--key <path>` | Use this SSH key |

## install.sh

```bash
./scripts/install.sh
./scripts/install.sh --hack push-manager --build
./scripts/install.sh --dry-run
```

| Flag | Effect |
|------|--------|
| `--host <address>` | Connect to this address instead of `push.local` |
| `--user <name>` | SSH as this user |
| `--key <path>` | Use this SSH key |
| `--hack <id>` | Deploy only this hack |
| `--build` | Build from source. Default: deploy the pre-built binary |
| `--dry-run` | Print the actions without doing them |
| `--yes` | Skip the confirmation prompt |

When it deploys `push-manager` (or all hacks), `install.sh` also copies `remote-script/PushHack` to `/data/Music/Ableton/User Library/Remote Scripts/PushHack`. You then pick `PushHack` in a control-surface slot in Live's Preferences (Input and Output: None) and restart Live. If it finds an old `PushHackBrowser` or `PushHackArrangement`, it warns and leaves it alone.

`push-display` is handled by `./scripts/install.sh`. Its `service.initd` copies `push_hook.so`, patches the `LD_PRELOAD` line into Push3's init script, and restarts Push3. For a standalone re-deploy of `push-display`, use `deploy.sh` below.

## uninstall.sh

```bash
./scripts/uninstall.sh
./scripts/uninstall.sh --purge --yes
```

| Flag | Effect |
|------|--------|
| `--host <address>` | Connect to this address instead of `push.local` |
| `--user <name>` | SSH as this user |
| `--key <path>` | Use this SSH key |
| `--hack <id>` | Remove only this hack |
| `--purge` | Also delete the `/data/push-hack/` data directory |
| `--yes` | Skip the confirmation prompt |

`uninstall.sh` also removes the `PushHack` folder from Live's User Library (with `--hack push-manager`, or with no `--hack`). It leaves other Remote Scripts alone. Clear the `PushHack` slot in Live's Preferences afterward.

## hacks/push-display/deploy.sh

```bash
hacks/push-display/deploy.sh
hacks/push-display/deploy.sh --no-build
```

| Flag | Effect |
|------|--------|
| `--no-build` | Deploy the pre-built `push_hook.so`. Skip the Docker build |
| `--remove` | Remove `push-display` instead of deploying it |

See [`hacks/push-display/README.md`](../hacks/push-display/README.md) for the full deploy and risk notes for this hack.

## package-release.sh

This script builds the Push Hack release files into `dist/`. Git does not track `dist/`. The release workflow runs the script. You can also run it on a Mac to see the output.

```bash
scripts/package-release.sh [v0.1.15-alpha]
```

The script does these steps:

1. It reads `VERSION`. If you give a tag, the script stops when the tag is not `v` plus `VERSION`.
2. It runs `make` in `hacks/push-manager` and `hacks/push-catalog`.
3. It takes `hacks/push-display/push_hook.so` as committed. That file builds through Docker.
4. It writes `dist/push-hack-<version>.tar.gz`.
5. It writes `dist/manifest.json`.

The manifest holds these fields:

- `version`.
- `shm_protocol`, from `SHM_VERSION` in `push_hook.c`.
- `needs_reinstall`, from `releasemeta.json`.
- The SHA-256, size and path of each file.
- The name, size and SHA-256 of the tarball.

The script does not sign anything. On macOS it sets `COPYFILE_DISABLE`, so the tarball has no `._` files.

For a test build with another version, set `PUSH_HACK_VERSION=0.1.99-alpha`. The Makefiles also read this variable. Never use it for a real release.

Set `needs_reinstall` to `true` in `releasemeta.json` when a release changes anything under `/etc`. The update on the device then refuses and asks for `install.sh`.
