---
title: "Linking rules"
description: "How a path in autolink/ becomes a path in $HOME: dots, (name) folders, folding, and what counts as an error."
---

# Linking rules

`homebot link` merges `private/` + `autolink/`, `common/` + this OS, and makes
symlinks in `$HOME`. Four rules cover everything.

::: callout tip "TL;DR"
1. Top-level names **gain a dot**: `config` → `~/.config`. Deeper names are literal.
2. A folder only one layer provides becomes **one symlink**.
3. `(name)` means **this folder must be a real folder**, never a link.
4. Every folder above a `(name)` folder is marked too (or built in).
:::

## 1. Names

| In `autolink/common/` | In `$HOME` | Why |
|---|---|---|
| `zshenv` | `~/.zshenv` | top level gains a dot |
| `config/starship.toml` | `~/.config/starship.toml` | only the top part changes |
| `config/(zsh)/.zshrc` | `~/.config/zsh/.zshrc` | deeper names are literal, dots included |
| `Library/Application Support/X` | `~/Library/Application Support/X` | `Library` is the one name that keeps no dot |
| `.gitkeep`, `.notes.md` | ignored | dot-names at the top of a layer are never linked |
| `dot-config` | **error** | old layout: rename it to `config` |

## 2. Folding: one folder, one link

A folder that comes from **one layer** of `autolink/`, isn't marked and isn't
built in, becomes **a single symlink**. New files the app writes there land in
the repo, which is usually what you want for config folders.

```text
autolink/common/config/nvim/         ~/.config/nvim  ->  ~/.home/autolink/common/config/nvim
```

When **two layers** both provide a folder, it can't be one link, so it becomes
a real folder holding per-file links:

```text
autolink/common/config/wezterm/wezterm.lua      ~/.config/wezterm/          (real folder)
autolink/linux/config/wezterm/platform.lua        ├── wezterm.lua  -> common/...
                                                  └── platform.lua -> linux/...
```

`private/` never folds: secrets are always linked file by file.

## 3. `(name)`: must stay a real folder

Wrap a folder's name in `()` when the app also writes files there you don't
want in the repo (sessions, caches, tokens). Only the files you put in the
repo get linked.

```text
autolink/common/(claude)/statusline-command.sh
  →  ~/.claude/                      real folder, Claude Code writes freely
       statusline-command.sh  ->  ~/.home/autolink/common/(claude)/statusline-command.sh
```

Nest them as deep as you need:

```text
(claude)/settings.json              real ~/.claude, linked settings.json
(claude)/(sessions)/x               real ~/.claude/sessions, linked x
(claude)/projects/                  ~/.claude/projects is one link
(a)/b/(c)/d                         ✗  b is plain but holds (c)
(a)/(b)/(c)/d                       ✓
```

### Built-ins: always real, no `()` needed

These are never links, however you spell them (`config` or `(config)` both work):

`.config` · `.local` · `.local/bin` · `.local/share` · `.local/state` · `.cache` ·
`.ssh` · `.gnupg` · `Library` · `Library/Application Support` · `Library/Preferences`

## 4. The chain rule

A plain folder may become a link. A link can't contain a folder that must not
be a link. So **every folder above a `(name)` folder must be marked or built in.**

| Repo path | Valid? |
|---|---|
| `config/(zsh)/conf.d/x.zsh` | ✓ `config` is built in |
| `(claude)/(sessions)/x` | ✓ |
| `a/(b)/c` | ✗ rename `a` to `(a)` |
| `(a)/b/(c)/d` | ✗ rename `b` to `(b)` |

## Layout errors

Anything that breaks a rule is a **layout error**. That part of the repo isn't
linked (links already there stay put), `homebot link` exits 1, and the inbox
shows it as **urgent**. Messages tell you the exact rename.

| Mistake | Message |
|---|---|
| Plain folder holds a `(…)` | `rename "a" to "(a)" in every layer: it holds …, which must not be a link, so it can't be one either` |
| Same folder, two spellings | `spelled "(zsh)" in common but "zsh" in linux; use one spelling everywhere` |
| Same file in two layers | `provided by both common and linux; keep it in one place` |
| `()` on a file | `() marks folders only: rename "(x)" to "x"` |
| `()`, `((a))`, `(a))` | `wrap the name in one pair of (), like "(a)"` |
| `(.foo)` at the top | `top-level names get their dot from homebot; rename it to "(foo)"` |
| `(.)` or `(..)`, at any depth | `"(..)" names no folder: . and .. would link outside the folder it is in; rename it` |
| A name that would land outside `$HOME` | `would be linked at <path>, outside the folder it is in; rename it` |
| `dot-config` at the top | `"dot-config" is the old layout: rename it to "config"` |

## Conflicts: something real is in the way

A **conflict** is your machine's fault, not the repo's: a real file, a real
folder, or a foreign symlink sits where a link should go. homebot **never
overwrites** anything.

```sh
homebot link --dry-run    # show what would happen
homebot link --backup     # rename blockers to *.homebot-backup-<time>, then link
homebot cleanup           # later: compare each backup with the link, then decide
```

"Moved aside" means renamed in place: `~/.zshenv` becomes
`~/.zshenv.homebot-backup-20261012-054107` and the link takes its name.
Nothing is deleted. To keep something from a file before it's replaced,
`homebot pack` it first.

A link to the repo through another path to it is not in the way. When
`~/.home` is a symlink to the folder the repo really is in, a link made
through either path counts as already linked, from either side, and
`--backup` moves nothing. New links use the path homebot found the repo by.

Just before it replaces, unfolds or removes a link, homebot checks it is
still the link it planned around. If something changed it in the meantime
(you, or another program), that one is skipped and reported, and the rest
go on; a real file is never removed.

Ctrl-C stops `homebot link` between two files: nothing is left half-done,
no further file is moved aside or linked, it says what it did, and it exits
130. Run it again to finish.

### Cleaning up backups

The inbox counts the backups next to links. `homebot cleanup` goes through
them one at a time. With fzf installed, each one opens a menu with the diff
in a preview pane: red lines are only in the backup, green lines only in what
is linked now. Without fzf, the diff is printed and a menu asks: move with the arrow keys and press Enter. When output is not a terminal (or `ACCESSIBLE` is set) it is a numbered list instead, and you type the number. Enter alone keeps.

| Choice | What happens |
|---|---|
| delete | The backup goes; the link stays as it is. |
| keep | Nothing changes; it's asked about again next time. |
| use | The backup's content is copied into the repo file the link points at, then the backup goes. `homebot save` commits it. Only offered for files in the repo. |
| merge | Take some of the backup's lines into the repo file. Only offered for files in the repo. |

**merge** opens lazygit when it is installed, else `git add -p`, on a scratch
copy: the repo file as committed, the backup as the change. In lazygit, press
Enter on the file; the backup's lines are green here (the preview's colors are
the other way round). Space takes the line under the cursor, shift+arrows
select several, `d` drops them, `q` finishes. In `git add -p`, `y` takes a
block, `n` skips it, `s` splits it smaller, `e` edits it. What you took is
written to the repo file (`homebot save` commits it), and the same backup
comes back with what is left, usually to delete it.

Esc (or `q`) stops; the rest stay untouched. In the menu without fzf, pick
quit to stop; Ctrl-C there stops too but exits 130, so a command chained
with `&&` does not run. Without a terminal, and with
`--list`, it only lists them. A backup that was a link to somewhere else is
moved aside as a link, and deleting it removes only the link.
When a backup is a link, the diff starts with "The backup is a link to ...",
naming where the link points the way the link names it (made absolute and
cleaned up, one hop), or where it really leads when that path would reach
another file.

## Pruning

Links are recorded per machine (`$XDG_STATE_HOME/homebot`). Remove a file from
the repo and the next `homebot link` (or the hourly helper) removes its
dangling link. Links homebot didn't make are never touched. `homebot unlink`
removes every link it made; the files stay in the repo, so the next
`homebot link`, or the hourly helper (`homebot tidy`) within the hour, links
them again. To keep a file unlinked, take it out of the repo.

Folders homebot creates to hold links into `private/` (`~/.gnupg`,
`~/.ssh`, ...) are made owner-only (0700). Folders that already exist keep
their mode.

## Adding things: `homebot pack`

Don't move files into the repo by hand; `pack` gets the spelling right.

```sh
homebot pack ~/.config/zed                  # autolink/common/config/zed
homebot pack ~/.config/zed --layer linux    # autolink/linux/config/zed
homebot pack ~/.netrc --private             # private/common/netrc (local) + netrc.age (committed)
```

It moves the item, links it back, and:

- reuses the spelling an ancestor already has in any layer (`(zsh)` stays `(zsh)`),
- marks new ancestors `(…)` so the parent stays real (`~/.claude/settings.json` → `(claude)/settings.json`),
- refuses non-dot top-level names (except `Library`), names already wrapped in `()`, things already in the repo, and new marked folders under an existing plain one (with the rename to do first).
