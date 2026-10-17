---
title: "homebot.yml"
description: "Every key in homebot.yml, with defaults."
---

# homebot.yml

At the repo root. Every key is optional; unknown keys are errors, so typos
don't fail silently. `ai` is reserved for a later version: it is accepted and
ignored. Must be `.yml`; a `homebot.yaml` gets a rename hint.

`persona` chooses homebot's voice. `name` chooses an optional command name;
the two settings are independent. See [Personas](/personas/) and
[Naming your homebot](/naming/). See [Persona files](/reference/personas/)
for local voices. Before adding `persona:` or `name:`,
update every machine that reads this repo to a build that has the `persona`
commands. An older build either rejects one or both keys, and so does its
background helper, or ignores `persona:` and stays neutral.

`update` chooses what the background helper does about new releases. Set it
only after every machine that reads this repo runs v0.1.0 or later and has
rerun `homebot setup`. See [Install and update](/install/#install-and-update-updates).

```yaml
profiles:
  default: workstation       # used when `homebot unpack` gets no name
  hosts:
    laptop: workstation      # per-hostname override
    nas: server

autosave:
  commit: daily              # off | hourly | daily
  push: true

exports:                     # refreshed by the helper about once a day
  - name: brew
    out: manifests/{os}/Brewfile
    run: brew bundle dump --force --file={out}   # {out} in run: the command writes the file
  - name: apt
    on: linux
    out: manifests/linux/apt.txt
    run: apt-mark showmanual | sort              # no {out}: stdout is saved
    profiles: [workstation]  # optional: only hosts whose default profile is listed
  - name: apt-repos
    on: linux
    out: manifests/linux/apt-repos               # a folder: the command made one at {out}
    run: homebot action apt-repos --export {out}
  - name: apt                # same name, another profile: its own list
    on: linux
    out: manifests/server/apt.txt
    run: apt-mark showmanual | sort
    profiles: [server]

triage:
  watch: ["~/.config/*", "~/.*"]   # entries end in /* or /.*

limits:
  maxFileMB: 50              # commits with bigger files are refused

update: off                   # off | notify | auto; anything else loads as off, with a warning
persona: neatpik              # optional; none selects neutral wording
name: homebot                 # optional; homebot means no other command name
```

## Keys

| Key | Default | Notes |
|---|---|---|
| `profiles.default` | none | profile `unpack` uses with no argument |
| `profiles.hosts.<hostname>` | none | wins over `default` |
| `autosave.commit` | `off` | `hourly` = at most every 50 min, `daily` = every 20 h. This repo: `daily` |
| `autosave.push` | `false` | push after each sync. This repo: `true` |
| `exports` | none | this repo: `brew`, `apt-repos`, `apt`, `flatpak` for `workstation` hosts; `apt-repos` and `apt` into `manifests/server/` for `server` hosts |
| `exports[].name` | | `homebot export <name>`. Two exports may share a name when no machine can run both: their `profiles` (or `on`) don't overlap. `homebot export apt` then runs this machine's one |
| `exports[].out` | | `{os}`, `{host}`, `{arch}` filled in. A file, or a folder when the command writes one at `{out}`; a folder is compared and replaced whole. Must stay inside the repo, also through symlinks and once the placeholders are filled in. Must not be, in any case, the repo itself, `manifests/` itself, `private/`, or anything homebot or git own: `autolink/` and its layers, `manifests/profiles/`, `manifests/actions/`, `furniture/`, `homebot/`, `.git/`, `homebot.yml`, `decisions.yml`, `.gitignore` or `.gitattributes`, or a path inside one of them. `manifests/<name>` is fine. An export never deletes a folder to write a file in its place, or a file to write a folder; it fails and says so. A symlink at `out` is replaced by the export, not followed. |
| `exports[].run` | | skipped (not failed) when the tool isn't installed. Runs from the repo root; `homebot` works in it even when it isn't on `PATH` |
| `exports[].on` | all | `linux` or `macos` |
| `exports[].profiles` | all | limit to hosts on these profiles |
| `triage.watch` | `~/.config/*`, `~/.*` | where new things are looked for |
| `limits.maxFileMB` | `50` | a number above 0 |
| `update` | `off` | `off`, `notify` or `auto`: how the helper treats new releases. Any other value (a typo, a bool, a list, a map) loads as `off` and prints a warning on every command that reads `homebot.yml`, naming the file and line; the warning goes to the debug log instead under `homebot tidy --quiet`. An empty `update:` means `off` with no warning. `off` makes no release requests in the background. `notify` lets the hourly helper check for a newer release about once a day and puts an `update-available` item in the inbox when there is one. `auto` does the same check and upgrades on its own, but only Homebrew cask and install-script installs, and only once a release has been out for 48 hours; other install methods get the inbox item with the command to run. `homebot version --check` says whether this copy is current and, when it isn't, what `auto` would do on this machine. See [Updates](/reference/inbox/#inbox-helper-updates). |
| `persona` | `neatpik` | A built-in or local persona ID; `none` turns the voice off. The ID is a lowercase ASCII letter followed by lowercase letters, digits or hyphens, at most 32 characters. An ID with invalid syntax, or a value that is not a string, is a config error that stops every command that reads `homebot.yml`, `homebot persona use`, `persona list` and a bare `persona validate` included (the last two still print what they can, then report it and exit 1), so fix the line by hand; a well-formed ID whose file is missing or unusable falls back to embedded Neatpik and adds `persona_broken` to the inbox. `HOMEBOT_PERSONA` takes precedence when nonempty. |
| `name` | `homebot` | The command name for this home. `homebot` disables the extra name; other values use the persona ID syntax, with at least two characters, and are checked for reserved names and conflicts on this machine. Use `homebot name` to manage it. |

## Voice and display environment

| Variable | Effect |
|---|---|
| `HOMEBOT_PERSONA` | Nonempty ID overrides `persona:` for the process. `none` means neutral; an empty value is unset. A bad choice falls back to Neatpik and warns once on a terminal. |
| `HOMEBOT_PLAIN=1` | Neutral wording in the process, including the nudge and notifications it writes. Any value other than empty or `0` counts. `homebot --plain <command>` applies it to one run. |
| `HOMEBOT_THEME=light` or `dark` | Selects the palette; an invalid value is ignored. It does not change wording. |
| `NO_COLOR=1` | Suppresses colour but keeps the selected voice. Any nonempty value has the same effect. |

::: callout warning "`link:` is gone"
Older versions had a `link:` section listing folders to keep real. Now the
folder's name says it: wrap it in `()`. A `link:` key is an error that tells
you so. See [Linking rules](/linking/).
:::

## decisions.yml

Written by `homebot triage` when you answer "ignore". Hand edits are fine.
Globs work.

```yaml
ignore:
  - "~/.cache"
  - "~/.config/google-chrome*"
  - "~/.zcompdump*"
```

Snoozes are per machine and live in `$XDG_STATE_HOME/homebot/snooze.json`, not here.

## Generated key index

The index below follows the loader's `config.Keys` table. It records each
key's type, default, allowed values, description and release.

<!-- gendocs:start config -->
<!-- Generated by go run ./tools/gendocs docs; do not edit this section. -->

| Path | Type | Default | Values | Description | Since |
|---|---|---|---|---|---|
| `profiles` | object |  |  | Choose which profile each machine unpacks. | 0.1.0 |
| `profiles.default` | string |  |  | Profile used when a host has no entry. | 0.1.0 |
| `profiles.hosts` | map | {} |  | Map each host name to its profile. | 0.1.0 |
| `triage` | object |  |  | Choose files the inbox offers to pack. | 0.1.0 |
| `triage.watch` | list | [~/.config/*, ~/.*] |  | Home-relative glob patterns to watch. | 0.1.0 |
| `autosave` | object |  |  | Set the background save cadence and push behavior. | 0.1.0 |
| `autosave.commit` | string | off | off, hourly, daily | Commit automatically: off, hourly or daily. | 0.1.0 |
| `autosave.push` | bool | false |  | Push background commits when enabled. | 0.1.0 |
| `exports` | list | [] |  | Programs that write files into the home repo. | 0.1.0 |
| `exports[].name` | string |  |  | Name used to select an export. | 0.1.0 |
| `exports[].on` | list | [linux, macos] | linux, macos | Operating systems that run this export. | 0.1.0 |
| `exports[].out` | string |  |  | Repo-relative output path for the export. | 0.1.0 |
| `exports[].run` | string |  |  | Shell command that produces the export. | 0.1.0 |
| `exports[].profiles` | list |  |  | Profiles allowed to run this export. | 0.1.0 |
| `limits` | object |  |  | Size limits enforced by the commit guard. | 0.1.0 |
| `limits.maxFileMB` | number | 50 |  | Maximum file size in megabytes. | 0.1.0 |
| `name` | string |  |  | Optional command name for this homebot. | 0.1.0 |
| `persona` | string |  |  | Optional persona id, or none for a neutral voice. | 0.1.0 |
| `update` | string | off | off, notify, auto | Release behavior: off, notify or auto. Automatic updates use a signed build after a soak period. | 0.1.0 |
| `ai` | any |  |  | Reserved for a future release; currently ignored. | future |
<!-- gendocs:end -->
