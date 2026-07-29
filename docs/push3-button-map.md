# Push 3 — Button / Encoder MIDI Map

Empirically verified on Push 3 hardware via MIDI Monitor.  
All messages are **channel 1** (0-indexed: channel 0).  
Channel in MIDI byte = `0x90` (Note On), `0x80` (Note Off), `0xB0` (CC).

---

## Screen buttons

| Position | Name | Type | Number | Notes |
|----------|------|------|--------|-------|
| Top row, left→right | Screen top 1–8 | CC | 102–109 | Press=127, release=0 |
| Bottom row, left→right | Screen bottom 1–8 | CC | 20–27 | Press=127, release=0 |

## Encoders

| Encoder | Touch (Note On/Off) | Rotate (CC) |
|---------|---------------------|-------------|
| Encoder 1 | Note 1 | CC 71 |
| Encoder 2 | Note 2 | CC 72 |
| Encoder 3 | Note 3 | CC 73 |
| Encoder 4 | Note 4 | CC 74 |
| Encoder 5 | Note 5 | CC 75 |
| Encoder 6 | Note 6 | CC 76 |
| Encoder 7 | Note 7 | CC 77 |
| Encoder 8 | Note 8 | CC 78 |
| Volume wheel | Note 9 | CC 79 |
| Tempo wheel | Note 10 | CC 14 |

> Touch: velocity 127 on contact, 0 on release (Note Off).  
> Rotation is **relative** (delta), same encoding as the jog wheel: CW=127, CCW=1.

## Jog wheel (main)

| Gesture | Type | Number | Value |
|---------|------|--------|-------|
| Rotate CW | CC | 70 | 127 |
| Rotate CCW | CC | 70 | 1 |
| Touch | Note On | 11 | 127 |
| Press | CC | 94 | 127 / 0 |
| Click left | CC | 93 | 127 / 0 |
| Click right | CC | 95 | 127 / 0 |

## Top-right cluster

| Button | CC |
|--------|----|
| Set | 80 |
| Settings | 30 |
| Help | 81 |
| User Mode | 59 |

## View buttons (right side)

| Button | CC |
|--------|----|
| Device View | 110 |
| Mixer View | 112 |
| Clip View | 113 |
| Session View | 34 |

## Edit

| Button | CC |
|--------|----|
| Undo | 119 |
| Save | 82 |
| Add | 32 |
| Swap | 33 |

## Track

| Button | CC |
|--------|----|
| Lock | 83 |
| Stop Clips | 29 |
| Mute | 60 |
| Solo | 61 |
| Select main channel | 28 |

## Transport / global

| Button | CC |
|--------|----|
| Tap Tempo | 3 |
| Metronome | 9 |
| Quantize | 116 |
| Fixed Length | 90 |
| Automate | 89 |
| New | 92 |
| Capture | 65 |
| Record | 86 |
| Play | 85 |

## Scene / step resolution (left column)

| Button | CC |
|--------|----|
| 1/4 | 36 |
| 1/4t | 37 |
| 1/8 | 38 |
| 1/8t | 39 |
| 1/16 | 40 |
| 1/16t | 41 |
| 1/32 | 42 |
| 1/32t | 43 |

## Mode buttons

| Button | CC |
|--------|----|
| Repeat | 56 |
| Accent | 57 |
| Scale | 58 |
| Layout | 31 |
| Note | 50 |
| Session | 51 |

## Loop / clip actions

| Button | CC |
|--------|----|
| Double Loop | 117 |
| Duplicate | 88 |
| Convert | 35 |
| Delete | 118 |

## Navigation

| Button | CC |
|--------|----|
| Octave Up | 55 |
| Octave Down | 54 |
| Page Left | 62 |
| Page Right | 63 |

## Modifiers

| Button | CC |
|--------|----|
| Shift | 49 |
| Select | 48 |

## D-Pad

| Gesture | Type | Number | Value |
|---------|------|--------|-------|
| Up | CC | 46 | 127/0 |
| Right | CC | 45 | 127/0 |
| Down | CC | 47 | 127/0 |
| Left | CC | 44 | 127/0 |
| Center press | CC | 91 | 127/0 |
| Center touch | Note On | 13 | 127 |

---

## Pad grid (8×8)

Push 3 pads send **Note On/Off on channel 1** (0-indexed ch 0).  
Note numbers follow the same layout as Push 2 — bottom-left = 36, top-right = 99.

```
Row 8 (top):  92  93  94  95  96  97  98  99
Row 7:        84  85  86  87  88  89  90  91
Row 6:        76  77  78  79  80  81  82  83
Row 5:        68  69  70  71  72  73  74  75
Row 4:        60  61  62  63  64  65  66  67
Row 3:        52  53  54  55  56  57  58  59
Row 2:        44  45  46  47  48  49  50  51
Row 1 (bot):  36  37  38  39  40  41  42  43
              L                               R
```

Velocity = pressure (1–127). Aftertouch = poly pressure (0xA0).

---

## Notes

- All values confirmed on Push 3 firmware `Push3@2.4.5b8`
- CC value 127 = button pressed, 0 = released (for all buttons above unless noted).
- Jog wheel rotation is **relative** (delta): CW=127, CCW=1. All encoders use the same encoding.
- Push 2 spec CC numbers mostly match Push 3 but are not identical — use this map, not the Push 2 doc.
