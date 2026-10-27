# debug: read-only look at Live's Python API, to check which properties this
# Live has before a namespace relies on them. Roots are fixed; a path only
# walks attributes (no calls), so nothing here changes the Set.
#
#   {"ns":"debug","cmd":"dir","root":"song","path":"view"}
#   -> {"names":[{"n":"selected_track","t":"Track"}, ...]}

from ..errors import CommandError

ROOTS = ("Live", "song", "app")


def _root(song, name):
    if name == "song":
        return song
    import Live
    if name == "Live":
        return Live
    if name == "app":
        return Live.Application.get_application()
    raise CommandError("bad_arg")


def dir_(song, msg):
    obj = _root(song, msg.get("root", "song"))
    for part in str(msg.get("path", "")).split("."):
        if not part:
            continue
        if part.startswith("_"):
            raise CommandError("bad_arg")
        try:
            obj = getattr(obj, part)
        except Exception:
            raise CommandError("bad_arg")
    out = []
    for n in sorted(a for a in dir(obj) if not a.startswith("_")):
        try:
            t = type(getattr(obj, n)).__name__
        except Exception:
            t = "?"
        out.append({"n": n, "t": t})
    return {"names": out[:400]}


def register(reg):
    reg.command("debug", "dir", dir_)
