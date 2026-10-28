# device: devices on a track (and inside racks), their parameters, the selected
# device, and the basic edits (select, delete, move, rack chains).
#
# A device is named by "track" and "device" (index on the track) or by "path":
# [device, chain, device, ...] to walk into racks. Parameter values are raw
# (between min and max); `display` is Live's text for the value. Parameter 0 is
# "Device On" in Live.

from .common import (bad, clamp, device_of, index_of, safe, to_float, to_int,
                     track_of)

MAX_PARAMS = 128


def _chains(dev):
    return safe(lambda: list(dev.chains), [])


def summary(i, dev):
    d = {"i": i, "name": safe(lambda: dev.name, ""),
         "class": safe(lambda: dev.class_name, ""),
         "kind": safe(lambda: int(dev.type), 0),
         "active": bool(safe(lambda: dev.is_active, True)),
         "params": len(safe(lambda: list(dev.parameters), [])),
         "chains": len(_chains(dev))}
    if safe(lambda: dev.can_have_drum_pads, False):
        d["drum_pads"] = True
    d["collapsed"] = bool(safe(lambda: dev.view.is_collapsed, False))
    return d


def list_(song, msg):
    """Devices on a track, or inside a rack when `path` ends on a rack."""
    track = track_of(song, msg.get("track"))
    path = msg.get("path")
    if path:
        rack = device_of(song, {"track": msg.get("track"), "path": path})
        chains = _chains(rack)
        ci = to_int(msg.get("chain", 0), "bad_device")
        if not 0 <= ci < len(chains):
            raise bad("bad_device")
        devices = list(chains[ci].devices)
    else:
        devices = list(track.devices)
    return {"devices": [summary(i, d) for i, d in enumerate(devices)]}


def _param(i, p):
    out = {"i": i, "name": safe(lambda: p.name, ""),
           "v": round(float(p.value), 5), "min": float(p.min), "max": float(p.max),
           "default": safe(lambda: float(p.default_value)),
           "display": safe(lambda: str(p.str_for_value(p.value)), ""),
           "quantized": bool(safe(lambda: p.is_quantized, False)),
           "auto": safe(lambda: int(p.automation_state), 0),
           "enabled": bool(safe(lambda: p.is_enabled, True))}
    if out["quantized"]:
        out["items"] = safe(lambda: [str(x) for x in p.value_items], [])
    return out


def params(song, msg):
    dev = device_of(song, msg)
    off = max(0, to_int(msg.get("offset", 0)))
    n = clamp(to_int(msg.get("count", MAX_PARAMS)), 1, MAX_PARAMS)
    ps = list(dev.parameters)
    return {"name": safe(lambda: dev.name, ""), "total": len(ps),
            "params": [_param(off + i, p) for i, p in enumerate(ps[off:off + n])]}


def set_param(song, msg):
    """Set parameter `index` to `value` (raw), or to `norm` (0..1 of its range)."""
    dev = device_of(song, msg)
    ps = list(dev.parameters)
    i = to_int(msg.get("index"), "bad_param")
    if not 0 <= i < len(ps):
        raise bad("bad_param")
    p = ps[i]
    if not safe(lambda: p.is_enabled, True):
        raise bad("param_disabled")
    lo, hi = float(p.min), float(p.max)
    if "norm" in msg:
        v = lo + clamp(to_float(msg["norm"]), 0.0, 1.0) * (hi - lo)
    elif "value" in msg:
        v = clamp(to_float(msg["value"]), lo, hi)
    else:
        raise bad("bad_arg")
    if safe(lambda: p.is_quantized, False):
        v = float(round(v))
    p.value = v
    return _param(i, p)


def set_active(song, msg):
    """Turn a device on or off (its "Device On" parameter)."""
    dev = device_of(song, msg)
    ps = list(dev.parameters)
    if not ps:
        raise bad("bad_param")
    ps[0].value = 1.0 if msg.get("on", True) else 0.0


def select(song, msg):
    dev = device_of(song, msg)
    song.view.selected_track = track_of(song, msg.get("track"))
    song.view.select_device(dev)


def selected(song, msg=None):
    """Which device is selected in Live: track reference and device index."""
    from .track import ref_of
    track = song.view.selected_track
    dev = safe(lambda: track.view.selected_device)
    if dev is None:
        return {"track": ref_of(song, track), "device": -1}
    return {"track": ref_of(song, track),
            "device": index_of(list(track.devices), dev),
            "name": safe(lambda: dev.name, "")}


def selected_with_params(song):
    """The selected device with its parameters: what Push shows under the knobs."""
    sel = selected(song)
    if sel["device"] < 0:
        return sel
    track = track_of(song, sel["track"])
    dev = list(track.devices)[sel["device"]]
    sel["params"] = [_param(i, p) for i, p in enumerate(list(dev.parameters)[:MAX_PARAMS])]
    return sel


def delete(song, msg):
    """Delete a device from a track, or from inside a rack (path)."""
    path = msg.get("path")
    if path and len(path) > 1:
        parent_path = path[:-1]
        if len(parent_path) % 2 == 0:  # [dev, chain]: the chain holds the device
            rack = device_of(song, {"track": msg.get("track"), "path": parent_path[:-1]})
            chain = _chains(rack)[to_int(parent_path[-1], "bad_device")]
            chain.delete_device(to_int(path[-1], "bad_device"))
            return
        raise bad("bad_device")
    track = track_of(song, msg.get("track"))
    i = to_int(msg.get("device") if not path else path[0], "bad_device")
    if not 0 <= i < len(list(track.devices)):
        raise bad("bad_device")
    track.delete_device(i)


def move(song, msg):
    """Move a device to another place on the same track (or another track)."""
    dev = device_of(song, msg)
    dest = track_of(song, msg.get("to_track", msg.get("track")))
    pos = to_int(msg.get("to_index"), "bad_device")
    if not 0 <= pos <= len(list(dest.devices)):
        raise bad("bad_device")
    song.move_device(dev, dest, pos)


def chains(song, msg):
    dev = device_of(song, msg)
    return {"chains": [{"i": i, "name": safe(lambda: c.name, ""), "mute": bool(safe(lambda: c.mute, False)),
                        "solo": bool(safe(lambda: c.solo, False)),
                        "color": safe(lambda: int(c.color), 0),
                        "devices": len(safe(lambda: list(c.devices), []))}
                       for i, c in enumerate(_chains(dev))]}


def set_chain(song, msg):
    dev = device_of(song, msg)
    cs = _chains(dev)
    i = to_int(msg.get("chain"), "bad_device")
    if not 0 <= i < len(cs):
        raise bad("bad_device")
    done = []
    for key in ("mute", "solo"):
        if key in msg:
            setattr(cs[i], key, bool(msg[key]))
            done.append(key)
    if "name" in msg:
        cs[i].name = str(msg["name"])[:64]
        done.append("name")
    if not done:
        raise bad("no_changes")
    return {"changed": done}


def register(reg):
    reg.command("device", "list", list_)
    reg.command("device", "params", params)
    reg.command("device", "set_param", set_param)
    reg.command("device", "set_active", set_active)
    reg.command("device", "select", select)
    reg.command("device", "selected", selected)
    reg.command("device", "delete", delete)
    reg.command("device", "move", move)
    reg.command("device", "chains", chains)
    reg.command("device", "set_chain", set_chain)
    reg.topic("device", selected_with_params, every=2)
