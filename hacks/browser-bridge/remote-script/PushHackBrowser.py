# PushHackBrowser — push-hack Ableton Live MIDI Remote Script.
#
# Loads .adv/.adg presets onto the selected track via the Live browser API.
# push-manager / Shadow UI connects to a localhost socket and sends one line:
#
#     load:<preset name>      e.g.  load:My Bass.adv
#     load_uri:<browser uri>  e.g.  load_uri:query:UserLibrary#...
#     ping
#
# The Live Object Model is single-threaded: every API call must run on Live's
# engine thread. The socket server (a daemon thread) only enqueues requests;
# they are drained from update_display(), which the ControlSurface framework
# calls periodically on the engine thread.
#
# Enabling this script is a one-time manual step: select "PushHackBrowser" in a
# free control-surface slot (Input/Output = None) in Live's preferences and
# restart Live. push-hack only copies the files into the User Library.

from __future__ import absolute_import

import logging
import socket
import threading

import Live

try:
    # Proven on this Push (same base AbleSet uses); has log_message + update_display.
    from _Framework.ControlSurface import ControlSurface
except ImportError:  # pragma: no cover - newer framework fallback
    from ableton.v2.control_surface import ControlSurface

PORT = 7704            # next free push-hack port (7701 push-manager, 7703 automation)
HOST = "127.0.0.1"     # localhost only — never exposed off-device

logger = logging.getLogger("PushHackBrowser")  # routed to Live's Log.txt


class PushHackBrowser(ControlSurface):

    # Live re-instantiates the control surface several times during startup. Bind
    # the socket ONCE at class level and share one queue, so whichever instance is
    # currently active (the one Live pumps update_display on) drains the commands —
    # regardless of which instance's thread accepted the connection. Binding per
    # instance instead races and yields "Address already in use" + a zombie
    # listener whose update_display is never called.
    _server = None
    _server_started = False
    _queue = []
    _lock = threading.Lock()

    # Reply-box for query commands (get_tempo, get_beat, get_playing).
    # The socket thread stores the open connection here; update_display() on
    # Live's engine thread reads it, executes the query, and populates _query_reply.
    _pending_query = None  # (cmd_str, conn) | None
    _query_reply = None    # str | None

    def __init__(self, c_instance):
        super(PushHackBrowser, self).__init__(c_instance)
        self._log("PushHackBrowser alive")
        self._ensure_server()

    def _song(self):
        # _Framework.ControlSurface exposes song() as a method; ableton.v2's
        # ControlSurface exposes it as a property. Support whichever is in use.
        s = self.song
        return s() if callable(s) else s

    def _log(self, msg):
        # ableton.v2 ControlSurface has no log_message; fall back to logging.
        try:
            self.log_message(msg)
        except Exception:
            logger.info(msg)

    # ── socket server (bound once per process; NO Live API calls in the thread) ──
    @classmethod
    def _ensure_server(cls):
        if cls._server_started:
            return
        try:
            srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            srv.bind((HOST, PORT))
            srv.listen(4)
        except Exception as e:
            logger.info("PushHackBrowser: bind failed: %r" % (e,))
            return
        cls._server = srv
        cls._server_started = True
        t = threading.Thread(target=cls._serve, name="PushHackBrowser")
        t.daemon = True
        t.start()
        logger.info("PushHackBrowser: listening on %s:%d" % (HOST, PORT))

    @classmethod
    def _serve(cls):
        import time as _time
        while True:
            try:
                conn, _ = cls._server.accept()
            except Exception:
                return
            try:
                data = conn.recv(8192).decode("utf-8", "replace").strip()
                if not data:
                    conn.sendall(b"OK\n")
                    conn.close()
                    continue

                # Query commands need a reply from Live's engine thread.
                # Hold the connection open; update_display() will populate _query_reply.
                if data in ("get_tempo", "get_beat", "get_playing"):
                    with cls._lock:
                        # If another query is already pending, reject this one.
                        if cls._pending_query is not None:
                            conn.sendall(b"BUSY\n")
                            conn.close()
                            continue
                        cls._pending_query = (data, conn)
                        cls._query_reply = None
                    # Wait up to 500ms for update_display() to answer.
                    deadline = _time.time() + 0.5
                    reply = None
                    while _time.time() < deadline:
                        with cls._lock:
                            if cls._query_reply is not None:
                                reply = cls._query_reply
                                cls._query_reply = None
                                cls._pending_query = None
                                break
                        _time.sleep(0.01)
                    if reply is None:
                        with cls._lock:
                            cls._pending_query = None
                        reply = "ERROR\n"
                    try:
                        conn.sendall(reply.encode("utf-8"))
                    except Exception:
                        pass
                    try:
                        conn.close()
                    except Exception:
                        pass
                    continue

                # Normal fire-and-forget commands.
                with cls._lock:
                    cls._queue.append(data)
                conn.sendall(b"OK\n")
            except Exception:
                pass
            finally:
                try:
                    conn.close()
                except Exception:
                    pass

    # ── drained on Live's engine thread ──────────────────────────────────────
    def update_display(self):
        super(PushHackBrowser, self).update_display()

        # Answer any pending query command.
        with PushHackBrowser._lock:
            pq = PushHackBrowser._pending_query
        if pq is not None:
            cmd, _ = pq
            try:
                song = self._song()
                if cmd == "get_tempo":
                    reply = "%.4f\n" % song.tempo
                elif cmd == "get_beat":
                    reply = "%.6f\n" % song.current_song_time
                elif cmd == "get_playing":
                    reply = "1\n" if song.is_playing else "0\n"
                else:
                    reply = "ERROR\n"
            except Exception as e:
                self._log("PushHackBrowser: query %r error: %r" % (cmd, e))
                reply = "ERROR\n"
            with PushHackBrowser._lock:
                PushHackBrowser._query_reply = reply

        # Drain normal fire-and-forget commands.
        with PushHackBrowser._lock:
            cmds = PushHackBrowser._queue
            PushHackBrowser._queue = []
        for cmd in cmds:
            try:
                self._handle(cmd)
            except Exception as e:
                self._log("PushHackBrowser: error handling %r: %r" % (cmd, e))

    def _handle(self, cmd):
        if cmd == "ping":
            self._log("PushHackBrowser: pong")
            return
        if cmd.startswith("load_uri:"):
            self._load(self._find_by_uri(cmd[len("load_uri:"):].strip()))
            return
        if cmd.startswith("load_sample:"):
            name = cmd[len("load_sample:"):].strip()
            self._load(self._find_by_name(name, "samples"))
            return
        if cmd.startswith("load:"):
            scope, name = self._split_scope(cmd[len("load:"):].strip())
            self._load(self._find_by_name(name, scope))
            return
        if cmd == "play":
            self._song().start_playing()
            return
        if cmd == "stop":
            self._song().stop_playing()
            return
        self._log("PushHackBrowser: unknown command %r" % (cmd,))

    # load:<category>:<name> scopes the lookup to a few candidate browser roots
    # (faster + avoids cross-category name collisions); load:<name> searches all.
    # Core Library *presets* live under "sounds"/"drums", not the device-centric
    # "instruments" root — so each category maps to a small candidate list, with
    # a full-search fallback in _find_by_name when the guess misses.
    # load_sample:<name> tries "samples" then "places" (filesystem) as fallback.
    SCOPE_ROOTS = {
        "instruments":   ("sounds", "instruments"),
        "drums":         ("drums", "sounds"),
        "audio_effects": ("audio_effects",),
        "midi_effects":  ("midi_effects",),
        "samples":       ("samples", "places"),
    }

    def _split_scope(self, arg):
        head, sep, rest = arg.partition(":")
        if sep and head in self.SCOPE_ROOTS:
            return head, rest
        return None, arg

    # ── load primitive ───────────────────────────────────────────────────────
    def _load(self, item):
        if item is None:
            self._log("PushHackBrowser: no matching browser item")
            return
        if not item.is_loadable:
            self._log("PushHackBrowser: item not loadable: %s" % item.name)
            return
        browser = Live.Application.get_application().browser
        browser.load_item(item)  # instantiates onto the selected track
        self._log("PushHackBrowser: loaded %s" % item.name)

    # ── browser lookup ───────────────────────────────────────────────────────
    # Category roots cover the Core Library + packs (instruments, sounds, …);
    # user_library / user_folders cover user presets and "Places". NOTE: this
    # walk runs on Live's engine thread — keep it bounded (MAX_DEPTH) so a deep
    # tree can't stall audio. Production should navigate incrementally / index.
    ROOT_ATTRS = (
        "instruments", "sounds", "drums", "audio_effects", "midi_effects",
        "user_library", "user_folders", "current_project", "packs",
        "samples", "places",
    )
    MAX_DEPTH = 12

    def _roots(self, attrs=None):
        b = Live.Application.get_application().browser
        if not attrs:
            attrs = self.ROOT_ATTRS
        roots = []
        for attr in attrs:
            try:
                r = getattr(b, attr)
            except Exception:
                continue
            if r is None:
                continue
            # A browser root is either a single BrowserItem (has .children) or a
            # BrowserItemVector / list of items (e.g. sounds, user_folders).
            if hasattr(r, "children"):
                roots.append(r)
            else:
                try:
                    roots.extend(list(r))
                except TypeError:
                    roots.append(r)
        return roots

    # Browser item names usually include the file extension (e.g.
    # "12 String Guitar.adv"); the index sends the stripped name for presets
    # and the full filename (with extension) for samples.
    LIVE_EXTS = ("adv", "adg", "adc", "alc", "als", "amxd", "agr", "aupreset",
                 "wav", "aif", "aiff", "flac", "mp3")

    def _name_matches(self, cname, target):
        if cname == target:
            return True
        # Browser item includes extension; index has stripped name (presets).
        cbase = cname.rsplit(".", 1)
        if len(cbase) == 2 and cbase[1].lower() in self.LIVE_EXTS and cbase[0] == target:
            return True
        # Index includes extension (samples); browser item may have it stripped.
        tbase = target.rsplit(".", 1)
        if len(tbase) == 2 and tbase[1].lower() in self.LIVE_EXTS and tbase[0] == cname:
            return True
        return False

    def _find_by_name(self, name, scope_key=None):
        match = lambda it: it.is_loadable and self._name_matches(it.name, name)
        if scope_key:
            item = self._walk(match, self.SCOPE_ROOTS.get(scope_key))
            if item is not None:
                return item
        # Unscoped / fallback: search every root.
        return self._walk(match, None)

    def _find_by_uri(self, uri):
        return self._walk(lambda it: getattr(it, "uri", None) == uri)

    def _walk(self, match, attrs=None):
        # Recurse into ANY node with children — not just is_folder. Live nests
        # presets under loadable *device* nodes (e.g. instruments > Operator >
        # Guitar & Plucked > "12 String Guitar"), which report is_folder=False;
        # gating on is_folder stops before reaching them.
        def rec(item, depth):
            if depth > self.MAX_DEPTH:
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
        for root in self._roots(attrs):
            found = rec(root, 0)
            if found is not None:
                return found
        return None

    def disconnect(self):
        # Leave the class-level socket open — Live re-instantiates surfaces during
        # startup, and an early instance must not tear down the shared listener.
        # The daemon thread is reclaimed when the Live process exits.
        super(PushHackBrowser, self).disconnect()
