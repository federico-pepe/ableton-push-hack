# Push Store

On-device installer for community `push-hack` hacks — browse and install
from your phone, no SSH or build toolchain needed. Port 7705.

Push Store hosts nothing itself. It reads a catalog
(`catalogue/catalog.json` in this repo) that's just a list of pointers to
hacks living in **their own GitHub repos**. Each hack repo publishes its own
GitHub Releases and keeps a `release.json` at its root; the store fetches
that live on every install, downloads the release tarball it points at, and
extracts it. See [`catalogue/ARCHITECTURE.md`](../../catalogue/ARCHITECTURE.md)
for the full model and [`catalogue/PUBLISHING.md`](../../catalogue/PUBLISHING.md)
for how to get your own hack into the catalog.

## API

| Route | Method | Description |
|---|---|---|
| `/` | GET | Web UI — lists the catalog, Install/Remove per hack, output log pane. |
| `/api/catalog` | GET | The catalog as JSON (`id`, `name`, `description`, `author`, `homepage`, `requires`). |
| `/api/installed` | GET | JSON array of hack ids currently present under `/data/push-hack/hacks/`. |
| `/api/install?id=<id>` | POST | Fetches the hack's `release.json`, downloads + extracts its release tarball, registers and starts its init.d service. Returns `{ok, output}` (the shell output, for the log pane). |
| `/api/remove?id=<id>` | POST | Stops the service, removes it from init.d, deletes the hack's directory. |

`id` is validated against `^[a-z0-9][a-z0-9-]{0,63}$` before it ever reaches
the shell.

## How an install works

1. Read `settings.registry` (this hack's `hack.json`) → fetch
   `catalogue/catalog.json`.
2. Look up the requested hack's `github_repo` (or a `release_url` override,
   used by local/dev catalog entries) and fetch its `release.json`:
   `https://raw.githubusercontent.com/<github_repo>/<default_branch>/release.json`
   → `{version, download_url}`.
3. Download the tarball at `download_url`; `tar -xzf` it straight into
   `/data/push-hack/hacks/` (the tarball's own `<id>/` top-level dir lands
   correctly).
4. Read the extracted `hack.json` for the binary name, generate an init.d
   service (`start-stop-daemon`, falling back to `nohup`), enable + start it
   — mirrors the framework's own `install.sh`.

No sha256 pin, no signing — the trust boundary is "this repo is on GitHub,
served over HTTPS, and its catalog entry was PR-reviewed once." See
`catalogue/ARCHITECTURE.md` for the reasoning.

## Development

```bash
cd hacks/push-store && PATH=$PATH:/usr/local/go/bin make        # build
bash push-store.sh --self-test                                   # offline checks
bash push-store.sh list                                          # needs PUSH_STORE_REGISTRY set
```

`push-store.sh` is the canonical installer script — `make embed` copies it
into `src/` so `go:embed` bakes it into the binary as a single
self-contained artifact. Edit the root copy; `make build`/`build-local`
re-embed it automatically.
