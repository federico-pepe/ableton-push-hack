# notes: scale and root note. Drum pads and slicing arrive in phase 4.


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


def register(reg):
    reg.command("notes", "scale", scale)
    reg.topic("scale", scale)
