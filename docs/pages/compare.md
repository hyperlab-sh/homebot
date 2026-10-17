---
title: "How it compares"
description: "homebot next to chezmoi, yadm, dotbot, rcm, vcsh and a bare git repo, row by row."
---

# How it compares

homebot leads on the remembering. It keeps one list of what needs you, saves and syncs on its own once you let it, asks once about each new file, and comes to you instead of waiting to be run. On the rest, the others are older and broader, and the tables below say where.

::: callout info "homebot is not released yet"
Its column describes what is built today, read from its code.
:::

The rows under **The remembering**, and **Nothing new to learn**, are ours. Every other row comes from [chezmoi's comparison table](https://www.chezmoi.io/comparison-table/), which chezmoi's maker keeps, as it stood on 14 October 2026. Where a tool's own docs say more, the cell adds it: yadm runs under Git Bash or Cygwin, and dotbot also installs from PyPI and has plugins for encryption. *With effort* stands for chezmoi's "possible with significant manual effort".

## The remembering

| | homebot | [chezmoi](https://chezmoi.io/) | [yadm](https://yadm.io/) | [dotbot](https://github.com/anishathalye/dotbot) | [rcm](https://github.com/thoughtbot/rcm) | [vcsh](https://github.com/RichiH/vcsh) | [bare git](https://www.atlassian.com/git/tutorials/dotfiles) |
|---|---|---|---|---|---|---|---|
| One list of what needs you, each item with its fix | Yes: [`homebot`](/reference/inbox/) | No | No | No | No | No | No |
| Commits and pushes without you running anything | Yes, hourly or daily, once you turn it on | Only when you run a chezmoi command (`autoCommit`) | With the AutoYADM add-on | No | No | No | No |
| Asks once about new files and remembers the answer | Yes: `homebot triage` | Lists them when you run `unmanaged`; remembers nothing | No | No | No | No | No |
| Comes to you: shell line, prompt count, notification | Yes, in zsh | No | No | No | No | No | No |

## Getting started

| | homebot | [chezmoi](https://chezmoi.io/) | [yadm](https://yadm.io/) | [dotbot](https://github.com/anishathalye/dotbot) | [rcm](https://github.com/thoughtbot/rcm) | [vcsh](https://github.com/RichiH/vcsh) | [bare git](https://www.atlassian.com/git/tutorials/dotfiles) |
|---|---|---|---|---|---|---|---|
| Distribution | Single binary | Single binary | Single script | Python package | Multiple files | Single script or package | — |
| Install method | One-line script, Homebrew cask, Debian or Fedora package, or `go install` | Many | Many | git submodule, or PyPI | Many | Many | Manual |
| Non-root install on a bare system | Yes; the `apt` steps use sudo | Yes | Yes | With effort | Yes | Yes | Yes |
| Windows support (not WSL) | No: macOS and Linux | Yes | Under Git Bash or Cygwin | Yes, if your account may make symlinks | No | No | Yes |
| Bootstrap requirements | git and bash; gitleaks to save | None | git | Python, git | Bash | sh, git | git |
| Implementation language | Go | Go | Bash | Python | Bash | POSIX shell | C |
| Nothing new to learn | No: its folder rules and commands | No: its file-name prefixes, templates and commands | Yes: it is git, run on your home folder | No: its YAML file | No: its commands and folder rules | Almost: git, one repo per topic | Yes: it is git |

## How your files are kept

| | homebot | [chezmoi](https://chezmoi.io/) | [yadm](https://yadm.io/) | [dotbot](https://github.com/anishathalye/dotbot) | [rcm](https://github.com/thoughtbot/rcm) | [vcsh](https://github.com/RichiH/vcsh) | [bare git](https://www.atlassian.com/git/tutorials/dotfiles) |
|---|---|---|---|---|---|---|---|
| Source repos | Single | Single | Single | Single | Multiple | Multiple | Single |
| Dotfiles are... | [Symlinks](/linking/) | Files | Files | Symlinks | Symlinks | Files | Files |
| Config file | Required: [`homebot.yml`](/reference/config/), every key optional | Optional | Optional | Required | Optional | None | Optional |
| Private files | Yes: files 0600, folders 0700 | Yes | Yes | No | No | No | No |
| Show differences without applying | The link changes, with `link --dry-run` | Yes | Yes | No | No | Yes | Yes |
| Executable files | Yes | Yes | Yes | Yes | Yes | Yes | Yes |
| Directory creation | Only as parents of links | Yes | Yes | Yes | Yes | Yes | Yes |
| File removal | Only stale links it made | Yes | Yes | No | No | Yes | No |
| Manage partial files | No | Yes | Yes | No | No | With effort | With effort |
| File creation with initial contents | No | Yes | No | No | No | Yes | No |
| Externals | No | Yes | No | No | No | No | No |
| Archive import | No | Yes | No | No | No | Yes | Yes |
| Archive export | No | Yes | No | No | No | Yes | Yes |

## Machines that differ

| | homebot | [chezmoi](https://chezmoi.io/) | [yadm](https://yadm.io/) | [dotbot](https://github.com/anishathalye/dotbot) | [rcm](https://github.com/thoughtbot/rcm) | [vcsh](https://github.com/RichiH/vcsh) | [bare git](https://www.atlassian.com/git/tutorials/dotfiles) |
|---|---|---|---|---|---|---|---|
| Machine-to-machine file differences | [A folder per OS](/layout/) and [a profile per host](/profiles/) | Templates | Templates, plus files per OS, class or host | Links made only if a shell test passes | Files per host or tag | A branch per machine | Branches, by hand |
| Custom variables in templates | No: there are no templates | Yes | No | No | No | No | No |
| Machine-to-machine symlink differences | Per OS only | Yes | Yes | No | No | With effort | With effort |

## Secrets

| | homebot | [chezmoi](https://chezmoi.io/) | [yadm](https://yadm.io/) | [dotbot](https://github.com/anishathalye/dotbot) | [rcm](https://github.com/thoughtbot/rcm) | [vcsh](https://github.com/RichiH/vcsh) | [bare git](https://www.atlassian.com/git/tutorials/dotfiles) |
|---|---|---|---|---|---|---|---|
| Whole file encryption | Yes: [age](/secrets/), built in | Yes | Yes: gpg or openssl | Through plugins (age, git-crypt) | No | No | No |
| Password manager integration | No | Yes: 1Password, Bitwarden, pass and more | No | No | No | No | No |

## Running things

| | homebot | [chezmoi](https://chezmoi.io/) | [yadm](https://yadm.io/) | [dotbot](https://github.com/anishathalye/dotbot) | [rcm](https://github.com/thoughtbot/rcm) | [vcsh](https://github.com/RichiH/vcsh) | [bare git](https://www.atlassian.com/git/tutorials/dotfiles) |
|---|---|---|---|---|---|---|---|
| Run scripts | Yes: [profile steps](/profiles/) and your own scripts | Yes | Yes | Yes | Yes | Yes | No |
| Run once scripts | No: every step runs each time, so steps must be safe to rerun | Yes | Yes | No | No | Yes | No |
| Shell completion | zsh, bash and fish complete <code data-hb-keep>homebot</code>; only zsh also completes a [set command name](/reference/commands/) | Yes | Yes | No | No | Yes | Yes |
