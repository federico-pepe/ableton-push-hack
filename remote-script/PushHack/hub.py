# Dispatch: turns client messages into replies and pushes subscribed events.
# No Live imports, no sockets: the caller passes send(client, text) and
# get_song(). Everything runs on Live's engine thread.

import time

from . import legacy, protocol
from .errors import CommandError
from .registry import Registry
from . import ns as namespaces
from .ns import arrangement


class Hub(object):
    def __init__(self, get_song, send, close=None, log=None, clock=None):
        self._get_song = get_song
        self._clock = clock or time.time
        self._tstate = {}   # topic -> [ticks seen, next recompute tick, last value]
        self._send = send
        self._close = close
        self._log = log or (lambda msg: None)
        self.registry = Registry()
        self.registry.log = self._log
        namespaces.register_all(self.registry)
        self._subs = {}    # client -> {topic: last sent data (None = not sent yet)}
        self._compat = {}  # old arrangement clients -> {"snapshot": last, "pos": last}
        self._compat_in = []

    # ---- input ----
    def on_legacy(self, client, text):
        """One bare text command (old Browser Bridge client)."""
        reply = legacy.handle(self.registry, self._get_song(), text, self._log)
        self._send(client, reply)
        if self._close:
            self._close(client)

    def on_connect(self, client):
        if getattr(client, "tag", None) == "arrangement":
            # Old arrangement protocol: streams to every client, asks for nothing.
            self._compat[client] = {"snapshot": None, "pos": None}
            self.registry.arrangement.invalidate()

    def on_line(self, client, line):
        if client in self._compat:
            msg = protocol.decode(line)
            if msg is not None:
                self._compat_in.append(msg)
            return
        msg = protocol.decode(line)
        if msg is None:
            self._send(client, protocol.err(None, "bad_json"))
            return
        req_id = msg.get("id")
        ns = msg.get("ns")
        cmd = msg.get("cmd")
        if not isinstance(cmd, str):
            self._send(client, protocol.err(req_id, "no_cmd"))
            return
        if ns is None:
            self._builtin(client, req_id, cmd, msg)
            return
        fn = self.registry.find_command(ns, cmd)
        if fn is None:
            code = "unknown_cmd" if self.registry.has_namespace(ns) else "unknown_ns"
            self._send(client, protocol.err(req_id, code))
            return
        try:
            data = fn(self._get_song(), msg)
        except CommandError as e:
            self._send(client, protocol.err(req_id, e.code))
            return
        except Exception as e:
            self._log("PushHack: %s.%s failed: %r" % (ns, cmd, e))
            self._send(client, protocol.err(req_id, "failed"))
            return
        self._send(client, protocol.ok(req_id, data))

    def _builtin(self, client, req_id, cmd, msg):
        if cmd == "hello":
            self._send(client, protocol.ok(req_id, {
                "protocol": protocol.PROTOCOL,
                "script_version": protocol.SCRIPT_VERSION,
                "namespaces": self.registry.namespaces(),
                "topics": self.registry.topics(),
            }))
        elif cmd in ("subscribe", "unsubscribe"):
            topics = msg.get("topics")
            if not isinstance(topics, list) or not all(
                    isinstance(t, str) and self.registry.topic_fn(t) for t in topics):
                self._send(client, protocol.err(req_id, "unknown_topic"))
                return
            subs = self._subs.setdefault(client, {})
            for t in topics:
                if cmd == "subscribe":
                    subs.setdefault(t, None)  # None: send current state next tick
                else:
                    subs.pop(t, None)
            self._send(client, protocol.ok(req_id, {"topics": sorted(subs)}))
        else:
            self._send(client, protocol.err(req_id, "unknown_cmd"))

    # ---- output ----
    def tick(self):
        """Run tick hooks, serve old arrangement clients, push changed topics."""
        song = self._get_song()
        for fn in self.registry.tick_fns():
            try:
                fn(song)
            except Exception as e:
                self._log("PushHack: tick hook failed: %r" % (e,))
        self._tick_compat(song)
        if not self._subs:
            return
        current = {}
        for subs in self._subs.values():
            for t in subs:
                if t not in current:
                    current[t] = self._topic_value(t, song)
        for client, subs in self._subs.items():
            for t in list(subs):
                value = current.get(t)
                if value is not None and value != subs[t]:
                    subs[t] = value
                    self._send(client, protocol.event(t, value))

    # A topic is recomputed at most every `every` ticks, and after a slow
    # build it waits (seconds x BUDGET_TICKS_PER_SEC) ticks, so building uses
    # about 5% of the time at most. In between, subscribers see the last value.
    BUDGET_TICKS_PER_SEC = 200

    def _topic_value(self, t, song):
        st = self._tstate.setdefault(t, [0, 0, None])
        st[0] += 1
        if st[2] is not None and st[0] < st[1]:
            return st[2]
        t0 = self._clock()
        try:
            st[2] = self.registry.topic_fn(t)(song)
        except Exception as e:
            self._log("PushHack: topic %s failed: %r" % (t, e))
            st[2] = None
        took = self._clock() - t0
        st[1] = st[0] + max(self.registry.topic_every(t), int(took * self.BUDGET_TICKS_PER_SEC))
        return st[2]

    def _tick_compat(self, song):
        msgs, self._compat_in = self._compat_in, []
        if msgs:
            try:
                arrangement.apply_compat(song, msgs)
            except Exception as e:
                self._log("PushHack: arrangement command failed: %r" % (e,))
        if not self._compat:
            return
        feed = self.registry.arrangement
        try:
            snap = feed.snapshot(song)
            pos = feed.position(song)
        except Exception as e:
            self._log("PushHack: arrangement update failed: %r" % (e,))
            return
        for client, last in self._compat.items():
            if snap != last["snapshot"]:
                last["snapshot"] = snap
                d = dict(snap)
                d["t"] = "snapshot"
                self._send(client, protocol.encode(d))
            if pos != last["pos"]:
                last["pos"] = pos
                d = dict(pos)
                d["t"] = "pos"
                self._send(client, protocol.encode(d))

    def drop_client(self, client):
        self._subs.pop(client, None)
        self._compat.pop(client, None)
