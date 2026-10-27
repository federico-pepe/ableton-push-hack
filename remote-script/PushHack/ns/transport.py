# transport: play state, tempo, position, loop, punch, time signature.

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
    }


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
    reg.topic("transport", snapshot)
