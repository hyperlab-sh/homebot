---
title: "Inbox & helper"
description: "How homebot gets your attention, and what it does on its own while you're not looking."
---

# Inbox & helper

homebot assumes you'll forget. So it does everything that can't lose data on
its own, and puts the rest in **one short list**, in your face, with the fix
attached.

```text
$ homebot
🧹 Neatpik · laptop · 3 things need you (1 urgent)

  1  ! A commit was refused
       furniture/big.iso is 4.2 GB (limit 50 MB)
       → homebot save
  2  • 2 new things to sort: track, ignore or snooze
       ~/.config/zed, ~/.config/ghostty
       → homebot triage
  3  • Profile workstation changed since the last unpack
       → homebot unpack

  Done for you since you last looked: sealed 1 secret, committed 4 files, pushed 1 commit.
  Do the first one now: homebot next
```

## Where it reaches you

| Where | What you see | Cost |
|---|---|---|
| New shell | `🧹 Neatpik: 3 things need you (1 urgent), run: homebot` | reads a cache file, no subprocess |
| Prompt | `HOMEBOT_COUNT` (e.g. `3!`), shown by Starship | one `read` per prompt |
| Desktop | notification when the set of **urgent** items changes, again daily while unresolved | `notify-send` / `osascript` |
| `homebot` | the full list | |

The shell refreshes the cache in the background when it's older than 30 minutes.
Every refresh rewrites the nudge, symbol and count from the persona in effect at
that moment. `homebot persona use` refreshes at once; if you edit `persona:` in
`homebot.yml` or a persona file by hand, the shell shows the old voice until the
next refresh. Run `homebot refresh` (or bare `homebot`) to apply it at once;
otherwise any command that changes things, the hourly helper or the shell's
30-minute check does it.
With a [persona](/personas/) (Neatpik unless you chose another), the inbox
header and nudge start with that persona's emoji and name, or your command
name when one is set, as above. The neutral voice says `homebot · laptop · …`
and `🚚 homebot: …` instead, with your command name in place of <code data-hb-keep>homebot</code>
when one is set. Every refresh (`homebot persona use`, bare `homebot`,
`homebot refresh` or the helper) writes `$XDG_CACHE_HOME/homebot/symbol` for
the Spaceship prompt section when a persona is active, and a custom zsh prompt
can call `homebot_symbol` to read it; the Starship snippet below always shows
`🚚`. With `persona: none`, with `--plain`/`HOMEBOT_PLAIN`, or
when the persona selected in `homebot.yml` is broken, it removes that file and
zsh falls back to `🚚`. An unusable persona selected in `homebot.yml` adds a
`persona_broken` todo item, and until it is fixed the nudge, the prompt symbol
and desktop notifications are neutral (the terminal output still falls back to
Neatpik); an unusable `HOMEBOT_PERSONA` only warns in that terminal session, and
a refresh started from that shell speaks as Neatpik (the hourly helper does not
see the variable and uses the `homebot.yml` choice).

## What it watches

::: tabs
== tab "Urgent (!)"
- Git has no name or email for you, so commits fail (a new machine; listed
  before the refused commit it causes)
- Signing is on but the key file (`~/.ssh/id_rsa` here) isn't on this
  machine, so every commit fails. The fix shown turns signing off in
  `~/.config/git/config.local`; `homebot unlock` brings the key back once it's in
  `private/`
- A commit was refused (guard reason attached)
- An automatic chore failed (seal, export, autosave, sync, link)
- **Layout errors** in the repo
- Secret conflict (changed here and elsewhere)
- `gitleaks` missing
- Commit guard not installed
- This machine and the remote both have new commits
- The repo isn't a git repository
== tab "To do (•)"
- Files in the way of links
- A `~/.gitconfig` that overrides the repo's `~/.config/git/config` (the fix
  moves it aside for `homebot cleanup`)
- Backups that `homebot link --backup` moved aside (`homebot cleanup`)
- Links not made yet
- New things to triage
- Secrets to unlock, or not sealed yet
- Uncommitted changes, unpushed or unpulled commits
- No remote / remote unreachable
- Profile never unpacked here, or changed since
- An export is failing
- Background helper missing or stale
- An installed helper or guard points at a homebot path that is missing, not
  executable or a versioned binary (`helper-path`); `homebot setup` rewrites it
- A homebot update needs attention (`update-available`): a newer release under `update: notify`, or under `update: auto` one homebot can't install on its own (see [Updates](#inbox-helper-updates))
- Keys not set up
- The configured persona file is missing or unusable (`persona_broken`); run `homebot persona validate <id>`
:::

## The background helper

`homebot setup` installs it: a **systemd user timer** on Linux
(`homebot-tidy.timer`, 5 min after boot, then hourly), a **launchd agent** on
macOS (`sh.homebot.tidy`). Each run is `homebot tidy --notify --quiet --helper`
(the last flag marks the unattended run; a `homebot tidy` you type never checks for
updates):

::: steps
1. **Guard**: reinstall the commit guard if `core.hooksPath` drifted.
2. **Seal**: encrypt changed secrets.
3. **Export**: refresh package snapshots due (about once a day).
4. **Autosave**: commit tracked changes on the `autosave.commit` cadence (`off` / `hourly` / `daily`).
5. **Sync**: pull (rebase), push if `autosave.push`.
6. **Link**: fix links that are safe to fix (never moves your files).
7. **Refresh** the inbox, notify if the urgent set changed.
8. **Update**: with `update: notify` or `auto`, check for a new homebot release (see [Updates](#inbox-helper-updates)).
:::

Each step that fails becomes an urgent item; the rest still run. A lock file
stops two runs from overlapping.

The timer or agent runs with the `PATH` and repo setup saw, and with each of
`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME`
that was set then, so it shares state, cache and lock with your shell. After
changing one of them, run `homebot setup` again. It also gets any of
`HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY` (and their lowercase forms),
`SSL_CERT_FILE` and `SSL_CERT_DIR` that setup saw, for the release check. setup refuses a relative
one: homebot, its helper and the shell integration treat a relative `XDG_*`
as unset.

## Updates

The `update` key in `homebot.yml` decides what the helper does about new
homebot releases. With `off` (the default) it never asks. Otherwise, after a
tidy that ran, the helper checks GitHub for the latest release about once a
day (six hours after a check that failed, for example offline). Only one
update step runs at a time, and the whole step, check and upgrade together,
gets five minutes. Source and development builds never check. Nothing else
asks in the background: the inbox only reads what the helper saved, and
`homebot version --check` asks once when you run it. A check that fails raises
no item and doesn't hide one that is showing: an `update-available` item stays,
saying what it said, until the running version is at least the latest release.

- **`notify`**: a newer release becomes an `update-available` to-do item,
  with the command that upgrades this install.
- **`auto`**: Homebrew cask and install-script installs upgrade on their
  own, once a release has been out for 48 hours since the helper first saw
  it, and at most one attempt a day. A cask install checks the latest release
  again right before upgrading. An install-script upgrade needs `cosign`
  (the minimum version is in the item) and verifies the release signature
  first. A successful upgrade writes a journal entry and sends a desktop
  notification. `.deb` and `.rpm` installs (which need root), Homebrew
  formula, `go install` and unrecognised installs get the inbox item with the
  command to run instead. A failed upgrade (signature check, timeout,
  missing `cosign`, any other error) becomes the item too, saying why; a
  Homebrew install left broken also sends a notification with the
  `brew reinstall` command. If Homebrew still hasn't offered the release 72
  hours after the first try, the item says so.

`homebot version --check` shows what applies to this machine.

::: callout info "What it never does"
It never overwrites a real file, never commits a file git doesn't already
track, other than sealed secrets and export snapshots (new files go through `homebot triage`; the one exception is the very
first commit of an empty repo), never decrypts (that needs your
passphrase) and never resolves a conflict for you.
:::

## Shell integration

Add this to your `.zshrc`:

```zsh
eval "$(homebot shell zsh)"
```

### Completion

`homebot completion zsh|bash|fish` prints a completion script for <code data-hb-keep>homebot</code>.
Homebrew cask, Debian and RPM installs already ship the zsh completion
(`_homebot`), so with `compinit` run before the `eval` line they need nothing
more. (On a Homebrew cask, `compinit` finds it only if Homebrew's `shellenv`
ran before it: see [Homebrew](/install/#install-and-update-homebrew).) Script and Go installs do not ship it: for zsh, source the script after
`compinit` and before the `eval` line:

```zsh
autoload -Uz compinit && compinit
source <(homebot completion zsh)
eval "$(homebot shell zsh)"
```

If you set a command name (`homebot name`) and `~/.local/bin/<name>` links to
homebot, `homebot shell zsh` also completes that name the same way. The line
does nothing until a completion for <code data-hb-keep>homebot</code> is registered, whether by the
packaged `_homebot` or by the `completion` line. With a script or Go install, if
the `completion` line comes after the `eval` line, only <code data-hb-keep>homebot</code> completes, not
the name; without it, neither completes. bash and fish complete <code data-hb-keep>homebot</code> only,
never the name.

::: tabs
== tab "Starship"
```toml
[env_var.HOMEBOT_COUNT]
style = 'bold yellow'
format = """[🚚 $env_value ]\
($style)"""
```
== tab "Spaceship"
Add `homebot` to `SPACESHIP_PROMPT_ORDER`.
:::

## State

Per machine, outside the repo:

| Path | Holds |
|---|---|
| `$XDG_STATE_HOME/homebot` | links made, secret hashes, snoozes, journal, last tidy |
| `$XDG_CACHE_HOME/homebot` | `nudge`, `count` and `symbol` for the shell; `voice.json`, the last variant each persona said |
