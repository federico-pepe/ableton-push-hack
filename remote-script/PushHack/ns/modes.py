# modes: what the user has selected and which view Live shows.
#
# The "selection" topic follows the selected track, scene and device, the
# detail clip and the focused view, so a hack knows what the user is looking at.

from ..errors import CommandError
from .common import bad, index_of, safe

VIEWS = ("Browser", "Arranger", "Session", "Detail", "Detail/Clip", "Detail/DeviceChain")


def app_view():
    import Live
    return Live.Application.get_application().view


def selection(song, view=None):
    from .track import ref_of
    view = view or safe(app_view)
    track = song.view.selected_track
    scenes = list(song.scenes)
    dev = safe(lambda: track.view.selected_device)
    clip = safe(lambda: song.view.detail_clip)
    out = {
        "track": ref_of(song, track),
        "scene": index_of(scenes, song.view.selected_scene),
        "device": index_of(list(track.devices), dev) if dev is not None else -1,
        "detail_clip": ({"name": safe(lambda: clip.name, "") or "",
                         "kind": "midi" if safe(lambda: clip.is_midi_clip, False) else "audio"}
                        if clip is not None else None),
        "follow_song": bool(safe(lambda: song.view.follow_song, False)),
        "draw_mode": bool(safe(lambda: song.view.draw_mode, False)),
    }
    if view is not None:
        out["view"] = safe(lambda: str(view.focused_document_view), "")
        out["browse"] = bool(safe(lambda: view.browse_mode, False))
        out["visible"] = {v: bool(safe(lambda: view.is_view_visible(v), False)) for v in VIEWS}
    return out


def get(song, msg):
    return selection(song)


def _view_name(msg):
    name = msg.get("view")
    if name not in VIEWS:
        raise bad("bad_view")
    return name


def focus(song, msg):
    name = _view_name(msg)
    app_view().focus_view(name)


def show(song, msg):
    name = _view_name(msg)
    app_view().show_view(name)


def hide(song, msg):
    name = _view_name(msg)
    app_view().hide_view(name)


def toggle_browse(song, msg):
    app_view().toggle_browse()


def set_(song, msg):
    done = []
    for key, attr in (("follow_song", "follow_song"), ("draw_mode", "draw_mode")):
        if key in msg:
            setattr(song.view, attr, bool(msg[key]))
            done.append(key)
    if not done:
        raise CommandError("no_changes")
    return {"changed": done}


def register(reg):
    reg.command("modes", "get", get)
    reg.command("modes", "focus", focus)
    reg.command("modes", "show", show)
    reg.command("modes", "hide", hide)
    reg.command("modes", "toggle_browse", toggle_browse)
    reg.command("modes", "set", set_)
    reg.topic("selection", lambda song: selection(song), every=2)
