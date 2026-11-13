# arrangement clips: read a clip's content (notes, audio file, markers) and
# edit clips in the arrangement. Commands live in the "arrangement" namespace.
#
# A clip is named by "track" (index into song.tracks) and "start" (its
# start_time in beats). Indexes into arrangement_clips shift after every
# edit; the start time does not.
#
# Live has no split or trim call for arrangement clips. Two things it does
# have make them possible:
#   - Track.duplicate_clip_to_arrangement(clip, time) pastes a copy and
#     OVERWRITES what is under it. A clip partly under the copy keeps the
#     part outside it, content still lined up (same as a paste in Live).
#   - Track.delete_clip(clip).
# Split, trim and move are built from these two. Copies that need shaping are
# made in a scratch area past the track's last clip, then removed. Nothing
# outside the clip's own range is touched.

from ..errors import CommandError
from .common import bad, clamp, safe, to_float, to_int, track_of

EPS = 1e-3
MAX_INFO_CLIPS = 8
MAX_NOTES = 512
SCRATCH_GAP = 64.0     # beats between the track's last clip and the scratch area
MAX_TIME = 1576800.0   # Live refuses clip positions past this
MIN_LEN = 1.0 / 64


# ---- finding clips ----
def _arr_track(song, msg, key="track"):
    t = track_of(song, msg.get(key))
    if safe(lambda: t.is_foldable, False) or not hasattr(t, "arrangement_clips"):
        raise bad("bad_track")
    return t


def _clips(track):
    return list(safe(lambda: track.arrangement_clips, []) or [])


def _at(track, start):
    for c in _clips(track):
        if abs(float(c.start_time) - start) < EPS:
            return c
    return None


def _clip(song, msg):
    track = _arr_track(song, msg)
    start = to_float(msg.get("start"))
    c = _at(track, start)
    if c is None:
        raise bad("no_clip")
    return track, c


def _ref(song, track, clip):
    tracks = list(song.tracks)
    i = -1
    for n, t in enumerate(tracks):
        if t == track:
            i = n
            break
    return {"track": i, "start": round(float(clip.start_time), 4),
            "end": round(float(clip.end_time), 4)}


def _is_midi(clip):
    return bool(safe(lambda: clip.is_midi_clip, False))


# ---- undo grouping ----
class _UndoStep(object):
    """One Undo press undoes the whole edit, where Live has begin_undo_step."""

    def __init__(self, song):
        self.song = song

    def __enter__(self):
        fn = getattr(self.song, "begin_undo_step", None)
        if fn:
            safe(fn)

    def __exit__(self, *exc):
        fn = getattr(self.song, "end_undo_step", None)
        if fn:
            safe(fn)
        return False


# ---- clip info (what the hack draws) ----
def _notes(clip):
    lo = min(float(clip.start_marker), float(clip.loop_start))
    hi = max(float(clip.end_marker), float(clip.loop_end))
    ns = list(clip.get_notes_extended(0, 128, lo, max(EPS, hi - lo)))
    ns.sort(key=lambda n: n.start_time)
    out = [[int(n.pitch), round(float(n.start_time), 4), round(float(n.duration), 4),
            int(n.velocity), 1 if n.mute else 0] for n in ns[:MAX_NOTES]]
    return out, len(ns)


def _warp_markers(clip):
    out = []
    for m in safe(lambda: list(clip.warp_markers), []) or []:
        bt = safe(lambda: float(m.beat_time))
        st = safe(lambda: float(m.sample_time))
        if bt is None or st is None:
            continue
        out.append([round(bt, 5), round(st, 6)])
    return out


def clip_info(clip):
    midi = _is_midi(clip)
    out = {
        "kind": "midi" if midi else "audio",
        "start": round(float(clip.start_time), 4),
        "end": round(float(clip.end_time), 4),
        "start_marker": float(safe(lambda: clip.start_marker, 0.0)),
        "end_marker": float(safe(lambda: clip.end_marker, 0.0)),
        "loop_start": float(safe(lambda: clip.loop_start, 0.0)),
        "loop_end": float(safe(lambda: clip.loop_end, 0.0)),
        "looping": bool(safe(lambda: clip.looping, False)),
        "muted": bool(safe(lambda: clip.muted, False)),
        "color_index": safe(lambda: int(clip.color_index), -1),
    }
    if midi:
        notes, total = safe(lambda: _notes(clip), ([], 0))
        out["notes"], out["notes_total"] = notes, total
    else:
        out.update(
            file=safe(lambda: str(clip.file_path), "") or "",
            warping=bool(safe(lambda: clip.warping, False)),
            warp_markers=_warp_markers(clip),
            sample_rate=float(safe(lambda: clip.sample_rate, 0.0) or 0.0),
            sample_length=int(safe(lambda: clip.sample_length, -1)),
            gain=float(safe(lambda: clip.gain, 0.0)),
            pitch_coarse=int(safe(lambda: clip.pitch_coarse, 0)))
    return out


def clips_info(song, msg):
    """Content of up to 8 clips: {clips: [{track, start}]} -> {clips: [info|null]}."""
    items = msg.get("clips")
    if not isinstance(items, list) or len(items) > MAX_INFO_CLIPS:
        raise bad("bad_arg")
    out = []
    for it in items:
        if not isinstance(it, dict):
            out.append(None)
            continue
        try:
            _, c = _clip(song, it)
            out.append(clip_info(c))
        except CommandError:
            out.append(None)
    return {"clips": out}


# ---- building blocks ----
def _scratch(track):
    end = 0.0
    for c in _clips(track):
        end = max(end, float(c.end_time))
    t = end + SCRATCH_GAP
    if t > MAX_TIME / 2:
        raise bad("too_big")
    return t


def _dup(track, clip, at):
    """Paste a copy of clip at `at` on track. Returns the copy."""
    if at < 0 or at > MAX_TIME:
        raise bad("bad_arg")
    track.duplicate_clip_to_arrangement(clip, at)
    c = _at(track, at)
    if c is None:
        raise bad("failed")
    return c


def _delete_from(track, t0):
    """Delete every clip that starts at or after t0 (the scratch area)."""
    for c in _clips(track):
        if float(c.start_time) >= t0 - EPS:
            track.delete_clip(c)


def _delete_inside(track, a, b, keep=None):
    """Delete the clips that lie fully inside [a, b), except `keep`."""
    for c in _clips(track):
        if keep is not None and c == keep:
            continue
        if float(c.start_time) >= a - EPS and float(c.end_time) <= b + EPS:
            track.delete_clip(c)


def _filler(track, clip, length, base):
    """A clip of exactly `length` beats at `base` (scratch area). Its content
    does not matter: it is only pasted to cut another clip."""
    f = _dup(track, clip, base)
    if float(f.end_time) - base > length + EPS:
        # cut its tail: paste another copy over it, then remove that copy
        _dup(track, clip, base + length)
        _delete_from(track, base + length)
        f = _at(track, base)
    if f is None or abs(float(f.end_time) - base - length) > EPS:
        raise bad("failed")
    return f


def _cut_tail(track, clip, at, base):
    """Make clip end at `at` (start < at < end). Returns the clip."""
    start, end = float(clip.start_time), float(clip.end_time)
    f = _filler(track, clip, end - at, base)
    _dup(track, f, at)
    _delete_inside(track, at, end)
    _delete_from(track, base)
    c = _at(track, start)
    if c is None or abs(float(c.end_time) - at) > EPS:
        raise bad("failed")
    return c


def _cut_head(track, clip, at, base):
    """Make clip start at `at` (start < at < end), content still in place."""
    start, end = float(clip.start_time), float(clip.end_time)
    f = _filler(track, clip, at - start, base)
    _dup(track, f, start)
    _delete_inside(track, start, at)
    _delete_from(track, base)
    c = _at(track, at)
    if c is None or abs(float(c.end_time) - end) > EPS:
        raise bad("failed")
    return c


def _inside(clip, t):
    return float(clip.start_time) + MIN_LEN <= t <= float(clip.end_time) - MIN_LEN


def _beats_to_clip_units(song, clip, beats):
    """Clip markers are in seconds for unwarped audio, else in beats."""
    if not _is_midi(clip) and not safe(lambda: clip.warping, True):
        return beats * 60.0 / float(song.tempo)
    return beats


def _next_start(track, clip):
    end = float(clip.start_time)
    nxt = None
    for c in _clips(track):
        s = float(c.start_time)
        if s > end + EPS and (nxt is None or s < nxt):
            nxt = s
    return nxt


# ---- edit commands ----
def clip_delete(song, msg):
    track, c = _clip(song, msg)
    with _UndoStep(song):
        track.delete_clip(c)


def clip_move(song, msg):
    """Move a clip to `to` (beats), on `to_track` if given. Overwrites what is
    there, like a drag in Live."""
    track, c = _clip(song, msg)
    dest = _arr_track(song, msg, "to_track") if msg.get("to_track") is not None else track
    to = max(0.0, to_float(msg.get("to", c.start_time)))
    start, end = float(c.start_time), float(c.end_time)
    if dest == track and abs(to - start) < EPS:
        return {"clip": _ref(song, track, c)}
    with _UndoStep(song):
        new = _dup(dest, c, to)
        if dest == track:
            # what is left of the original sits inside its old range
            _delete_inside(track, start, end, keep=new)
        else:
            track.delete_clip(c)
        return {"clip": _ref(song, dest, new)}


def clip_duplicate(song, msg):
    """Copy a clip to `to` (default: right after it), on `to_track` if given."""
    track, c = _clip(song, msg)
    dest = _arr_track(song, msg, "to_track") if msg.get("to_track") is not None else track
    to = max(0.0, to_float(msg.get("to", c.end_time)))
    with _UndoStep(song):
        new = _dup(dest, c, to)
        return {"clip": _ref(song, dest, new)}


def clip_split(song, msg):
    """Split a clip in two at `at` (beats)."""
    track, c = _clip(song, msg)
    at = to_float(msg.get("at"))
    if not _inside(c, at):
        raise bad("bad_arg")
    start, length = float(c.start_time), float(c.end_time) - float(c.start_time)
    with _UndoStep(song):
        base = _scratch(track)
        work = base + length + SCRATCH_GAP                # fillers go past the copy
        copy = _dup(track, c, base)                       # whole clip, in scratch
        right = _cut_head(track, copy, base + (at - start), work)
        _cut_tail(track, c, at, work)                     # original keeps [start, at)
        new = _dup(track, right, at)                      # right part fills [at, end)
        _delete_from(track, base)
        left = _at(track, start)
        return {"clip": _ref(song, track, new),
                "left": _ref(song, track, left) if left is not None else None}


def clip_trim(song, msg):
    """Move a clip's start and/or end edge: `new_start`, `new_end` (beats).
    Shortening works for every clip. Lengthening the end works for clips that
    do not loop. The start edge can only move right (shorten)."""
    track, c = _clip(song, msg)
    start, end = float(c.start_time), float(c.end_time)
    ns = to_float(msg.get("new_start", start))
    ne = to_float(msg.get("new_end", end))
    if ne - ns < MIN_LEN or ns < start - EPS:
        raise bad("bad_arg")
    with _UndoStep(song):
        if ne < end - EPS:
            c = _cut_tail(track, c, ne, _scratch(track))
        elif ne > end + EPS:
            c = _extend(song, track, c, ne)
        if ns > start + EPS:
            c = _cut_head(track, c, ns, _scratch(track))
        return {"clip": _ref(song, track, c)}


def _extend(song, track, c, ne):
    if safe(lambda: c.looping, False):
        raise bad("looping")
    nxt = _next_start(track, c)
    if nxt is not None:
        ne = min(ne, nxt)
    start, end = float(c.start_time), float(c.end_time)
    add = _beats_to_clip_units(song, c, ne - end)
    if add <= 0:
        return c
    new_end = float(c.loop_end) + add
    c.loop_end = new_end
    if float(c.end_marker) < new_end - EPS:
        safe(lambda: setattr(c, "end_marker", new_end))
    c2 = _at(track, start) or c
    if float(c2.end_time) < end + EPS:
        raise bad("failed")  # Live did not grow it (audio file too short)
    return c2


SETTABLE = {
    "muted": bool,
    "name": lambda v: str(v)[:64],
    "color_index": lambda v: clamp(to_int(v), 0, 69),
    "gain": lambda v: clamp(to_float(v), 0.0, 1.0),
    "pitch_coarse": lambda v: clamp(to_int(v), -48, 48),
}


def clip_set(song, msg):
    track, c = _clip(song, msg)
    done = []
    with _UndoStep(song):
        for key, conv in SETTABLE.items():
            if key not in msg:
                continue
            if key in ("gain", "pitch_coarse") and _is_midi(c):
                raise bad("wrong_clip_kind")
            setattr(c, key, conv(msg[key]))
            done.append(key)
    if not done:
        raise bad("no_changes")
    return {"changed": done, "clip": _ref(song, track, c)}


def _all_notes(c):
    fn = getattr(c, "get_all_notes_extended", None)
    if fn is not None:
        return list(fn())
    return list(c.get_notes_extended(0, 128, -16384.0, 32768.0))


def clip_transpose(song, msg):
    """Shift pitch by `semitones`: the notes of a MIDI clip, the pitch of an
    audio clip."""
    track, c = _clip(song, msg)
    n = to_int(msg.get("semitones"))
    if n == 0:
        raise bad("no_changes")
    with _UndoStep(song):
        if _is_midi(c):
            notes = _all_notes(c)
            for x in notes:
                x.pitch = clamp(int(x.pitch) + n, 0, 127)
            c.apply_note_modifications(notes)
        else:
            c.pitch_coarse = clamp(int(c.pitch_coarse) + n, -48, 48)
    return {"clip": _ref(song, track, c)}


def clip_velocity(song, msg):
    """Add `delta` to the velocity of every note of a MIDI clip."""
    track, c = _clip(song, msg)
    if not _is_midi(c):
        raise bad("wrong_clip_kind")
    d = to_float(msg.get("delta"))
    with _UndoStep(song):
        notes = _all_notes(c)
        for x in notes:
            x.velocity = clamp(float(x.velocity) + d, 1.0, 127.0)
        c.apply_note_modifications(notes)
    return {"clip": _ref(song, track, c)}


def clip_quantize(song, msg):
    """Quantize: `grid` is Live's grid index (default 5 = 1/16), `amount` 0..1."""
    track, c = _clip(song, msg)
    with _UndoStep(song):
        c.quantize(to_int(msg.get("grid", 5)), clamp(to_float(msg.get("amount", 1.0)), 0.0, 1.0))
    return {"clip": _ref(song, track, c)}


def clip_crop(song, msg):
    track, c = _clip(song, msg)
    with _UndoStep(song):
        c.crop()
    return {"clip": _ref(song, track, c)}


def clip_new(song, msg):
    """New empty MIDI clip at `start` with `length` beats (MIDI tracks only)."""
    track = _arr_track(song, msg)
    if not safe(lambda: track.has_midi_input, False):
        raise bad("wrong_track_kind")
    start = max(0.0, to_float(msg.get("start")))
    length = clamp(to_float(msg.get("length", 4.0)), MIN_LEN, 4096.0)
    with _UndoStep(song):
        track.create_midi_clip(start, length)
    c = _at(track, start)
    if c is None:
        raise bad("failed")
    return {"clip": _ref(song, track, c)}


def register(reg):
    reg.command("arrangement", "clips_info", clips_info)
    reg.command("arrangement", "clip_delete", clip_delete)
    reg.command("arrangement", "clip_move", clip_move)
    reg.command("arrangement", "clip_duplicate", clip_duplicate)
    reg.command("arrangement", "clip_split", clip_split)
    reg.command("arrangement", "clip_trim", clip_trim)
    reg.command("arrangement", "clip_set", clip_set)
    reg.command("arrangement", "clip_transpose", clip_transpose)
    reg.command("arrangement", "clip_velocity", clip_velocity)
    reg.command("arrangement", "clip_quantize", clip_quantize)
    reg.command("arrangement", "clip_crop", clip_crop)
    reg.command("arrangement", "clip_new", clip_new)
