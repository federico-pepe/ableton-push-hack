# API reference

HTTP routes and wire protocols for the core hacks. For the file layout
behind each route, see [architecture.md](architecture.md). Push Manager's
own README has full request/response detail for every route below.

## Push Manager — `http://push.local:7701`

**Routes:**

`/api/list`, `/api/download`, `/api/upload`, `/api/delete`, `/api/rename`,
`/api/copy`, `/api/unmount`, `/api/stats`, `/api/assets/<path>`,
`/api/display/{status,mode,image,screenshot}` (`screenshot` is a GET
request; it returns a PNG of the current framebuffer with an
`X-Display-Mode` header), `/api/midi/{events,stream,filter,ports,subscribe,
chords,led,palette,mapping,mapping/config}` (`ports?writable=1` lists output
destinations), `/api/presets`, `/api/presets/{refresh,facets,meta}`,
`/api/live/load`, `/api/live/tempo`, `/api/live/playing`, `/api/live/play`,
`/api/live/stop`, `/api/hacks/installed`, `/api/ui/tabs` (GET and POST).

### RemotePanel contract

A display-owning hack that wants a Shadow UI tab does not draw pixels
itself. push-manager polls `http://127.0.0.1:<port><path>` every 300ms for a
JSON `remoteView`:

```
{ "title": ..., "status": ..., "rows": [...], "cursor": ..., "buttons": [...], "hint": ... }
```

push-manager renders that view with `core/gfx/widgets.RenderList`, and posts
every button press back to the same hack as `{cc, value}`. The hack owns all
state; push-manager only renders and forwards input. See
`src/remote_panel.go` in `docs/architecture.md` and push-manager's README
for the full field list.

## Push Display — shared memory protocol

Push Display and push-manager talk over a shared-memory region. Both
`push_hook.c` and `core/display/shm.go` must stay in sync with this layout:

```
offset  0: uint32 magic      (0x50555348 "PUSH")
offset  4: uint32 version    (1)
offset  8: uint32 mode       (0=passthrough, 1=bar, 2=takeover)
offset 12: uint32 frame_seq  (incremented by push-manager on each image write)
offset 16: uint8[655360]     BGR565 pixels (960x160, stride 1024, frame duplicated)
total: 655376 bytes, permissions 0666
```

**Display geometry:** 960x160 px, BGR565, XOR-shaped
(`{0xE7,0xF3,0xE7,0xFF}` repeated), stride 1024, with each frame sent twice.

A hack never mmaps this region on its own. See "Display-owning hacks" in
[CLAUDE.md](../CLAUDE.md) for the rule, and use `POST /api/display/mode` and
`POST /api/display/image` on push-manager's HTTP API instead — or the Go
client in `core/pmclient`.

## Push Hack Catalog — `http://push.local:7702`

**Routes:**

`GET /`, `GET /api/catalog`, `GET /api/installed`, `POST /api/install?id=<id>`,
`POST /api/remove?id=<id>`, `POST /api/disable?id=<id>`,
`POST /api/enable?id=<id>`.
