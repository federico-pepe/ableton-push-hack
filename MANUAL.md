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

## Updating Push's Software with the hack installed

Whenever a software update for Push is released, we **highly recommend** uninstalling the hack by following the instructions above before proceeding with the update, then reinstalling it afterwards.

If you forget to do this and start the update, Push will (hopefully) still update. However, once the update finishes, it will get stuck while rebooting, and the screen will remain black.

If this happens: hold down the power button on Push for a few seconds until it turns off completely. Wait a few seconds, then turn it back on. Finally, verify that the update was installed successfully.

## Additional Set up
Some hacks required additional set up to work properly. Please follow these instructions.

### Browser Bridge Hack
The **Browser Bridge** hack is not installed by default — install it from the Push Hack Catalog. For the setup steps and screenshots, see its own repo: [push-hack-browser-bridge](https://github.com/federico-pepe/push-hack-browser-bridge).

### Automation Hack
The **Automation** hack is not installed by default — install it from the Push Hack Catalog. For the setup steps and screenshots, see its own repo: [push-hack-automation](https://github.com/federico-pepe/push-hack-automation).
