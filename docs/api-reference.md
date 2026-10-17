# API reference

HTTP routes and wire protocols for the core hacks. For the file layout
behind each route, see [architecture.md](architecture.md). Push Manager's
own README has full request/response detail for every route below.

## Push Manager — `http://push.local:7701`

**Routes:**

`/api/list`, `/api/download`, `/api/upload`, `/api/delete`, `/api/rename`,
`/api/copy`, `/api/unmount`, `/api/stats`, `/api/assets/<path>`,
`/api/display/{status,mode,image,frame,screenshot}` (`frame` takes one raw
BGR565 frame, see push-manager's README) (`screenshot` is a GET
request; it returns a PNG of the current framebuffer with an
`X-Display-Mode` header), `/api/midi/{events,stream,filter,ports,subscribe,
chords,led,palette,mapping,mapping/config}` (`ports?writable=1` lists output
destinations), `/api/presets`, `/api/presets/{refresh,facets,meta}`,
`/api/live/load`, `/api/live/plugins`, `/api/live/tempo`, `/api/live/scale`, `/api/live/listen`, `/api/live/playing`, `/api/live/play`,
`/api/live/stop`, `/api/hacks/installed`, `/api/ui/tabs` (GET and POST),
`/api/surface`, `/api/surface/{session,status,input,leave,open,star}`
(Push Hack Home, below).

### Push Hack Home: who owns the surface

push-manager lends Push's screen, buttons and pads to one **app** at a time
(a hack whose `hack.json` declares `surface`). The user opens Home with
Shift + Help and picks the app; the superstar opens with Shift + Help,
Help. Full behaviour: push-manager's README, "Push Hack Home".

- **Session.** `GET /api/surface/session?app=<id>&pass_cc=<cc,...>` is a
  server-sent event stream the app keeps open while it runs: `ready`,
  then `focus` (`{"input": true}`) when it owns the surface and `blur`
  when it no longer does, with a `: ping` comment every second. While the
  session is open the app is RUNNING in Home; if it closes while the app
  owns the surface, Home takes over. A newer session for the same app
  replaces the older.
- **The app's calls.** `POST /api/surface/status` (`{"app", "lines"}`, the
  card's two lines), `/api/surface/input` (`{"app", "input": bool}`,
  screen only or the whole surface), `/api/surface/leave` (`{"app", "to":
  "live"|"home"}`), `/api/surface/open` (`{"id"}`).
- **Who may draw.** Frames (`/api/display/frame`, `/api/display/image`)
  carry the header `X-Push-Hack-App: <id>`. While Home or an app owns the
  surface, only the owning app's frames reach the screen; the rest get
  409. Frames without the header, and `/api/display/mode` and
  `/api/midi/filter` from anyone but the owning app, work only while the
  surface is "outside" (Live, the Shadow UI, an older hack).
- **State.** `GET /api/surface`: the owner (`outside`, `home`, `app`), the
  app, the last app and Home's entries. 404 on a push-manager without
  Home.

`core/pmclient` does all of it: `Client.App`, `RunSurface`,
`SurfaceStatus`, `SurfaceInput`, `SurfaceLeave`, `SurfaceOpen`,
`HasSwitcher` (see core/README.md).

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
[CLAUDE.md](../CLAUDE.md) for the rule. A hack that takes the whole surface
is an app of Push Hack Home (above) and sends frames with
`pmclient.PushFrame`; a hack that only shows an image can still use
`POST /api/display/mode` and `POST /api/display/image` while nothing of
Home owns the surface.

## Push Hack Catalog — `http://push.local:7702`

**Routes:**

`GET /`, `GET /api/catalog`, `GET /api/installed`, `POST /api/install?id=<id>`,
`POST /api/remove?id=<id>`, `POST /api/disable?id=<id>`,
`POST /api/enable?id=<id>`.
