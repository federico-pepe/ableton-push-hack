# Registry format & how to publish a hack

The catalog is one file: `index.json`. Installing a hack means the store reads
this, downloads the entry's `assets`, verifies their `sha256`, drops them into
`/data/push-hack/hacks/<id>/`, writes the `hack` object as that hack's
`hack.json`, and registers an init.d service. Nothing else.

## Publishing = a pull request

1. Build your hack for **`linux/amd64`** (Push 3 Standalone is x86-64):
   `GOOS=linux GOARCH=amd64 go build -o myhack ./src`
2. Upload the binary as a **GitHub Release asset** on your own repo. Get its sha256:
   `sha256sum myhack`
3. Add one entry to `index.json` (below) and open a PR. That's the "upload".

Later (Phase 3) you'll be able to host your own tap instead of PR-ing here:
`push-store tap add <you>/<your-repo>`.

## Entry schema

```jsonc
{
  "id": "my-hack",                 // unique, kebab-case, == hack.json id
  "name": "My Hack",
  "description": "One line, shown in the list.",
  "version": "0.1.0",
  "author": "your-name",
  "homepage": "https://github.com/...",   // optional
  "requires": ["push-manager"],           // optional: other hack ids that must be installed
  "hack": { ...hack.json contents... },   // written verbatim to the hack's hack.json
  "assets": [
    {
      "name": "my-hack",           // filename inside the hack dir
      "url":  "https://github.com/.../releases/download/v0.1.0/my-hack",
      "sha256": "<sha256 of the file>",
      "exec": true                 // optional: chmod +x after download (the binary)
    }
  ]
}
```

- `hack` is the standard framework `hack.json` — same fields the framework's own
  hacks use (`id`, `name`, `version`, `port`, `binary`, `enabled`, ...). The
  store does **not** invent a new metadata format.
- `assets` is every file the hack needs on-device: the binary (`exec: true`),
  plus any data files, `.rules`, remote-script payloads, etc. Each is
  sha256-pinned — that pin is the trust boundary until signing lands.
- `requires` is advisory in the MVP (the store warns); enforced later.
