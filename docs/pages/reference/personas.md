---
title: "Persona files"
description: "Persona TOML fields, validation, fallback and line rotation."
---

# Persona files

Built-in personas ship with homebot. Your files go in
`$XDG_CONFIG_HOME/homebot/personas/<id>.toml` (normally
`~/.config/homebot/personas/<id>.toml`). A local file with a built-in ID
overrides that built-in. IDs are 1–32 ASCII characters: a lowercase letter
followed by lowercase letters, digits or hyphens. `none` is reserved for
turning the voice off. Run `homebot persona ids` for the current list of
messages a persona may reword.

```toml
schema = 1
id = "tester" # optional; must match tester.toml
name = "Tester"
emoji = "🧪"
tagline = "A careful eye on the details"
extends = "neatpik" # optional; omitted means the embedded Neatpik
lang = "en" # optional

[colors]
accent = { light = "#31466B", dark = "#AFC4E9" }
ok = { light = "#245D38", dark = "#9DE0AF" }
muted = { light = 240, dark = 248 }

[lines]
"inbox.header.count" = ["{n:# thing needs|# things need} you."]
```

`schema = 1`, a name and one emoji are required. `id` is optional but, when
present, must match the filename. The name is 1–32 characters. The tagline
is one line of at most 100 characters and cannot contain `$` or a backtick.
`lang` is a BCP 47 language tag; English is the default. Colours may be
`#RRGGBB` or ANSI indices 0–255. Each light/dark token omitted here is
inherited from the parent. `extends = "none"` stops the chain at that file:
the embedded Neatpik supplies nothing, so a colour the chain does not set
stays the terminal's own (`persona show` says `terminal default` for it), and
a message with no line is said by the catalog's English. Any other chain ends with the embedded Neatpik, then the catalog's
English. `extends = "neatpik"` is the default, so writing it is the same as
omitting it: both mean the embedded copy, never a local `neatpik.toml` (that
file only matters when you pick `neatpik` itself). Explicit parent chains may
have at most four files, the persona included (the embedded Neatpik does not
count), and cannot cycle. A `[colors]` that is not a table of `accent`, `ok`
and `muted`, each with `light` and `dark`, makes the persona unusable. For
each message ID,
the first file in that chain with usable lines supplies all its variants.

## File safety and checks

User files must be regular files of at most 256 KiB. A symlink is accepted only
when it points to a regular file. Pipes, devices, sockets and folders are
refused; reading cannot block the background helper on a pipe. Unknown TOML
keys, invalid syntax, a bad header, an invalid ID or an invalid parent make the
persona unusable. An invalid line variant is dropped; usable variants remain,
and an ID with none falls through to its parent. `homebot persona validate
<id>` loads the whole chain, as `show` and `preview` do, and prints every
finding of every file in it to stderr. Each finding starts with the path of the
file it is in, so a problem in a parent is not taken for one in the file you
named. The path is relative to the config directory (`personas/tester.toml`)
when the file is under it, and absolute otherwise. A missing or broken parent,
a chain of more than four files and a cycle make the persona unusable, like a
bad header in any of its files; invalid lines are reported as errors too, and
`validate` exits with an error for them, although at load such a variant is
only dropped (as said above). `validate <file.toml>` does the same for a file
you have not installed yet, with the file as the first of the chain and the
files it extends found by ID among the installed ones. With no argument it
checks the persona in use; when that is `none`, there is no file to check and
it says the neutral voice is selected. If `homebot.yml` cannot be loaded, the
config error is reported as `persona use` reports it and the exit is 1; the
persona named by `HOMEBOT_PERSONA`, if any, is still checked, and with none set
there is nothing to check. Naming `none` is refused: it is reserved and has no
file. Findings go to stderr, like every other warning and error of the CLI, so
the stdout of `show` and `preview` is only what they print. `homebot persona
use <id>` refuses a persona with any error in its chain, even a single bad line
variant that would otherwise just be dropped, and `homebot persona list` marks
such a persona `[unusable]` with its first error, the same verdict `use` and
`validate` give: a persona is judged with the files it extends, so one under a
broken parent, in a cycle or in a chain of five files is `[unusable]` too. When
the error is in another file than the persona's own, the reason starts with
that file's path, relative to the config directory when the file is under it.
`list` still prints every persona when `homebot.yml` cannot be loaded, reports
that error as `use` does, exits 1 and marks none as current.

Line IDs must be in `homebot persona ids` and marked for voice. A variant must
parse, preserve required and fixed placeholders, plural counts (a count the
English shows must still be shown, as a bare `{n}` or in a plural form with
`#`: `{n} {n:step|steps}` and `{n:# step|# steps}` both show it,
`{n:step|steps}` alone hides it), leading/trailing whitespace, a leading
`{name}: ` prefix, a leading `,`, `;` or `(`, placeholders that touch in the
English (`{a}{b}`) still touching, punctuation that protects an appended
command, and the original number of line breaks. An empty list of variants is
an error too. The validator rejects control and format characters, including
terminal escapes and bidi controls. In a line, it allows only the same newlines
as the English; the emoji field alone may contain the joiner and variation
marks needed for one emoji. It warns about low colour contrast, uncertain emoji
width, duplicate or long variants, non-English language tags and possible added
facts. A variant beyond the hard length limit is rejected. Run `homebot persona
preview <id>` to see the voice in both themes, or add `--event <message-id>`
and `--all-variants` to inspect individual lines. A slot the inbox fills with
another message or a count is filled the same way (`3 things need you (1
urgent)`); any other slot shows a generic sample such as `<profile>`.

## What is voiced

A persona words what homebot prints, not what it stores. The reasons
`homebot link` gives for a link it will not make (a name that breaks the
layout rules, something in the way in `$HOME`) are in the persona's voice when
printed, in `link`, `link --dry-run`, the inbox item and the error of the
`link` unpack action. They are English, whatever the persona, in anything
homebot stores, and when stdout is piped or `--plain` is set.

## Selection and rotation

`HOMEBOT_PERSONA` (when nonempty) wins over `persona:` in `homebot.yml`; the
default is Neatpik. `none` selects the neutral voice. A missing or unusable
choice falls back to the embedded Neatpik, even if a broken local
`neatpik.toml` shadows the built-in. A configured failure raises
`persona_broken` in the inbox; a failing environment override warns once on a
terminal. The neutral English is used when a message has no usable variant.

For each message ID, homebot picks one variant per process and tries not to
repeat the previous process's choice. Previous picks are stored per machine
in `$XDG_CACHE_HOME/homebot/voice.json`; a cache read or write failure only
changes the pick. The main nudge line uses a stable pick keyed on the inbox
items; notifications and the nudge's urgent suffix always use the same variant
for a persona. Neither changes wording on every refresh. This state
stays outside the home repo.

## Generated file fields and voice IDs

These tables come from the accepted persona fields and the message catalog.
They are updated by `go run ./tools/gendocs docs`.

<!-- gendocs:start personas -->
<!-- Generated by go run ./tools/gendocs docs; do not edit this section. -->

### Fields

| Field | Type | Required | Meaning |
|---|---|---|---|
| `schema` | integer | yes | File format version; must be 1. |
| `id` | string | no | Optional ID; when present, it must match the filename. |
| `name` | string | yes | Speaker name, 1 to 32 characters. |
| `emoji` | string | yes | One emoji grapheme, without ASCII characters. |
| `tagline` | string | no | One line, at most 100 characters; no dollar sign or backtick. |
| `extends` | string | no | Explicit parent ID; none stops explicit parent traversal, then embedded Neatpik remains the fallback. |
| `lang` | string | no | BCP 47 language tag; English is the default. |
| `colors` | object | no | Accent, ok and muted light/dark colours; missing values inherit. |
| `lines` | object | no | Voice message IDs, each with an array of variants. |

### Voice IDs

| ID | Kind | English |
|---|---|---|
| `action.error.unavailable` | error | <code>{name}: this action is not available in this build</code> |
| `action.link.error.layout` | error | <code>{action} the repo breaks {n:a layout rule&#124;# layout rules}:<br>  {reasons}</code> |
| `action.link.error.unknown_tree` | error | <code>{action} unknown tree &#34;{tree}&#34; (use {autolink} or {private})</code> |
| `action.link.status.changed` | status | <code>{action} {n} changed</code> |
| `action.link.warning.in_way` | warning | <code>, {n} in the way (see: {cmd})</code> |
| `action.run.error.exit` | error | <code>{action} &#34;{command}&#34; exited with {code}</code> |
| `action.unlock.status.decrypted` | status | <code>{action} {n} decrypted</code> |
| `action.unlock.status.nothing` | status | <code>{action} nothing to decrypt</code> |
| `action.unlock.warning.conflicts` | warning | <code>, {n} conflicts (see: {self}{unlock})</code> |
| `cleanup.diff.backup_is_link` | status | <code>The backup is a link to {dest}.</code> |
| `cleanup.diff.kind_mismatch` | status | <code>The backup is a {backup_kind}, but {path} is a {now_kind} now.</code> |
| `cleanup.diff.only_copy` | status | <code>Nothing is at {path} now; this backup is the only copy.</code> |
| `cleanup.diff.same` | status | <code>Identical to what is linked there now.</code> |
| `cleanup.error.merge_not_repo` | error | <code>{path} isn&#39;t a file in the repo, so there is nothing to merge into</code> |
| `cleanup.error.scratch_git` | error | <code>{git} {subcommand} in the merge scratch repo failed.</code> |
| `cleanup.error.use_not_repo` | error | <code>{path} isn&#39;t a file in the repo, so the backup can&#39;t go there</code> |
| `cleanup.hint.terminal` | hint | <code>Review them in a terminal:</code> |
| `cleanup.status.nothing_taken` | status | <code>Nothing taken from the backup.</code> |
| `cleanup.status.summary` | status | <code>Deleted {deleted}, used {used}, kept {kept}.</code> |
| `cleanup.status.summary_merged` | status | <code>Deleted {deleted}, used {used}, kept {kept}; merged lines {merged:# time&#124;# times}.</code> |
| `cleanup.success.deleted` | success | <code>deleted {path}</code> |
| `cleanup.success.merged` | success | <code>merged into {repo_path}; {self}{save} commits it</code> |
| `cleanup.success.none` | success | <code>No backups to review.</code> |
| `cleanup.success.used` | success | <code>copied into {repo_path} and deleted the backup; {self}{save} commits it</code> |
| `cli.error.args_hint` | hint | <code>See: {homebot}{command_space}{help}</code> |
| `cli.error.unknown_action` | error | <code>No action named &#34;{name}&#34;. See:</code> |
| `cli.error.unknown_command` | error | <code>Unknown command &#34;{name}&#34;. See:</code> |
| `cli.error.version_args` | error | <code>{version} takes no arguments, not &#34;{argument}&#34;. See:</code> |
| `config.error.autosave` | error | <code>{file}: {autosave} must be a mapping</code> |
| `config.error.autosave_commit` | error | <code>{file}: {commit} must be {choices}</code> |
| `config.error.autosave_push` | error | <code>{file}: {push} must be {boolean}</code> |
| `config.error.export_duplicate` | error | <code>{file}: {exports_open}{i}{exports_close}: duplicate export name &#34;{name}&#34; (give each one {profiles} that don&#39;t overlap)</code> |
| `config.error.export_mapping` | error | <code>{where}: expected a mapping with {keys}</code> |
| `config.error.export_out_absolute` | error | <code>{where}: {out_key} {out} of export &#34;{name}&#34; is an absolute path; give a path inside the repo, relative to it</code> |
| `config.error.export_out_git` | error | <code>{where}: {out_key} {out} of export &#34;{name}&#34; is in {git_dir}, which belongs to git</code> |
| `config.error.export_out_managed` | error | <code>{where}: {out_key} {out} of export &#34;{name}&#34; is {managed}, which homebot keeps for itself; put exports under {manifests}</code> |
| `config.error.export_out_managed_inside` | error | <code>{where}: {out_key} {out} of export &#34;{name}&#34; is inside {managed}, which homebot keeps for itself; put exports under {manifests}</code> |
| `config.error.export_out_outside` | error | <code>{where}: {out_key} {out} of export &#34;{name}&#34; leads outside the repo</code> |
| `config.error.export_out_private` | error | <code>{where}: {out_key} {out} of export &#34;{name}&#34; is in {private}, where your secrets are</code> |
| `config.error.export_out_root` | error | <code>{where}: {out_key} {out} of export &#34;{name}&#34; is the repo itself; name a file or folder inside it</code> |
| `config.error.export_out_symlink` | error | <code>{where}: {out_key} {out} of export &#34;{name}&#34; goes through a symlink to {target}, which is outside the repo, the repo itself, or a path homebot keeps for itself</code> |
| `config.error.export_profiles` | error | <code>{where}.{profiles}: expected a list of profile names</code> |
| `config.error.export_required` | error | <code>{where}: {key_name} is required</code> |
| `config.error.exports` | error | <code>{file}: {exports} must be a list</code> |
| `config.error.limits` | error | <code>{file}: {limits} must be a mapping</code> |
| `config.error.limits_max` | error | <code>{file}: {max_file} must be a positive number</code> |
| `config.error.link_gone` | error | <code>{file}: &#34;{link}&#34; is gone. Wrap a folder&#39;s name in {marks} in {autolink} to keep it a real folder, e.g. {example}</code> |
| `config.error.name` | error | <code>{where}: {name_key} must be a word, the command you call your homebot by, such as {example}</code> |
| `config.error.name_syntax` | error | <code>{where}: {name_key} &#34;{value}&#34; must be {rule}</code> |
| `config.error.not_mapping` | error | <code>{file}: expected a mapping at the top level</code> |
| `config.error.old_name` | error | <code>homebot reads {config_file} now: rename {path} to {config_file}</code> |
| `config.error.persona` | error | <code>{where}: {key} must be a string: {allowed}</code> |
| `config.error.persona_syntax` | error | <code>{where}: {key} &#34;{value}&#34; must be {allowed}</code> |
| `config.error.profiles` | error | <code>{file}: {profiles} must be a mapping</code> |
| `config.error.profiles_default` | error | <code>{file}: {default_key} must be a profile name</code> |
| `config.error.profiles_hosts` | error | <code>{file}: {hosts_key} must map host names to profile names</code> |
| `config.error.string_list` | error | <code>{where}: expected a string or a list of strings</code> |
| `config.error.triage` | error | <code>{file}: {triage} must be a mapping</code> |
| `config.error.triage_watch` | error | <code>{file}: {watch} entries end in {star} or {dot_star} (got &#34;{value}&#34;)</code> |
| `config.error.unknown_key` | error | <code>{where}: unknown key &#34;{key_name}&#34; (expected {keys})</code> |
| `config.error.unknown_key_under` | error | <code>{where}: unknown key &#34;{key_name}&#34; under {parent} (expected {keys})</code> |
| `config.error.unknown_os` | error | <code>{where}: unknown OS &#34;{value}&#34; (use {linux} or {macos})</code> |
| `config.error.yaml` | error | <code>{file}: invalid {yaml}: {parser_message}</code> |
| `conflict.different` | error | <code>already exists with different content</code> |
| `conflict.file_vs_folder` | error | <code>is a file, but the repo has a folder here</code> |
| `conflict.folder_vs_file` | error | <code>is a folder, but the repo has a file here</code> |
| `conflict.symlink_outside` | error | <code>is a symlink to {dest}, outside this repo</code> |
| `context.error.no_config` | error | <code>No {config} found. Run {self} inside your home repo, or set {env}.</code> |
| `context.error.no_home` | error | <code>{env} is not set.</code> |
| `context.error.platform` | error | <code>homebot supports {oses}, not {platform}.</code> |
| `context.hint.setup` | hint | <code>New here? This clones your home repo or starts one:</code> |
| `export.error.empty_file` | error | <code>produced an empty file; kept the previous {out}</code> |
| `export.error.empty_folder` | error | <code>produced an empty folder; kept the previous {out}</code> |
| `export.error.exit` | error | <code>exited with {code}{stderr}</code> |
| `export.error.kept_aside` | error | <code>{error}<br>The previous export is kept at {path}.</code> |
| `export.error.out_is_file` | error | <code>wrote a folder, but {out} is a file; kept the file. Remove it yourself, or give the export another {out_key}</code> |
| `export.error.out_is_folder` | error | <code>wrote a file, but {out} is a folder; kept the folder. Remove it yourself, or give the export another {out_key}</code> |
| `export.error.unknown` | error | <code>No export named {names} for this OS. Exports here: {known}.</code> |
| `export.reason.not_installed` | status | <code>{tool} is not installed</code> |
| `export.status.none` | status | <code>No exports configured for this OS in {config}.</code> |
| `export.warning.left_behind` | warning | <code>The temporary folder {path} could not be removed. It is safe to delete.</code> |
| `fsutil.error.bad_json` | error | <code>{path} is not valid {format}. Fix or delete it (it is machine state, safe to delete).</code> |
| `git.error.failed` | error | <code>{git} {args} failed.</code> |
| `helper.error.launchctl` | error | <code>{command} failed.</code> |
| `helper.error.no_systemd` | error | <code>No systemd user session here, so the hourly helper can&#39;t be installed.<br>Run this yourself, or from cron:</code> |
| `helper.error.systemctl` | error | <code>{command}{args} failed.</code> |
| `identity.error.config_failed` | error | <code>{git_config} {scope} {setting} {value} failed.</code> |
| `identity.hint.set_once` | hint | <code>Set it once:</code> |
| `identity.hint.track` | hint | <code>Track it so every machine gets it:</code> |
| `identity.prompt.email` | prompt | <code>Your email:</code> |
| `identity.prompt.name` | prompt | <code>Your name:</code> |
| `identity.status.skipped` | hint | <code>Skipped. Set it later:</code> |
| `identity.success.saved` | success | <code>saved {name} &lt;{email}&gt; in {config}</code> |
| `identity.warning.unknown` | warning | <code>Git doesn&#39;t know who you are yet, and {bot} commits as you.</code> |
| `inbox.backups.title` | inbox | <code>{n:# backup&#124;# backups} that {self}{link} moved aside: compare, then delete or keep</code> |
| `inbox.behind.title` | inbox | <code>{n:# new commit&#124;# new commits} on the remote</code> |
| `inbox.blocked.title` | inbox | <code>A commit was refused</code> |
| `inbox.broken.title` | inbox | <code>Could not check {check}</code> |
| `inbox.diverged.title` | inbox | <code>This machine and the remote both have new commits</code> |
| `inbox.done.heading` | status | <code>  Done for you since you last looked:</code> |
| `inbox.export_failing.title` | inbox | <code>Export {name} is failing</code> |
| `inbox.gitconfig_shadow.detail` | inbox | <code>Git reads it after {repo_config}, so its name, email or signing win. Move it aside; {self}{cleanup} then shows it, and anything this machine needs goes in {local_config}.</code> |
| `inbox.gitconfig_shadow.title` | inbox | <code>{gitconfig} overrides the git config from the repo</code> |
| `inbox.header.clear` | success | <code>nothing needs you. Home is in order.</code> |
| `inbox.header.count` | inbox | <code>{n:# thing needs&#124;# things need} you</code> |
| `inbox.helper_no_linger.detail` | inbox | <code>systemd ends your timers with your last session; with lingering on it keeps them running</code> |
| `inbox.helper_no_linger.title` | inbox | <code>The background helper stops when you log out</code> |
| `inbox.helper_stale.title` | inbox | <code>The background helper last ran {ago}</code> |
| `inbox.link_conflict.title` | inbox | <code>{n:# file is&#124;# files are} in the way of links</code> |
| `inbox.link_invalid.title` | inbox | <code>{n:# layout error&#124;# layout errors} in the repo; those parts aren&#39;t linked</code> |
| `inbox.link_pending.title` | inbox | <code>{n:# link&#124;# links} to make or fix</code> |
| `inbox.missing_profile.detail` | inbox | <code>Write the steps that set up this machine</code> |
| `inbox.missing_profile.title` | inbox | <code>Profile {profile} does not exist yet</code> |
| `inbox.more` | hint | <code>  ...and {n} more:</code> |
| `inbox.name_blocked.title` | inbox | <code>The name {name} is blocked on this machine</code> |
| `inbox.name_helper_path.title` | inbox | <code>The helper cannot use the name {name} yet</code> |
| `inbox.name_no_bin.title` | inbox | <code>The name {name} needs a bin directory here</code> |
| `inbox.name_shadowed.detail` | inbox | <code>The other program is at {path}.</code> |
| `inbox.name_shadowed_other.title` | inbox | <code>Another program shadows the name {name}</code> |
| `inbox.name_shadowed_ours.title` | inbox | <code>The name {name} shadows another program</code> |
| `inbox.never_pushed.title` | inbox | <code>Not backed up yet: nothing pushed to the remote</code> |
| `inbox.never_unpacked.last` | inbox | <code> (last unpacked: {other})</code> |
| `inbox.never_unpacked.title` | inbox | <code>Profile {profile} was never unpacked here{last}</code> |
| `inbox.next_hint` | hint | <code>  Do the first one now:</code> |
| `inbox.no_gitleaks.title` | inbox | <code>{tool} is missing, so every commit is refused</code> |
| `inbox.no_guard.title` | inbox | <code>The commit guard is not installed</code> |
| `inbox.no_helper.detail` | inbox | <code>It seals, exports, links and reminds you every hour</code> |
| `inbox.no_helper.title` | inbox | <code>The background helper is not installed</code> |
| `inbox.no_identity.detail` | inbox | <code>Commits by {self}{save} and the hourly autosave fail without a name and email.</code> |
| `inbox.no_identity.title` | inbox | <code>Git doesn&#39;t know who you are</code> |
| `inbox.no_keys.title` | inbox | <code>Secrets are not set up yet</code> |
| `inbox.no_profile.detail` | inbox | <code>Set {default_key} or {hosts_key} in {config_file}</code> |
| `inbox.no_profile.title` | inbox | <code>No profile chosen for {host}</code> |
| `inbox.no_remote.title` | inbox | <code>Not backed up: the repo has no remote</code> |
| `inbox.no_repo.title` | inbox | <code>{repo} is not a git repository</code> |
| `inbox.no_signing_key.detail` | inbox | <code>Every commit fails until it&#39;s here. Restore it ({self}{unlock}), or stop signing on this machine only:</code> |
| `inbox.no_signing_key.title` | inbox | <code>Commits are signed with {key_path}, which this machine doesn&#39;t have</code> |
| `inbox.notify.body` | notification | <code>{title}{more}. Run:</code> |
| `inbox.notify.more` | notification | <code> (+{n} more)</code> |
| `inbox.notify.title` | notification | <code>{bot} needs you</code> |
| `inbox.nudge` | nudge | <code>{bot}: {n:# thing needs&#124;# things need} you{urgent}, run:</code> |
| `inbox.profile_changed.title` | inbox | <code>Profile {profile} changed since the last unpack</code> |
| `inbox.secret_conflict.title` | inbox | <code>{n:# secret&#124;# secrets} changed here and on another machine</code> |
| `inbox.secret_locked.title` | inbox | <code>{n:# secret is&#124;# secrets are} still locked</code> |
| `inbox.secret_unsealed.title` | inbox | <code>{n:# secret&#124;# secrets} not sealed yet</code> |
| `inbox.secret_upstream.title` | inbox | <code>{n:# secret&#124;# secrets} changed on another machine</code> |
| `inbox.tidy_failed.title` | inbox | <code>Automatic {step_name} failed {ago}</code> |
| `inbox.triage.title` | inbox | <code>{n:# new thing&#124;# new things} to sort: track, ignore or snooze</code> |
| `inbox.uncommitted.title` | inbox | <code>{n:# uncommitted change&#124;# uncommitted changes}</code> |
| `inbox.unpack_preview` | inbox | <code>It runs every step without asking; to see them first: {self}{preview}</code> |
| `inbox.unpushed.title` | inbox | <code>{n:# commit&#124;# commits} not pushed</code> |
| `inbox.unreachable.title` | inbox | <code>Last reached the remote {ago}</code> |
| `inventory.exports.failing` | error | <code>{name} failing</code> |
| `inventory.exports.never` | status | <code>{name} never</code> |
| `inventory.exports.none` | status | <code>none configured</code> |
| `inventory.exports.ok` | status | <code>{name} {ago}</code> |
| `inventory.helper.installed` | status | <code>installed · last ran {ago}</code> |
| `inventory.helper.missing` | warning | <code>not installed ({self}{setup})</code> |
| `inventory.links.invalid` | error | <code>, {n} layout errors</code> |
| `inventory.links.value` | status | <code>{ok} ok, {to_fix} to fix, {blocked} blocked, {locked} locked</code> |
| `inventory.profile.changed` | warning | <code>, changed since</code> |
| `inventory.profile.never` | status | <code>never unpacked here</code> |
| `inventory.profile.none` | warning | <code>none chosen for this machine</code> |
| `inventory.profile.unpacked` | status | <code>unpacked {ago}</code> |
| `inventory.profile.value` | status | <code>{profile} · {status}</code> |
| `inventory.repo.never_pushed` | warning | <code> (never pushed: {self}{remote})</code> |
| `inventory.repo.no_remote` | warning | <code> (no remote: {self}{remote})</code> |
| `inventory.repo.not_repo` | error | <code>not a git repository</code> |
| `inventory.repo.upstream` | status | <code> → {upstream}</code> |
| `inventory.repo.value` | status | <code>{branch}{upstream} · {ahead} ahead, {behind} behind, {uncommitted} uncommitted</code> |
| `inventory.secrets.count` | status | <code>{n} {state}</code> |
| `inventory.secrets.no_keys` | warning | <code>no keys yet ({self}{keys_init})</code> |
| `inventory.secrets.none` | status | <code>none yet</code> |
| `inventory.triage.value` | status | <code>{to_sort} to sort, {ignored} ignored</code> |
| `keys.error.mismatch` | error | <code>The passphrases didn&#39;t match; nothing was saved.</code> |
| `keys.hint.commit` | hint | <code>Commit {identity} and {recipients}:</code> |
| `keys.hint.passphrase` | hint | <code>Leave it empty and {bot} makes up a strong one and prints it. Save it in your password manager: without it, the secrets in this repo can&#39;t be recovered.</code> |
| `keys.prompt.confirm` | prompt | <code>Confirm passphrase:</code> |
| `keys.prompt.passphrase` | prompt | <code>Enter passphrase (leave empty to autogenerate a secure one):</code> |
| `keys.status.autogenerated` | status | <code>using autogenerated passphrase &#34;{passphrase}&#34;</code> |
| `keys.status.creating` | status | <code>Creating your secrets key. {bot} asks for a passphrase next.</code> |
| `keys.success.created` | success | <code>Keys created. The public key:</code> |
| `layout.both_layers` | error | <code>provided by both {providers}; keep it in one place</code> |
| `layout.dot_parens` | error | <code>&#34;{raw}&#34; names no folder: {dot_names} would link outside the folder it is in; rename it</code> |
| `layout.double_parens` | error | <code>&#34;{raw}&#34;: wrap the name in one pair of {marks}, like &#34;{open}{name}{close}&#34;</code> |
| `layout.empty_parens` | error | <code>&#34;{marks}&#34; has no name inside</code> |
| `layout.file_parens` | error | <code>{marks} marks folders only: rename &#34;{raw}&#34; to &#34;{name}&#34;</code> |
| `layout.inner_file_parens` | error | <code>{path}: {marks} marks folders only: rename &#34;{raw}&#34; to &#34;{name}&#34;</code> |
| `layout.inside_repo` | error | <code>would be linked at {target}, inside the home repo; rename it</code> |
| `layout.must_mark` | error | <code>rename &#34;{raw}&#34; to &#34;{open}{raw}{close}&#34;{every_layer}: it holds {inner}, which must not be a link, so it can&#39;t be one either</code> |
| `layout.old_name` | error | <code>&#34;{raw}&#34; is the old layout: rename it to &#34;{new}&#34;</code> |
| `layout.outside_folder` | error | <code>would be linked at {target}, outside the folder it is in; rename it</code> |
| `layout.spelling` | error | <code>spelled &#34;{marked}&#34; in {where_marked} but &#34;{plain}&#34; in {where_plain}; use one spelling everywhere</code> |
| `layout.top_dot` | error | <code>&#34;{raw}&#34;: top-level names get their dot from homebot; rename it to &#34;{open}{name}{close}&#34;</code> |
| `link.error.backup_exists` | error | <code>Cannot move {target} aside: {backup} is already there. Try again in a second.</code> |
| `link.hint.backup` | hint | <code>To move what is in the way aside (renamed {backup_pattern}, nothing is deleted):</code> |
| `link.hint.compare` | hint | <code>Compare and delete them when you have a minute:</code> |
| `link.status.dry_run_ok` | status | <code>{n} already linked</code> |
| `link.status.interrupted` | status | <code>Stopped; nothing else was moved or linked. Run {self}{link} again to finish.</code> |
| `link.status.moved_aside` | status | <code>moved aside {path}</code> |
| `link.summary.base` | status | <code>{n:# change&#124;# changes}, {ok} already linked</code> |
| `link.summary.blocked` | warning | <code>, {n} blocked</code> |
| `link.summary.invalid` | error | <code>, {n:# layout error&#124;# layout errors} in the repo</code> |
| `link.summary.locked` | status | <code>, {n} locked ({self}{unlock})</code> |
| `lock.status.nothing` | status | <code>Nothing to seal.</code> |
| `lock.status.wiped` | status | <code>Sealed {sealed}, removed {n:# plaintext file&#124;# plaintext files}.</code> |
| `lock.success.sealed` | success | <code>sealed {paths}</code> |
| `lock.warning.kept` | warning | <code>kept {private}{name} (not safely sealed; resolve it first)</code> |
| `manifest.error.action_name` | error | <code>{where}: &#34;{name}&#34; is not a valid action name</code> |
| `manifest.error.args` | error | <code>{where}: arguments must be a value or a list of values</code> |
| `manifest.error.description` | error | <code>{file}: {key} must be text</code> |
| `manifest.error.include` | error | <code>{file}: {key} must be a profile name or a list of them</code> |
| `manifest.error.loop` | error | <code>Profiles include each other in a loop: {chain}</code> |
| `manifest.error.name` | error | <code>{where}: {key} must be text</code> |
| `manifest.error.no_profile` | error | <code>No profile named &#34;{name}&#34; in {dir}.</code> |
| `manifest.error.no_profile.available` | error | <code>No profile named &#34;{name}&#34; in {dir}. Available: {names}</code> |
| `manifest.error.not_mapping` | error | <code>{file}: expected a mapping with {steps}</code> |
| `manifest.error.old_extension` | error | <code>Profiles end in {ext} now: rename {path} to {name}{ext}</code> |
| `manifest.error.on` | error | <code>{where}: {key} must be {oses}, or a list of them</code> |
| `manifest.error.on_os` | error | <code>{where}: unknown OS &#34;{value}&#34; in {key} (use {oses})</code> |
| `manifest.error.one_action` | error | <code>{where}: each step needs exactly one action, found {names}</code> |
| `manifest.error.one_action.none` | error | <code>{where}: each step needs exactly one action, found none</code> |
| `manifest.error.step_shape` | error | <code>{where}: expected {shape}</code> |
| `manifest.error.steps` | error | <code>{file}: {key} must be a list</code> |
| `manifest.error.unknown_key` | error | <code>{file}: unknown key &#34;{key_name}&#34; (expected {keys})</code> |
| `manifest.error.unknown_variable` | error | <code>{where}: unknown variable {braced_key} (available: {vars})</code> |
| `manifest.error.yaml` | error | <code>{file}: invalid {yaml}: {parser_message}</code> |
| `name.error.refused` | error | <code>Cannot use {name}: {reason} ({path})</code> |
| `name.error.refused_no_path` | error | <code>Cannot use {name}: {reason}</code> |
| `name.hint.path` | hint | <code>Add ~/.local/bin to your PATH to type {name}; homebot works now.</code> |
| `name.reason.foreign` | error | <code>a foreign file already has that name; it was left untouched</code> |
| `name.reason.in_repo` | error | <code>the home repo already places that name in ~/.local/bin</code> |
| `name.reason.no_homebot` | error | <code>there is no stable homebot binary to link to</code> |
| `name.reason.on_path` | error | <code>a program with that name is on PATH</code> |
| `name.reason.reserved` | error | <code>homebot uses it as a command</code> |
| `name.reason.shell` | error | <code>the shell uses that word</code> |
| `name.reason.synced_bin` | error | <code>another tool manages ~/.local/bin</code> |
| `name.reason.syntax` | error | <code>use 2 to 32 lowercase letters, digits and hyphens, starting with a letter</code> |
| `name.reason.temp_blocked` | error | <code>the temporary link is occupied; your current link was kept</code> |
| `name.reason.tool` | error | <code>homebot runs a program by that name</code> |
| `name.status.configured` | status | <code>Configured name: {name}</code> |
| `name.status.effective` | status | <code>Effective name here: {name}</code> |
| `name.status.off` | success | <code>Name turned off; homebot works as before.</code> |
| `name.status.old_stops` | warning | <code>The old name {name} stops working; no forwarding link is left.</code> |
| `name.status.set` | success | <code>Name set to {name}.</code> |
| `name.warning.in_work_tree` | warning | <code>Your ~/.local/bin is inside a git work tree; another tool could commit this link.</code> |
| `name.why.bad_record` | status | <code>The machine&#39;s name record at {path} cannot be read.</code> |
| `name.why.config_off` | status | <code>The machine still has a name link, but homebot.yml has no name.</code> |
| `name.why.link_broken` | status | <code>The recorded link at {path} is missing or points elsewhere.</code> |
| `name.why.no_record` | status | <code>This machine has not recorded that name yet.</code> |
| `name.why.not_on_path` | status | <code>The name&#39;s link is not on this process&#39;s PATH.</code> |
| `name.why.recorded_other` | status | <code>This machine still uses {recorded}; run homebot name {configured} to finish the change.</code> |
| `name.why.shadowed` | status | <code>Another program at {path} comes before the name&#39;s link on PATH.</code> |
| `next.hint.no_fix` | hint | <code>No automatic fix for this one; the detail above says what to change.</code> |
| `next.hint.run_yourself` | hint | <code>Run this yourself:</code> |
| `next.success.nothing` | success | <code>Nothing needs you.</code> |
| `pack.error.already_in_repo` | error | <code>{repo_path} is already in the repo. Pack the new things inside it instead.</code> |
| `pack.error.inside_managed` | error | <code>{path} is inside {dir}, which is already managed. It is in the repo already.</code> |
| `pack.error.is_repo` | error | <code>{path} is (or contains) the home repo itself.</code> |
| `pack.error.layer_not_linked` | error | <code>The {layer} layer is not linked on {os}, so a file packed there could not be linked back (use {layers}).</code> |
| `pack.error.link_failed` | error | <code>Moved {path} into the repo but could not link it back. Link it with:</code> |
| `pack.error.managed` | error | <code>{path} is already managed.</code> |
| `pack.error.missing` | error | <code>{path} does not exist.</code> |
| `pack.error.nested_repo` | error | <code>{path} is a git repository of its own: the home repo would hold only a pointer to it, and its files would never reach the remote. Remove its {git_dir}, or leave it out of the home.</code> |
| `pack.error.no_args` | error | <code>What should be packed? e.g.</code> |
| `pack.error.no_keys` | error | <code>Create keys before packing secrets:</code> |
| `pack.error.outside_home` | error | <code>Only things inside your home folder can be packed (got {path}).</code> |
| `pack.error.parens` | error | <code>&#34;{name}&#34; is wrapped in {marks}, which the repo uses to mark folders that must not be links, so it can&#39;t be tracked.</code> |
| `pack.error.plain_parent` | error | <code>{repo_path} is a plain folder, so it can&#39;t hold a new folder that must stay real. Rename it to {open}{name}{close} in every layer first.</code> |
| `pack.error.private_reserved` | error | <code>{path} can&#39;t go into {private}: git would stage it as plaintext there. {readme}, {gitignore}, {gitkeep} and {recipients} files, {age_glob} files and {git_dir} folders are the repo&#39;s own; rename or remove it first.</code> |
| `pack.error.source_not_removed` | error | <code>Copied {path} into the repo as {repo_path}, but could not remove the original: {reason}<br>Remove it, then link the copy with:</code> |
| `pack.error.symlink` | error | <code>{path} is a symlink. Pack the real file it points to instead.</code> |
| `pack.error.top_level` | error | <code>Only dotfiles and {exceptions} can be tracked at the top of your home folder (got {home_prefix}{name}).</code> |
| `pack.error.unknown_layer` | error | <code>Unknown layer &#34;{layer}&#34; (use {layers}).</code> |
| `pack.error.unreadable` | error | <code>Can&#39;t read {path}, so homebot can&#39;t check it for a git repository of its own. Nothing was moved; make it readable, or leave it out of the home.</code> |
| `pack.hint.commit` | hint | <code>Commit it with:</code> |
| `pack.status.moved` | status | <code>moved {from} → {to}</code> |
| `pack.success.packed` | success | <code>packed {from} → {to}</code> |
| `pack.success.sealed_suffix` | success | <code> (sealed {n})</code> |
| `persona.success.created` | success | <code>Created persona file: {path}</code> |
| `persona.success.greeting` | success | <code>{bot} is ready.</code> |
| `persona.success.selected` | success | <code>{id} is selected.</code> |
| `remote.error.bad_name` | error | <code>  &#34;{name}&#34; won&#39;t do: {rule}</code> |
| `remote.error.create` | error | <code>{cli} couldn&#39;t create {name}.</code> |
| `remote.error.no_address` | error | <code>{cli} created {name} but didn&#39;t say its address. Add it with:</code> |
| `remote.error.set_url` | error | <code>Couldn&#39;t set the remote to {url}.</code> |
| `remote.error.unreachable` | error | <code>Can&#39;t reach the remote.</code> |
| `remote.error.unreachable_login` | error | <code>Can&#39;t reach the remote: git has no login for {https} here.</code> |
| `remote.error.unreachable_ssh` | error | <code>Can&#39;t reach it over ssh either.</code> |
| `remote.heading.none` | status | <code>Nothing is backed up yet: the repo has no remote.</code> |
| `remote.hint.create_in_terminal` | hint | <code>Run this in a terminal to create a private {forges} repository for it:</code> |
| `remote.hint.existing` | hint | <code>Use a repository you already have:</code> |
| `remote.hint.first_save` | hint | <code>Nothing committed yet. This commits and pushes:</code> |
| `remote.hint.have_key` | hint | <code>You have an ssh key, so the same repository can be reached over ssh:</code> |
| `remote.hint.https` | hint | <code>There&#39;s no ssh key in {ssh_dir}, so the remote uses {https}. To let git use {cli}&#39;s login:</code> |
| `remote.hint.log_in` | hint | <code>Log git in:</code> |
| `remote.hint.log_in_or_ssh` | hint | <code>Log git in to that host, or add an ssh key to {ssh_dir} and switch to the ssh address:</code> |
| `remote.hint.login` | hint | <code>  {cli} is installed but not logged in. To offer {forge} too: {cli}{auth_login}</code> |
| `remote.hint.or_existing` | hint | <code>Or use a repository you already have:</code> |
| `remote.hint.or_ssh` | hint | <code>Or add an ssh key to {ssh_dir} and switch to the ssh address:</code> |
| `remote.hint.pick` | hint | <code>  pick one of the letters above</code> |
| `remote.hint.rename` | hint | <code>This repo has a remote named {names}, but {bot} pushes to the one named {origin}. Rename it:</code> |
| `remote.hint.to_switch` | hint | <code>To switch:</code> |
| `remote.prompt.name` | prompt | <code>Name for the new private {forge} repository &#91;{suggested}&#93;:</code> |
| `remote.prompt.switch` | prompt | <code>Switch to it? {choice}</code> |
| `remote.prompt.url` | prompt | <code>URL:</code> |
| `remote.status.kept_https` | status | <code>Kept {https}. To switch later:</code> |
| `remote.status.not_now` | status | <code>Not now. When you&#39;re ready:</code> |
| `remote.status.up_to_date` | status | <code>Already backed up: the remote has every commit.</code> |
| `remote.success.created` | success | <code>created {name}, a private {forge} repository</code> |
| `remote.success.pushed` | success | <code>pushed the repo is backed up</code> |
| `remote.success.reachable` | success | <code>reachable {url}</code> |
| `remote.success.remote` | success | <code>remote {url}</code> |
| `remote.warning.waiting` | warning | <code>The remote has new commits. Commit or discard the changes here, then:</code> |
| `save.error.diverged` | error | <code>This machine and the remote changed the same files. Resolve it by hand:<br>  {git_prefix}{repo_abs}{rebase}</code> |
| `save.error.fast_forward` | error | <code>Could not fast-forward to the remote.</code> |
| `save.error.git_add` | error | <code>{git_add} failed.</code> |
| `save.error.push` | error | <code>Push failed.</code> |
| `save.error.rebase_stuck` | error | <code>This machine and the remote changed the same files, and git could not undo its rebase: {repo_abs} is still in the middle of one. Resolve the conflicts and run {continue_cmd}, or give up on it with:<br>  {git_prefix}{repo_abs}{abort}</code> |
| `save.error.refused` | error | <code>Commit refused: {reason}</code> |
| `save.status.nothing` | status | <code>Nothing new to commit.</code> |
| `save.success.committed` | success | <code>committed {n:# file&#124;# files}</code> |
| `save.success.pulled` | success | <code>pulled {n:# commit&#124;# commits}</code> |
| `save.success.pushed` | success | <code>pushed {n:# commit&#124;# commits}</code> |
| `save.success.sealed` | success | <code>sealed {n:# secret&#124;# secrets}</code> |
| `save.warning.unreachable` | warning | <code>Couldn&#39;t reach the remote, so nothing was pushed. To see why:</code> |
| `save.warning.waiting` | warning | <code>The remote has new commits. Commit or discard the changes here, then save again.</code> |
| `secrets.error.cancelled` | error | <code>Stopped at the passphrase prompt; nothing changed.</code> |
| `secrets.error.decrypt` | error | <code>Could not decrypt {private}{name}{age}.</code> |
| `secrets.error.empty_recipients` | error | <code>{recipients} has no public key in it. Restore it from the repo&#39;s history.</code> |
| `secrets.error.encrypt` | error | <code>Could not encrypt {private}{name}.</code> |
| `secrets.error.encrypt_key` | error | <code>Encrypting the new key failed; nothing was saved.</code> |
| `secrets.error.identity_unreadable` | error | <code>{identity} is not a passphrase-encrypted age key. Restore it from the repo&#39;s history.</code> |
| `secrets.error.keys_exist` | error | <code>Keys already exist ({identity}, {recipients}). Delete both first to start over.</code> |
| `secrets.error.no_identity` | error | <code>No {identity} yet. Create keys first:</code> |
| `secrets.error.no_recipients` | error | <code>No {recipients} yet. Create keys first:</code> |
| `secrets.error.no_terminal` | error | <code>There is no terminal to ask for the passphrase on. Run this in a terminal.</code> |
| `secrets.error.passphrase` | error | <code>Could not decrypt {identity} (wrong passphrase?)</code> |
| `secrets.error.reserved` | error | <code>Refusing to write over {path}: it is not a secret.</code> |
| `secrets.error.save_gitignore` | error | <code>Writing {path} failed, so nothing was set up: without it, git would take the plaintext secrets in {private}.</code> |
| `secrets.error.save_key` | error | <code>Saving the new key to {path} failed; nothing was saved.</code> |
| `secrets.error.unsupported_ssh_key` | error | <code>Line {line} of {recipients} holds a key age cannot encrypt to ({key_type}). Use an {ed25519} key, or an {rsa} key of at least {bits}; homebot never seals to fewer keys than the file lists.</code> |
| `setup.error.clone_auth_gh` | error | <code>git couldn&#39;t sign in to GitHub to clone {url}. Sign in with the GitHub CLI, which sets git up to use that login, then run {setup} again:<br>  {command}</code> |
| `setup.error.clone_auth_key` | error | <code>git couldn&#39;t sign in to {host} to clone {url}. Add an ssh key to your account there, then run {setup} again.</code> |
| `setup.error.clone_failed` | error | <code>Couldn&#39;t clone {url}. Check the address and that you can reach it, then run {setup} again.</code> |
| `setup.error.git_too_old` | error | <code>Your git is {found}, and homebot needs {minimum} or newer: its commit guard relies on a git command that older versions lack, so every save would be refused. Update git, then run {command} again.</code> |
| `setup.error.git_too_old_brew` | error | <code>Your git is {found}, and homebot needs {minimum} or newer: its commit guard relies on a git command that older versions lack, so every save would be refused. Install a newer one with Homebrew, then run {setup} again:<br>  {command}</code> |
| `setup.error.home_taken` | error | <code>{path} is there but holds no {config}, so it isn&#39;t a home repo and setup won&#39;t touch it. Move it aside, or set {env} to another folder, then run {setup} again:<br>  {command}</code> |
| `setup.error.install_failed` | error | <code>{command} failed. Install {tools} yourself, then run {setup} again.</code> |
| `setup.error.name_syntax` | error | <code>{flag} {name} must be {rule}</code> |
| `setup.error.no_terminal` | error | <code>No home repo yet, and setup starts or clones one only in a terminal. Run it in one:<br>  {setup}<br>Or clone yours by hand:<br>  {clone} {path}</code> |
| `setup.error.not_a_home_repo` | error | <code>{url} has no {config}, so it isn&#39;t a home repo. It is in {path} now: move it aside, then run {setup} again.</code> |
| `setup.error.persona_syntax` | error | <code>{flag} {persona} must {rule}</code> |
| `setup.error.remove_flags` | error | <code>{remove_flag} cannot be combined with {name_flag} or {persona_flag}</code> |
| `setup.error.root_relative` | error | <code>{env} is &#34;{value}&#34;, which is not a full path, so homebot would find the repo only from this folder. Set it to a full path, or unset it, then run {setup} again.</code> |
| `setup.error.tools_missing` | error | <code>Missing: {tools}. The commit guard needs it to check every commit. Install it, then run {setup} again:<br>  {command}</code> |
| `setup.error.xdg_relative` | error | <code>{name} is &#34;{value}&#34;, which is not a full path, so the background helper can&#39;t use it. Set it to a full path, or unset it, then run {command} again.</code> |
| `setup.hint.keys` | hint | <code>Secrets aren&#39;t set up yet. To keep passwords and tokens in the repo, encrypted, create the keys:</code> |
| `setup.hint.name_path` | hint | <code>Add {bin} to your {path_env} in {file} to type {name}; homebot works now.</code> |
| `setup.hint.xdg` | hint | <code>The helper keeps the {xdg_glob} folders it was set up with. After changing {config_env}, {data_env}, {state_env} or {cache_env}, run {self}{setup} again.</code> |
| `setup.hint.zshrc` | hint | <code>Add this to your {zshrc} so new terminals tell you when something needs you:</code> |
| `setup.prompt.backup` | prompt | <code>{n:# file is&#124;# files are} in the way of links. Move them aside (nothing is deleted) and link? {yn}</code> |
| `setup.prompt.change_name` | prompt | <code>This repo calls it {repo} on every machine. Change the shared name to {flag}? {choice}</code> |
| `setup.prompt.install_tools` | prompt | <code>Missing: {tools}. Install now with {command}? {yn}</code> |
| `setup.prompt.keys` | prompt | <code>Keep passwords and tokens in the repo, encrypted? This creates your keys. {yn}</code> |
| `setup.prompt.name` | prompt | <code>Give your homebot a command name? For example {example} ({enter} keeps homebot):</code> |
| `setup.prompt.name_path` | prompt | <code>Put {bin} on your {path_env} in {file} so the name works? {choice}</code> |
| `setup.prompt.save` | prompt | <code>Commit and push now? {yn}</code> |
| `setup.prompt.save_local` | prompt | <code>Commit now? {yn}</code> |
| `setup.prompt.triage` | prompt | <code>{n:# thing&#124;# things} in your home could be tracked. Sort them now, one key each? {yn}</code> |
| `setup.prompt.unpack` | prompt | <code>Run {n:this step&#124;these # steps}? {yn}</code> |
| `setup.prompt.url` | prompt | <code>URL of your home repo, or {example} on GitHub:</code> |
| `setup.prompt.zshrc` | prompt | <code>Add it to {file} now? {yn}</code> |
| `setup.status.changed_here` | status | <code>Changed on this machine:</code> |
| `setup.status.cloning` | status | <code>Cloning {url} into {path}...</code> |
| `setup.status.guard_present` | status | <code>Commit guard already installed.</code> |
| `setup.status.name_blocked` | status | <code>  no name link for {name} was made here</code> |
| `setup.status.name_link` | status | <code>  name link: {bin}{name} points to {binary}</code> |
| `setup.status.name_uncommitted` | status | <code>The new name is in {config} but stays uncommitted with your other work; your next save carries it.</code> |
| `setup.status.no_repo` | status | <code>No home repo yet.</code> |
| `setup.status.open_shell` | status | <code>From now on, type {name}. Open a new terminal first; on some Linux systems, log out and back in.</code> |
| `setup.status.path_added` | status | <code>Added {bin} to {file}.</code> |
| `setup.status.path_line` | status | <code>  added the {path_env} startup line</code> |
| `setup.status.persona` | status | <code>Persona in this repo: {persona}</code> |
| `setup.status.removed` | status | <code>Background helper removed.</code> |
| `setup.status.repo_name` | status | <code>This repo calls your homebot {name}.</code> |
| `setup.status.repo_name_wins` | status | <code>This repo calls it {repo}; {name_flag} {flag} was ignored without a terminal.</code> |
| `setup.status.zsh_only` | status | <code>Your shell is {shell}. The startup line and the prompt count are {zsh}-only for now.</code> |
| `setup.status.zshrc_in_repo` | status | <code>That file links into your home repo, so {path} changed there.</code> |
| `setup.status.zshrc_linked` | status | <code>{file} is a link to {target}, and setup writes only through a link to a file in your home repo. Add the line there yourself.</code> |
| `setup.success.created` | success | <code>created {path}, a new home repo with a {profile} profile</code> |
| `setup.success.guard` | success | <code>installed commit guard ({hook_key})</code> |
| `setup.success.helper` | success | <code>installed {helper}</code> |
| `setup.success.zshrc` | success | <code>added the startup line to {file}</code> |
| `setup.warning.clone_failed` | warning | <code>Couldn&#39;t clone {url}.</code> |
| `setup.warning.name_blocked` | warning | <code>{name} cannot be used as a command here: {reason}. Homebot will keep working.</code> |
| `setup.warning.name_invalid` | warning | <code>{name} must be {rule}. Try another name.</code> |
| `setup.warning.no_profile` | warning | <code>No profile for this machine ({host}) yet, so nothing was unpacked. Name one in {config} under {hosts} or {default}, then:</code> |
| `setup.warning.no_repo` | warning | <code>{root} is not a git repo yet; skipped the commit guard.</code> |
| `setup.warning.path_link` | warning | <code>Cannot safely edit {file} (link target: {target}). Add {bin} to your {path_env} yourself.</code> |
| `tidy.did.committed` | status | <code>committed {n:# file&#124;# files}</code> |
| `tidy.did.exported` | status | <code>refreshed {outs}</code> |
| `tidy.did.guard` | status | <code>installed the commit guard</code> |
| `tidy.did.linked` | status | <code>fixed {n:# link&#124;# links}</code> |
| `tidy.did.name_off` | status | <code>removed this machine&#39;s name link</code> |
| `tidy.did.named` | status | <code>kept this machine&#39;s name link in sync</code> |
| `tidy.did.pulled` | status | <code>pulled {n:# commit&#124;# commits}</code> |
| `tidy.did.pushed` | status | <code>pushed {n:# commit&#124;# commits}</code> |
| `tidy.did.sealed` | status | <code>sealed {n:# secret&#124;# secrets}</code> |
| `tidy.error.autosave_no_guard` | error | <code>skipped, because the commit guard could not be installed this run (see the guard step)</code> |
| `tidy.error.lock` | error | <code>Could not lock {path}: {reason}. tidy takes a file lock so two runs never overlap, and the filesystem holding the state directory does not support it. Point {env} at a local disk.</code> |
| `tidy.status.inbox_count` | status | <code>{n:# thing&#124;# things} in the inbox.</code> |
| `tidy.status.locked` | status | <code>Another tidy is running.</code> |
| `tidy.status.nothing` | status | <code>Nothing to tidy.</code> |
| `triage.error.git_add` | error | <code>{git_add} {key} failed.</code> |
| `triage.error.ignore_list` | error | <code>{file}: {ignore} must be a list of paths</code> |
| `triage.error.line_break` | error | <code>{key} has a line break in its name, which a {gitignore} line can&#39;t hold. Nothing was written; rename it, or snooze it.</code> |
| `triage.error.old_file` | error | <code>homebot reads {decisions} now: rename {path} to {decisions}</code> |
| `triage.hint.header_suffix` | hint | <code> Each answer is saved right away; {quit} stops.</code> |
| `triage.hint.pick` | hint | <code>  pick one of the letters above</code> |
| `triage.status.header` | status | <code>{n} things to decide.</code> |
| `triage.success.gitignored` | success | <code>added /{key} to {gitignore}</code> |
| `triage.success.ignored` | success | <code>will never ask about {key} again</code> |
| `triage.success.none` | success | <code>Nothing to sort. Everything has a decision.</code> |
| `triage.success.packed` | success | <code>packed {key} into {repo_path}</code> |
| `triage.success.snoozed` | success | <code>snoozed {key} for {duration}</code> |
| `triage.success.staged` | success | <code>staged {key}</code> |
| `unlink.hint.helper` | hint | <code>The background helper links them again within the hour, as {self}{link} would. To keep a file unlinked, take it out of the repo.</code> |
| `unlink.status.removed` | status | <code>Removed {n:# link&#124;# links}. To put them back:</code> |
| `unlock.error.conflict` | error | <code>conflict {private}{name}: changed here and in the repo</code> |
| `unlock.hint.ours` | hint | <code>Keep this machine&#39;s versions:</code> |
| `unlock.hint.theirs` | hint | <code>Or take the repo&#39;s:</code> |
| `unlock.prompt.passphrase` | prompt | <code>Enter passphrase:</code> |
| `unlock.status.linked` | status | <code>linked {n:# secret&#124;# secrets}</code> |
| `unlock.success.unlocked` | success | <code>unlocked {n:# secret&#124;# secrets}</code> |
| `unlock.success.verified_suffix` | success | <code>, verified {n}</code> |
| `unpack.error.from_nan` | error | <code>{from_flag} takes a step number</code> |
| `unpack.error.from_range` | error | <code>{from_flag} must be between {first_step} and {n}</code> |
| `unpack.error.no_action` | error | <code>Step {i}: no action named &#34;{action}&#34;. See:</code> |
| `unpack.error.no_profile` | error | <code>Which profile? Pass one ({example}) or set {default_key} in {config_file}.</code> |
| `unpack.error.step_exit` | error | <code>Step {i} ({step}) exited with {code}.</code> |
| `unpack.error.step_failed` | error | <code>Step {i} failed: {message}</code> |
| `unpack.hint.resume` | hint | <code>Fix it, then resume:</code> |
| `unpack.status.header` | status | <code>Unpacking {profile} on {host} ({os}): {n} steps</code> |
| `unpack.status.header_dry_run` | status | <code>Would unpack {profile} on {host} ({os}): {n} steps</code> |
| `unpack.success.done` | success | <code>Unpacked {profile}.</code> |
<!-- gendocs:end -->
