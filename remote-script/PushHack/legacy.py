# Old PushHackBrowser text commands, kept for one release cycle so push-manager
# and the Browser Bridge client keep working. One command per connection.
# Replies are bare text ("OK", a number, "ERROR"). No Live imports.
#
# Browser commands (load:, load_uri:, load_plugin:, load_sample:, dump:,
# list_plugins, get_scale) arrive with the browser namespace (plan phase 2).


def handle(song, text, log=None):
    cmd = text.strip()
    if cmd in ("", "ping"):
        return "OK"
    try:
        if cmd == "play":
            song.start_playing()
            return "OK"
        if cmd == "stop":
            song.stop_playing()
            return "OK"
        if cmd == "get_tempo":
            return "%.4f" % song.tempo
        if cmd == "get_beat":
            return "%.6f" % song.current_song_time
        if cmd == "get_playing":
            return "1" if song.is_playing else "0"
    except Exception as e:
        if log:
            log("PushHack: legacy %r failed: %r" % (cmd, e))
        return "ERROR"
    if log:
        log("PushHack: legacy command not supported yet: %r" % (cmd,))
    return "ERROR"
