# homebot

**Attention-Deficit Home Discipline.** A dotfiles manager for people who
won't remember to run it.

Most dotfiles tools do what you tell them and then wait. homebot assumes you
will forget: an hourly background helper seals secrets, snapshots installed
packages, commits, syncs and fixes links on its own, and whatever needs a
human lands in one short inbox. Every item in the inbox comes with the exact
command that deals with it.

```
$ homebot
homebot · forge · 3 things need you (1 urgent)

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

The same count shows up as one line when a shell starts, as a prompt segment,
and as a desktop notification when something urgent appears.

## Install

With Homebrew on macOS:

```sh
brew install --cask hyperlab-sh/tap/homebot
```

Or use the front door, which needs `bash`. It installs the latest release (its
installer is checked against the release's checksums first), after installing
Homebrew and the tools homebot needs (each with your OK), and takes `--persona`
and `--name`:

```sh
curl -fsSL https://homebot.sh/install | bash
```

Or install the release binary without `sudo`, with the installer attached to
each release (it carries `--version`, and runs under `sh`):

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/hyperlab-sh/homebot/releases/latest/download/install.sh | sh
```

You can read the sources before running either:
[the front door](https://homebot.sh/install) and
[the release installer](https://github.com/hyperlab-sh/homebot/blob/main/install.sh).
The release copy is covered by the signed checksums. Debian and RPM packages
are on the [release page](https://github.com/hyperlab-sh/homebot/releases/latest).
If you have Go 1.26 or newer, you can also run:

```sh
go install github.com/hyperlab-sh/homebot/cmd/homebot@latest
```

The [install guide](https://homebot.sh/docs/install/) covers signature
verification, completions, updates and removal for each method.

## The repo layout is the configuration

| Folder       | Behavior                                                                  |
|--------------|---------------------------------------------------------------------------|
| `autolink/`  | Linked into `$HOME`.                                                      |
| `private/`   | Linked into `$HOME` too, but committed only as age-encrypted `*.age`.     |
| `manifests/` | Install profiles and package lists, run by `homebot unpack`.              |
| `furniture/` | Passive resources (fonts, drivers, vendored repos). Used by profile steps. |

`autolink/` and `private/` each have `common/`, `linux/` and `macos/` layers.
Names at the top of a layer gain a dot (`config/nvim` is `~/.config/nvim`);
deeper names are literal. Wrap a folder's name in `()` to keep it a real
folder: `(claude)/settings.json` links one file into a real `~/.claude`.

## Commands

| Command | |
|---------|-|
| `homebot` | The inbox. `--all` shows everything, not just the top five. |
| `homebot next` | Run the fix for the first inbox item. |
| `homebot unpack [profile]` | Run a profile's steps. `--dry-run`, `--from N` to resume. |
| `homebot pack <path>` | Move a file or folder from `$HOME` into the repo and link it back. `--layer linux`, `--private`. |
| `homebot link` | Make every link and list what's in the way. `--backup` renames blockers, `--dry-run`. |
| `homebot unlink` | Remove every link homebot made. Files stay in the repo. |
| `homebot cleanup` | Go through the backups `link --backup` left, one at a time: a diff (in fzf when installed), then delete, keep, copy it into the repo file, or merge some of its lines into it (lazygit, else `git add -p`). `--list`. |
| `homebot keys init` | Create the age key pair; the private key is stored encrypted with a passphrase. |
| `homebot unlock` / `lock` | Decrypt / encrypt `private/`. |
| `homebot triage` | One question per new file in watched folders: track, ignore, or snooze. |
| `homebot inventory` | Summary of links, secrets, repo, exports and the helper. |
| `homebot remote [url]` | Set up where the repo is backed up: create a private repository with `gh` or `glab`, or take a URL; then check it and push. |
| `homebot save` | Seal, commit, pull (rebase) and push. `-m`, `--no-push`. |
| `homebot export [name]` | Refresh package snapshots. |
| `homebot tidy` | Run the background chores now. |
| `homebot setup` | Install the commit guard and the hourly helper (systemd user timer or launchd). Asks for a git name and email when git has none, saved in `~/.config/git/config`. |
| `homebot action <name> [args]` | Run one action by hand. |

`homebot <command> --help` (or `homebot help <command>`) prints that command's
own page: what it does, its options and examples. It never runs the command.

## Linking

homebot links the merged tree of `private/` and `autolink/` for the current
OS. A folder that comes from a single `autolink/` layer becomes one symlink;
everything else is created as a real folder holding per-file links, so apps
can write their own files beside yours.

| In the repo (`autolink/common/`) | In `$HOME`                                  |
|----------------------------------|---------------------------------------------|
| `zshenv`                         | `~/.zshenv` (top-level names gain a dot)    |
| `config/nvim/`                   | `~/.config/nvim` → one link                 |
| `(claude)/settings.json`         | real `~/.claude/`, linked `settings.json`   |
| `(claude)/(sessions)/x`          | real `~/.claude/sessions/`, linked `x`      |
| `Library/Application Support/X/` | `~/Library/Application Support/X` → one link |
| `.notes.md`                      | ignored (dot-name at the top of a layer)    |

Rules, checked on every run:

- `(name)` marks folders only. Every folder above a marked one must be marked
  too, or be built in: `.config`, `.local`, `.local/bin`, `.local/share`,
  `.local/state`, `.cache`, `.ssh`, `.gnupg`, `Library`,
  `Library/Application Support`, `Library/Preferences`. `a/(b)` is an error:
  rename `a` to `(a)`.
- A folder is spelled the same way in every layer and tree.
- A file lives in one layer only.

Breaking a rule is a layout error: that part of the repo is not linked (links
already made stay), `homebot link` exits 1, and the inbox shows it as urgent.

It never overwrites real files. Anything in the way is reported (and in the
inbox); `homebot link --backup` renames blockers to `*.homebot-backup-<time>`,
and `homebot cleanup` reviews them later.
Links are recorded per machine, so links to files removed from the repo are
cleaned up, and links homebot didn't make are never touched.

## Secrets

`private/recipients.txt` holds the public key, so sealing needs no
passphrase: the commit guard and the helper encrypt changed secrets on their
own. `private/identity.age` is the private key, encrypted with your
passphrase, and is only needed to unlock. Per-machine hashes tell "changed
here" from "changed on another machine", so a pull never overwrites a local
edit silently; conflicts show up in the inbox.

## The commit guard

`homebot setup` writes a shim to `$XDG_DATA_HOME/homebot/hooks/pre-commit` (`~/.local/share/homebot/hooks` by default) and points the repo's local `core.hooksPath` at that folder. Before every commit
it seals changed secrets and refuses the commit if:

- plaintext from `private/` is staged (even with `git add -f`); when a
  `.gitignore` lets it through, the refusal names that file,
- a `*.age` file in `private/` is not age-encrypted,
- `private` is staged as a symlink, a file or a submodule, or the commit
  deletes every sealed file and leaves none,
- a file is bigger than `limits.maxFileMB`,
- gitleaks finds a secret anywhere in the staged changes,
- gitleaks is not installed.

Names in `private/` compare without regard to case, as git does on macOS.
`homebot save` runs the same checks itself before committing, hook or no
hook, so it needs gitleaks even where `homebot setup` never ran. It also
writes `private/.gitignore` when `private/` has none, as `homebot keys init`
and `homebot pack --private` do. When git cannot list what is staged, read
the remote or abort a failed rebase, save stops with an error instead of
carrying on.

A refused automatic commit becomes an urgent inbox item. So do the two
things that make every commit fail on a new machine: git has no name or
email, or commits are signed with an ssh key file that isn't there yet.

## Profiles

```yaml
# manifests/profiles/workstation.yml
description: Daily driver
include: [base]
steps:
  - link
  - brew: "{os}/Brewfile"
  - apt: linux/apt.txt
    on: linux
  - fonts: [JetBrainsMono]
  - run: ./scripts/gnome-settings.sh
    name: Desktop settings
```

Each step is an action. Built in: `link`, `unlock`, `run`, `brew`,
`apt-repos`, `apt`, `flatpak`, `fonts`. Any `manifests/actions/<name>.sh` adds
an action or replaces a built-in one; `homebot action <name> [args]` runs one
by hand. Scripts run from `manifests/` with `HOMEBOT_OS`,
`HOMEBOT_HOST`, `HOMEBOT_ARCH`, `HOMEBOT_ROOT`, `HOMEBOT_MANIFESTS`,
`HOMEBOT_FURNITURE`, `HOMEBOT_AUTOLINK` and `HOMEBOT_PRIVATE` set.

## homebot.yml

```yaml
profiles:
  default: workstation       # used when `homebot unpack` gets no name
  hosts:
    forge: workstation       # per-hostname override

autosave:
  commit: daily              # off | hourly | daily
  push: true

exports:                     # refreshed by the helper about once a day
  - name: brew
    out: manifests/{os}/Brewfile
    run: brew bundle dump --force --file={out}   # {out}: the command writes it
  - name: apt
    on: linux
    out: manifests/linux/apt.txt
    run: apt-mark showmanual | sort              # otherwise stdout is saved
    profiles: [workstation]  # optional: only machines whose default profile is listed export
  - name: apt-repos
    on: linux
    out: manifests/linux/apt-repos               # {out} may also be a folder
    run: homebot action apt-repos --export {out} # homebot works here even off PATH
  - name: apt                # a name may repeat when the profiles don't overlap
    on: linux
    out: manifests/server/apt.txt
    run: apt-mark showmanual | sort
    profiles: [server]

triage:
  watch: ["~/.config/*", "~/.*"]

limits:
  maxFileMB: 50
```

Triage answers are stored in `decisions.yml` (globs allowed) and snoozes per
machine. An export whose command fails or prints nothing never replaces the
last good snapshot. An export's `out` must stay inside the repo, also through
symlinks and once `{os}`, `{host}` and `{arch}` are filled in, and must not
be, in any case, the repo itself, `manifests/`, or anything homebot or git
own: `autolink/`, `manifests/profiles/`, `manifests/actions/`, `furniture/`,
`homebot/`, `private/`, `.git/`, `homebot.yml`, `decisions.yml`,
`.gitignore` or `.gitattributes`, or a path inside one of them. Put exports
under `manifests/`. An export never deletes a folder to write a file in its
place, or a file to write a folder; it fails and says so.

## Shell

```zsh
eval "$(homebot shell zsh)"
```

Prints the nudge line when a shell starts (from a cache file, no subprocess)
and refreshes the cache in the background when it is older than 30 minutes.
Before each prompt it exports `HOMEBOT_COUNT` (the inbox count, unset when
empty), which any prompt can show. For Starship:

```toml
[env_var.HOMEBOT_COUNT]
format = '[🚚 $env_value ]($style)'
style = 'bold yellow'
```

For the Spaceship prompt, add `homebot` to `SPACESHIP_PROMPT_ORDER`.

## State

Per-machine state lives outside the repo: `$XDG_STATE_HOME/homebot` (links,
secret hashes, snoozes, journal) and `$XDG_CACHE_HOME/homebot` (the nudge
cache). The repo is found from `$HOMEBOT_ROOT`, the current directory, the
binary's location, or `~/.home`.

## Development

homebot is a Go program.

```sh
go build ./cmd/homebot    # ./homebot
go vet ./...
```

### Running the tests

Tests run in a container, on the Docker host of the docker context you name
(`docker context ls` lists yours). There is no default host:

```sh
scripts/docker-test.sh --context NAME   # the whole gate: generate (no diff), build, vet,
                                        # go test, golangci-lint, shellcheck
scripts/docker-test.sh --context NAME go-test ./internal/cli -run TestExit -v
scripts/docker-test.sh --context NAME lint
scripts/docker-test.sh --context NAME generate   # go generate; changed files are copied back
scripts/docker-test.sh --context NAME update ./internal/msg -run TestCatalogSnapshot   # go test ... -update; copies golden files back
```

The script ships the worktree (tracked and untracked-not-ignored files, so
uncommitted work is tested) into a fresh container as a non-root user. The
image is built on the Docker host the first time and again whenever
`scripts/docker-test/Dockerfile` changes; Go modules and the Go build cache
live in `homebot-test-*` volumes.

Without `--context` the script exits with status 64 and names the flag.
`--cpus N`, `--memory SIZE`, `--cpu-shares N` and `--pids-limit N` (before the
subcommand) cap the container. There is no slot lock.
