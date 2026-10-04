# Run Linux VST3 plugins on Push 3

Live on Push 3 can load Linux VST3 plugins itself. You do not need a hack,
a virtual sound card, or MIDI routing. Tested on 2026-10-03 with Surge XT on
AbletonOS v3.23 (Live 12.4.15b5). The plugin loaded, and the pads played it
through the normal Push audio path.

Push does not show VST3 plugins in its browser. You load a plugin from a
saved preset file (`.adg`) instead. This page explains how to do that.

> **Warning:** This changes Live's `Preferences.cfg`. Make a backup first.
> The steps below never write to `/opt`, `/boot`, or `/etc`.

## What works and what is not tested

| Item | Status |
|------|--------|
| Linux VST3 instrument scanned and loaded | Works (Surge XT) |
| Plugin plays from the pads | Works |
| Preset made on a Mac, copied to Push | Works |
| `.vstpreset` in the User Library, loaded through Browser Bridge | Works (Surge XT) |
| Preset made by `make-vst3-preset.py` (no Mac) | Written, **not yet confirmed on the device** |
| Parameters on the encoders, from a preset with a saved map (`.adg` made on a Mac) | Works |
| Parameters on the encoders without a saved map | Does not work. Same on a Mac. `-_PluginAutoPopulateThreshold=16` did not help. |
| Parameter list read from the plugin (`--list-params`) | Works on Push (Surge XT, 2855 parameters; IDs match the Mac file) |
| Parameter map written by `make-vst3-preset.py --params` | Same XML as the Mac file, **not yet confirmed on the device** |
| CPU load while playing | Not measured. Idle Push uses about 13% (Live) and 6% (Push3). |
| VST3 effects | Not tested. The script makes instruments only. |
| VST2 (`.so` files) | Does not work with the system-paths flag. See below. |

## Tested plugins

| Plugin | Result on Push |
|--------|----------------|
| Surge XT 1.4 (instrument) | Scans, loads, plays, presets and encoder map work |
| Dragonfly Reverb 3.2.10 (Early Reflections, Hall, Plate, Room; effects) | All four scan and are listed. Load them on an **audio** track. |
| Dexed 1.0.1 (instrument) | The official Linux download does **not** load: it needs glibc 2.38 and Push has 2.35. Built from source on Ubuntu 22.04 it scans and is listed. |

Rule of thumb: use a plugin built on Ubuntu 22.04 (glibc 2.35) or older. The
error in `PluginScanner.txt` for a build that is too new is
`version 'GLIBC_2.38' not found`. If a plugin has no Linux x86_64 `.vst3`, or
its build is too new, build it yourself. See
[Build a plugin from source](#build-a-plugin-from-source-docker).

## How it works

1. Live scans these folders for `.vst3` bundles: `~/.vst3`, `/usr/lib/vst3`,
   and `/usr/local/lib/vst3`. On Push, `~` for the `ableton` user is `/data`,
   so use `/data/.vst3`.
2. Scanning is **off** in Push's stock `Preferences.cfg`. The `Vst3Preferences`
   block has a flag, `AreSystemPathsEnabled`, set to 0. Live then runs
   `PluginManager: Scan start` and `Scan end` with nothing between them.
3. After you set the flag to 1, the scanner process loads each plugin and
   records it in `/data/.local/share/Ableton/Live Database/Live-plugins-1.db`.
   The scanner log is `PluginScanner.txt` in the Live config folder.
4. Push's browser does not list plugins. A saved `.adg` that names the plugin
   still loads, because Live finds the plugin by its class ID.

## Steps

You need SSH access as `root@push.local`, and Push must be idle.

### 1. Check that the plugin fits

The plugin must be a Linux x86_64 `.vst3`. The system on Push has glibc 2.35
and these libraries: libstdc++, libfreetype, libfontconfig, libX11 and
related X11 libraries. A plugin that needs a newer glibc or a library that is
not on Push will fail to scan. There is no package manager on Push.

### 2. Copy the plugin

```bash
ssh root@push.local 'mkdir -p /data/.vst3 && chown ableton:users /data/.vst3'
scp -r "Surge XT.vst3" root@push.local:/data/.vst3/
ssh root@push.local 'chown -R ableton:users /data/.vst3'
```

### 3. Turn on VST3 scanning

Live rewrites `Preferences.cfg` when it exits, so stop the Push stack first.

```bash
ssh root@push.local 'mkdir -p /data/push-hack/backup'
ssh root@push.local 'python3 - <<EOF
import glob, os, shutil
p = max(glob.glob("/data/.config/Ableton/Live */Preferences.cfg"), key=os.path.getmtime)
shutil.copy2(p, "/data/push-hack/backup/Preferences.cfg.bak")
os.system("/etc/init.d/push3 stop"); 
d = bytearray(open(p, "rb").read())
k = b"\x0fVst3Preferences"
o = d.rfind(k) + len(k)
assert d[o:o+4] == bytes([4,0,0,0]) and d[o+4] == 0, "unexpected layout"
d[o+4] = 1
open(p, "wb").write(d)
print("patched", p)
EOF'
ssh root@push.local '/etc/init.d/push3 start'
```

The byte after `Vst3Preferences` plus a 4-byte count is
`AreSystemPathsEnabled`. The script stops if the bytes are not what it
expects.

To undo this, stop the stack, copy the backup over `Preferences.cfg`, remove
`/data/.vst3`, and start the stack.

### 4. Check the scan

After about 30 seconds, look for the plugin:

```bash
ssh root@push.local 'grep -A8 "VST3: found" "/data/.config/Ableton/Live 12.4.15b5/PluginScanner.txt"'
```

You should see a line such as `VST3: found: Surge XT` with a `device-class-id`.
The `device-class-id` is the plugin's class ID. A scan failure appears as
`VST3: not a plugin` or `Failed to load`.

### 5. Make a preset

Push cannot list the plugin, so make a preset file for it. Use either way.

**Without a Mac (script):**

```bash
./scripts/make-vst3-preset.py --from-device push.local --install
```

The script reads each scanned VST3 instrument from Live's plugin database,
writes one `.adg` for each, and copies them to
`/data/Music/Ableton/User Library/Presets/Instruments/VST3/`. If you know the
class ID, you can skip the device:

```bash
./scripts/make-vst3-preset.py --name "Surge XT" \
    --uid abcdef01-9182-faeb-566d-624153675854
```

The preset has no saved patch. The plugin starts with its own default sound.

**With a Mac:** Install the same plugin on the Mac. Put it on an empty MIDI
track and click the save icon in the device title bar. Copy the `.adg` to the
User Library on Push. This saves the plugin's current patch too.

### 6. Load it on Push

Open the Browser on Push, go to User Library, then Presets, then Instruments,
and load the preset on a MIDI track.

### Optional: map parameters to the encoders

A plugin loaded without a saved map shows no parameters on Push's encoders.
`-_PluginAutoPopulateThreshold=16` in `/data/settings/Options.txt` did not
change this. Live accepts the line, but it had no effect in our test.

A preset that holds a parameter map works. The map is a list of VST3
parameter IDs in the preset's `ParameterSettings` element. Push shows them in
this order, 8 per page.

1. On a Mac, load the plugin and open Configure in the device title bar.
   Click the parameters you want. Save the device as an `.adg`.
2. Copy the `.adg` to Push, as in step 5.

To make the map without a Mac, list the plugin's parameters and pick some.
The script loads the plugin on Push, reads its parameter list, and prints it.
It never opens audio and never shows a window.

```bash
./scripts/make-vst3-preset.py --from-device push.local --name "Surge XT" \
    --list-params --filter cutoff
```

Then write the preset. Give each parameter as an ID or as its exact title
(case does not matter). Push shows them in this order, 8 per page:

```bash
./scripts/make-vst3-preset.py --from-device push.local --name "Surge XT" --install \
    --params "A Filter 1 Cutoff,A Filter 1 Resonance,A Osc 1 Pitch"
```

Parameters that are a list of choices (`list` in the output) are written as
`PluginEnumParameter`. All others are `PluginFloatParameter`. Add `:enum` to
an ID to force the list type. Without Push, give IDs and `--uid` by hand and
no titles.

## The preset file

An `.adg` is gzip-compressed XML. The plugin is a `Vst3Preset` element
inside an instrument rack:

- `Uid` has `Fields.0` to `Fields.3`. These are the class ID's 16 bytes, read
  as four big-endian signed 32-bit integers. For
  `abcdef01-9182-faeb-566d-624153675854`, `Fields.0` is `0xABCDEF01`, which is
  `-1412567295`.
- `DeviceType` is `1` for an instrument.
- `Name` is the display name.
- `ProcessorState` is the saved patch. The generated preset leaves it empty.
- `ParameterSettings` is the encoder map. It has one `PluginParameterSettings`
  per parameter, with `Index`, `VisualIndex` and `ParameterId`.
- The file reference has a Mac path when saved on a Mac. Push ignores it.

The template is `scripts/templates/vst3-instrument.adg.xml`. The schema is
in `/opt/push3/products/live/Live/AppLive/Resources/Schema/` on Push
(`Vst3PluginInfo`, `Vst3Preset`, `PluginDevice`).

## Build a plugin from source (Docker)

Many plugins ship no Linux `.vst3`, or ship one built on a new distribution.
You can build it in an Ubuntu 22.04 container. Its glibc is 2.35, the same as
Push, so the result loads. You need Docker. On an Apple Silicon Mac the
container runs as x86_64 under emulation (`--platform linux/amd64`). This is
slow: the Dexed build below took more than 20 minutes.

We tested this recipe with **Dexed 1.0.1**. Other plugins need other
packages and a different target name, so treat it as a starting point.

1. Save this as `build.sh`. Change `REPO`, `TAG` and `TARGET` for your plugin.
   JUCE plugins use the CMake target `<Name>_VST3`.

   ```bash
   set -eo pipefail
   export DEBIAN_FRONTEND=noninteractive
   REPO=https://github.com/asb2m10/dexed.git
   TAG=v1.0.1
   TARGET=Dexed_VST3
   BUNDLE=Dexed.vst3

   apt-get update -qq
   apt-get install -y -qq git cmake build-essential pkg-config \
     libasound2-dev libfreetype-dev libfontconfig1-dev libx11-dev \
     libxcursor-dev libxext-dev libxinerama-dev libxrandr-dev \
     libgl1-mesa-dev libjack-jackd2-dev libcurl4-openssl-dev ca-certificates
   git clone --depth 1 --branch "$TAG" --recurse-submodules \
     --shallow-submodules "$REPO" /src
   cd /src
   cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
   # The link step can fail under a parallel build ("write jobserver: Bad
   # file descriptor"). The second, serial run only redoes the link.
   cmake --build build --target "$TARGET" -j4 || true
   cmake --build build --target "$TARGET" -j1
   cp -r "$(find build -type d -name "$BUNDLE" | head -1)" /out/
   ```

2. Run it:

   ```bash
   mkdir -p out
   docker run --rm --platform linux/amd64 \
     -v "$PWD/build.sh:/build.sh:ro" -v "$PWD/out:/out" \
     ubuntu:22.04 bash /build.sh
   ```

3. Check the result before you copy it. The highest `GLIBC_` version must be
   2.35 or lower:

   ```bash
   objdump -T out/Dexed.vst3/Contents/x86_64-linux/Dexed.so \
     | grep -o 'GLIBC_[0-9.]*' | sort -uV | tail -1
   ```

4. Copy the whole `.vst3` folder to Push and restart Live, as in
   [Steps](#steps), step 2.

Notes:

- The `.vst3` is a folder. The file inside it,
  `Contents/x86_64-linux/<Name>.so`, is the plugin. Copy the folder, not the
  `.so`.
- The packages in step 1 are the ones JUCE needs. The JACK header
  (`libjack-jackd2-dev`) is only for compiling. The Dexed binary does not link
  against JACK.
- A plugin whose code has no Linux build at all (a Windows-only or Mac-only
  plugin) cannot be built this way.
- Check the plugin's licence before you copy a build you made to other people.

## Plugin presets (`.vstpreset`)

A `.vstpreset` file holds one patch for one plugin. The file starts with the
text `VST3`, a version number, and the plugin's class ID as 32 hex digits. This
ID says which plugin the file belongs to. Live reads it and lists the preset
under that plugin.

To use a preset on Push, copy the file into the **User Library**:
`/data/Music/Ableton/User Library/`. Live indexes it in about 40 seconds with
no restart. A file in `~/.vst3/presets/<Vendor>/<Plugin>/` was **not** indexed
in our test. You can make a `.vstpreset` in Live on a Mac (save the plugin's
preset from the device).

Push's browser does not list plugins, so Push cannot show the preset either.
[Browser Bridge](https://github.com/federico-pepe/push-hack-browser-bridge)
can load it: `load_plugin:Surge XT:Surge-Bass1` loads the plugin with that
patch, and `list_plugins` lists plugins with their presets. Tested with Surge XT.

push-manager has a **PLUGINS** tab in the Shadow UI and a Plugins list in the
web Browser view. Both call Browser Bridge, so you do not need SSH to load a
plugin or a preset.

## VST2

VST2 does not load. `Vst2Preferences` has the same flag as VST3, and also a
custom folder. We tested both on 2026-10-04 with a Dragonfly Reverb VST2 `.so`:

- **System paths flag only:** the scanner log shows no VST2 line at all.
- **Custom folder `/data/.vst2` plus the flag:** the log shows
  `VST2: scanning plugins in "/data/.vst2" (custom)` and then
  `VST2: finished scanning plugins`, with no `check plugin at path` line. The
  scanner listed no file in that folder. We tried the names `Name-vst.so` and
  `Name.vst`.

So Live reads the VST2 folder, but finds nothing in it. The Linux VST2 scan
may be a stub in this build. We did not find a file name that works. Use VST3.

The custom path is a string inside `Preferences.cfg`: a 4-byte length in
characters, then UTF-16LE text. It sits after the two flag bytes of
`Vst2Preferences`.

## Known limits

- Linux plugins only. Windows and macOS plugins do not load.
- A plugin's own window cannot show on the 960x160 screen.
- Plugins run inside Live, so a heavy plugin uses Live's audio CPU time.
- A Push OS update can replace `Preferences.cfg` or reset the flag. Repeat
  step 3 if the plugin is no longer scanned.
