# Push Hack Home: one switcher for every hack that uses Push's surface

Status: built and tested on the device (2026-10-17). Branch
`feat/hack-switcher`, stacked on `feat/live-listen` (#37).

Changes from the design, found while building it:

- The switcher owns only the states it creates (Home, an app). Everything
  else (Live, the Shadow UI, an older hack) is one state, "outside", run
  exactly as before, so push-manager's other paths that flip the intercept
  (chords, the MIDI panel, the web UI, preset loads) did not change.
- Home tracks Shift from every Shift press. The release that fires Shift
  + Help goes to the gesture, and Home kept thinking Shift was held, so
  the next STAR became Shift + STAR (found on the device).
- push-catalog now gives a port to a hack that declares only `surface`.
- A stopped card shows PRESS TO START without the LAST tag (they overlapped).
- Taking the buttons from Live waits 120 ms and tells Push3 Shift and Help
  are up first (`settleLive`). push-manager switched the intercept on so
  fast that the hook swallowed the Shift release before Push3 read it; Live
  kept Shift held, and Play in Riffs did nothing (found by the user).

## The problem

Every hack that takes Push's screen, buttons and pads (Stacks, Riffs)
opens with a gesture of its own. Each hack reads every button press, so
two gestures can collide without anything noticing. They already have:

- push-manager binds Shift + Set to `browser_open` (the Shadow UI's
  Browse tab). Riffs also opens with Shift + Set. On the device on
  2026-10-17 at 16:22:27, one press did both: push-manager started the
  Shadow UI, and in the same second Riffs posted `/api/midi/filter` and
  `/api/display/mode`. Two programs drew on one screen.
- Nothing decides who owns the surface. Any hack can switch the intercept
  on and post frames at any time.
- If a hack crashes while it owns the surface, the intercept stays on and
  the screen freezes. The user has to know that Shift + Settings turns the
  intercept off.
- With three or four such hacks, nobody can remember the gestures.

## Goals

1. **One gesture for everything:** Shift + Help opens Home, a switcher
   with every app and tool. No hack has a gesture of its own.
2. **Favorites:** starred entries come first, so they are one press after
   Shift + Help.
3. **A superstar:** one entry opens straight from anywhere with Shift +
   Help, Help. The same gesture inside it goes back to Live.
4. **push-manager is the only owner of the surface.** At any moment
   exactly one of these owns it: Live, Home, a tool, or one app. Frames
   from anyone else never reach the screen.
5. **Nothing gets stuck.** An app that stops, crashes or hangs gives the
   surface back within a second, and Home says what happened.
6. **Old hacks keep working.** A hack that uses the display and intercept
   APIs directly still works while Live owns the surface.
7. **No cost when closed.** Home draws only while it is open, and only
   when something changes.

Not goals: changing the push-display hook in Push3 (too risky for this);
changing Shift + Settings (the Shadow UI toggle stays as it is upstream).

## The experience

### Gestures

| Gesture | From Live | From Home | From a tool | From an app |
|---|---|---|---|---|
| Shift + Help | Home | Live | Home | Home |
| Shift + Help, Help | the superstar | the superstar | the superstar | the superstar; from the superstar itself: Live |
| Help | Live's own Help | Live | nothing | Live |
| Help held (0.5 s) | Home | nothing | Home | Home |

"Shift + Help, Help" means: hold Shift, tap Help twice, let go of Shift.
Both gestures fire when Shift is let go. That is the safe moment: Live has
seen Shift go down and up again, so it does not think Shift is still held
(Stacks found that taking the surface mid-press left Live with a stuck
Shift).

Help alone (added 2026-10-19, asked for on device: "always use the help
button to return to Live") goes back to Live from Home or an app, on
Help's release. Shift, or another button pressed while Help is held,
spoils it. In Live and in a tool, push-manager does not take Help. An app
with only the screen (Stacks' listening mode) goes back to Live too; Live
sees that press as well and opens its help screen, which push-manager
closes with one more Help tap into Live's input.

Help held for 0.5 s (asked for on device the same day, "in Live and in
module") is Shift + Help: Home, at once, while Help is still down; its
release is nothing. From Home it is nothing, since going to Live would
hand Live a release it never saw pressed. Where Live had the buttons, its
help screen is closed first (Help up, then a tap), before Home takes the
buttons and the tap could no longer get through.

While an app or Home owns the surface, the intercept is on, so Live does
not see Shift + Help at all. From Live, Live sees Shift + Help, as it does
today with Stacks; Help pressed twice toggles Live's help view twice, so
it ends where it started.

push-manager's other chords (Shift + Settings, Shift + Set) work only when
Live or a tool owns the surface. Inside an app they do nothing, so they
cannot open the Shadow UI over the app.

### Home

Home shows up to eight entries a page, one in each of the screen's eight
columns, each under its top button:

- **Apps**: hacks that take the screen, buttons and pads (Stacks, Riffs).
  A colour stripe, the name, APP, two status lines the app sends
  ("FUNK  EMIN G", "PLAYING"), and its state: RUNNING, PRESS TO START,
  STARTING...
- **Tools**: push-manager's Shadow UI panels (Browser, Plugins, Catalog,
  Files, Stats, MIDI) and any hack's `shadow_ui` tab. Opening one opens the
  Shadow UI on that panel.

Order: the superstar, then the starred entries, then the rest in the
user's order (the same order as the Shadow UI tabs and the web menu,
`ui_tabs.json`). So page 1 is always full, and the favorites are the
first presses.

The top pad row is a quiet hint of the columns: one pad per entry, lit in
its colour (dim when the app is stopped), opens that entry; the selected
entry's pad is white, and every other pad stays dark. (Until 2026-10-19
every pad was lit and any pad in a column opened it: a whole lit grid read
as something to play.)

| Control | In Home |
|---|---|
| Top button 1-8 | open the entry in that column |
| Top pad row 1-8 | open the entry in that column |
| Jog turn / press | choose / open |
| Page < > | the next or previous page |
| Bottom 1 `< LAST` | back to the app you came from |
| Bottom 2 `STAR` | star or unstar the chosen entry (lit amber while starred) |
| Shift + `STAR` | make the chosen entry the superstar (again: no superstar) |
| Bottom 8 `LIVE` | back to Live |

When Home opens, the app you came from is chosen, and tagged LAST.

### When something goes wrong

- **An app stops while it is open** (crash, restart, killed): within a
  second the surface goes to Home, the card says STOPPED, and the bottom
  line says "RIFFS STOPPED -- PRESS IT TO START AGAIN".
- **Press to start:** pressing a stopped app asks push-catalog to start it
  (`/api/enable`, which restarts the service). The card says STARTING...,
  and the app opens by itself as soon as it connects. After 15 seconds
  without it: "RIFFS DID NOT START -- SEE ITS LOG".
- **The power button:** as today (release.go): the surface goes to Live at
  once, and nobody gets it back until the device is off.
- **push-manager restarts:** as today, it gives the surface to Live at its
  start. The apps reconnect by themselves and are listed again.

## How it works

### Who owns the surface

push-manager keeps one owner:

```
Live --(Shift+Help)--> Home --(open app)--> App(id)
  ^                     |  ^                  |
  |                     |  +--(Shift+Help)----+
  +-------(LIVE)--------+  +--(app stops)-----+
Home --(open tool)--> Tool(panel) --(Shift+Help)--> Home
any --(power button)--> Live, and stays Live
```

For each owner, push-manager sets the surface the same way, in one place:

| Owner | Display | Intercept | Who draws | Who gets presses |
|---|---|---|---|---|
| Live | Push3 (mode 0) | off | Push3 | Live |
| Home | mode 2 | on | push-manager (Home) | push-manager |
| Tool | mode 2 | on | push-manager (Shadow UI) | push-manager |
| App | mode 2 | on, with the app's pass-through list | the app | the app, Live for the pass-through buttons |
| App, screen only (Stacks' listen mode) | mode 2 | off | the app | Live |

The intercept stays on through Home -> App -> Home: Live never sees
presses in between.

### Hacks declare an app, then hold a session

In `hack.json`:

```json
"surface": { "label": "RIFFS", "color": "sky" }
```

`label` is ASCII, up to 7 letters to fit the large type. `color` is a
name from Push's palette (`core/push3` NamedColors).

At runtime the app holds one session with push-manager:

| Call | What it does |
|---|---|
| `GET /api/surface` | the owner, the entries, stars, superstar. A 404 means push-manager has no switcher (the app falls back to its old way, or says to update). |
| `GET /api/surface/session?app=ID&pass_cc=...` | Server-sent events, kept open. Events: `ready`, `focus` (`{"input": true}`), `blur`, and a ping every second. While it is open the app is RUNNING. When it closes while the app owns the surface, the surface goes to Home. A new session for the same app replaces the old. |
| `POST /api/surface/status` | `{"app", "lines": [two lines]}`: the card's status. At most twice a second. |
| `POST /api/surface/input` | `{"app", "input": false}`: screen only (listen mode), or back to full. |
| `POST /api/surface/leave` | `{"app", "to": "live"\|"home"}`: the app's own LIVE tab. |
| `POST /api/surface/open` | `{"id"}`: open an entry, for the web UI. Refused while another app owns the surface. |
| `POST /api/surface/star` | `{"id", "star": bool, "super": bool}`: the web UI's switches. |

Frames: an app posts `/api/display/frame` with the header
`X-Push-Hack-App: ID`. Frames from an app that does not own the surface
get 409 and never reach the screen.

`core/pmclient` gets a `Surface` type that does all of this: open the
session, reconnect after a push-manager restart, call `OnFocus` and
`OnBlur`, and send frames and status with the app's id.

### Old hacks

A hack with no session uses `/api/display/mode`, `/api/midi/filter` and
frames without the header, as today. That works while Live owns the
surface; push-manager then counts the owner as "a legacy hack". While
Home, a tool or an app owns the surface, those calls get 409 "the surface
belongs to X". This also covers Stacks 0.2.0, whose own Shift + Help is
now push-manager's: push-manager opens Home and refuses Stacks' takeover,
so the two cannot both draw. Stacks 0.3.0 uses the session.

## Rules the tests hold

1. Exactly one owner at any time.
2. The intercept is on exactly when the owner is Home, a tool, or an app
   with input; the display is in mode 2 exactly when the owner is not
   Live.
3. Only the owning app's frames reach the screen.
4. An owning app's session ending gives the surface to Home within a
   second.
5. After the power button, nothing but Live owns the surface.
6. Inside an app, only Shift + Help (and Shift + Help, Help) and Help do
   anything in push-manager.

Tests:

- The state machine: a table of transitions, and 10,000 random sequences
  of events (gestures, sessions opening and closing, legacy calls, frames,
  the power button), checking the six rules after every event.
- Gestures: Shift down, Help tap, Shift up opens Home; two taps the
  superstar; Shift up with no Help, or Help without Shift, does nothing.
- Sessions over a real HTTP server: focus and blur arrive; closing the
  session gives the surface back; a second session replaces the first.
- `pmclient.Surface` against a test server, reconnect included.
- On the device: Home from Live and back, with Live's Shift not stuck;
  opening Stacks and Riffs; the superstar both ways; stopping Riffs while
  it is open (its own service, never Push3) and seeing Home take over;
  restarting push-manager with an app open.

## Steps

1. push-manager: `surface.go` (owner, transitions, sessions), `home.go`
   (the Home screen and pads), the gesture in the chord detector, chord
   scopes, frame checks, stars in `ui_tabs.json`, star switches on the web
   UI's Display page. `core/pmclient`: `Surface`. Docs: api-reference,
   push-manager README, catalog/schema.md (the `surface` hook), CLAUDE.md
   (display-owning hacks). One PR, stacked on #37.
2. Stacks 0.3.0: a session; its own Shift + Help only when push-manager
   has no switcher; status lines (progression, key).
3. Riffs 0.1.0: a session, no gesture of its own; status lines (flavor,
   chords, playing). Then its release and the catalog entry (and Stacks'
   bump).
