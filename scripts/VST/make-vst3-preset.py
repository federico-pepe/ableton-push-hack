#!/usr/bin/env python3
"""Make Live device presets (.adg) for VST3 instruments on Push 3, no Mac needed.

Push's browser does not list VST3 plugins, but it does load a saved .adg that
points at one. Live finds the plugin by its class ID, so the file needs only
the ID and a name. Parameters and patch state use the plugin's own defaults.

Two ways to give the plugin ID:

  # Read every scanned VST3 instrument from Push (needs ssh as root@push.local)
  ./scripts/VST/make-vst3-preset.py --from-device push.local --install

  # List a plugin's parameters (runs a small VST3 loader on Push), then map some
  ./scripts/VST/make-vst3-preset.py --from-device push.local --name "Surge XT" --list-params --filter cutoff
  ./scripts/VST/make-vst3-preset.py --from-device push.local --name "Surge XT" --install \\
      --params "A Filter 1 Cutoff,A Filter 1 Resonance,643940465"

  # Without Push: pass the ID and parameter IDs by hand
  ./scripts/VST/make-vst3-preset.py --name "Surge XT" --uid abcdef01-9182-faeb-566d-624153675854 \\
      --params 643940465,627352114,627352115

  # Or pass the ID by hand (dashed or plain 32 hex digits)
  ./scripts/VST/make-vst3-preset.py --name "Surge XT" --uid abcdef01-9182-faeb-566d-624153675854

Only instruments are supported. See docs/vst3-on-push3.md for the full steps.
"""
import argparse
import gzip
import json
import os
import re
import shlex
import struct
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
TEMPLATE = os.path.join(HERE, "templates", "vst3-instrument.adg.xml")
PARAMS_SCRIPT = os.path.join(HERE, "vst3_params.py")
USER_PRESETS = "/data/Music/Ableton/User Library/Presets/Instruments/VST3"

DEVICE_QUERY = """
import sqlite3, json
c = sqlite3.connect("file:/data/.local/share/Ableton/Live Database/Live-plugins-1.db?mode=ro", uri=True)
rows = c.execute("select p.dev_identifier, p.name, m.path from plugins p "
                 "join plugin_modules m on m.module_id = p.module_id where p.enabled=1").fetchall()
print(json.dumps(rows))
"""


def uid_fields(uid):
    """32 hex digits -> four signed 32-bit ints, big-endian, as Live stores them."""
    h = re.sub(r"[^0-9a-fA-F]", "", uid)
    if len(h) != 32:
        raise ValueError("plugin ID must have 32 hex digits, got %d: %r" % (len(h), uid))
    return struct.unpack(">4i", bytes.fromhex(h))


def params_xml(params):
    """Parameters to show on Push's encoders, in order. Each is (id, kind)."""
    if not params:
        return "<ParameterSettings />"
    pad = "\t" * 6
    rows = []
    for i, (pid, kind) in enumerate(params):
        rows.append(
            "{p}<PluginParameterSettings Id=\"{i}\">\n"
            "{p}\t<Index Value=\"{i}\" />\n"
            "{p}\t<VisualIndex Value=\"{i}\" />\n"
            "{p}\t<ParameterId Value=\"{pid}\" />\n"
            "{p}\t<Type Value=\"Plugin{kind}Parameter\" />\n"
            "{p}\t<MacroControlIndex Value=\"-1\" />\n"
            "{p}\t<MidiControllerRange />\n"
            "{p}\t<LomId Value=\"0\" />\n"
            "{p}</PluginParameterSettings>".format(p=pad + "\t", i=i, pid=pid, kind=kind)
        )
    return "<ParameterSettings>\n" + "\n".join(rows) + "\n" + pad + "</ParameterSettings>"


def parse_params(text, listing=None):
    """'643940465,A Osc 1 Shape,627352114:enum' -> [(id, 'Float' | 'Enum'), ...]

    Each item is a parameter ID or, with a listing from the plugin, its exact
    title (case-insensitive). ':enum' marks a list parameter by hand.
    """
    by_id = {p["id"]: p for p in (listing or [])}
    by_title = {}
    for p in listing or []:
        by_title.setdefault(p["title"].lower(), []).append(p)
    out = []
    for item in text.split(","):
        item, _, kind = item.strip().partition(":")
        if item.isdigit():
            pid = int(item)
            known = by_id.get(pid)
        else:
            hits = by_title.get(item.lower(), [])
            if not hits:
                sys.exit("no parameter named %r (try --list-params --filter)" % item)
            if len(hits) > 1:
                sys.exit("%d parameters are named %r; use the ID" % (len(hits), item))
            known = hits[0]
            pid = known["id"]
        is_enum = kind.lower() == "enum" or bool(known and known["list"])
        out.append((pid, "Enum" if is_enum else "Float"))
    return out


def render(name, uid, params=None):
    f = uid_fields(uid)
    xml = open(TEMPLATE, encoding="utf-8").read().replace("@PARAMS@", params_xml(params))
    for i, v in enumerate(f):
        xml = xml.replace("@F%d@" % i, str(v))
    safe = name.replace("&", "&amp;").replace('"', "&quot;").replace("<", "&lt;")
    return xml.replace("@NAME@", safe)


def write_adg(out_dir, name, uid, params=None):
    os.makedirs(out_dir, exist_ok=True)
    fname = re.sub(r"[^\w .()-]", "_", name) + ".adg"
    path = os.path.join(out_dir, fname)
    with gzip.open(path, "wb") as g:
        g.write(render(name, uid, params).encode("utf-8"))
    return path


def ssh_python(host, source, *args):
    out = subprocess.run(
        ["ssh", "root@" + host, "HOME=/tmp nice -n 19 python3 - " + " ".join(shlex.quote(x) for x in args)],
        input=source, capture_output=True, text=True,
    )
    if out.returncode != 0:
        sys.exit("ssh failed: " + out.stderr.strip())
    return out.stdout


def read_device(host):
    found = []
    for ident, name, path in json.loads(ssh_python(host, DEVICE_QUERY)):
        # device:vst3:instr:<uuid>?n=Name
        m = re.match(r"device:vst3:(\w+):([0-9a-fA-F-]{32,36})", ident)
        if not m:
            continue
        if m.group(1) != "instr":
            print("skip (not an instrument): " + name, file=sys.stderr)
            continue
        found.append((name, m.group(2), path))
    return found


def read_params(host, path):
    """All parameters of one plugin, read on Push. Needs the plugin to load there."""
    src = open(PARAMS_SCRIPT, encoding="utf-8").read()
    classes = json.loads(ssh_python(host, src, path))
    return classes[0]["params"]


def install(host, paths):
    subprocess.run(["ssh", "root@" + host, "mkdir -p '%s'" % USER_PRESETS], check=True)
    for p in paths:
        subprocess.run(["scp", "-q", p, "root@%s:%s/" % (host, USER_PRESETS)], check=True)
    subprocess.run(
        ["ssh", "root@" + host, "chown -R ableton:users '%s'" % USER_PRESETS], check=True
    )


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--from-device", metavar="HOST", help="read scanned plugins from Push over ssh")
    ap.add_argument("--name", help="plugin name; with --from-device a part of it picks one plugin")
    ap.add_argument("--uid", help="plugin class ID (with --name, without --from-device)")
    ap.add_argument("--list-params", action="store_true",
                    help="print the plugin's parameters (needs --from-device and --name)")
    ap.add_argument("--filter", metavar="TEXT", help="with --list-params: only titles that contain TEXT")
    ap.add_argument("--params", metavar="ID|NAME,...",
                    help="parameters to map to Push's encoders, in order. Each is an ID "
                         "or an exact title (titles need --from-device). Add :enum for a "
                         "list parameter.")
    ap.add_argument("--out", default="out-presets", help="local output folder")
    ap.add_argument("--install", action="store_true",
                    help="with --from-device: copy presets into Push's User Library")
    a = ap.parse_args()

    if a.from_device:
        plugins = read_device(a.from_device)
        if a.name:
            plugins = [p for p in plugins if a.name.lower() in p[0].lower()]
        if not plugins:
            sys.exit("no matching VST3 instrument in Live's plugin database; "
                     "see docs/vst3-on-push3.md (flag + restart)")
    elif a.name and a.uid and not a.list_params:
        plugins = [(a.name, a.uid, None)]
    else:
        ap.error("use --from-device HOST, or --name with --uid")

    if a.list_params or (a.params and not a.params.replace(",", "").replace(":enum", "").isdigit()):
        if not a.from_device:
            ap.error("--list-params and parameter titles need --from-device")
        if len(plugins) != 1:
            sys.exit("pick one plugin with --name; matches: " + ", ".join(p[0] for p in plugins))
        listing = read_params(a.from_device, plugins[0][2])
    else:
        listing = None

    if a.list_params:
        shown = [p for p in listing if not p["hidden"]
                 and (not a.filter or a.filter.lower() in p["title"].lower())]
        for p in shown:
            kind = "list" if p["list"] else ("%d steps" % p["steps"] if p["steps"] else "")
            print("%12d  %s%s%s" % (p["id"], p["title"],
                                    " (%s)" % p["units"] if p["units"] else "",
                                    "  [%s]" % kind if kind else ""))
        print("%d of %d parameters" % (len(shown), len(listing)), file=sys.stderr)
        return

    params = parse_params(a.params, listing) if a.params else None
    paths = [write_adg(a.out, n, u, params) for n, u, _ in plugins]
    for p in paths:
        print("wrote " + p)
    if a.install:
        if not a.from_device:
            ap.error("--install needs --from-device")
        install(a.from_device, paths)
        print("installed to " + USER_PRESETS + " on " + a.from_device)


if __name__ == "__main__":
    main()
