# browser: load presets, samples, plugins and URIs onto the selected track.
#
# The tree walk runs on Live's engine thread: MAX_DEPTH keeps it bounded so a
# deep tree cannot stall audio.

from ..errors import CommandError

LIVE_EXTS = ("adv", "adg", "adc", "alc", "als", "amxd", "agr", "aupreset",
             "wav", "aif", "aiff", "flac", "mp3", "vstpreset")

# load name scoped to a category searches a few candidate roots first, then
# every root. Core Library presets live under "sounds"/"drums", not the
# device-centric "instruments" root.
SCOPE_ROOTS = {
    "instruments":   ("sounds", "instruments"),
    "drums":         ("drums", "sounds"),
    "audio_effects": ("audio_effects",),
    "midi_effects":  ("midi_effects",),
    "samples":       ("samples", "places"),
    "plugins":       ("plugins",),
}

# Category roots cover the Core Library and packs; user_library / user_folders
# cover user presets and "Places".
ROOT_ATTRS = (
    "instruments", "sounds", "drums", "audio_effects", "midi_effects",
    "user_library", "user_folders", "current_project", "packs",
    "samples", "places", "plugins",
)
MAX_DEPTH = 12
MAX_DUMP_LINES = 400


def live_browser():
    import Live
    return Live.Application.get_application().browser


def split_scope(arg):
    head, sep, rest = arg.partition(":")
    if sep and head in SCOPE_ROOTS:
        return head, rest
    return None, arg


def strip_ext(name):
    base, dot, ext = name.rpartition(".")
    return base if dot and ext.lower() in LIVE_EXTS else name


def name_matches(cname, target):
    """Browser items carry their extension; the preset index does not."""
    if cname == target:
        return True
    cbase = cname.rsplit(".", 1)
    if len(cbase) == 2 and cbase[1].lower() in LIVE_EXTS and cbase[0] == target:
        return True
    tbase = target.rsplit(".", 1)
    if len(tbase) == 2 and tbase[1].lower() in LIVE_EXTS and tbase[0] == cname:
        return True
    return False


class Browser(object):
    def __init__(self, log, get_browser=live_browser):
        self._log = log
        self._get_browser = get_browser

    # ---- tree ----
    def roots(self, attrs=None):
        b = self._get_browser()
        roots = []
        for attr in (attrs or ROOT_ATTRS):
            try:
                r = getattr(b, attr)
            except Exception:
                continue
            if r is None:
                continue
            # A root is one BrowserItem (has .children) or a vector of them.
            if hasattr(r, "children"):
                roots.append(r)
            else:
                try:
                    roots.extend(list(r))
                except TypeError:
                    roots.append(r)
        return roots

    def walk(self, match, attrs=None):
        # Recurse into ANY node with children, not just folders: Live nests
        # presets under loadable device nodes that report is_folder=False.
        def rec(item, depth):
            if depth > MAX_DEPTH:
                return None
            try:
                children = item.children
            except Exception:
                return None
            for child in children:
                if match(child):
                    return child
                found = rec(child, depth + 1)
                if found is not None:
                    return found
            return None
        for root in self.roots(attrs):
            found = rec(root, 0)
            if found is not None:
                return found
        return None

    def find_by_name(self, name, scope=None):
        def match(it):
            return it.is_loadable and name_matches(it.name, name)
        if scope:
            item = self.walk(match, SCOPE_ROOTS.get(scope))
            if item is not None:
                return item
        return self.walk(match, None)

    def find_by_uri(self, uri):
        return self.walk(lambda it: getattr(it, "uri", None) == uri)

    # ---- plugins ----
    # browser.plugins is Plug-Ins > <Vendor> > <Plugin>; a plugin's children
    # are its presets.
    def plugin_items(self):
        for root in self.roots(("plugins",)):
            for vendor in root.children:
                if vendor.is_loadable:  # no vendor level
                    yield "", vendor
                    continue
                for plugin in vendor.children:
                    if plugin.is_loadable:
                        yield vendor.name, plugin

    def list_plugins(self):
        out = []
        for vendor, plugin in self.plugin_items():
            presets = []
            try:
                presets = [strip_ext(c.name) for c in plugin.children if c.is_loadable]
            except Exception:
                pass
            out.append({"vendor": vendor, "name": plugin.name, "presets": presets})
        return out

    def find_plugin(self, plugin_name, preset_name):
        for _, plugin in self.plugin_items():
            if plugin.name != plugin_name:
                continue
            if not preset_name:
                return plugin
            for child in plugin.children:
                if child.is_loadable and name_matches(child.name, preset_name):
                    return child
            self._log("PushHack: plugin %r has no preset %r" % (plugin_name, preset_name))
            return None
        return None

    # ---- load ----
    def load(self, item):
        """Load onto the selected track. Returns an error code or None."""
        if item is None:
            self._log("PushHack: no matching browser item")
            return "not_found"
        if not item.is_loadable:
            self._log("PushHack: item not loadable: %s" % item.name)
            return "not_loadable"
        self._get_browser().load_item(item)
        self._log("PushHack: loaded %s" % item.name)
        return None

    # ---- commands ----
    def _result(self, item):
        code = self.load(item)
        if code:
            raise CommandError(code)
        return {"loaded": item.name}

    def cmd_load(self, song, msg):
        return self._result(self.find_by_name(str(msg["name"]), msg.get("scope")))

    def cmd_load_sample(self, song, msg):
        return self._result(self.find_by_name(str(msg["name"]), "samples"))

    def cmd_load_uri(self, song, msg):
        return self._result(self.find_by_uri(str(msg["uri"])))

    def cmd_load_plugin(self, song, msg):
        return self._result(self.find_plugin(str(msg["plugin"]), str(msg.get("preset", ""))))

    def cmd_list_plugins(self, song, msg):
        return {"plugins": self.list_plugins()}

    def cmd_dump(self, song, msg):
        """Log a root's tree to Log.txt. For finding out what Live exposes."""
        root = str(msg["root"])
        depth = int(msg.get("depth", 2))
        b = self._get_browser()
        if not hasattr(b, root):
            self._log("PushHack: dump: browser has no root %r; roots: %s" % (
                root, [a for a in dir(b) if not a.startswith("_")]))
            raise CommandError("no_such_root")
        lines = []

        def rec(item, d):
            lines.append("%s%s [loadable=%s folder=%s] %s" % (
                "  " * d, item.name, item.is_loadable, item.is_folder, getattr(item, "uri", "")))
            if d >= depth or len(lines) > MAX_DUMP_LINES:
                return
            try:
                for c in item.children:
                    rec(c, d + 1)
            except Exception:
                pass
        roots = self.roots((root,))
        for r in roots:
            rec(r, 0)
        self._log("PushHack: dump %s: %d root item(s)" % (root, len(roots)))
        for line in lines[:MAX_DUMP_LINES]:
            self._log("PushHack: dump| " + line)
        return {"lines": min(len(lines), MAX_DUMP_LINES)}


def register(reg):
    b = Browser(lambda m: reg.log(m))
    reg.command("browser", "load", b.cmd_load)
    reg.command("browser", "load_sample", b.cmd_load_sample)
    reg.command("browser", "load_uri", b.cmd_load_uri)
    reg.command("browser", "load_plugin", b.cmd_load_plugin)
    reg.command("browser", "list_plugins", b.cmd_list_plugins)
    reg.command("browser", "dump", b.cmd_dump)
    reg.browser = b
