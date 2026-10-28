# session: the clip grid (tracks x scenes), launching and scenes.
#
# A slot in the grid is 0 when it has no clip, else
# [color, name, state, length]. state: 0 stopped, 1 playing, 2 triggered
# (queued), 3 recording.

from .common import (bad, color_int, safe, scene_of, slot_of, to_float,
                     to_int, track_of)

DEFAULT_TRACKS = 24
DEFAULT_SCENES = 40
MAX_CELLS = 2000


def slot_state(slot):
    if safe(lambda: slot.clip.is_recording, False):
        return 3
    if safe(lambda: slot.is_playing, False):
        return 1
    if safe(lambda: slot.is_triggered, False):
        return 2
    return 0


def cell(slot):
    if not safe(lambda: slot.has_clip, False):
        return 0
    c = slot.clip
    return [color_int(c), safe(lambda: c.name, "") or "", slot_state(slot),
            round(float(safe(lambda: c.length, 0.0)), 3)]


def grid(song, msg=None):
    msg = msg or {}
    t0 = max(0, to_int(msg.get("track_offset", 0)))
    s0 = max(0, to_int(msg.get("scene_offset", 0)))
    tn = max(1, to_int(msg.get("track_count", DEFAULT_TRACKS)))
    sn = max(1, to_int(msg.get("scene_count", DEFAULT_SCENES)))
    if tn * sn > MAX_CELLS:
        raise bad("too_big")
    tracks = list(song.tracks)[t0:t0 + tn]
    scenes = list(song.scenes)[s0:s0 + sn]
    rows = []
    for si, _ in enumerate(scenes):
        row = []
        for t in tracks:
            slots = safe(lambda: list(t.clip_slots), [])
            idx = s0 + si
            row.append(cell(slots[idx]) if idx < len(slots) else 0)
        rows.append(row)
    return {
        "track_offset": t0, "scene_offset": s0,
        "tracks": [{"name": safe(lambda: t.name, ""), "color": color_int(t),
                    "arm": bool(safe(lambda: t.arm, False)),
                    "playing": safe(lambda: int(t.playing_slot_index), -1),
                    "fired": safe(lambda: int(t.fired_slot_index), -1)} for t in tracks],
        "scenes": [{"name": safe(lambda: s.name, ""), "color": color_int(s),
                    "triggered": bool(safe(lambda: s.is_triggered, False))} for s in scenes],
        "grid": rows,
        "total_tracks": len(list(song.tracks)), "total_scenes": len(list(song.scenes)),
    }


def fire_clip(song, msg):
    slot_of(song, msg).fire()


def stop_clip(song, msg):
    slot_of(song, msg).stop()


def fire_scene(song, msg):
    scene = scene_of(song, msg.get("scene"))
    if msg.get("as_selected"):
        scene.fire_as_selected()
    else:
        scene.fire()


def stop_all(song, msg):
    song.stop_all_clips()


def create_clip(song, msg):
    slot = slot_of(song, msg)
    if slot.has_clip:
        raise bad("slot_taken")
    length = to_float(msg.get("length", 4.0))
    if not 0.25 <= length <= 1024:
        raise bad("bad_arg")
    slot.create_clip(length)


def delete_clip(song, msg):
    slot = slot_of(song, msg)
    if not slot.has_clip:
        raise bad("no_clip")
    slot.delete_clip()


def capture_scene(song, msg):
    """Capture the playing clips into a new scene (Live's Capture and Insert)."""
    song.capture_and_insert_scene()


def select_scene(song, msg):
    song.view.selected_scene = scene_of(song, msg.get("scene"))


def select_clip_slot(song, msg):
    song.view.highlighted_clip_slot = slot_of(song, msg)
    song.view.selected_track = track_of(song, msg.get("track"))
    song.view.selected_scene = scene_of(song, msg.get("scene"))


def create_scene(song, msg):
    song.create_scene(to_int(msg.get("index", -1)))
    return {"scenes": len(list(song.scenes))}


def duplicate_scene(song, msg):
    i = to_int(msg.get("scene"), "bad_scene")
    if not 0 <= i < len(list(song.scenes)):
        raise bad("bad_scene")
    song.duplicate_scene(i)


def delete_scene(song, msg):
    i = to_int(msg.get("scene"), "bad_scene")
    if not 0 <= i < len(list(song.scenes)) or len(list(song.scenes)) <= 1:
        raise bad("bad_scene")
    song.delete_scene(i)


def set_scene(song, msg):
    s = scene_of(song, msg.get("scene"))
    done = []
    if "name" in msg:
        s.name = str(msg["name"])[:64]
        done.append("name")
    if "color" in msg:
        s.color = to_int(msg["color"]) & 0xFFFFFF
        done.append("color")
    if "tempo" in msg:
        v = to_float(msg["tempo"])
        if v and not 20 <= v <= 999:
            raise bad("bad_arg")
        s.tempo = v
        done.append("tempo")
    if not done:
        raise bad("no_changes")
    return {"changed": done}


def register(reg):
    reg.command("session", "get", grid)
    reg.command("session", "fire_clip", fire_clip)
    reg.command("session", "stop_clip", stop_clip)
    reg.command("session", "fire_scene", fire_scene)
    reg.command("session", "stop_all", stop_all)
    reg.command("session", "create_clip", create_clip)
    reg.command("session", "delete_clip", delete_clip)
    reg.command("session", "capture_scene", capture_scene)
    reg.command("session", "select_scene", select_scene)
    reg.command("session", "select_clip_slot", select_clip_slot)
    reg.command("session", "create_scene", create_scene)
    reg.command("session", "duplicate_scene", duplicate_scene)
    reg.command("session", "delete_scene", delete_scene)
    reg.command("session", "set_scene", set_scene)
    reg.topic("session", grid, every=3)
