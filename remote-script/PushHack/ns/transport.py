# transport: play state, tempo, position, loop, punch, time signature.

from ..errors import CommandError

MIN_TEMPO = 20.0
MAX_TEMPO = 999.0


def snapshot(song):
    return {
        "playing": bool(song.is_playing),
        "tempo": round(float(song.tempo), 4),
        "beat": round(float(song.current_song_time), 3),
        "signature": [int(song.signature_numerator), int(song.signature_denominator)],
        "loop": {
            "on": bool(song.loop),
            "start": float(song.loop_start),
            "length": float(song.loop_length),
        },
        "punch_in": bool(song.punch_in),
        "punch_out": bool(song.punch_out),
        "metronome": bool(getattr(song, "metronome", False)),
        "record_mode": bool(getattr(song, "record_mode", False)),
        "session_record": bool(getattr(song, "session_record", False)),
        "overdub": bool(getattr(song, "arrangement_overdub", False)),
        "counting_in": bool(getattr(song, "is_counting_in", False)),
        "can_undo": bool(getattr(song, "can_undo", False)),
        "can_redo": bool(getattr(song, "can_redo", False)),
    }


def tempo(song):
    return {"tempo": round(float(song.tempo), 4)}


def _get(song, msg):
    return snapshot(song)


def _play(song, msg):
    song.start_playing()


def _stop(song, msg):
    song.stop_playing()


def _play_toggle(song, msg):
    if song.is_playing:
        song.stop_playing()
    else:
        song.start_playing()


def _set_tempo(song, msg):
    song.tempo = min(MAX_TEMPO, max(MIN_TEMPO, float(msg["v"])))


def _set_time(song, msg):
    song.current_song_time = max(0.0, float(msg["v"]))


def _set_loop(song, msg):
    song.loop_start = max(0.0, float(msg.get("start", song.loop_start)))
    song.loop_length = max(0.0001, float(msg.get("length", song.loop_length)))


def _flag(attr):
    def fn(song, msg):
        setattr(song, attr, bool(msg["on"]))
    return fn


# Boolean switches of the Song, by the name a client sends.
SWITCHES = {"metronome": "metronome", "record_mode": "record_mode",
            "session_record": "session_record", "overdub": "arrangement_overdub"}


def _set(song, msg):
    """Set any of: metronome, record_mode, session_record, overdub (booleans),
    groove_amount (0..1), swing (0..1)."""
    done = []
    for key, attr in SWITCHES.items():
        if key in msg:
            setattr(song, attr, bool(msg[key]))
            done.append(key)
    if "groove_amount" in msg:
        song.groove_amount = max(0.0, min(1.0, float(msg["groove_amount"])))
        done.append("groove_amount")
    if "swing" in msg:
        song.swing_amount = max(0.0, min(1.0, float(msg["swing"])))
        done.append("swing")
    if not done:
        raise CommandError("no_changes")
    return {"changed": done}


def _undo(song, msg):
    if not song.can_undo:
        raise CommandError("nothing_to_undo")
    song.undo()


def _redo(song, msg):
    if not song.can_redo:
        raise CommandError("nothing_to_redo")
    song.redo()


def _tap_tempo(song, msg):
    song.tap_tempo()


def _jump_cue(song, msg):
    d = msg.get("to")
    if d == "next" and song.can_jump_to_next_cue:
        song.jump_to_next_cue()
    elif d == "prev" and song.can_jump_to_prev_cue:
        song.jump_to_prev_cue()
    else:
        raise CommandError("no_cue")


def _continue(song, msg):
    song.continue_playing()


def _capture_midi(song, msg):
    if not song.can_capture_midi:
        raise CommandError("nothing_to_capture")
    song.capture_midi()


def register(reg):
    reg.command("transport", "get", _get)
    reg.command("transport", "play", _play)
    reg.command("transport", "stop", _stop)
    reg.command("transport", "play_toggle", _play_toggle)
    reg.command("transport", "set_tempo", _set_tempo)
    reg.command("transport", "set_time", _set_time)
    reg.command("transport", "set_loop", _set_loop)
    reg.command("transport", "set_loop_on", _flag("loop"))
    reg.command("transport", "set_punch_in", _flag("punch_in"))
    reg.command("transport", "set_punch_out", _flag("punch_out"))
    reg.command("transport", "set", _set)
    reg.command("transport", "undo", _undo)
    reg.command("transport", "redo", _redo)
    reg.command("transport", "tap_tempo", _tap_tempo)
    reg.command("transport", "jump_cue", _jump_cue)
    reg.command("transport", "continue", _continue)
    reg.command("transport", "capture_midi", _capture_midi)
    reg.topic("transport", snapshot)
    # Only the tempo: "transport" changes ~10x/s while playing (the beat moves).
    reg.topic("tempo", tempo)
