# Dispatch: turns client messages into replies and pushes subscribed events.
# No Live imports, no sockets: the caller passes send(client, text) and
# get_song(). Everything runs on Live's engine thread.

from . import legacy, protocol
from .registry import Registry
from . import ns as namespaces


class Hub(object):
    def __init__(self, get_song, send, close=None, log=None):
        self._get_song = get_song
        self._send = send
        self._close = close
        self._log = log or (lambda msg: None)
        self.registry = Registry()
        namespaces.register_all(self.registry)
        self._subs = {}  # client -> {topic: last sent data (None = not sent yet)}

    # ---- input ----
    def on_legacy(self, client, text):
        """One bare text command (old Browser Bridge client)."""
        reply = legacy.handle(self._get_song(), text, self._log)
        self._send(client, reply)
        if self._close:
            self._close(client)

    def on_line(self, client, line):
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
        """Push each subscribed topic whose value changed since the last send."""
        if not self._subs:
            return
        song = self._get_song()
        current = {}
        for subs in self._subs.values():
            for t in subs:
                if t not in current:
                    try:
                        current[t] = self.registry.topic_fn(t)(song)
                    except Exception as e:
                        self._log("PushHack: topic %s failed: %r" % (t, e))
                        current[t] = None
        for client, subs in self._subs.items():
            for t in list(subs):
                value = current.get(t)
                if value is not None and value != subs[t]:
                    subs[t] = value
                    self._send(client, protocol.event(t, value))

    def drop_client(self, client):
        self._subs.pop(client, None)
