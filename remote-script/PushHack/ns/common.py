# Shared helpers for the namespaces: resolve tracks, scenes, clips and devices
# from the references a client sends, and read numbers safely. No Live imports.
#
# References:
#   track   an index into song.tracks, "master", "return:N" (return track N),
#           or "selected"
#   scene   an index into song.scenes
#   device  a "device" index on the track, or a "path" list: [device, chain,
#           device, ...] to walk into racks

from ..errors import CommandError


def bad(code):
    return CommandError(code)


def to_int(value, code="bad_arg"):
    try:
        if isinstance(value, bool):
            raise ValueError
        return int(value)
    except (TypeError, ValueError):
        raise bad(code)


def to_float(value, code="bad_arg"):
    try:
        if isinstance(value, bool):
            raise ValueError
        return float(value)
    except (TypeError, ValueError):
        raise bad(code)


def clamp(v, lo, hi):
    return max(lo, min(hi, v))


def track_of(song, ref):
    """The Track a client names. Raises bad_track."""
    if ref == "master":
        return song.master_track
    if ref == "selected":
        return song.view.selected_track
    if isinstance(ref, str) and ref.startswith("return:"):
        i = to_int(ref[len("return:"):], "bad_track")
        rts = list(song.return_tracks)
        if not 0 <= i < len(rts):
            raise bad("bad_track")
        return rts[i]
    i = to_int(ref, "bad_track")
    tracks = list(song.tracks)
    if not 0 <= i < len(tracks):
        raise bad("bad_track")
    return tracks[i]


def scene_of(song, ref):
    i = to_int(ref, "bad_scene")
    scenes = list(song.scenes)
    if not 0 <= i < len(scenes):
        raise bad("bad_scene")
    return scenes[i]


def slot_of(song, msg):
    """The ClipSlot at msg["track"] x msg["scene"]."""
    track = track_of(song, msg.get("track"))
    slots = getattr(track, "clip_slots", None)
    if slots is None:
        raise bad("bad_track")  # master and return tracks have no slots
    i = to_int(msg.get("scene"), "bad_scene")
    slots = list(slots)
    if not 0 <= i < len(slots):
        raise bad("bad_scene")
    return slots[i]


def clip_of(song, msg):
    """The Clip at track x scene, or the detail clip when msg has "detail"."""
    if msg.get("detail"):
        clip = song.view.detail_clip
    else:
        slot = slot_of(song, msg)
        clip = slot.clip if slot.has_clip else None
    if clip is None:
        raise bad("no_clip")
    return clip


def device_of(song, msg):
    """The Device at msg["track"] and msg["device"] (or msg["path"])."""
    track = track_of(song, msg.get("track"))
    path = msg.get("path")
    if path is None:
        path = [msg.get("device")]
    if not isinstance(path, list) or not path:
        raise bad("bad_device")
    devices = list(track.devices)
    dev = None
    for step, idx in enumerate(path):
        i = to_int(idx, "bad_device")
        if step % 2 == 0:  # a device in the current list
            if not 0 <= i < len(devices):
                raise bad("bad_device")
            dev = devices[i]
            devices = None
        else:              # a chain of the device found so far
            chains = list(getattr(dev, "chains", []))
            if not 0 <= i < len(chains):
                raise bad("bad_device")
            devices = list(chains[i].devices)
    if devices is not None and dev is not None and len(path) % 2 == 0:
        raise bad("bad_device")  # a path must end on a device
    if dev is None:
        raise bad("bad_device")
    return dev


def index_of(seq, item):
    for i, x in enumerate(seq):
        if x == item:
            return i
    return -1


def color_int(obj):
    try:
        return int(obj.color)
    except Exception:
        return 0


def safe(fn, default=None):
    try:
        return fn()
    except Exception:
        return default
