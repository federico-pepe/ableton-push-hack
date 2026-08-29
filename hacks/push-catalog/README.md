# Push Hack Catalog

On-device installer for community `push-hack` hacks — browse and install
from your phone, no SSH or build toolchain needed. Port 7702.

Push Hack Catalog hosts nothing itself. It reads a catalog
(`catalog/catalog.json` in this repo) that's just a list of pointers to
hacks living in **their own GitHub repos**. Each hack repo publishes its own
GitHub Releases and keeps a `release.json` at its root; the daemon fetches
that live on every install, downloads the release tarball it points at, and
extracts it. See [`catalog/ARCHITECTURE.md`](../../catalog/ARCHITECTURE.md)
for the full model and [`catalog/PUBLISHING.md`](../../catalog/PUBLISHING.md)
for how to get your own hack into the catalog.

## API

| Route | Method | Description |
|---|---|---|
| `/` | GET | Web UI — lists the catalog (name, author, live version, last-update date), Install/Update/Remove per hack (Update replaces Install when an installed hack's version is behind the catalog's), output log pane. |
| `/api/catalog` | GET | The catalog as JSON: `id`, `name`, `description`, `author`, `homepage`, `requires` from the catalog entry, plus `version` and `released_at` fetched live from each hack's own `release.json` (`null` if that hack's repo is unreachable — degrades per-entry, never fails the whole listing). Also `installed_version` (read from that hack's locally installed `hack.json`, `null` if not installed) and `update_available` (`true` when `installed_version` differs from the live `version`). |
| `/api/installed` | GET | JSON array of hack ids currently present under `/data/push-hack/hacks/`. |
| `/api/install?id=<id>` | POST | Fetches the hack's `release.json`, downloads + extracts its release tarball, registers and starts its init.d service. Returns `{ok, output}` (the shell output, for the log pane). |
| `/api/remove?id=<id>` | POST | Stops the service, removes it from init.d, deletes the hack's directory. |

`id` is validated against `^[a-z0-9][a-z0-9-]{0,63}$` before it ever reaches
the shell.

## How an install works

1. Read `settings.registry` (this hack's `hack.json`) → fetch
   `catalog/catalog.json`.
2. Look up the requested hack's `github_repo` (or a `release_url` override,
   used by local/dev catalog entries) and fetch its `release.json`:
   `https://raw.githubusercontent.com/<github_repo>/<default_branch>/release.json`
   → `{version, download_url, released_at}`.
3. Download the tarball at `download_url`; `tar -xzf` it straight into
   `/data/push-hack/hacks/` (the tarball's own `<id>/` top-level dir lands
   correctly).
4. Read the extracted `hack.json` for the binary name, generate an init.d
   service (plain `nice`-backgrounded, same pattern as the framework's own
   `install.sh` — not `start-stop-daemon`, which was found on real hardware
   to silently drop the log redirection when detaching), enable + start it.

No sha256 pin, no signing — the trust boundary is "this repo is on GitHub,
served over HTTPS, and its catalog entry was PR-reviewed once." See
`catalog/ARCHITECTURE.md` for the reasoning.

## Development

```bash
cd hacks/push-catalog && PATH=$PATH:/usr/local/go/bin make        # build
bash push-catalog.sh --self-test                                   # offline checks
bash push-catalog.sh list                                          # needs PUSH_CATALOG_REGISTRY set
```

`push-catalog.sh` is the canonical installer script — `make embed` copies
it into `src/` so `go:embed` bakes it into the binary as a single
self-contained artifact. Edit the root copy; `make build`/`build-local`
re-embed it automatically.
