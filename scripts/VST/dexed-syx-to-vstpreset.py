#!/usr/bin/env python3
"""Turn Dexed (DX7) .syx cartridges into .vstpreset files, one per voice.

    ./scripts/VST/dexed-syx-to-vstpreset.py Cartridges \\
        --template "Say Again.vstpreset" --out presets

A cartridge is a 32-voice DX7 bulk dump (4104 bytes). Dexed saves its state as
an XML document that holds the whole loaded cartridge and the current voice:

  <dexedState ... currentProgram="N" ...>
    <dexedBlob base64:sysex="4104.<cartridge>" base64:program="161.<voice>"/>

so one preset per voice is: the same cartridge, `currentProgram` = N, and
`program` = voice N unpacked to Dexed's 161-byte layout (the DX7's packed
128-byte voice, expanded; the last 6 bytes are zero in a saved state).

The blobs use JUCE's base64: "<size>." then 6-bit groups taken from the low bits
of each byte, with the alphabet ".A-Za-z0-9+".

Everything else (the settings in the XML, the 'VC2!' header and the trailing
JUCE data) comes from the template, a .vstpreset that Dexed saved once.
Checked: with the template's own cartridge and voice, the result is
byte-for-byte the template. See docs/vst3-on-push3.md ("Convert Dexed
cartridges").
"""
import argparse
import os
import re
import struct
import sys

ALPHABET = ".ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+"
VOICES = 32
CART_LEN = 4104  # F0 43 00 09 20 00 + 32 * 128 + checksum + F7


def juce_b64_encode(data):
    val = nbits = 0
    out = []
    for byte in data:
        val |= byte << nbits
        nbits += 8
        while nbits >= 6:
            out.append(ALPHABET[val & 63])
            val >>= 6
            nbits -= 6
    if nbits:
        out.append(ALPHABET[val & 63])
    return "%d.%s" % (len(data), "".join(out))


def juce_b64_decode(text):
    size, _, body = text.partition(".")
    size = int(size)
    out, val, nbits = bytearray(), 0, 0
    for ch in body:
        val |= ALPHABET.index(ch) << nbits
        nbits += 6
        while nbits >= 8 and len(out) < size:
            out.append(val & 0xFF)
            val >>= 8
            nbits -= 8
    return bytes(out[:size])


def unpack_voice(bulk):
    """A packed 128-byte DX7 voice -> Dexed's 161-byte unpacked layout."""
    u = bytearray(161)
    for op in range(6):
        b, o = op * 17, op * 21
        u[o:o + 11] = bulk[b:b + 11]
        u[o + 11] = bulk[b + 11] & 3
        u[o + 12] = (bulk[b + 11] >> 2) & 3
        u[o + 13] = bulk[b + 12] & 7
        u[o + 14] = bulk[b + 13] & 3
        u[o + 15] = bulk[b + 13] >> 2
        u[o + 16] = bulk[b + 14]
        u[o + 17] = bulk[b + 15] & 1
        u[o + 18] = bulk[b + 15] >> 1
        u[o + 19] = bulk[b + 16]
        u[o + 20] = (bulk[b + 12] >> 3) & 0x7F
    u[126:135] = bulk[102:111]
    u[135] = bulk[111] & 7
    u[136] = bulk[111] >> 3
    u[137:141] = bulk[112:116]
    u[141] = bulk[116] & 1
    u[142] = (bulk[116] >> 1) & 7
    u[143] = bulk[116] >> 4
    u[144:155] = bulk[117:128]
    return bytes(u)  # bytes 155-160 stay 0, as in a saved state


class Template:
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
        comp = dict(self.chunks)[b"Comp"]
        if comp[:4] != b"VC2!":
            sys.exit("template state is not a Dexed state (no 'VC2!' header)")
        (xml_len,) = struct.unpack("<I", comp[4:8])
        self.xml = comp[8:8 + xml_len].decode("utf-8")
        self.tail = comp[8 + xml_len:]

    def build(self, cartridge, index):
        xml = re.sub(r'(base64:sysex=")[^"]*(")',
                     lambda m: m.group(1) + juce_b64_encode(cartridge) + m.group(2), self.xml)
        voice = unpack_voice(cartridge[6 + index * 128:6 + (index + 1) * 128])
        xml = re.sub(r'(base64:program=")[^"]*(")',
                     lambda m: m.group(1) + juce_b64_encode(voice) + m.group(2), xml)
        xml = re.sub(r'(currentProgram=")\d+(")', lambda m: m.group(1) + str(index) + m.group(2), xml)
        raw = xml.encode("utf-8")
        comp = b"VC2!" + struct.pack("<I", len(raw)) + raw + self.tail
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


def voice_name(cartridge, index):
    raw = cartridge[6 + index * 128 + 118:6 + index * 128 + 128]
    return "".join(chr(b) if 32 <= b < 127 else " " for b in raw).strip() or "Voice"


def safe(text):
    return re.sub(r'[\\/:*?"<>|]+', "-", text).strip(" .") or "voice"


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("source", help="a .syx cartridge or a folder of them (searched recursively)")
    ap.add_argument("--template", required=True,
                    help="a .vstpreset that Dexed saved (any voice)")
    ap.add_argument("--out", default="out-dexed", help="output folder")
    ap.add_argument("--flat", action="store_true",
                    help="no sub-folder per source folder")
    a = ap.parse_args()

    tpl = Template(a.template)
    files = []
    if os.path.isdir(a.source):
        for dirpath, _, names in sorted(os.walk(a.source)):
            files += [os.path.join(dirpath, n) for n in sorted(names) if n.lower().endswith(".syx")]
    else:
        files = [a.source]
    count = 0
    for path in files:
        data = open(path, "rb").read()
        if len(data) != CART_LEN or data[:4] != b"\xf0\x43\x00\x09":
            print("skipped %s: not a 32-voice DX7 cartridge" % path, file=sys.stderr)
            continue
        rel = os.path.relpath(os.path.dirname(path), a.source) if os.path.isdir(a.source) else "."
        folder = a.out if (a.flat or rel == ".") else os.path.join(a.out, rel)
        os.makedirs(folder, exist_ok=True)
        cart = safe(os.path.splitext(os.path.basename(path))[0])
        for i in range(VOICES):
            name = "%s - %02d %s" % (cart, i + 1, safe(voice_name(data, i)))
            with open(os.path.join(folder, name + ".vstpreset"), "wb") as f:
                f.write(tpl.build(data, i))
            count += 1
    print("wrote %d presets to %s" % (count, a.out))


if __name__ == "__main__":
    main()
