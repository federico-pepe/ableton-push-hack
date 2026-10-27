# PushHack: the one shared push-hack Remote Script. Hacks talk to it over local
# sockets (see protocol.py); it runs their requests on Live's engine thread.
#
# Enabling is a one-time manual step: pick "PushHack" in a free control-surface
# slot (Input/Output = None) in Live's Preferences and restart Live.

from __future__ import absolute_import

import logging

try:
    from _Framework.ControlSurface import ControlSurface
except ImportError:  # pragma: no cover - newer framework fallback
    from ableton.v2.control_surface import ControlSurface

from . import server as srv
from .hub import Hub

logger = logging.getLogger("PushHack")


class PushHack(ControlSurface):

    # Live re-creates the control surface several times while it starts. Bind
    # once per process and share one server and hub; the instance Live pumps
    # last (the active one) drives them. Binding per instance races and leaves a
    # zombie listener nobody pumps.
    _server = None
    _hub = None
    _active = None
    _started = False

    def __init__(self, c_instance):
        super(PushHack, self).__init__(c_instance)
        PushHack._active = self
        self._ensure_server()
        self._log("PushHack alive")

    def _song_obj(self):
        # _Framework exposes song() as a method, ableton.v2 as a property.
        s = self.song
        return s() if callable(s) else s

    def _log(self, msg):
        try:
            self.log_message(msg)
        except Exception:
            logger.info(msg)

    @classmethod
    def _active_song(cls):
        return cls._active._song_obj()

    @classmethod
    def _active_log(cls, msg):
        if cls._active is not None:
            cls._active._log(msg)

    @classmethod
    def _ensure_server(cls):
        if cls._started:
            return
        listeners = []
        for name, make, tag in cls._listener_makers():
            try:
                listeners.append((make(), tag))
                cls._active_log("PushHack: listening on %s" % name)
            except Exception as e:
                # Port or path taken (an old script still enabled?). Run on.
                cls._active_log("PushHack: cannot listen on %s: %r" % (name, e))
        cls._started = True
        cls._hub = Hub(cls._active_song, send=lambda c, t: cls._server.send(c, t),
                       close=lambda c: cls._server.close_after_flush(c),
                       log=cls._active_log)
        cls._server = srv.Server(listeners, log=cls._active_log,
                                 on_drop=cls._hub.drop_client,
                                 on_accept=cls._hub.on_connect)

    @staticmethod
    def _listener_makers():
        # (name, open, tag). The tag "arrangement" marks the old PushHackArrangement
        # endpoint: same socket path (TCP 7720 on Windows), old protocol.
        makers = [("tcp %s:%d" % (srv.TCP_HOST, srv.TCP_PORT), srv.open_tcp, None)]
        if hasattr(__import__("socket"), "AF_UNIX"):
            makers.append(("unix %s" % srv.UNIX_PATH, srv.open_unix, None))
            makers.append(("unix %s" % srv.ARRANGEMENT_UNIX_PATH,
                           lambda: srv.open_unix(srv.ARRANGEMENT_UNIX_PATH), "arrangement"))
        else:
            makers.append(("tcp %s:%d" % (srv.TCP_HOST, srv.ARRANGEMENT_TCP_PORT),
                           lambda: srv.open_tcp(srv.TCP_HOST, srv.ARRANGEMENT_TCP_PORT), "arrangement"))
        return makers

    # ---- Live tick ----
    def update_display(self):
        super(PushHack, self).update_display()
        if PushHack._active is not self or PushHack._server is None:
            return
        try:
            for client, kind, data in PushHack._server.poll():
                if kind == "legacy":
                    PushHack._hub.on_legacy(client, data.decode("utf-8", "replace"))
                else:
                    PushHack._hub.on_line(client, data)
            PushHack._hub.tick()
            PushHack._server.flush()
        except Exception as e:
            self._log("PushHack: tick error: %r" % (e,))

    def disconnect(self):
        # Keep the listeners: Live calls disconnect on each old instance while
        # it builds the new one, and a rebind would race.
        if PushHack._active is self:
            PushHack._active = None
        super(PushHack, self).disconnect()
