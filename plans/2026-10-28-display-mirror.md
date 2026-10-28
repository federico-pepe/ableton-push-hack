# Display mirror: GET /api/display/mirror

Date: 2026-10-28. Status: done (PR #77).

## Goal

Show what the Push screen shows now, in a browser, while Push Hack is loaded. Like the mirror in push-tethered-app, but Live draws the screen here, not us.

## Options in core

- `display.Shm` `ReadFrame` (`framebuf`): push-manager writes it only. In passthrough it is stale, so it cannot show Live's UI.
- `display.USB`: works only while Push3 is stopped.
- The hook sees every frame sent to the screen. This is the only source of the real picture.

## Decision

- `push_hook.c` copies each outgoing frame (after any bar or takeover change) into a new `mirror` file. Seqlock, one writer. Separate from `framebuf`, so `SHM_VERSION` and the self-update check do not change.
- `core/display.Mirror` maps it read-only and removes the wire XOR.
- push-manager serves a page that fetches one frame at a time. Nothing runs without a viewer.

## What we tried

| Try | Result |
|---|---|
| MJPEG stream (`multipart/x-mixed-replace`) | Worked, but Chrome showed the newest frame one change late. A closing boundary after each frame did not fix it. Removed. |
| Canvas page, one JPEG per fetch | In sync. |
| Lossless PNG (fastest level) | Sharper text and colors. 2.3 ms and 6.9 KB per frame on the device, cheaper than JPEG q95 (3.3 ms, 11 KB). |
| Dedupe by hook frame number | Not enough: the hook sends about 54 frames a second even when the picture is the same. Compare the pixels, and give each picture a version. |
| Cap at 15 reads a second | CPU in a poll test fell from 9-10% to 2.5-5.5%. Looks good enough on device. |

## Open

- The hook could report a new frame number only when the picture changes. This would remove the copy and compare on each poll. It needs another Push3 restart. Not done.
- `/api/display/screenshot` could read the mirror when it exists, so it also shows Live's UI.
