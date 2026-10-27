# Old PushHackBrowser text commands, kept for one release cycle so push-manager
# and the Browser Bridge client keep working. One command per connection.
# Replies are bare text ("OK", a number, JSON, "ERROR"). No Live imports.
#
# The old load commands answered "OK" before they ran (they were queued); here
# they run first and still answer "OK" either way, with a failure in the log.

import json

from .errors import CommandError


def _call(reg, song, ns, cmd, msg=None):
    return reg.find_command(ns, cmd)(song, msg or {})


def handle(reg, song, text, log=None):
    log = log or (lambda m: None)
    cmd = text.strip()
    if cmd in ("", "ping"):
        return "OK"
    try:
        if cmd == "play":
            _call(reg, song, "transport", "play")
            return "OK"
        if cmd == "stop":
            _call(reg, song, "transport", "stop")
            return "OK"
        if cmd == "get_tempo":
            return "%.4f" % song.tempo
        if cmd == "get_beat":
            return "%.6f" % song.current_song_time
        if cmd == "get_playing":
            return "1" if song.is_playing else "0"
        if cmd == "get_scale":
            return json.dumps(_call(reg, song, "notes", "scale"))
        if cmd == "list_plugins":
            return json.dumps(_call(reg, song, "browser", "list_plugins"))
        if cmd == "get_listen":
            return json.dumps(_call(reg, song, "listen", "get"))
        if cmd == "listen_off":
            return json.dumps(_call(reg, song, "listen", "off"))
        if cmd.startswith("listen_on:"):
            return json.dumps(_call(reg, song, "listen", "on", {"port": cmd[len("listen_on:"):]}))
        return _load_command(reg, song, cmd, log)
    except CommandError as e:
        log("PushHack: legacy %r failed: %s" % (cmd, e.code))
        return "ERROR"
    except Exception as e:
        log("PushHack: legacy %r failed: %r" % (cmd, e))
        return "ERROR"


def _load_command(reg, song, cmd, log):
    from .ns.browser import split_scope
    try:
        if cmd.startswith("load_uri:"):
            _call(reg, song, "browser", "load_uri", {"uri": cmd[len("load_uri:"):].strip()})
        elif cmd.startswith("load_plugin:"):
            # load_plugin:<plugin> or load_plugin:<plugin>:<preset>
            plugin, _, preset = cmd[len("load_plugin:"):].strip().partition(":")
            _call(reg, song, "browser", "load_plugin",
                  {"plugin": plugin.strip(), "preset": preset.strip()})
        elif cmd.startswith("load_sample:"):
            _call(reg, song, "browser", "load_sample", {"name": cmd[len("load_sample:"):].strip()})
        elif cmd.startswith("load:"):
            scope, name = split_scope(cmd[len("load:"):].strip())
            _call(reg, song, "browser", "load", {"name": name, "scope": scope})
        elif cmd.startswith("dump:"):
            root, _, depth = cmd[len("dump:"):].strip().partition(":")
            _call(reg, song, "browser", "dump",
                  {"root": root, "depth": int(depth) if depth.isdigit() else 2})
        else:
            log("PushHack: unknown legacy command %r" % (cmd,))
            return "ERROR"
    except CommandError as e:
        log("PushHack: legacy %r: %s" % (cmd, e.code))
    except Exception as e:
        log("PushHack: legacy %r failed: %r" % (cmd, e))
    return "OK"
