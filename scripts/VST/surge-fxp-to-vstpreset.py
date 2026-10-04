#!/usr/bin/env python3
"""Turn Surge XT .fxp patches into .vstpreset files for the Push 3 User Library.

Push's browser hides plugins, but the PLUGINS tab (push-manager) and Browser
Bridge list a plugin's .vstpreset files from the User Library. Surge ships its
factory patches as .fxp, so this converts them. No Live, no Surge needed.

    ./scripts/VST/surge-fxp-to-vstpreset.py \\
        "/Library/Application Support/Surge XT/patches_factory" \\
        --out out-surge --install push.local

How the files are built (read from a .vstpreset that Live saved for Surge XT):

  .fxp           'CcnK' header, 60 bytes, then the patch chunk.
  patch chunk    'sub3' + xml size + ... + XML + 4108 bytes of tail data.
  Comp state     the same patch chunk, then 16 zero bytes and 'JUCEPrivateData'
                 (the JUCE VST3 wrapper's end mark). Surge loads both with the
                 same code, so the chunk needs no conversion.
  .vstpreset     'VST3', version 1, the plugin class ID as 32 ASCII hex digits,
                 the offset of the chunk list, then the chunks 'Comp' (state),
                 'Cont' (empty) and 'Info' (MetaInfo XML), then the list.

With --folders the files keep the source's category folders (Basses/, Pads/ ...).
Names are "<Category> - <Patch>" so two patches with the same name in
different folders stay apart: Browser Bridge finds a preset by name.
"""
import argparse
import os
import re
import struct
import subprocess
import sys

SURGE_XT_CLASS_ID = "ABCDEF019182FAEB566D624153675854"
USER_LIBRARY = "/data/Music/Ableton/User Library"
DEFAULT_SUBDIR = "Surge XT Factory"

INFO_XML = (
    "<?xml version='1.0' encoding='utf-8'?>\n<MetaInfo>"
    "<Attribute id='MediaType' value='VstPreset' type='string' flags='writeProtected'></Attribute>"
    "<Attribute id='PlugInCategory' value='Instrument|Synth' type='string' flags='writeProtected'></Attribute>"
    "<Attribute id='plugtype' value='Instrument|Synth' type='string' flags='writeProtected'></Attribute>"
    "<Attribute id='PlugInName' value='Surge XT' type='string' flags='writeProtected'></Attribute>"
    "<Attribute id='plugname' value='Surge XT' type='string' flags='writeProtected'></Attribute>"
    "<Attribute id='PlugInVendor' value='Surge Synth Team' type='string' flags='writeProtected'></Attribute>"
    "</MetaInfo>"
).encode("utf-8")

JUCE_END_MARK = bytes(16) + b"JUCEPrivateData"


def fxp_chunk(data):
    """The patch chunk inside an .fxp (chunk-type, opaque 'FPCh' file)."""
    if data[:4] != b"CcnK" or data[8:12] != b"FPCh":
        raise ValueError("not an opaque-chunk .fxp")
    (size,) = struct.unpack(">I", data[56:60])
    chunk = data[60:60 + size]
    if len(chunk) != size or chunk[:4] != b"sub3":
        raise ValueError("unexpected patch chunk (no 'sub3' header)")
    return chunk


def vstpreset(class_id, comp, info=INFO_XML):
    """A .vstpreset with Comp, an empty Cont and Info chunks."""
    body = bytearray()
    entries = []
    for cid, payload in ((b"Comp", comp), (b"Cont", b""), (b"Info", info)):
        entries.append((cid, 48 + len(body), len(payload)))
        body += payload
    list_off = 48 + len(body)
    out = bytearray(b"VST3")
    out += struct.pack("<I", 1)
    out += class_id.encode("ascii")
    out += struct.pack("<Q", list_off)
    out += body
    out += b"List" + struct.pack("<I", len(entries))
    for cid, off, size in entries:
        out += cid + struct.pack("<QQ", off, size)
    return bytes(out)


def patch_name(chunk, fallback):
    """Name from the patch XML's <meta name=...>, else the file name."""
    (xml_size,) = struct.unpack("<I", chunk[4:8])
    xml = chunk[32:32 + xml_size].decode("utf-8", "replace")
    m = re.search(r'<meta\s+name="([^"]*)"', xml)
    return (m.group(1) if m and m.group(1).strip() else fallback).strip()


def safe(text):
    return re.sub(r'[\\/:*?"<>|]+', "-", text).strip(" .") or "patch"


def convert(src_root, out_dir, class_id, subdir, folders=False):
    made, skipped = [], []
    used = {}
    for dirpath, _, files in sorted(os.walk(src_root)):
        for fn in sorted(files):
            if not fn.lower().endswith(".fxp"):
                continue
            path = os.path.join(dirpath, fn)
            rel = os.path.relpath(dirpath, src_root)
            category = "Patches" if rel == "." else rel.split(os.sep)[0]
            try:
                with open(path, "rb") as f:
                    chunk = fxp_chunk(f.read())
            except (ValueError, struct.error) as e:
                skipped.append((path, str(e)))
                continue
            name = patch_name(chunk, os.path.splitext(fn)[0])
            base = safe("%s - %s" % (category, name))
            n = used.get(base, 0)
            used[base] = n + 1
            if n:
                base = "%s (%d)" % (base, n + 1)
            target_dir = os.path.join(out_dir, subdir) if subdir else out_dir
            if folders and rel != ".":
                target_dir = os.path.join(target_dir, rel)  # same folders as the source
            os.makedirs(target_dir, exist_ok=True)
            target = os.path.join(target_dir, base + ".vstpreset")
            with open(target, "wb") as f:
                f.write(vstpreset(class_id, chunk + JUCE_END_MARK))
            made.append(target)
    return made, skipped


def install(host, out_dir, subdir):
    src = os.path.join(out_dir, subdir) if subdir else out_dir
    dst = "%s/%s" % (USER_LIBRARY, subdir) if subdir else USER_LIBRARY
    subprocess.run(["ssh", "root@" + host, "mkdir -p '%s'" % dst], check=True)
    subprocess.run(["rsync", "-a", src + "/", "root@%s:%s/" % (host, dst)], check=True)
    subprocess.run(["ssh", "root@" + host, "chown -R ableton:users '%s'" % dst], check=True)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("source", help="folder with .fxp patches (searched recursively)")
    ap.add_argument("--out", default="out-surge", help="local output folder")
    ap.add_argument("--subdir", default=DEFAULT_SUBDIR,
                    help="folder inside the User Library (default: %(default)s); '' for none")
    ap.add_argument("--folders", action="store_true",
                    help="keep the source's category folders (names stay '<Category> - <Patch>')")
    ap.add_argument("--class-id", default=SURGE_XT_CLASS_ID,
                    help="plugin class ID, 32 hex digits (default: Surge XT)")
    ap.add_argument("--install", metavar="HOST", help="copy the result to Push over ssh")
    a = ap.parse_args()

    class_id = re.sub(r"[^0-9A-Fa-f]", "", a.class_id).upper()
    if len(class_id) != 32:
        ap.error("--class-id needs 32 hex digits")
    made, skipped = convert(a.source, a.out, class_id, a.subdir, a.folders)
    print("wrote %d presets to %s" % (len(made), a.out))
    for path, why in skipped:
        print("skipped %s: %s" % (path, why), file=sys.stderr)
    if a.install and made:
        install(a.install, a.out, a.subdir)
        print("installed to %s on %s" % (USER_LIBRARY, a.install))


if __name__ == "__main__":
    main()
