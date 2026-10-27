# Push Hack Manual

This is a simple getting started guide that explains how to install, uninstall, configure and use **Push Hack**.

Please remember that this hack is **unofficial**, and this project is <ins>**NOT**</ins> approved, endorsed or supported by Ableton. 

**Use this hack at your own risk**.

I don't provide support for this but if you need help, join the [Discord Server](https://discord.gg/8y6aYxy9nU).

## Before we start
1. The Push Hack works *only* with Push 3 Standalone.
2. Make sure that Push is updated to the latest version available. This hack has been tested on:
    * Beta release: **2.4.5b8** (aka Live 12.4.5b8)
    * Stable release: **2.4.3** (aka Live 12.4.3)
3. When a new version of the Push software is available, **you need to uninstall the hack and reinstall it before upgrading**.

These instructions are written for users of the macOS operating system. Windows users, see the [Windows section](#windows-users) below before starting.

## Installing

You have two ways to install Push Hack:

1. Use the [Push Hack Installer](https://github.com/federico-pepe/push-hack-installer) — a cross-platform app that guides you through pairing and installation. This is the easiest option.
2. Use the included scripts, as described below.

Your computer and Push 3 Standalone must be connected to the same Wi‑Fi network. Make sure the connection is **stable** and that it doesn't drop during installation. If you're concerned about network reliability, you can enable the hotspot directly on Push from the settings and connect to it instead.

At the end of the installation, Push will automatically reboot. Make sure to save any important files before proceeding.

* Go to http://push.local/pair to pair your computer with Push.
* Download the Push Hack and unzip it
* Open your Terminal (`Applications > Utilities > Terminal`)
* Go to the downloaded folder:
    
    You can type `cd ~/Downloads/push-hack` or simply `cd `(with a space at the end) and then drag and drop the unzipped folder into the Terminal and then press `ENTER`
* Now type: `./scripts/install.sh` and press `ENTER` and then follow the on-screen instructions.

If you never connected to your Push via SSH the installation script will automatically create an SSH key for you. Follow the instruction provided by the script. Make sure that you add your SSH key to http://push.local/ssh and then press `Shift`+`Select`+`Preferences` (gear icon) on your Push.

## Windows users

The install/uninstall scripts are bash scripts — they don't run in PowerShell or `cmd.exe`. Use **[Git for Windows](https://git-scm.com/download/win)**, which includes **Git Bash**.

1. Install Git for Windows (default options are fine).
2. Download the Push Hack and unzip it.
3. Open **Git Bash** (not PowerShell, not Command Prompt).
4. Go to the downloaded folder, e.g.: `cd "/c/Users/<you>/Downloads/push-hack"`
5. Run `./scripts/install.sh` and follow the on-screen instructions.

No other tools needed — the scripts don't depend on `jq`, `python`, or anything else outside Git Bash's built-in coreutils.

**Note on `python`/`python3` on Windows:** Windows ships fake `python`/`python3` stubs that just open the Microsoft Store instead of running Python, even if you don't have Python installed. If `command -v python3` seems to "find" something but it fails when run, that's why — installing `jq` sidesteps the issue entirely.

## Uninstalling

To uninstall the Push Hack, connect your computer and your Push 3 to the same WiFi network as instructed above.

Browse to the downloaded Push Hack folder and run the uninstall script:

`./scripts/uninstall.sh`

If you want to remove ALL the data, add the --purge flag

`./scripts/unistall.sh --purge`

## Updating Push Hack

Push Hack can update itself. This is different from an update of Push's own software (see the next section).

### How you know that an update exists

Push Manager asks GitHub once a day if a newer release exists. A yellow bar then shows:

- On the web page, at the bottom of every tab.
- On the Push screen, at the bottom of the Shadow UI. The bar shows only while the Shadow UI is open.

The right-most button under the screen shows UPDATE. Its light is yellow.

### Install the update from the Push

1. Open the Shadow UI.
2. Press UPDATE (the right-most button under the screen).
3. Press OK (the second button from the right) to install. Press CANCEL (the right-most button) to stop.

The right-most button opens the question and also cancels it. A double tap on that button does not install anything. The question closes by itself after 15 seconds.

### Install the update from the web page

1. Open the System tab.
2. Press **Update now**, or press **Update** in the yellow bar.
3. Confirm in the dialog.

### What happens during the update

- The screen goes dark for a few seconds. Live and the audio keep running.
- Apps that use the Push screen (for example Stacks) go back to Live.
- The web page reloads by itself when the new version runs.
- A message shows the result: "Push Hack updated to ..." or "failed and was rolled back".

If the new version does not start, Push Manager puts the old version back. The update takes about 20 seconds.

### Turn the check off, or hide the bar

Open the System tab. In the card **Push Hack update**:

- Clear **Check for updates automatically** to stop all calls to GitHub.
- Clear **Include alpha and beta releases** to see stable releases only.
- Press **Dismiss** in the yellow bar to hide it for that version. A newer release shows it again.

### When the update is not possible

- **The release changes system files.** The update refuses and tells you to run `install.sh` (or the Push Hack Installer) from a computer.
- **The release has a new display hook.** The update refuses until you restart Push. Push Manager never restarts Push itself, because that ends your Live session.
- **The display hook file changes.** Push Manager puts the new file in place. Push loads it at the next restart.

The update changes only Push Manager, Push Catalog and the display hook. It does not change hacks that you installed from the Push Hack Catalog.

Push Hack versions that were installed before this feature do not update themselves. Install once with `install.sh` or the Push Hack Installer.

## Updating Push's Software with the hack installed

Whenever a software update for Push is released, we **highly recommend** uninstalling the hack by following the instructions above before proceeding with the update, then reinstalling it afterwards.

If you forget to do this and start the update, Push will (hopefully) still update. However, once the update finishes, it will get stuck while rebooting, and the screen will remain black.

If this happens: hold down the power button on Push for a few seconds until it turns off completely. Wait a few seconds, then turn it back on. Finally, verify that the update was installed successfully.

## Additional Set up
Some hacks required additional set up to work properly. Please follow these instructions.

### PushHack Remote Script
Push Manager's preset browser, plugin list, scale and tempo features, and several hacks, talk to Live through the **PushHack** Remote Script. It installs with Push Hack. You select it once in Live:

1. On Push, open **Settings > MIDI** (or Live's **Preferences > Link, Tempo & MIDI**).
2. In an empty slot under **Control Surface**, choose **PushHack**. Leave Input and Output set to None.
3. Restart Live.

The **System** page in Push Manager shows if PushHack is installed and running. It also lists the old **PushHackBrowser** and **PushHackArrangement** scripts if they are still there, with a Remove button for each. The old Browser Bridge hack is no longer needed.

### Automation Hack
The **Automation** hack is not installed by default — install it from the Push Hack Catalog. For the setup steps and screenshots, see its own repo: [push-hack-automation](https://github.com/federico-pepe/push-hack-automation).
