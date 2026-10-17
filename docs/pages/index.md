---
title: "homebot"
description: "A home-repo bot: it turns a fresh machine into yours and keeps it tidy, so you don't have to remember to."
---

# homebot

::: callout info "No release yet"
There are no packages yet, and the install script installs releases only: until
the first one is published it says so and changes nothing.
:::

One git repo, your home repo, holds everything that makes a machine yours:
dotfiles, secrets, install lists, fonts, notes. **homebot** links it into
`$HOME`, installs what the profile lists, and keeps nudging, gently and in one
place, until nothing needs you.

::: callout tip "The one command to remember"
Run `homebot`. It prints a short to-do list, and every item ends with the
exact command that fixes it. `homebot next` runs the first one.
:::

## Install homebot

On macOS or Linux, one line:

```sh
curl -fsSL https://homebot.sh/install | bash
```

Read it first: [homebot.sh/install](https://homebot.sh/install). It asks before
it installs Homebrew, runs anything with `sudo`, or edits your shell's startup
files, and it ends by listing what it installed or edited. First, before it
changes anything, it downloads the latest release's `install.sh` and
`checksums.txt` from GitHub and checks the script's sha256 against the
checksums; with no release yet, or a script that does not match, it stops there.
Then it:

- finds [Homebrew](https://brew.sh), or installs it with your OK, using Homebrew's own installer;
- does what Homebrew's **Next steps** say: puts `brew` on your `PATH` in your shell's startup file and, on Linux, installs the build tools (and `git`, if it is missing) with `sudo`;
- installs `git`, `age` and `gitleaks` with `brew`, whichever you lack (or whose version is too old);
- runs the release's own installer, which puts homebot in `~/.local/bin`, and puts that folder on your `PATH`;
- starts `homebot setup`.

Open a new terminal afterwards (or run `exec $SHELL -l`) so the `PATH` changes
take effect. It won't run as root or with `sudo`, and without a terminal it
changes nothing unless you pass `--yes`, which answers yes to every question
(Homebrew then installs unattended, so it needs your `sudo` password typed
first, or passwordless `sudo`). `--no-setup` stops before `homebot setup` and
prints the command to run. `--persona ID` and `--name NAME` (or `--persona=ID`
and `--name=NAME`) are passed on to `homebot setup`, so you can pick the robot
and the command name up front: `curl -fsSL https://homebot.sh/install | bash -s -- --persona ID --name NAME`.
Both take lowercase letters, digits and `-`, start with a letter and run to 32
characters (a name needs at least 2); the script refuses anything else before it
changes anything. `homebot setup` still checks that the persona exists and loads,
and with `--no-setup`, or without a terminal, the command it prints carries your
choices.
`--version vX.Y.Z` installs that release instead of the latest.

### Install a release

The front door runs this same installer for you. To run it on its own, it
installs a checksum-checked release into your own bin directory, without `sudo`
and without building anything:

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/hyperlab-sh/homebot/releases/latest/download/install.sh | sh
```

It does not take `--persona` or `--name`: pass them to `homebot setup` yourself.
You can also use the Homebrew cask, a Debian or Fedora package, or `go install`.
See [Install and update](/install/) for the exact commands, signature
verification, and update behavior.

### Rather do it by hand?

::: steps
1. **Install Homebrew**: follow [brew.sh](https://brew.sh), or copy the script below.
   ```sh
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
   ```
   When it finishes, it prints **Next steps**: run those commands. They put `brew` on your `PATH` and, on Linux, install the compilers it needs (`sudo apt-get install build-essential`).
2. **Install the tools homebot uses**
   ```sh
   brew install git age gitleaks
   ```
3. **Install the release**: download `install.sh` and `checksums.txt` from the
   [latest release](https://github.com/hyperlab-sh/homebot/releases/latest),
   check that `install.sh`'s sha256 is the one in `checksums.txt`, then run it:
   ```sh
   sh install.sh
   ```
4. **Put `~/.local/bin` on your `PATH`**, in your shell's startup file, then check:

   <pre data-hb-keep><code>export PATH="$HOME/.local/bin:$PATH"
homebot --help
</code></pre>
:::

To update later, see [Install and update](/install/).

| Tool | Why |
|---|---|
| git 2.40 or newer | homebot's commit guard needs it |
| `age` (optional) | for recovering a secret by hand |
| `gitleaks` | checks every commit for leaked secrets; without it, commits are refused |
| zsh (optional) | the startup line and the prompt count; bash isn't supported yet |
| `gh` or `glab` (optional) | `homebot remote` creates your backup repo with it |
| `fzf`, `lazygit` (optional) | nicer diffs and line merging in `homebot cleanup` |

Without Homebrew: Debian 13 has `age` and `gitleaks` in apt.

## Set up a machine

Run one command, on your first machine or your tenth:

```sh
homebot setup
```

It asks one question at a time; Enter answers yes to the yes-or-no ones:

1. **Missing tools**: without `gitleaks`, it offers `brew install gitleaks`, or tells you what to install and stops.
2. **Your home repo**: `c` clones it (paste the URL, or `owner/repo` for GitHub), `n` starts a new one in `~/.home`, Enter stops for now. A `~/.home` that holds something else is never touched: setup tells you to move it aside.
3. **A new repo**: creates your secrets key. It asks for a passphrase; leave it empty and homebot makes one up. Put it in your password manager: without it, your secrets can't be recovered.
4. **A cloned repo**: lists this machine's profile steps and runs them when you say yes. A real file where a link should go is never overwritten: setup offers to move it aside, and `homebot cleanup` goes through those later.
5. **Git, guard and helper**: asks for your git name and email if git has none, then installs the commit guard and the hourly background helper.
6. **Your shell**: on zsh, adds `eval "$(homebot shell zsh)"` to your `.zshrc` for the startup line and the prompt count.
7. **A new repo**: offers to sort what's in your `$HOME`, one key per thing (`homebot triage`).
8. **Backup**: with no remote yet, `g` creates a private GitHub repo (`l` for GitLab, `u` to paste a URL you already have).
9. **Commit**: offers to commit what setup changed, and pushes when there's a remote. Then it shows the inbox.

Steps already done are skipped, so running it again is safe. Without a
terminal it asks nothing, creates no repo, and prints what's left.

### By hand

The same steps as separate commands, for a new repo:

<pre data-hb-keep><code>mkdir ~/.home &amp;&amp; cd ~/.home &amp;&amp; git init -b main
mkdir -p manifests/profiles
printf 'profiles:\n  default: base\n' &gt; homebot.yml
printf 'steps:\n  - unlock\n  - link\n' &gt; manifests/profiles/base.yml
homebot keys init
homebot pack ~/.zshrc                # moves it into the repo and links it back
homebot pack ~/.netrc --private      # a secret: only the encrypted copy is committed
homebot setup
homebot remote
homebot save
</code></pre>

For a repo you already have:

<pre data-hb-keep><code>git clone &lt;your home repo&gt; ~/.home
homebot unpack --dry-run    # lists the steps without running them
homebot unpack
homebot link --backup       # only if real files are in the way of links
homebot setup
</code></pre>

Then add `eval "$(homebot shell zsh)"` to your `.zshrc`, and follow the
inbox (`homebot`) until it's empty.

## Day to day

| I want to | Run |
|---|---|
| See what needs me | `homebot` |
| Just do the next thing | `homebot next` |
| Start tracking a config | `homebot pack ~/.config/thing` |
| Track it on this OS only | `homebot pack ~/.config/thing --layer linux` |
| Track a secret | `homebot pack ~/.netrc --private` |
| Decide about new files in `$HOME` | `homebot triage` |
| Commit and push now | `homebot save` |
| Fix links after pulling | `homebot link` |

Everything else happens on its own: the helper seals secrets, snapshots
installed packages, pulls and repairs links every hour. Turn on
[autosave](/reference/config/) and it commits and pushes too.

## What's where

| Folder | What happens to it |
|---|---|
| `autolink/` | Symlinked into `$HOME`. `common/config/x` is `~/.config/x`. Edit here = edit live. |
| `private/` | Same as `autolink/`, but only encrypted `*.age` files are committed. |
| `manifests/` | Install profiles and package lists. Nothing runs until `homebot unpack`. |
| `furniture/` | Passive stuff: fonts, drivers, themes, vendored repos, vaults. |
| `bin/` | Personal scripts. Put it on your `PATH` in your shell startup. |

Any other folder is yours; homebot leaves it alone.

::: card "Next: how linking works" icon:link
The folder names in `autolink/` decide everything. Five minutes on
[Linking rules](/linking/) saves an hour of guessing.
:::
