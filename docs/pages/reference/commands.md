---
title: "Commands"
description: "Every homebot command, grouped by when you'd reach for it."
---

# Commands

`homebot --help` prints the same list. `homebot <command> --help` (or `homebot
help <command>`) prints that command's own page: what it does, its options and
examples. Each `homebot persona` subcommand has a page of its own (`homebot
persona use --help`, `homebot help persona use`). `--help` never runs the
command, whatever else is on the line. Every command that changes something
refreshes the inbox afterwards.

`homebot unpack` runs every step without asking; `homebot unpack --dry-run`
lists them first.

Use `homebot persona` to list voices and [choose or write one](/personas/).
Personas change eligible terminal messages, while stored text and commands
keep their original wording.

Use `homebot name [word]` to show or set this home's optional command name,
and `homebot name --off` to remove it. <code data-hb-keep>homebot</code> always remains available.
See [Naming your homebot](/naming/) for `PATH`, conflicts and removal.

## Global options

`--plain` makes homebot speak in its neutral voice for one run, with no
persona; `HOMEBOT_PLAIN=1` does it for every run. It goes before the command
(`homebot --plain save`) or among the options of a command that takes
options (`homebot save --plain`). `next`, `keys`, `triage`, `inventory`,
`export`, `actions`, `action` and `shell` take the words after their name as
typed, so there `--plain` goes before the command's name. `homebot help`
takes it on either side of the command's name (`homebot help --plain link`).
`homebot --help` does not list it.

## Attention

| Command | Does |
|---|---|
| `homebot` | The inbox: top 5 things that need you. `--all` for everything. |
| `homebot next` | Run the fix for the first item. |

## Moving in

| Command | Does | Flags |
|---|---|---|
| `homebot unpack [profile]` | Run a profile's steps | `--dry-run`, `--from N` |
| `homebot pack <path>` | Move from `$HOME` into the repo, link back | `--layer common\|linux\|macos`, `--private` |
| `homebot link` | Make every link, list what's in the way. Exits 1 on [layout errors](/linking/#linking-rules-layout-errors). | `--backup`, `--dry-run` |
| `homebot unlink` | Remove every link homebot made. Files stay in the repo, so the next `homebot link` (or the hourly helper) links them again; to keep a file unlinked, take it out of the repo. | |
| `homebot cleanup` | Go through the backups `link --backup` left: a diff for each (in fzf when it's installed), then delete, keep, use it, or merge some of its lines into the repo file. See [Cleaning up backups](/linking/#linking-rules-conflicts-something-real-is-in-the-way-cleaning-up-backups). | `--list` |

## Secrets

| Command | Does | Flags |
|---|---|---|
| `homebot keys init` | Create the key pair (asks for a passphrase) | |
| `homebot unlock` | Decrypt `private/` | `--theirs` (take the other machine's version) |
| `homebot lock` | Encrypt what changed | `--ours` (keep this machine's version), `--wipe` (delete plaintext) |

## Keeping order

| Command | Does | Flags |
|---|---|---|
| `homebot triage` | One question per new file in watched folders | |
| `homebot inventory` | Links, secrets, repo, exports, helper at a glance | |
| `homebot remote [url]` | Set up where the repo is backed up. See [Backing up](#commands-backing-up-homebot-remote). | |
| `homebot save` | Seal, commit, pull `--rebase`, push | `-m "msg"`, `--no-push` |
| `homebot export [name...]` | Snapshot installed packages into `manifests/` | |
| `homebot tidy` | Run the hourly chores now | `--notify`, `--quiet` |
| `homebot setup` | Take a machine from nothing to an empty inbox: checks for `gitleaks` (offers `brew install`), clones your home repo or starts one in `~/.home` (never touching a `~/.home` that holds something else), unpacks a clone, installs the commit guard and the background helper, asks for your git name and email when git has none, and offers the zsh startup line, triage, a remote and a commit of what it changed (never your own uncommitted work). Steps already done are skipped, and it ends on the inbox. Without a terminal it asks nothing and creates no repo. The helper gets the `XDG_*` folders setup ran with; run setup again after changing one. A relative `XDG_*` is refused. `--name` chooses the command name (see `homebot name`). `--persona` writes `persona:` after checking the persona as `homebot persona use` does: it must exist and load, and a refusal leaves the machine untouched. | `--remove`, `--name WORD`, `--persona ID` |
| `homebot version` | Print the version. `--check` asks GitHub for the latest release, the only release request a command you type makes (the inbox reads what the helper saved), then prints the current and latest versions and how this copy was installed. When a newer release exists it also prints the command that upgrades this install; when this copy is already current it says so and prints no command and nothing about automatic updates. Source and development builds say they don't update themselves. With `update: auto` and a newer release it also says what the helper will do here: the install method can't update itself, `cosign` is missing or too old, the helper hasn't seen the release yet, the release is in its 48-hour soak (with the time it ends), the last attempt failed or left the old version in place (the helper tries again no sooner than 24 hours after an attempt), or an automatic update is eligible, on the helper's next run or, when its next check isn't due yet, on its first run after the time shown. | `--check` |
| `homebot actions` | List the actions profiles can use. It takes no arguments; to run one, use `homebot action <name>`. | |
| `homebot action <name> [args]` | Run one action by hand, e.g. `homebot action apt-repos linux/apt-repos` | |
| `homebot shell zsh` | Print the shell integration. When a command name is set and `~/.local/bin/<name>` links to homebot, it also completes that name, once zsh completion for homebot is loaded before the `eval` line: by `compinit` on a Homebrew cask, Debian or RPM install, which ship `_homebot` (for the cask, Homebrew's `shellenv` must run before `compinit`: see [Homebrew](/install/#install-and-update-homebrew)), or by sourcing `homebot completion zsh` on a script or Go install (see [Shell integration](/reference/inbox/#inbox-helper-shell-integration)). | |
| `homebot name [word]` | Show or set the command name for this home; `--off` returns to <code data-hb-keep>homebot</code>. This is separate from the persona. Tab completion for the name works in zsh only (see `homebot shell zsh`); bash and fish complete <code data-hb-keep>homebot</code> but not the name. | `--off` |
| `homebot persona [list]` | List built-in and local personas and mark the current one. A persona with an error in its chain is marked `[unusable]`. If `homebot.yml` cannot be loaded, the list is still printed, nothing is marked current, the config error is reported as `persona use` reports it, and the exit is 1. | |
| `homebot persona show <id>` | Show the header, colours and four voiced samples. | |
| `homebot persona use <id\|none>` | Validate and write `persona:` in `homebot.yml`, then refresh the shell nudge, prompt symbol and count. Says so when `HOMEBOT_PERSONA` overrides the new choice in this shell. | |
| `homebot persona new <id>` | Create a local persona file without overwriting one. | `--from ID` |
| `homebot persona validate [id\|file]` | Check the active choice, an ID or a TOML path, together with the personas it extends; exit 1 on errors, and on a `homebot.yml` that cannot be loaded. Findings name files under the config directory relative to it. | |
| `homebot persona preview <id>` | Show light and dark samples without changing your repo or sending a notification. `--event` fills the slots the inbox fills with another message or a count; other slots show a generic sample. | `--event ID`, `--all-variants` |
| `homebot persona ids` | List voice IDs, English text and placeholder rules. | |

## Triage keys

`homebot triage` shows each new thing in `~/.config/*` and `~/.*` once:

| Key | Answer |
|---|---|
| `t` | track everywhere (`autolink/common`) |
| `o` | track on this OS only |
| `i` | ignore forever (written to `decisions.yml`) |
| `s` | snooze 7 days (this machine only) |
| `Enter` | next |
| `q` | quit |

## Backing up: `homebot remote`

Without a remote, nothing leaves the machine, and the inbox says "Not backed
up". `homebot remote` sets one up:

- `g`: create a private GitHub repository with `gh`; `l`: the same on GitLab
  with `glab`. Only offered when that tool is installed and logged in; when it
  is installed but not logged in, it says to run `gh auth login` first. It asks
  for a name (the repo folder's, `home` for `~/.home`, by default).
- `u`: paste the URL of a repository you already have.
- Enter: not now.

A new repository's ssh address is used when `~/.ssh` has a key, else its https
address (then `gh auth setup-git` lets git use gh's login). Then it pushes.

With a remote already set, it checks that git can reach it and pushes what
isn't there yet. When an https remote fails because nobody is logged in and
there is an ssh key, it offers the ssh address of the same repository.
`homebot remote <url>` makes that URL the remote. Without a terminal it only
explains.

The inbox shows "Not backed up yet" while a remote has never received a
push, which is also how a remote git can't log in to shows up.

## Where the repo is found

`$HOMEBOT_ROOT` → the current directory (walking up to a `homebot.yml`) → the
binary's location → `~/.home`.

## Generated command details

The sections below come from the same command tree and help catalog used by
the binary. Regenerate them with `go run ./tools/gendocs docs`.

<!-- gendocs:start commands -->
<!-- Generated by go run ./tools/gendocs docs; do not edit this section. -->

### inbox

**Usage:** `homebot [--all]`

Lists what needs you, most urgent first, each with the command that fixes it,<br>and what the background helper did since you last looked. `homebot inbox` is<br>the same command.

| Option | Meaning |
|---|---|
| `--all` | show every item, not only the first five |

### next

**Usage:** `homebot next`

Runs the fix for the first inbox item, the command shown next to it. When the<br>fix is a shell command homebot shouldn&#39;t run for you, it prints it instead.

### unpack

**Usage:** `homebot unpack [profile]`

Runs a profile&#39;s steps in order, without asking first: package installs<br>(brew, apt, flatpak...), links, secrets and `run:` commands. Each step is<br>printed as it starts. Without a name it uses this machine&#39;s profile from<br>homebot.yml (profiles.hosts, else profiles.default).<br><br>Preview the steps with --dry-run. A step that fails stops the run and prints<br>the --from that resumes after you fix it. Steps that are already done (a<br>package that is installed, a link that exists) do nothing the second time.

| Option | Meaning |
|---|---|
| `--dry-run` | list the steps without running any |
| `--from N` | start at step N, skipping the ones before |

| Example | Meaning |
|---|---|
| `homebot unpack --dry-run` | what this machine&#39;s profile would do |
| `homebot unpack server --from 4` | resume the server profile at step 4 |

### pack

**Usage:** `homebot pack &lt;path&gt;...`

Moves a file or folder from $HOME into the repo and puts a link in its place,<br>so it is tracked from then on. It goes to autolink/common unless --layer says<br>otherwise. With --private it is sealed under private/ (encrypted with age)<br>instead. Commit it with homebot save.

| Option | Meaning |
|---|---|
| `--layer common\|linux\|macos` | the layer to put it in (default: common) |
| `--private` | a secret: seal it under private/ |

| Example | Meaning |
|---|---|
| `homebot pack ~/.config/zed` | track Zed&#39;s settings on every machine |
| `homebot pack --layer linux ~/.config/foo` | track it on Linux only |
| `homebot pack --private ~/.ssh/id_ed25519` | track a key, encrypted |

### link

**Usage:** `homebot link`

Links everything under autolink/ (and unlocked secrets under private/) into<br>$HOME. It never overwrites anything: what is in the way is listed as blocked.<br>A file identical to the repo&#39;s copy is replaced by the link, since nothing is<br>lost.<br><br>--backup moves the blockers aside first, renamed &lt;name&gt;.homebot-backup-&lt;time&gt;,<br>and then links. homebot cleanup goes through those backups later.

| Option | Meaning |
|---|---|
| `--dry-run` | show what would change, change nothing |
| `--backup` | move what is in the way aside, then link |

### unlink

**Usage:** `homebot unlink`

Removes every link homebot made. The files stay in the repo, so the links<br>come back with the next homebot link, and within the hour from the background<br>helper (homebot tidy). To keep a file unlinked, take it out of the repo.

### cleanup

**Usage:** `homebot cleanup`

Goes through the backups homebot link --backup left<br>(&lt;name&gt;.homebot-backup-&lt;time&gt;) one at a time. Each shows how it differs from<br>what is linked there now (beside the choices, when fzf is installed), and you<br>pick:<br><br>  delete   remove the backup; what is linked there now stays<br>  keep     leave it; homebot asks again next time<br>  use      copy the backup over the repo file, then remove it<br>  merge    take some of the backup&#39;s lines into the repo file<br><br>merge opens lazygit when it is installed. The backup&#39;s lines are the green<br>ones: space takes the line under the cursor, shift+arrows select several, d<br>drops them, q finishes. Without lazygit it is git add -p: y takes a block, n<br>skips it, s splits it smaller, e edits it. What you took is written to the<br>repo file, and the same backup comes back with what is left, so you can<br>delete it.

| Option | Meaning |
|---|---|
| `--list` | only list the backups |

### keys

**Usage:** `homebot keys init`

Creates the age key pair that seals private/. It asks for a passphrase, then<br>writes private/identity.age (the key, encrypted with that passphrase) and<br>private/recipients.txt (the public key). Run it once ever; other machines get<br>the key with the repo and run homebot unlock.

### unlock

**Usage:** `homebot unlock`

Decrypts the secrets in private/ and links them into $HOME. A secret that<br>changed both here and in the repo is left alone and listed as a conflict.

| Option | Meaning |
|---|---|
| `--theirs` | for conflicts, take the repo&#39;s version |

### lock

**Usage:** `homebot lock`

Seals the secrets in private/ that changed since they were last sealed. The<br>background helper does this on its own before each autosave.

| Option | Meaning |
|---|---|
| `--ours` | for conflicts, keep this machine&#39;s version |
| `--wipe` | also delete the decrypted copies (keeps any that aren&#39;t safely sealed) |

### triage

**Usage:** `homebot triage`

Asks once about each new thing in the watched folders (~/.config/*, ~/.*) and<br>each untracked file in the repo. Each answer is saved right away:<br><br>  t        track it everywhere (moves it into autolink/common, links it back)<br>  o        track it on this OS only<br>  i        ignore it for good (written to decisions.yml)<br>  s        snooze it for 7 days (this machine only)<br>  enter    next<br>  q        stop

### inventory

**Usage:** `homebot inventory`

One screen: this machine&#39;s profile and whether it changed since the last<br>unpack, links, secrets, things to triage, the repo and its remote, package<br>exports and the background helper.

### remote

**Usage:** `homebot remote [url]`

Without a remote, offers to create a private repository with gh (GitHub) or<br>glab (GitLab), when one is installed and logged in, or takes a URL you paste.<br>Then it pushes. With a URL, makes that the remote (origin).<br><br>With a remote already set, checks that it can be reached. When an https<br>remote fails for lack of a login and you have an ssh key, it offers the ssh<br>address of the same repository.

| Example | Meaning |
|---|---|
| `homebot remote` | check the remote, or set one up |
| `homebot remote git@github.com:you/home.git` | use this one |

### save

**Usage:** `homebot save`

Seals changed secrets, commits the changes to tracked files and package<br>snapshots, then pulls (rebasing local commits) and pushes. New files aren&#39;t<br>committed: triage decides about them first. The background helper does the<br>same on its own (autosave in homebot.yml).

| Option | Meaning |
|---|---|
| `-m, --message TEXT` | the commit message (default: one listing the files) |
| `--no-push` | commit and pull, but don&#39;t push |

### export

**Usage:** `homebot export [name...]`

Writes what is installed (Brewfile, apt and flatpak lists, vendor apt<br>repositories...) into manifests/, as configured under exports in homebot.yml.<br>Without names it runs every export for this OS. The background helper runs<br>them about once a day.

| Example | Meaning |
|---|---|
| `homebot export apt apt-repos` | after installing a package from a new apt repository |

### tidy

**Usage:** `homebot tidy`

Runs what the background helper runs on its timer: installs the commit guard<br>if it&#39;s missing, seals secrets, refreshes package exports that are due,<br>autosaves and syncs (when homebot.yml turns autosave on), fixes links that<br>are safe to fix, and refreshes the inbox.

| Option | Meaning |
|---|---|
| `--notify` | show a desktop notification when something needs you |
| `--quiet` | print nothing |

### setup

**Usage:** `homebot setup`

Takes you from nothing to an empty inbox, one question at a time. It checks<br>for git and gitleaks, clones your home repo or starts a new one in ~/.home,<br>installs the commit guard and the background helper (a systemd user timer on<br>Linux, a launchd agent on macOS), and offers to load the shell integration,<br>back the repo up and commit. Steps already done are skipped, so running it<br>again is safe. Without a terminal it asks nothing, creates no repo, and says<br>what is left.

| Option | Meaning |
|---|---|
| `--remove` | uninstall the background helper |
| `--name WORD` | choose a command name for your homebot |
| `--persona ID` | choose the persona for this home repo |

### actions

**Usage:** `homebot actions`

Lists the actions profile steps can use: the built-in ones (link, unlock,<br>run) and the scripts in homebot/actions/.

### action

**Usage:** `homebot action &lt;name&gt; [args...]`

Runs one action with the environment a profile step gets. Everything after<br>the name goes to the action, so `homebot action &lt;name&gt; --help` reaches the<br>action; this page is `homebot action --help`.

| Example | Meaning |
|---|---|
| `homebot action apt-repos linux/apt-repos` | add the vendor apt repositories |

### shell

**Usage:** `homebot shell zsh`

Prints the zsh integration: the prompt line that says when something needs<br>you, refreshed in the background. Load it from .zshrc:<br><br>  eval &#34;$(homebot shell zsh)&#34;

### name

**Usage:** `homebot name [word]`

Shows the name in homebot.yml and the name that works here. Give a word<br>to make it a command on this machine, or use --off to remove a link homebot<br>made. A name may need a new shell before it works.

| Option | Meaning |
|---|---|
| `--off` | remove the name and a link homebot made |

### persona

**Usage:** `homebot persona [command]`

List, inspect, create, and choose the voice homebot speaks in.

| Option | Meaning |
|---|---|
| `--from ID` | copy an existing persona&#39;s header and lines when creating a new one |
| `--event ID` | preview one voiced message id |
| `--all-variants` | show every valid variant in a preview |

### version

**Usage:** `homebot version`

Prints the version of this homebot. `homebot --version` and `homebot -v`<br>print the same.

| Option | Meaning |
|---|---|
| `--check` | Check the latest release and show how this install can upgrade |
<!-- gendocs:end -->
