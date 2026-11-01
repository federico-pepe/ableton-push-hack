#!/usr/bin/env python3
"""Make plugin-folders.json for push-manager from Airwindopedia.txt.

Reads the category lines ("Reverb: VerbSevens, kRockstar2, ...") under the
"# Categories" heading. Each category becomes a folder under one top-level
folder. Names are matched to plugin folder names, ignoring spaces and case
("Hard Vacuum" is HardVacuum). Names with no match are listed on stderr and
left out. The order of the file is kept: Airwindopedia lists the best first.

    make-plugin-folders.py --airwindopedia Airwindopedia.txt \
        --plugins-dir airwindows/plugins/LinuxVST/src \
        --out resources/VST/Airwindows/plugin-folders.json
"""
import argparse
import json
import os
import re
import sys

# One line of Airwindopedia has "XBandpass. XHighpass": a period, not a comma.
LINE = re.compile(r"^([^:#]+):\s*(.+)$")


def read_categories(path):
    """[(category, [name, ...])] from the block after '# Categories'."""
    cats, inside = [], False
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.rstrip("\n")
            if not inside:
                inside = line.strip().lower() == "# categories"
                continue
            if line.startswith("#"):
                break
            m = LINE.match(line.strip())
            if m:
                cats.append((m.group(1).strip(), [n.strip() for n in re.split(r",|\.\s", m.group(2)) if n.strip()]))
    return cats


def key(name):
    return re.sub(r"\s+", "", name).lower()


def build(cats, plugin_names, root_name="Airwindows"):
    """Returns (config dict, [(category, name) with no match])."""
    by_key = {key(n): n for n in plugin_names}
    missing, folders = [], []
    for cat, names in cats:
        found = []
        for n in names:
            real = by_key.get(key(n))
            if real is None:
                missing.append((cat, n))
            elif real not in found:
                found.append(real)
        if found:
            folders.append({"name": cat, "plugins": found})
    return {"folders": [{"name": root_name, "folders": folders}]}, missing


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--airwindopedia", required=True)
    ap.add_argument("--plugins-dir", required=True, help="folder with one sub-folder per plugin")
    ap.add_argument("--out", required=True)
    ap.add_argument("--root-name", default="Airwindows")
    a = ap.parse_args()
    names = sorted(d for d in os.listdir(a.plugins_dir) if os.path.isdir(os.path.join(a.plugins_dir, d)))
    cats = read_categories(a.airwindopedia)
    if not cats:
        sys.exit("no category lines found under '# Categories'")
    cfg, missing = build(cats, names, a.root_name)
    for cat, n in missing:
        print("no plugin for %s: %s" % (cat, n), file=sys.stderr)
    os.makedirs(os.path.dirname(os.path.abspath(a.out)), exist_ok=True)
    with open(a.out, "w", encoding="utf-8") as f:
        json.dump(cfg, f, indent=2)
        f.write("\n")
    n = sum(len(f["plugins"]) for f in cfg["folders"][0]["folders"])
    print("%d categories, %d entries -> %s" % (len(cfg["folders"][0]["folders"]), n, a.out))


if __name__ == "__main__":
    main()
