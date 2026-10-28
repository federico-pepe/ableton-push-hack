# Wiring of the ControlSurface subclass, with a stub of Live's framework.
import json
import os
import socket
import sys
import time
import types
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from fakes import FakeSong  # noqa: E402


class StubControlSurface(object):
    song_obj = None

    def __init__(self, c_instance):
        self.logs = []

    def song(self):
        return StubControlSurface.song_obj

    def log_message(self, msg):
        self.logs.append(msg)

    def update_display(self):
        pass

    def disconnect(self):
        pass


def install_stub():
    fw = types.ModuleType("_Framework")
    cs = types.ModuleType("_Framework.ControlSurface")
    cs.ControlSurface = StubControlSurface
    fw.ControlSurface = cs
    sys.modules["_Framework"] = fw
    sys.modules["_Framework.ControlSurface"] = cs


class PushHackClassTest(unittest.TestCase):
    def setUp(self):
        install_stub()
        StubControlSurface.song_obj = FakeSong()
        from PushHack import server
        from PushHack import PushHack as mod
        self.mod = mod
        self.cls = mod.PushHack
        self.saved = (server.open_tcp, server.open_unix, self.cls._server,
                      self.cls._hub, self.cls._active, self.cls._started)
        self.server = server
        # Never bind the real port or socket path in a test.
        server.open_tcp = lambda *a: server.__dict__["_orig_tcp"]("127.0.0.1", 0)
        server._orig_tcp = self.saved[0]
        server.open_unix = lambda *a: None
        self.cls._server = self.cls._hub = self.cls._active = None
        self.cls._started = False
        self.cls._listener_makers = staticmethod(lambda: [("tcp", server.open_tcp)])

    def tearDown(self):
        if self.cls._server:
            self.cls._server.close()
        (self.server.open_tcp, self.server.open_unix, self.cls._server,
         self.cls._hub, self.cls._active, self.cls._started) = self.saved
        del self.cls._listener_makers

    def test_tick_answers_a_client(self):
        surface = self.cls(object())
        addr = self.cls._server._listeners[0].getsockname()
        c = socket.create_connection(addr)
        self.addCleanup(c.close)
        c.sendall(b'{"id":1,"ns":"transport","cmd":"get"}\n')
        c.settimeout(0.05)
        reply = b""
        end = time.time() + 3
        while b"\n" not in reply and time.time() < end:
            surface.update_display()
            try:
                reply += c.recv(4096)
            except socket.timeout:
                pass
        self.assertEqual(json.loads(reply)["data"]["tempo"], 120.0)

    def test_only_active_instance_pumps_and_listeners_survive_disconnect(self):
        old = self.cls(object())
        new = self.cls(object())
        self.assertIs(self.cls._active, new)
        listeners = list(self.cls._server._listeners)
        old.disconnect()
        self.assertIs(self.cls._active, new)
        self.assertEqual(self.cls._server._listeners, listeners)
        self.assertTrue(self.cls._started)
        new.disconnect()
        self.assertIsNone(self.cls._active)
        new.update_display()  # no active instance: must not raise


if __name__ == "__main__":
    unittest.main()
