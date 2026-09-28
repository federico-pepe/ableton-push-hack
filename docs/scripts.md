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
