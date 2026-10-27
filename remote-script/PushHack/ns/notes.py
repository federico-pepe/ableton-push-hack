# notes: the Set's scale, drum pads and Simpler slices.

from ..errors import CommandError
from .common import bad, device_of, safe, to_int


def scale(song, msg=None):
    """The Set's scale, the one Push's Scale button edits (Live 12).

    root is 0-11 (0 = C); intervals are semitones from it, as Live keeps
    them; mode is Scale Mode (Push's "In Key"). A Live without the scale API
    answers {"supported": false} rather than an error.
    """
    try:
        return {
            "supported": True,
            "root": int(song.root_note),
            "name": str(song.scale_name),
            "intervals": [int(i) for i in song.scale_intervals],
            "mode": bool(getattr(song, "scale_mode", True)),
        }
    except AttributeError:
        return {"supported": False}


def set_scale(song, msg):
    """Set any of: root (0-11, 0 = C), name (a Live scale name), mode (Scale Mode)."""
    done = []
    if "root" in msg:
        r = to_int(msg["root"])
        if not 0 <= r <= 11:
            raise bad("bad_arg")
        song.root_note = r
        done.append("root")
    if "name" in msg:
        song.scale_name = str(msg["name"])
        done.append("name")
    if "mode" in msg:
        song.scale_mode = bool(msg["mode"])
        done.append("mode")
    if not done:
        raise bad("no_changes")
    return {"changed": done}


# ---- drum pads ----
def _pads(song, msg):
    dev = device_of(song, msg)
    if not safe(lambda: dev.can_have_drum_pads, False):
        raise bad("not_a_drum_rack")
    return dev, list(dev.drum_pads)


def _pad_at(song, msg):
    dev, pads = _pads(song, msg)
    note = to_int(msg.get("note"), "bad_pad")
    for p in pads:
        if p.note == note:
            return dev, p
    raise bad("bad_pad")


def drum_pads(song, msg):
    """The pads of a Drum Rack. `filled: true` lists only pads that hold a chain."""
    dev, pads = _pads(song, msg)
    if msg.get("filled"):
        pads = [p for p in pads if len(safe(lambda: list(p.chains), []))]
    sel = safe(lambda: dev.view.selected_drum_pad)
    return {"pads": [{"note": int(p.note), "name": safe(lambda: p.name, "") or "",
                      "mute": bool(safe(lambda: p.mute, False)),
                      "solo": bool(safe(lambda: p.solo, False)),
                      "chains": len(safe(lambda: list(p.chains), [])),
                      "selected": sel is not None and p == sel} for p in pads]}


def set_drum_pad(song, msg):
    _, p = _pad_at(song, msg)
    done = []
    for key in ("mute", "solo"):
        if key in msg:
            setattr(p, key, bool(msg[key]))
            done.append(key)
    if not done:
        raise bad("no_changes")
    return {"changed": done}


def select_drum_pad(song, msg):
    dev, p = _pad_at(song, msg)
    dev.view.selected_drum_pad = p


def pad_to_track(song, msg):
    """Move a drum pad's chain into a new MIDI track."""
    _, p = _pad_at(song, msg)
    import Live
    Live.Conversions.create_midi_track_from_drum_pad(song, p)


# ---- Simpler slices ----
def _simpler(song, msg):
    dev = device_of(song, msg)
    if not hasattr(dev, "sample") or not hasattr(dev, "playback_mode"):
        raise bad("not_a_simpler")
    return dev


def slices(song, msg):
    dev = _simpler(song, msg)
    sample = dev.sample
    return {"playback_mode": safe(lambda: int(dev.playback_mode)),
            "slicing_mode": safe(lambda: int(dev.slicing_playback_mode)),
            "slices": [int(x) for x in safe(lambda: list(sample.slices), [])],
            "length": safe(lambda: int(sample.length), 0)}


def slices_to_drum_rack(song, msg):
    dev = _simpler(song, msg)
    import Live
    Live.Conversions.sliced_simpler_to_drum_rack(song, dev)


def register(reg):
    reg.command("notes", "scale", scale)
    reg.command("notes", "set_scale", set_scale)
    reg.command("notes", "drum_pads", drum_pads)
    reg.command("notes", "set_drum_pad", set_drum_pad)
    reg.command("notes", "select_drum_pad", select_drum_pad)
    reg.command("notes", "pad_to_track", pad_to_track)
    reg.command("notes", "slices", slices)
    reg.command("notes", "slices_to_drum_rack", slices_to_drum_rack)
    reg.topic("scale", scale, every=5)
