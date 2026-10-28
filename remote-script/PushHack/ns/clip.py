# clip: one clip's properties, loop, notes and conversion to MIDI.
#
# A clip is named by "track" and "scene" (the session slot), or "detail": true
# for the clip shown in Live's detail view.

from ..errors import CommandError
from .common import (bad, clamp, clip_of, color_int, safe, to_float, to_int)

MAX_NOTES = 2000

# Writable properties: name -> (conversion, applies to). `audio`/`midi` limit
# a property to that kind of clip.
SETTABLE = {
    "name": (lambda v: str(v)[:64], None),
    "color": (lambda v: to_int(v) & 0xFFFFFF, None),
    "looping": (bool, None),
    "muted": (bool, None),
    "loop_start": (to_float, None),
    "loop_end": (to_float, None),
    "start_marker": (to_float, None),
    "end_marker": (to_float, None),
    "launch_mode": (to_int, None),
    "launch_quantization": (to_int, None),
    "legato": (bool, None),
    "velocity_amount": (lambda v: clamp(to_float(v), -1.0, 1.0), "midi"),
    "gain": (lambda v: clamp(to_float(v), 0.0, 1.0), "audio"),
    "pitch_coarse": (lambda v: clamp(to_int(v), -48, 48), "audio"),
    "pitch_fine": (lambda v: clamp(to_int(v), -50, 50), "audio"),
    "warping": (bool, "audio"),
    "warp_mode": (to_int, "audio"),
}


def get(song, msg):
    c = clip_of(song, msg)
    midi = bool(safe(lambda: c.is_midi_clip, False))
    out = {
        "name": safe(lambda: c.name, "") or "", "color": color_int(c),
        "kind": "midi" if midi else "audio",
        "length": float(safe(lambda: c.length, 0.0)),
        "looping": bool(safe(lambda: c.looping, False)),
        "loop_start": float(safe(lambda: c.loop_start, 0.0)),
        "loop_end": float(safe(lambda: c.loop_end, 0.0)),
        "start_marker": float(safe(lambda: c.start_marker, 0.0)),
        "end_marker": float(safe(lambda: c.end_marker, 0.0)),
        "muted": bool(safe(lambda: c.muted, False)),
        "playing": bool(safe(lambda: c.is_playing, False)),
        "triggered": bool(safe(lambda: c.is_triggered, False)),
        "recording": bool(safe(lambda: c.is_recording, False)),
        "position": float(safe(lambda: c.playing_position, 0.0)),
        "signature": [safe(lambda: int(c.signature_numerator), 4),
                      safe(lambda: int(c.signature_denominator), 4)],
        "launch_mode": safe(lambda: int(c.launch_mode)),
        "launch_quantization": safe(lambda: int(c.launch_quantization)),
        "legato": bool(safe(lambda: c.legato, False)),
        "has_envelopes": bool(safe(lambda: c.has_envelopes, False)),
    }
    if midi:
        out["velocity_amount"] = float(safe(lambda: c.velocity_amount, 0.0))
    else:
        out.update(gain=float(safe(lambda: c.gain, 0.0)),
                   gain_display=safe(lambda: str(c.gain_display_string), ""),
                   pitch_coarse=safe(lambda: int(c.pitch_coarse), 0),
                   pitch_fine=safe(lambda: int(c.pitch_fine), 0),
                   warping=bool(safe(lambda: c.warping, False)),
                   warp_mode=safe(lambda: int(c.warp_mode)),
                   file=safe(lambda: str(c.file_path), ""))
    return out


def set_(song, msg):
    c = clip_of(song, msg)
    midi = bool(safe(lambda: c.is_midi_clip, False))
    done = []
    for key, (conv, only) in SETTABLE.items():
        if key not in msg:
            continue
        if (only == "midi" and not midi) or (only == "audio" and midi):
            raise bad("wrong_clip_kind")
        setattr(c, key, conv(msg[key]))
        done.append(key)
    if not done:
        raise bad("no_changes")
    return {"changed": done}


def fire(song, msg):
    clip_of(song, msg).fire()


def stop(song, msg):
    clip_of(song, msg).stop()


def quantize(song, msg):
    """Quantize a MIDI clip's notes: `grid` is Live's grid index, `amount` 0..1."""
    c = clip_of(song, msg)
    if not safe(lambda: c.is_midi_clip, False):
        raise bad("wrong_clip_kind")
    c.quantize(to_int(msg.get("grid")), clamp(to_float(msg.get("amount", 1.0)), 0.0, 1.0))


def crop(song, msg):
    clip_of(song, msg).crop()


def duplicate_loop(song, msg):
    c = clip_of(song, msg)
    if not safe(lambda: c.looping, False):
        raise bad("not_looping")
    c.duplicate_loop()


def _note(n):
    return {"id": safe(lambda: int(n.note_id)), "pitch": int(n.pitch),
            "start": round(float(n.start_time), 5), "duration": round(float(n.duration), 5),
            "velocity": float(n.velocity), "mute": bool(n.mute),
            "probability": safe(lambda: float(n.probability), 1.0)}


def _range(c, msg):
    return (clamp(to_int(msg.get("from_pitch", 0)), 0, 127),
            clamp(to_int(msg.get("pitch_span", 128)), 1, 128),
            to_float(msg.get("from_time", 0.0)),
            to_float(msg.get("time_span", safe(lambda: c.length, 4.0))))


def notes(song, msg):
    c = clip_of(song, msg)
    if not safe(lambda: c.is_midi_clip, False):
        raise bad("wrong_clip_kind")
    ns = list(c.get_notes_extended(*_range(c, msg)))
    return {"notes": [_note(n) for n in ns[:MAX_NOTES]], "total": len(ns)}


def _spec_class():
    import Live
    return Live.Clip.MidiNoteSpecification


def add_notes(song, msg):
    """Add notes: [{pitch, start, duration, velocity, mute?}]."""
    c = clip_of(song, msg)
    if not safe(lambda: c.is_midi_clip, False):
        raise bad("wrong_clip_kind")
    items = msg.get("notes")
    if not isinstance(items, list) or not items or len(items) > MAX_NOTES:
        raise bad("bad_arg")
    cls = _spec_class()
    specs = []
    for n in items:
        specs.append(cls(pitch=clamp(to_int(n.get("pitch")), 0, 127),
                         start_time=max(0.0, to_float(n.get("start"))),
                         duration=max(0.001, to_float(n.get("duration", 0.25))),
                         velocity=clamp(to_float(n.get("velocity", 100)), 1.0, 127.0),
                         mute=bool(n.get("mute", False))))
    c.add_new_notes(tuple(specs))
    return {"added": len(specs)}


def remove_notes(song, msg):
    c = clip_of(song, msg)
    if not safe(lambda: c.is_midi_clip, False):
        raise bad("wrong_clip_kind")
    c.remove_notes_extended(*_range(c, msg))


# ---- conversion (Live.Conversions) ----
CONVERSIONS = ("harmony", "melody", "drums", "simpler", "drum_rack")


def _conversions():
    import Live
    return Live.Conversions


def convert_options(song, msg):
    c = clip_of(song, msg)
    if safe(lambda: c.is_midi_clip, False):
        return {"options": []}
    ok = bool(safe(lambda: _conversions().is_convertible_to_midi(song, c), False))
    return {"options": list(CONVERSIONS) if ok else ["simpler", "drum_rack"]}


def convert(song, msg):
    """Convert an audio clip: harmony/melody/drums (to a MIDI clip), simpler
    (new MIDI track with Simpler), drum_rack (new MIDI track with a Drum Rack)."""
    c = clip_of(song, msg)
    if safe(lambda: c.is_midi_clip, False):
        raise bad("wrong_clip_kind")
    kind = msg.get("kind")
    conv = _conversions()
    if kind in ("harmony", "melody", "drums"):
        t = getattr(conv.AudioToMidiType, kind + "_to_midi")
        conv.audio_to_midi_clip(song, c, t)
    elif kind == "simpler":
        conv.create_midi_track_with_simpler(song, c)
    elif kind == "drum_rack":
        conv.create_drum_rack_from_audio_clip(song, c)
    else:
        raise bad("bad_arg")


def register(reg):
    reg.command("clip", "get", get)
    reg.command("clip", "set", set_)
    reg.command("clip", "fire", fire)
    reg.command("clip", "stop", stop)
    reg.command("clip", "quantize", quantize)
    reg.command("clip", "crop", crop)
    reg.command("clip", "duplicate_loop", duplicate_loop)
    reg.command("clip", "notes", notes)
    reg.command("clip", "add_notes", add_notes)
    reg.command("clip", "remove_notes", remove_notes)
    reg.command("clip", "convert_options", convert_options)
    reg.command("clip", "convert", convert)
