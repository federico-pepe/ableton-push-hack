# track: tracks, return tracks and the master, with mixer, routing and state.
#
# A track is named by a reference (see common.py): its index in song.tracks,
# "return:N", "master" or "selected". Volume, pan and sends are the raw values
# of Live's mixer parameters (volume 0..1, 0.85 = 0 dB, pan -1..1); `display`
# strings come from Live.

from ..errors import CommandError
from .common import (bad, clamp, color_int, index_of, safe, to_float, to_int,
                     track_of)

KINDS = ("midi", "audio", "return")


def kind_of(song, track):
    if track == song.master_track:
        return "master"
    if index_of(list(song.return_tracks), track) >= 0:
        return "return"
    if safe(lambda: track.is_foldable, False):
        return "group"
    if safe(lambda: track.has_midi_input, False):
        return "midi"
    return "audio"


def ref_of(song, track):
    """The reference a client sends back for this track."""
    if track == song.master_track:
        return "master"
    i = index_of(list(song.return_tracks), track)
    if i >= 0:
        return "return:%d" % i
    return index_of(list(song.tracks), track)


def _param(p):
    return {"v": round(float(p.value), 4), "min": float(p.min), "max": float(p.max),
            "display": safe(lambda: str(p.str_for_value(p.value)), "")}


def mixer(track):
    m = track.mixer_device
    out = {"volume": _param(m.volume), "pan": _param(m.panning),
           "sends": [_param(s) for s in safe(lambda: list(m.sends), [])]}
    act = safe(lambda: m.track_activator)
    if act is not None:
        out["active"] = bool(act.value)
    return out


def summary(song, track):
    """What a track list needs; cheap."""
    kind = kind_of(song, track)
    out = {"ref": ref_of(song, track), "name": safe(lambda: track.name, ""),
           "kind": kind, "color": color_int(track)}
    if kind not in ("master", "return"):
        group = safe(lambda: track.group_track)
        out["group"] = index_of(list(song.tracks), group) if group is not None else -1
        out["foldable"] = bool(safe(lambda: track.is_foldable, False))
        out["folded"] = bool(safe(lambda: track.fold_state, False))
        out["arm"] = bool(safe(lambda: track.arm, False))
        out["can_arm"] = bool(safe(lambda: track.can_be_armed, False))
        out["frozen"] = bool(safe(lambda: track.is_frozen, False))
        out["visible"] = bool(safe(lambda: track.is_visible, True))
    if kind != "master":
        out["mute"] = bool(safe(lambda: track.mute, False))
        out["solo"] = bool(safe(lambda: track.solo, False))
    mx = safe(lambda: mixer(track), {})
    out.update(mx)
    return out


def list_(song, msg=None):
    tracks = [summary(song, t) for t in song.tracks]
    return {"tracks": tracks,
            "returns": [summary(song, t) for t in song.return_tracks],
            "master": summary(song, song.master_track),
            "selected": ref_of(song, song.view.selected_track)}


def _names(seq):
    return [safe(lambda: x.display_name, str(x)) for x in seq]


def get(song, msg):
    t = track_of(song, msg.get("track"))
    out = summary(song, t)
    out["devices"] = len(safe(lambda: list(t.devices), []))
    out["has_midi_input"] = bool(safe(lambda: t.has_midi_input, False))
    out["has_audio_input"] = bool(safe(lambda: t.has_audio_input, False))
    if kind_of(song, t) not in ("master", "return"):
        out["monitoring"] = safe(lambda: int(t.current_monitoring_state))
        out["playing_slot"] = safe(lambda: int(t.playing_slot_index), -1)
        out["fired_slot"] = safe(lambda: int(t.fired_slot_index), -1)
        out["input"] = safe(lambda: {"type": t.input_routing_type.display_name,
                                     "channel": t.input_routing_channel.display_name})
        out["input_types"] = safe(lambda: _names(t.available_input_routing_types), [])
    out["output"] = safe(lambda: {"type": t.output_routing_type.display_name,
                                  "channel": t.output_routing_channel.display_name})
    out["output_types"] = safe(lambda: _names(t.available_output_routing_types), [])
    out["meters"] = _meters(t)
    return out


def _set_param(p, value):
    p.value = clamp(to_float(value), float(p.min), float(p.max))


def set_(song, msg):
    """Change any of: name, color, mute, solo, arm, volume, pan, send,
    monitoring, fold. Master and return tracks have no arm or fold."""
    t = track_of(song, msg.get("track"))
    kind = kind_of(song, t)
    done = []
    if "name" in msg:
        t.name = str(msg["name"])[:64]
        done.append("name")
    if "color" in msg:
        t.color = to_int(msg["color"]) & 0xFFFFFF
        done.append("color")
    for key in ("mute", "solo"):
        if key in msg:
            if kind == "master" and key == "mute":
                raise bad("bad_arg")
            setattr(t, key, bool(msg[key]))
            done.append(key)
    if "arm" in msg:
        if kind in ("master", "return") or not t.can_be_armed:
            raise bad("cannot_arm")
        t.arm = bool(msg["arm"])
        done.append("arm")
    if "volume" in msg:
        _set_param(t.mixer_device.volume, msg["volume"])
        done.append("volume")
    if "pan" in msg:
        _set_param(t.mixer_device.panning, msg["pan"])
        done.append("pan")
    if "send" in msg:
        s = msg["send"]
        if not isinstance(s, dict):
            raise bad("bad_arg")
        sends = list(t.mixer_device.sends)
        i = to_int(s.get("index"))
        if not 0 <= i < len(sends):
            raise bad("bad_arg")
        _set_param(sends[i], s.get("value"))
        done.append("send")
    if "monitoring" in msg:
        if kind in ("master", "return"):
            raise bad("bad_arg")
        v = to_int(msg["monitoring"])
        if v not in (0, 1, 2):
            raise bad("bad_arg")
        t.current_monitoring_state = v
        done.append("monitoring")
    if "fold" in msg:
        if kind == "group" or safe(lambda: t.is_foldable, False):
            t.fold_state = 1 if msg["fold"] else 0
            done.append("fold")
        else:
            raise bad("bad_arg")
    if not done:
        raise bad("no_changes")
    return {"changed": done}


# ---- routing ----
# A routing type (and a channel) is an object with a display_name. Live lists
# the types a track may use; the channels listed depend on the type chosen. A
# client names them by display name ("Ext. In", "1/2", "Main"). Return and
# master tracks have no input.

def _sides(kind):
    return ("output",) if kind in ("master", "return") else ("input", "output")


def _available(t, side, what):
    return safe(lambda: list(getattr(t, "available_%s_routing_%s" % (side, what))), None)


def routing(song, msg):
    """Current and available input and output routing, by name."""
    t = track_of(song, msg.get("track"))
    out = {}
    for side in _sides(kind_of(song, t)):
        types = _available(t, side, "types")
        if types is None:
            continue
        cur_type = safe(lambda: getattr(t, "%s_routing_type" % side))
        cur_chan = safe(lambda: getattr(t, "%s_routing_channel" % side))
        out[side] = {
            "type": safe(lambda: cur_type.display_name, ""),
            "channel": safe(lambda: cur_chan.display_name, ""),
            "types": _names(types),
            "channels": _names(_available(t, side, "channels") or []),
        }
    return out


def _find(items, name):
    """The item whose display name is `name`: exact first, then ignoring case."""
    for x in items:
        if safe(lambda: x.display_name) == name:
            return x
    low = str(name).lower()
    for x in items:
        if str(safe(lambda: x.display_name, "")).lower() == low:
            return x
    return None


def _apply_routing(t, kind, msg):
    """Apply the routing keys of msg to track t. Returns the names changed.
    A channel may be a name or a list of names: the first one Live offers wins."""
    changed = []
    for side in ("input", "output"):
        wanted_type, wanted_chan = msg.get("%s_type" % side), msg.get("%s_channel" % side)
        if wanted_type is None and wanted_chan is None:
            continue
        if side not in _sides(kind):
            raise bad("bad_arg")
        for what, wanted in (("type", wanted_type), ("channel", wanted_chan)):
            if wanted is None:
                continue
            items = _available(t, side, what + "s")
            if items is None:
                raise bad("bad_arg")
            candidates = wanted if isinstance(wanted, list) else [wanted]
            pick = None
            for name in candidates:
                pick = _find(items, name)
                if pick is not None:
                    break
            if pick is None:
                raise CommandError("bad_routing", {"side": side, "what": what,
                                                   "want": [str(c) for c in candidates],
                                                   "options": _names(items), "changed": changed})
            setattr(t, "%s_routing_%s" % (side, what), pick)
            changed.append("%s_%s" % (side, what))
    return changed


ROUTING_KEYS = ("input_type", "input_channel", "output_type", "output_channel")


def set_routing(song, msg):
    """Set any of input_type, input_channel, output_type, output_channel, by
    display name. The type is set before the channel, because the channels Live
    offers depend on the type. A name Live does not offer fails with
    `bad_routing` and the valid names in `data.options`. A channel may be a list
    of names: the first one Live offers is used."""
    t = track_of(song, msg.get("track"))
    if not any(k in msg for k in ROUTING_KEYS):
        raise bad("no_changes")
    changed = _apply_routing(t, kind_of(song, t), msg)
    return {"changed": changed, "routing": routing(song, {"track": msg.get("track")})}


def ensure(song, msg):
    """Find the audio or MIDI track called `name`, or create it, and set its
    routing and monitoring. Safe to call again: a track that exists is not
    created twice. Takes `kind` ("audio" or "midi"), `name`, any of the routing
    keys (see set_routing) and `monitoring` (0 In, 1 Auto, 2 Off). Returns the
    track's reference, whether it was created, and its routing.

    Setting a Set's tracks from a hack changes the user's Set: call this on the
    user's say-so, not silently on every start."""
    kind = msg.get("kind")
    name = msg.get("name")
    if kind not in ("audio", "midi") or not isinstance(name, str) or not name.strip() or len(name) > 64:
        raise bad("bad_arg")
    tracks = list(song.tracks)
    found = None
    for t in tracks:
        if safe(lambda: t.name) == name and not safe(lambda: t.is_foldable, False):
            found = t
            break
    created = False
    if found is None:
        (song.create_audio_track if kind == "audio" else song.create_midi_track)(-1)
        found = list(song.tracks)[-1]
        found.name = name
        created = True
    elif kind == "audio" and safe(lambda: found.has_midi_input, False) or \
            kind == "midi" and not safe(lambda: found.has_midi_input, False):
        raise bad("wrong_track_kind")   # a track of that name exists, but it is the other kind
    ref = ref_of(song, found)
    changed = _apply_routing(found, kind_of(song, found), msg)
    if "monitoring" in msg:
        v = to_int(msg["monitoring"])
        if v not in (0, 1, 2):
            raise bad("bad_arg")
        found.current_monitoring_state = v
        changed.append("monitoring")
    return {"track": ref, "created": created, "changed": changed,
            "routing": routing(song, {"track": ref})}


def select(song, msg):
    song.view.selected_track = track_of(song, msg.get("track"))


def stop(song, msg):
    t = track_of(song, msg.get("track"))
    if not hasattr(t, "stop_all_clips") or kind_of(song, t) in ("master", "return"):
        raise bad("bad_track")
    t.stop_all_clips()


def create(song, msg):
    kind = msg.get("kind")
    if kind not in KINDS:
        raise bad("bad_arg")
    idx = to_int(msg.get("index", -1))
    if kind == "midi":
        song.create_midi_track(idx)
    elif kind == "audio":
        song.create_audio_track(idx)
    else:
        song.create_return_track()
    return {"tracks": len(list(song.tracks)), "returns": len(list(song.return_tracks))}


def _meters(t):
    return [round(float(safe(lambda: t.output_meter_left, 0.0)), 2),
            round(float(safe(lambda: t.output_meter_right, 0.0)), 2)]


def meters(song):
    return {"tracks": [_meters(t) for t in song.tracks],
            "returns": [_meters(t) for t in song.return_tracks],
            "master": _meters(song.master_track)}


def register(reg):
    reg.command("track", "list", list_)
    reg.command("track", "get", get)
    reg.command("track", "set", set_)
    reg.command("track", "routing", routing)
    reg.command("track", "set_routing", set_routing)
    reg.command("track", "ensure", ensure)
    reg.command("track", "select", select)
    reg.command("track", "stop", stop)
    reg.command("track", "create", create)
    reg.command("meters", "get", lambda song, msg: meters(song))
    reg.topic("tracks", list_, every=5)
    reg.topic("meters", meters, every=1)
