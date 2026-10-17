---
title: "Profiles & packages"
description: "What gets installed on which machine, and which package manager owns what."
---

# Profiles & packages

A **profile** is an ordered list of steps in `manifests/profiles/<name>.yml`.
`homebot unpack` runs the profile for this host (`homebot.yml` →
`profiles.hosts`, else `profiles.default`).

## The three profiles today

::: tabs
== tab "base.yml"
```yaml
description: What every machine gets, servers included

steps:
  - unlock
  - link
```
== tab "workstation.yml"
```yaml
description: Daily driver (laptops and desktops)

include:
  - base

steps:
  - brew: "{os}/Brewfile"
  # Vendor repos (docker, tailscale, wezterm...) first, so apt can find their packages.
  - apt-repos: linux/apt-repos
    on: linux
  - apt: linux/apt.txt
    on: linux
  - flatpak: linux/flatpak.txt
    on: linux
  - fonts: [OpenDyslexicNerd]
```
== tab "server.yml"
```yaml
description: Home server. Its lists live in manifests/server/.

include:
  - base

steps:
  # Vendor repos (docker, tailscale) first, so apt can find their packages.
  - apt-repos: server/apt-repos
    on: linux
  - apt: server/apt.txt
    on: linux
```
:::

Each profile keeps its own package lists, and each machine's exports write
only to its profile's lists: the workstation's under `manifests/<os>/`, the
server's under `manifests/server/`.


## Step syntax

```yaml
include: [base]               # run base's steps first
steps:
  - link                      # action, no arguments
  - brew: "{os}/Brewfile"     # {os} {host} {arch} are filled in
  - apt: linux/apt.txt
    on: linux                 # skipped elsewhere
  - fonts: [A, B]             # a list becomes several arguments
  - run: ./scripts/thing.sh   # any shell, cwd = manifests/
    name: Set up thing        # label in the progress line
```

```sh
homebot unpack --dry-run      # print the steps, run nothing
homebot unpack --from 4       # resume after a failure (the error tells you N)
homebot actions               # list every action available
```

## Actions

| Action | Does | Kind |
|---|---|---|
| `link` | `homebot link` | internal |
| `unlock` | decrypt `private/` (asks once) | internal |
| `run` | shell command from `manifests/` | internal |
| `brew` | `brew bundle install --file=…` | script |
| `apt-repos` | adds the vendor apt repositories and keys in a folder (see below) | script |
| `apt` | installs only the missing packages; `sudo` unless root. Packages no repository offers here (another distro's, a removed vendor repo) are named and skipped | script |
| `flatpak` | `flatpak install` from Flathub | script |
| `fonts` | copies `furniture/fonts/<Family>` to `~/.local/share/fonts` or `~/Library/Fonts` | script |

**Add your own**: drop `manifests/actions/<name>.sh` and use `- name: args`.
Same name as a built-in replaces it. First comment line is its summary in
`homebot actions`. Exit non-zero to stop the unpack. `homebot action <name>
[args]` runs one by hand.

::: collapsible "Environment every action gets"
`HOMEBOT_ROOT` · `HOMEBOT_OS` (`linux`/`macos`) · `HOMEBOT_HOST` · `HOMEBOT_ARCH` ·
`HOMEBOT_MANIFESTS` · `HOMEBOT_FURNITURE` · `HOMEBOT_AUTOLINK` · `HOMEBOT_PRIVATE`

```bash
#!/usr/bin/env bash
# Enable systemd user services.   - services: [syncthing, ollama]
set -euo pipefail
for svc in "$@"; do systemctl --user enable --now "$svc"; done
```
:::

## Package lists are snapshots

`manifests/{os}/Brewfile`, `linux/apt.txt`, `linux/flatpak.txt` and
`linux/apt-repos/` are **written by `homebot export`**, which the helper runs
about once a day on workstations. They always match what's installed. To
change them, install or remove the package, then let the export (or
`homebot export`) catch up. An export that fails or prints nothing never
replaces the last good file.

## Vendor apt repositories

`linux/apt-repos/` is a copy of this machine's third-party repositories:
the `.list` and `.sources` files from `/etc/apt/sources.list.d`, their signing
keys in `keyrings/`, and `origin`, the release codename they came from
(`noble`). The distribution's own sources and anything commented out or
`Enabled: no` stay behind.

The `apt-repos` step puts them back before `apt` runs, with `sudo`:

- A repo already set up, under its own name or another file pointing at the
  same URL, is left alone.
- A repo whose file names the origin codename is skipped on a machine running
  another release (Docker's `noble` repo on Debian `trixie`), and the step
  says so. Add that machine's repo by hand; the next export records it.
- A key goes where the file's `signed-by` says, unless one is already there.
- `apt-get update` runs once, only when something was added.

Add a repo the vendor's way (their install instructions), and the next export
copies it here.

## Which package manager owns what

| Use | For | Why |
|---|---|---|
| **brew** | CLI and dev tools; apps also used on the Mac (`fzf`, `starship`, `zoxide`, `atuin`, `bat`, `fd`, `gh`, `lazygit`, `cask "zed"`) | newer versions, same names as the Mac, no sudo, one list |
| **apt** | drivers, kernel modules, services, libraries and build deps, COSMIC parts | needs root and system integration |
| **apt + vendor repo** | Chrome, Docker, 1Password | they hook into the system; the vendor ships updates |
| **Flatpak** | Linux-only desktop apps not in brew | never dev tools: the sandbox hides your toolchains |

::: callout warning "brew comes first on PATH"
Don't `brew install` `python`, `openssl` or `pkg-config` unless something
needs it. They shadow the system ones and break builds against apt libraries.
:::
