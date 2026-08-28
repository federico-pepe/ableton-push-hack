# Publishing a hack to Push Store

This is the step-by-step for getting your hack installable via `push-store`
on any Push. For the field reference, see [`schema.md`](schema.md); for why
the model looks like this, see [`ARCHITECTURE.md`](ARCHITECTURE.md).

Your hack lives in **your own GitHub repo** — this repo only carries a
pointer to it (`catalogue/catalog.json`). You keep full control of your code,
your releases, and your version cadence.

## 1. Repo layout

At minimum:
```
your-repo/
  hack.json     # standard framework metadata: id, name, version, port, binary, enabled, ...
  Makefile       # builds GOOS=linux GOARCH=amd64 (Push 3 Standalone is x86-64)
  src/ …
```
`hack.json`'s `version` is what you bump for each release, and what the
release workflow below checks your git tag against.

## 2. Add the release workflow

Add `.github/workflows/release.yml`, triggered on `v*` tags, that:

1. Checks the pushed tag (`vX.Y.Z`) matches `hack.json`'s `version` — fail
   fast if you forgot to bump it.
2. Builds the binary: `GOOS=linux GOARCH=amd64 go build -o dist/<id>/<id> ./src`
   (or your build system's equivalent — no cgo/Docker needed for a plain-Go
   hack).
3. Copies `hack.json` (and any other runtime files — `remote-script/`, data
   files, etc.) into `dist/<id>/` alongside the binary.
4. Packages it: `tar -czf <id>.tar.gz -C dist <id>/` — the tarball's single
   top-level entry must be `<id>/`, matching your hack's id.
5. Creates a GitHub Release for the tag with `<id>.tar.gz` as the asset
   (e.g. `softprops/action-gh-release`).
6. Commits an updated `release.json` back to your default branch:
   ```json
   { "version": "X.Y.Z", "download_url": "https://github.com/<you>/<repo>/releases/download/vX.Y.Z/<id>.tar.gz" }
   ```

`hacks/keyboard-visualizer`'s own repo (once split out) has a working
example of this workflow — copy it as a starting point.

## 3. Cut your first release

```bash
git tag v0.1.0
git push origin v0.1.0
```
The workflow builds, releases, and updates `release.json` for you. Verify:
```bash
curl -s https://raw.githubusercontent.com/<you>/<repo>/main/release.json
```

## 4. Open the catalog PR

Add one entry to this repo's `catalogue/catalog.json`:
```jsonc
{
  "id": "your-hack",
  "name": "Your Hack",
  "description": "One line, shown in the list.",
  "author": "you",
  "homepage": "https://github.com/you/your-hack",
  "github_repo": "you/your-hack",
  "default_branch": "main",
  "asset_name": "your-hack.tar.gz",
  "requires": []          // optional: other hack ids that must already be installed
}
```
Open a PR here with just that entry. A maintainer reviews and merges — the
review is one-time; every future release you tag is picked up automatically,
since the store always fetches your `release.json` live, not a version
pinned in the catalog.

## Updating later

Just cut a new tag in your own repo (step 3) — no catalog PR needed, no
review needed, unless you're also changing `github_repo`, `default_branch`,
or `asset_name`.
