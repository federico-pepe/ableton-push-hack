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

Single patches (messages at 02 00 xx 00) are converted with --patch-template,
a preset the plugin saved after selecting a patch. The plugin loads a patch into
the Upper part of the current performance: the performance and the Lower tone
stay as they were, and only the tone at 01 00 40 00 changes. So a patch preset
is the template with that one message replaced. The patch tone is converted
like a performance tone (address, extra 0x00 byte, checksum).

The patch manager data at the end of the state holds an MD5 of the loaded
performance or patch: the MD5 of the dump data bytes (messages cut to their data,
joined). It is refreshed in every preset. The bank and program numbers there are
not (they are only labels of the patch manager, and do not change the sound).
"""
import argparse
import hashlib
import os
import re
import struct
import sys

COMMON_LEN = 48   # performance common message
TONE_LEN = 251    # tone message in a dump (252 in the plugin's own state)
TEMP_HI = (0x01, 0x00)
UPPER_TONE_LO = (0x40, 0x00)  # where the plugin keeps the Upper tone


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


def to_state_message(msg, lower=None):
    """A dump message -> the same message in the plugin's temporary area.

    lower: the last two address bytes to write, for a patch (02 00 xx 00) that
    goes to the Upper tone (40 00); a performance message keeps its own.
    """
    body = bytearray(msg[6:-2])  # address + data
    body[0], body[1] = TEMP_HI
    if lower:
        body[2], body[3] = lower
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


def set_hash(comp, md5hex):
    """Write md5hex after every 'hash|' in the patch manager data (same length)."""
    out, i = bytearray(comp), 0
    while True:
        i = bytes(out).find(b"hash|", i)
        if i < 0:
            return bytes(out)
        out[i + 5:i + 5 + 32] = md5hex.encode("ascii")
        i += 5 + 32


def data_md5(messages):
    return hashlib.md5(b"".join(m[10:-2] for m in messages)).hexdigest()


def patches(syx):
    """List of (name, message) for each single patch (251 bytes at 02 00 xx 00)."""
    return [(m[10:26].decode("latin1").rstrip(), m) for m in split_sysex(syx)
            if len(m) == TONE_LEN and m[6] == 0x02]


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

    def build_patch(self, tone, md5hex):
        """The template with its Upper tone (01 00 40 00) replaced."""
        comp = bytearray(self.comp)
        i = 0
        while i < len(comp):
            if comp[i] != 0xF0:
                i += 1
                continue
            j = bytes(comp).index(b"\xf7", i)
            if bytes(comp[i + 6:i + 10]) == b"\x01\x00\x40\x00":
                if j + 1 - i != len(tone):
                    sys.exit("template Upper tone has another length than a converted tone")
                comp[i:j + 1] = tone
                return self.vstpreset(set_hash(bytes(comp), md5hex))
            i = j + 1
        sys.exit("no Upper tone (01 00 40 00) in the patch template")

    def build(self, messages, md5hex=None):
        new = b"".join(messages)
        old_len = self.last - self.first
        comp = bytearray(self.comp[:self.first] + new + self.comp[self.last:])
        delta = len(new) - old_len
        if delta:  # the three sizes that cover the message block
            for off in (16, 28, 32):
                (v,) = struct.unpack("<I", comp[off:off + 4])
                comp[off:off + 4] = struct.pack("<I", v + delta)
        return self.vstpreset(set_hash(bytes(comp), md5hex) if md5hex else bytes(comp))

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
    ap.add_argument("--patch-template", metavar="VSTPRESET",
                    help="also convert the single patches; a preset saved after "
                         "selecting a patch in the plugin")
    ap.add_argument("--out", default="out-je8086", help="output folder")
    a = ap.parse_args()

    tpl = Template(a.template)
    syx = open(a.syx, "rb").read()
    perfs = performances(syx)
    perf_dir = os.path.join(a.out, "Performances") if a.patch_template else a.out
    os.makedirs(perf_dir, exist_ok=True)
    used = {}
    for name, msgs in perfs:
        if len(msgs) < 5:
            print("skipped %r: only %d messages" % (name, len(msgs)), file=sys.stderr)
            continue
        base = safe(name)
        used[base] = used.get(base, 0) + 1
        if used[base] > 1:
            base = "%s (%d)" % (base, used[base])
        data = tpl.build([to_state_message(m) for m in msgs[:5]], data_md5(msgs[:5]))
        with open(os.path.join(perf_dir, base + ".vstpreset"), "wb") as f:
            f.write(data)
    print("wrote %d performances to %s" % (sum(used.values()), perf_dir))

    if a.patch_template:
        ptpl = Template(a.patch_template)
        patch_dir = os.path.join(a.out, "Patches")
        os.makedirs(patch_dir, exist_ok=True)
        taken = set(used)
        n = 0
        for name, msg in patches(syx):
            base = safe(name)
            if base in taken:  # same name as a performance: Browser Bridge loads by name
                base += " (patch)"
            taken.add(base)
            data = ptpl.build_patch(to_state_message(msg, UPPER_TONE_LO), data_md5([msg]))
            with open(os.path.join(patch_dir, base + ".vstpreset"), "wb") as f:
                f.write(data)
            n += 1
        print("wrote %d patches to %s" % (n, patch_dir))


if __name__ == "__main__":
    main()
