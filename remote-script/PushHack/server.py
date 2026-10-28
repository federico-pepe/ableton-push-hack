# Non-blocking sockets for all clients. No Live imports.
#
# One JSON object per line, both ways. A client whose first byte is not "{" is
# an old text-protocol client (the Browser Bridge of before PushHack): it gets
# one error line and is closed, so it fails at once instead of waiting.

import os
import socket

from .protocol import MAX_LINE_BYTES

UNIX_PATH = "/tmp/push-hack.sock"

TCP_HOST = "127.0.0.1"
TCP_PORT = 7704          # fixed framework port: push-manager's Live bridge
MAX_OUT_BYTES = 8 * 1024 * 1024  # drop a client that cannot keep up
OLD_PROTOCOL_REPLY = '{"ok":false,"error":"old_protocol"}'


class Client(object):
    def __init__(self, sock):
        self.sock = sock
        self.inbuf = b""
        self.out = b""
        self.close_after_flush = False
        self.checked = False   # has the first chunk been looked at


def open_tcp(host=TCP_HOST, port=TCP_PORT):
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    # Windows: SO_REUSEADDR lets another process take the port.
    if hasattr(socket, "SO_EXCLUSIVEADDRUSE"):
        s.setsockopt(socket.SOL_SOCKET, socket.SO_EXCLUSIVEADDRUSE, 1)
    else:
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind((host, port))
    s.listen(4)
    s.setblocking(False)
    return s


def open_unix(path=UNIX_PATH):
    # Deletes an existing socket file first. A test must pass a temp path.
    if os.path.exists(path):
        os.unlink(path)
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.bind(path)
    os.chmod(path, 0o600)
    s.listen(4)
    s.setblocking(False)
    return s


class Server(object):
    def __init__(self, listeners, log=None, on_drop=None):
        self._listeners = list(listeners)
        self._on_drop = on_drop
        self._log = log or (lambda msg: None)
        self.clients = []

    def poll(self):
        """Accept, read. Returns [(client, "line", bytes)]."""
        events = []
        for ls in self._listeners:
            while True:
                try:
                    conn, _ = ls.accept()
                except (BlockingIOError, InterruptedError):
                    break
                except Exception:
                    break
                conn.setblocking(False)
                c = Client(conn)
                self.clients.append(c)
        for c in list(self.clients):
            try:
                data = c.sock.recv(65536)
            except (BlockingIOError, InterruptedError):
                continue
            except Exception:
                self.drop(c)
                continue
            if data == b"":
                self.drop(c)
                continue
            self._feed(c, data, events)
        return events

    def _feed(self, c, data, events):
        if not c.checked:
            c.checked = True
            if data.lstrip()[:1] != b"{":
                # An old text-protocol client. Tell it at once and hang up.
                self.send(c, OLD_PROTOCOL_REPLY)
                self.close_after_flush(c)
                return
        if c.close_after_flush:
            return
        c.inbuf += data
        if len(c.inbuf) > MAX_LINE_BYTES and b"\n" not in c.inbuf:
            self.drop(c)
            return
        while b"\n" in c.inbuf:
            line, c.inbuf = c.inbuf.split(b"\n", 1)
            if line.strip():
                events.append((c, "line", line))

    def send(self, c, text):
        c.out += text.encode("utf-8") + b"\n"
        if len(c.out) > MAX_OUT_BYTES:
            self.drop(c)

    def close_after_flush(self, c):
        c.close_after_flush = True

    def flush(self):
        for c in list(self.clients):
            if c.out:
                try:
                    n = c.sock.send(c.out)
                    c.out = c.out[n:]
                except (BlockingIOError, InterruptedError):
                    continue
                except Exception:
                    self.drop(c)
                    continue
            if c.close_after_flush and not c.out:
                self.drop(c)

    def drop(self, c):
        try:
            c.sock.close()
        except Exception:
            pass
        if c in self.clients:
            self.clients.remove(c)
            if self._on_drop:
                self._on_drop(c)

    def close(self, unix_paths=()):
        for c in list(self.clients):
            self.drop(c)
        for ls in self._listeners:
            try:
                ls.close()
            except Exception:
                pass
        for p in unix_paths:
            if os.path.exists(p):
                os.unlink(p)
