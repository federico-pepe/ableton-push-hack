#!/usr/bin/env python3
"""Turn JE8086 (JP-8000 emulation) .syx performances into .vstpreset files.

    ./scripts/VST/je8086-syx-to-vstpreset.py Factory_presets.syx \\
        --template Chariot.vstpreset --out presets/Performances

The JE8086 plugin keeps its state as MIDI System Exclusive messages: the
temporary performance, as Roland DT1 messages at address 01 00 xx xx. A
.syx performance dump holds the same messages at 03 pp xx xx. So a preset is
a template .vstpreset (saved from the plugin once) with those messages
swapped for the ones in the dump. What changes between the dump and the state:

  address      03 pp xx xx  ->  01 00 xx xx
  tone (251 B) one extra 0x00 data byte before the checksum (240 data bytes)
  checksum     recomputed

Checked on the factory dump: the first performance, converted, matches the
messages in a preset the plugin saved byte for byte. See
docs/vst3-on-push3.md ("Convert JE8086 performances").

Only performances are converted (the 48-byte common message starts one).
Single patches (messages at 02 00 xx 00) are skipped: how the plugin stores a
single patch is not known yet.
"""
import argparse
import os
import re
import struct
import sys

COMMON_LEN = 48   # performance common message
TONE_LEN = 251    # tone message in a dump (252 in the plugin's own state)
TEMP_HI = (0x01, 0x00)


def split_sysex(data):
    msgs, i = [], 0
    while i < len(data):
        if data[i] != 0xF0:
            i += 1
            continue
        j = data.index(b"\xf7", i)
        msgs.append(data[i:j + 1])
        i = j + 1
    return msgs


def checksum(body):
    return (128 - (sum(body) & 0x7F)) & 0x7F


def to_state_message(msg):
    """A dump message -> the same message in the plugin's temporary area."""
    body = bytearray(msg[6:-2])  # address + data
    body[0], body[1] = TEMP_HI
    if len(msg) == TONE_LEN:
        body.append(0x00)
    return bytes(msg[:6]) + bytes(body) + bytes([checksum(body)]) + b"\xf7"


def performances(syx):
    """List of (name, [messages]) for each performance in the dump.

    A performance is a 48-byte common message at 03 pp 00 00, then its two
    effect messages (19 bytes) and its two tones (251 bytes) at 03 pp xx xx.
    Messages in other areas (patch memory, 02 00 xx 00) are not part of it.
    """
    out, cur = [], None
    for m in split_sysex(syx):
        if len(m) < 12 or m[:6] != b"\xf0\x41\x10\x00\x06\x12":
            continue
        if len(m) == COMMON_LEN and m[6] == 0x03:
            name = m[10:26].decode("latin1").rstrip()
            cur = (name, [m])
            out.append(cur)
        elif cur and m[6] == 0x03 and m[7] == cur[1][0][7]:
            cur[1].append(m)
    return out


class Template:
    """A .vstpreset saved by the plugin, with its Comp chunk cut open."""

    def __init__(self, path):
        d = open(path, "rb").read()
        if d[:4] != b"VST3":
            sys.exit("%s is not a .vstpreset" % path)
        self.class_id = d[8:40]
        (lo,) = struct.unpack("<Q", d[40:48])
        (n,) = struct.unpack("<I", d[lo + 4:lo + 8])
        self.chunks = []
        for i in range(n):
            o = lo + 8 + i * 20
            cid = d[o:o + 4]
            off, size = struct.unpack("<QQ", d[o + 4:o + 20])
            self.chunks.append((cid, d[off:off + size]))
        self.comp = dict(self.chunks)[b"Comp"]
        # The temporary performance: DT1 messages at 01 00 xx xx.
        self.first = self.last = None
        i = 0
        while i < len(self.comp):
            if self.comp[i] == 0xF0:
                j = self.comp.index(b"\xf7", i)
                m = self.comp[i:j + 1]
                if m[:6] == b"\xf0\x41\x10\x00\x06\x12" and m[6:8] == bytes(TEMP_HI):
                    if self.first is None:
                        self.first = i
                    self.last = j + 1
                i = j + 1
            else:
                i += 1
        if self.first is None:
            sys.exit("no temporary performance (01 00 xx xx) in the template; "
                     "save a preset with a performance loaded")

    def build(self, messages):
        new = b"".join(messages)
        old_len = self.last - self.first
        comp = bytearray(self.comp[:self.first] + new + self.comp[self.last:])
        delta = len(new) - old_len
        if delta:  # the three sizes that cover the message block
            for off in (16, 28, 32):
                (v,) = struct.unpack("<I", comp[off:off + 4])
                comp[off:off + 4] = struct.pack("<I", v + delta)
        return self.vstpreset(bytes(comp))

    def vstpreset(self, comp):
        body, entries = bytearray(), []
        for cid, payload in self.chunks:
            payload = comp if cid == b"Comp" else payload
            entries.append((cid, 48 + len(body), len(payload)))
            body += payload
        out = bytearray(b"VST3") + struct.pack("<I", 1) + self.class_id
        out += struct.pack("<Q", 48 + len(body)) + body
        out += b"List" + struct.pack("<I", len(entries))
        for cid, off, size in entries:
            out += cid + struct.pack("<QQ", off, size)
        return bytes(out)


def safe(text):
    return re.sub(r'[\\/:*?"<>|]+', "-", text).strip(" .") or "preset"


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("syx", help="the .syx dump of the plugin's performances")
    ap.add_argument("--template", required=True,
                    help="a .vstpreset saved by the plugin with a performance loaded")
    ap.add_argument("--out", default="out-je8086", help="output folder")
    a = ap.parse_args()

    tpl = Template(a.template)
    perfs = performances(open(a.syx, "rb").read())
    os.makedirs(a.out, exist_ok=True)
    used = {}
    for name, msgs in perfs:
        if len(msgs) < 5:
            print("skipped %r: only %d messages" % (name, len(msgs)), file=sys.stderr)
            continue
        base = safe(name)
        used[base] = used.get(base, 0) + 1
        if used[base] > 1:
            base = "%s (%d)" % (base, used[base])
        data = tpl.build([to_state_message(m) for m in msgs[:5]])
        with open(os.path.join(a.out, base + ".vstpreset"), "wb") as f:
            f.write(data)
    print("wrote %d performances to %s" % (sum(used.values()), a.out))


if __name__ == "__main__":
    main()
