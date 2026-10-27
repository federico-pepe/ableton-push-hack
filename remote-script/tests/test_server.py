import os
import socket
import sys
import tempfile
import threading
import time
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from PushHack import server  # noqa: E402


def _quiet_sendall(sock, data):
    try:
        sock.sendall(data)
    except OSError:
        pass


def pump(srv, want=1, timeout=2.0):
    events = []
    end = time.time() + timeout
    while time.time() < end and len(events) < want:
        events += srv.poll()
        time.sleep(0.005)
    return events


def read_all(sock, timeout=2.0):
    sock.settimeout(timeout)
    buf = b""
    try:
        while True:
            d = sock.recv(4096)
            if not d:
                break
            buf += d
    except socket.timeout:
        pass
    return buf


class TcpServerTest(unittest.TestCase):
    def setUp(self):
        self.ls = server.open_tcp("127.0.0.1", 0)
        self.addr = self.ls.getsockname()
        self.drops = []
        self.srv = server.Server([self.ls], on_drop=self.drops.append)

    def tearDown(self):
        self.srv.close()

    def connect(self):
        c = socket.create_connection(self.addr)
        self.addCleanup(c.close)
        return c

    def test_json_lines_split_and_kept_open(self):
        c = self.connect()
        c.sendall(b'{"a":1}\n{"b":2}\n{"c"')
        ev = pump(self.srv, 2)
        self.assertEqual([(k, d) for _, k, d in ev], [("line", b'{"a":1}'), ("line", b'{"b":2}')])
        c.sendall(b':3}\n')
        ev = pump(self.srv)
        self.assertEqual(ev[0][2], b'{"c":3}')

    def test_legacy_without_newline_replies_then_closes(self):
        c = self.connect()
        c.sendall(b"get_tempo")
        (client, kind, data), = pump(self.srv)
        self.assertEqual((kind, data), ("legacy", b"get_tempo"))
        self.srv.send(client, "120.0000")
        self.srv.close_after_flush(client)
        self.srv.flush()
        self.assertEqual(read_all(c), b"120.0000\n")
        self.assertEqual(self.srv.clients, [])
        self.assertEqual(len(self.drops), 1)

    def test_legacy_extra_data_ignored(self):
        c = self.connect()
        c.sendall(b"ping")
        pump(self.srv)
        c.sendall(b"play")
        time.sleep(0.05)
        self.assertEqual(self.srv.poll(), [])

    def test_disconnect_drops_client(self):
        c = self.connect()
        c.sendall(b'{"a":1}\n')
        pump(self.srv)
        c.close()
        end = time.time() + 2
        while self.srv.clients and time.time() < end:
            self.srv.poll()
            time.sleep(0.005)
        self.assertEqual(self.srv.clients, [])
        self.assertEqual(len(self.drops), 1)

    def test_oversized_line_without_newline_drops_client(self):
        c = self.connect()
        payload = b"{" + b"x" * (server.MAX_LINE_BYTES + 10)
        # sendall blocks until the server reads, so send from a thread.
        t = threading.Thread(target=lambda: _quiet_sendall(c, payload))
        t.start()
        end = time.time() + 5
        seen = False
        while time.time() < end:
            self.srv.poll()
            seen = seen or bool(self.srv.clients)
            if seen and not self.srv.clients:
                break
            time.sleep(0.002)
        c.close()
        t.join(2)
        self.assertTrue(seen)
        self.assertEqual(self.srv.clients, [])

    def test_slow_client_dropped_when_output_piles_up(self):
        c = self.connect()
        c.sendall(b'{"a":1}\n')
        (client, _, _), = pump(self.srv)
        self.srv.send(client, "x" * (server.MAX_OUT_BYTES + 1))
        self.assertEqual(self.srv.clients, [])

    def test_two_clients_at_once(self):
        a, b = self.connect(), self.connect()
        a.sendall(b'{"a":1}\n')
        b.sendall(b'{"b":1}\n')
        ev = pump(self.srv, 2)
        self.assertEqual(sorted(d for _, _, d in ev), [b'{"a":1}', b'{"b":1}'])


@unittest.skipUnless(hasattr(socket, "AF_UNIX"), "no unix sockets")
class UnixServerTest(unittest.TestCase):
    def test_unix_socket_is_owner_only_and_replaces_stale_file(self):
        d = tempfile.mkdtemp()
        path = os.path.join(d, "t.sock")
        open(path, "w").close()  # stale file at the path
        ls = server.open_unix(path)
        srv = server.Server([ls])
        try:
            self.assertEqual(os.stat(path).st_mode & 0o777, 0o600)
            c = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            self.addCleanup(c.close)
            c.connect(path)
            c.sendall(b'{"a":1}\n')
            self.assertEqual(pump(srv)[0][2], b'{"a":1}')
        finally:
            srv.close(unix_paths=[path])
        self.assertFalse(os.path.exists(path))
        os.rmdir(d)


if __name__ == "__main__":
    unittest.main()
