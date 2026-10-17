---
title: "Naming your homebot"
description: "Give this home a short command name and keep it working on each machine."
---

# Naming your homebot

The homebot command always works. You can give this home a second command name, such as
`zork`, and use either command for the same home. The name is stored in
`homebot.yml`; each machine makes its own link in `~/.local/bin`. The robot's
persona controls its voice and is independent of its command name.

## Choose or change a name

During first setup, use `homebot setup --name zork` or answer the name question.
If you install with the front door, `curl -fsSL https://homebot.sh/install | bash -s -- --name zork`
passes the same flag on to setup. The name link does not exist until setup or
<code data-hb-keep>homebot name</code> makes it, so these first commands use <code data-hb-keep>homebot</code>. Later, run:

<pre data-hb-keep><code>homebot name zork
</code></pre>

```sh
homebot name         # show the configured name and what works here
homebot name rosie   # rename this home; other machines follow once it is saved and synced
homebot name --off   # remove the name and its link; keep homebot
```

`homebot name homebot` also turns the extra name off. A name starts with a
lowercase letter, uses only lowercase letters, digits and hyphens, and is
2–32 characters long. homebot checks its own commands, shell words, tools it
runs, links it manages and your machine's `PATH` before making the link. If
another program or a real file owns the name, homebot reports the conflict
and leaves it alone. A name from a synced repo can work on one machine while
being blocked on another; `homebot name` shows both the configured and
effective name here.

homebot processes coordinate with each other and check that a link belongs
to homebot before automatically repointing or removing it. An uncooperative
process running as the same user can replace a path between that check and
the rename or unlink operation. This is an unavoidable same-user race, so
the protection against replacing a foreign file is a cooperative-use
guarantee, not an absolute one.

## Put the name on `PATH`

Your shell needs `~/.local/bin` on `PATH` to find the short name. Setup offers
to add it to your shell's startup file: `~/.zshrc` for zsh (`$ZDOTDIR/.zshrc`
when `ZDOTDIR` is set), `~/.bash_profile` for bash on macOS or `~/.bashrc` for
bash on Linux, fish's `config.fish`, or `~/.profile` for other shells. Open a
new shell afterward. A script can
use the name when it inherits a suitable `PATH`, or can invoke
`~/.local/bin/zork` directly. The background helper checks the link on each
machine and follows a rename after the repo syncs.

Cron usually starts with a small `PATH` that does not include
`~/.local/bin`. Set it explicitly in the crontab, or use the full path of the
name link, which exists on every machine where the name works:

<pre data-hb-keep><code>
PATH=/usr/bin:/bin:/home/me/.local/bin
0 * * * * /home/me/.local/bin/zork tidy
</code></pre>

The <code data-hb-keep>homebot</code> command itself is at `~/.local/bin/homebot` only when the front
door built it. With Homebrew it is in Homebrew's `bin` directory
(`/opt/homebrew/bin/homebot` on Apple silicon macOS; check `brew --prefix`), so
use that path when you want the plain command in cron.

homebot does not edit your crontab. It also cannot discover aliases or shell
functions from your interactive rc files: a Go program does not evaluate
your interactive shell setup. Choose a different name if one of those would
collide.

## Before uninstalling

Run `homebot setup --remove` before `brew uninstall --cask hyperlab-sh/tap/homebot` to remove the
helper and the name link. If you uninstall first, the link in
`~/.local/bin` may dangle; reinstall and run `homebot setup --remove`, or
remove that link yourself after checking it is the one homebot made.

Shell completion for the name works in zsh only. `homebot shell zsh` completes
the name too when `~/.local/bin/<name>` links to homebot and a completion for
<code data-hb-keep>homebot</code> is registered before the `eval "$(homebot shell zsh)"` line. The Homebrew
cask and the Debian and RPM packages ship `_homebot`, so `compinit` alone is
enough there. With a script or Go install, `source <(homebot completion zsh)`
must run after `compinit` and before the `eval` line: put it after and only
<code data-hb-keep>homebot</code> completes; leave it out and neither does. bash and fish complete
<code data-hb-keep>homebot</code> only, never the name. See
[Shell integration](/reference/inbox/#inbox-helper-shell-integration-completion)
for the setup lines and
[commands](/reference/commands/) for the full command reference.
