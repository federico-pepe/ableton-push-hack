# arrangement: the Set's arrangement (tracks, clips, locators) and the position,
# for the arrangement hack. Built from a Song-like object, so tests use a fake.
#
# The snapshot costs time on a big Set, so a Feed rebuilds it at most about once
# a second and waits longer after a slow build (BUILD_BUDGET_TICKS_PER_SEC).
# Loop and punch state are NOT in the snapshot: they live in the position,
# which is cheap and sent when it changes, so a loop toggle or a knob turn
# shows up almost at once.

from . import transport

SNAPSHOT_EVERY_TICKS = 10         # update_display runs ~10x/s -> ~1 build/s
BUILD_BUDGET_TICKS_PER_SEC = 200  # wait (build seconds x 200) ticks: <= ~5% of the time


def _safe(fn, default=None):
    try:
        return fn()
    except Exception:
        return default


def build_snapshot(song):
    all_tracks = list(song.tracks)
    tracks = []
    for t in all_tracks:
        if _safe(lambda: t.is_foldable, False):
            kind = "group"
        elif _safe(lambda: t.has_midi_input, False):
            kind = "midi"
        else:
            kind = "audio"
        group = -1
        g = _safe(lambda: t.group_track)
        if g is not None:
            group = _safe(lambda: all_tracks.index(g), -1)
        clips = []
        if kind != "group":
            for c in _safe(lambda: list(t.arrangement_clips), []):
                clips.append([
                    round(float(c.start_time), 4),
                    round(float(c.end_time), 4),
                    _safe(lambda: c.name, "") or "",
                    _safe(lambda: int(c.color), 0),
                ])
        tracks.append({
            "name": _safe(lambda: t.name, "") or "",
            "kind": kind,
            "color": _safe(lambda: int(t.color), 0),
            "group": group,
            "clips": clips,
        })
    locators = []
    for cp in _safe(lambda: list(song.cue_points), []):
        locators.append({"time": round(float(cp.time), 4), "name": _safe(lambda: cp.name, "") or ""})
    return {
        "tracks": tracks,
        "locators": locators,
        "sig": [_safe(lambda: int(song.signature_numerator), 4),
                _safe(lambda: int(song.signature_denominator), 4)],
        "tempo": float(_safe(lambda: song.tempo, 120.0)),
    }


def build_position(song):
    return {
        "time": round(float(song.current_song_time), 3),
        "playing": bool(song.is_playing),
        "bta": bool(_safe(lambda: song.back_to_arranger, False)),
        "loop_on": bool(_safe(lambda: song.loop, False)),
        "loop_start": round(float(_safe(lambda: song.loop_start, 0.0)), 4),
        "loop_length": round(float(_safe(lambda: song.loop_length, 4.0)), 4),
        "punch_in": bool(_safe(lambda: song.punch_in, False)),
        "punch_out": bool(_safe(lambda: song.punch_out, False)),
    }


class Feed(object):
    """Rate-limited snapshot shared by every subscriber."""

    def __init__(self, log=None, clock=None):
        import time
        self._log = log or (lambda m: None)
        self._clock = clock or time.time
        self._tick = 0
        self._next = 0
        self._cache = None

    def invalidate(self):
        self._next = 0

    def snapshot(self, song):
        self._tick += 1
        if self._cache is None or self._tick >= self._next:
            t0 = self._clock()
            self._cache = build_snapshot(song)
            took = self._clock() - t0
            self._next = self._tick + max(SNAPSHOT_EVERY_TICKS, int(took * BUILD_BUDGET_TICKS_PER_SEC))
            if took > 0.1:
                self._log("PushHack: arrangement snapshot took %.0f ms; next in %d ticks"
                          % (took * 1000, self._next - self._tick))
        return self._cache

    def position(self, song):
        return build_position(song)


# ---- commands ----
def back_to_arrangement(song):
    # Same call Push's own script uses: writing 0 triggers it.
    if hasattr(song, "back_to_arranger"):
        song.back_to_arranger = 0
        return
    for t in list(song.tracks):
        try:
            if hasattr(t, "back_to_arranger"):
                t.back_to_arranger = 0
        except Exception:
            pass


def _bta(song, msg):
    back_to_arrangement(song)


def _toggle(attr):
    def fn(song, msg):
        setattr(song, attr, not getattr(song, attr))
    return fn


# Old arrangement protocol: {"t": kind, ...}. Only the last of these matters
# when several arrive in one tick (jog wheel, loop knobs).
COALESCED = ("set_time", "set_loop")

COMPAT = {
    "set_time": transport._set_time,
    "play_toggle": transport._play_toggle,
    "set_loop": transport._set_loop,
    "bta": _bta,
    "loop_toggle": _toggle("loop"),
    "punch_in_toggle": _toggle("punch_in"),
    "punch_out_toggle": _toggle("punch_out"),
}


def apply_compat(song, msgs):
    """Apply old-protocol messages: coalesced kinds only once, last one wins."""
    latest = {}
    rest = []
    for m in msgs:
        kind = m.get("t")
        if kind in COALESCED:
            latest[kind] = m
        else:
            rest.append(m)
    n = 0
    for m in rest + list(latest.values()):
        fn = COMPAT.get(m.get("t"))
        if fn is not None:
            fn(song, m)
            n += 1
    return n


def register(reg):
    feed = Feed(lambda m: reg.log(m))
    reg.command("arrangement", "snapshot", lambda song, msg: build_snapshot(song))
    reg.command("arrangement", "position", lambda song, msg: feed.position(song))
    reg.command("arrangement", "bta", _bta)
    reg.command("arrangement", "loop_toggle", COMPAT["loop_toggle"])
    reg.command("arrangement", "punch_in_toggle", COMPAT["punch_in_toggle"])
    reg.command("arrangement", "punch_out_toggle", COMPAT["punch_out_toggle"])
    reg.topic("arrangement", feed.snapshot)
    reg.topic("position", feed.position)
    reg.arrangement = feed
