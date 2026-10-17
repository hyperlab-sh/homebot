---
title: "Layout"
description: "The folder a file is in decides what happens to it. No config file maps paths."
---

# Layout

The repo has no "what goes where" config. **The folder is the configuration.**

```text
~/.home/
├── autolink/          linked into $HOME
│   ├── common/        every OS
│   ├── linux/         Linux only
│   └── macos/         macOS only
├── private/           linked too, committed only as *.age
│   ├── common/ linux/ macos/
│   ├── recipients.txt public key (committed)
│   └── identity.age   private key, passphrase-encrypted (committed)
├── manifests/
│   ├── profiles/      base.yml, workstation.yml
│   ├── actions/       your own profile steps (*.sh)
│   ├── common/ linux/ macos/   Brewfile, apt.txt, apt-repos/, flatpak.txt
├── furniture/         fonts/ drivers/ themes/ vaults/ vendor/
├── bin/               personal scripts (zsh-bench, ...)
├── homebot.yml        tool settings (required, may be empty)
├── decisions.yml      triage answers ("never ask about X")
└── bootstrap.sh       optional: your own first-run script
```

## Layers

`autolink/` and `private/` each have three **layers**: `common/`, `linux/`,
`macos/`. On a given machine homebot merges `common/` with the current OS
layer, and `private/` with `autolink/`.

- Folders merge across layers and trees: `common/config/zsh` and
  `linux/config/zsh` both feed `~/.config/zsh`.
- **A file lives in exactly one place.** The same file in two layers is a
  [layout error](/linking/#linking-rules-layout-errors), not an override.

::: callout info "Why no overrides?"
Overrides make you diff two files in your head to know what's live. One file,
one place: `grep` finds it, and the per-OS bit goes in a per-OS file
(`env.d/10-linux.zsh`, `wezterm/platform.lua`).
:::

## The other folders

| Folder | Rule |
|---|---|
| `manifests/` | Read by `homebot unpack`. `Brewfile`, `apt.txt`, `apt-repos/`, `flatpak.txt` are **generated** by `homebot export`; change what's installed, don't hand-edit. See [Profiles](/profiles/). |
| `furniture/` | Never linked or copied unless a profile step asks (`- fonts: [Family]`). Files over `limits.maxFileMB` (50) are refused at commit. |
| `bin/` | Not linked. Put it on your `PATH` in your shell startup (for example `path=(~/.home/bin $path)` in `.zshenv`). |
| anything else | Yours. homebot leaves it alone. |

## Config files

Both are `.yml`. A `.yaml` file is refused with a rename hint.

| File | Who writes it |
|---|---|
| `homebot.yml` | You. See [homebot.yml](/reference/config/). |
| `decisions.yml` | `homebot triage` (hand edits are fine). Globs work. |
| `manifests/profiles/*.yml` | You. |
