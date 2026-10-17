---
title: "Personas"
description: "Choose how homebot speaks, or write a voice of your own."
---

# Personas

A persona gives homebot a name, emoji, colours and alternate wording for some
messages. Neatpik is the default. Run `homebot persona list` to see the built-in
and local choices, with the active one marked. `homebot persona show <id>` shows
its header, colours and four sample messages.

Eight personas are built in: HAL 9000, Johnny 5, Marvin, Miss Minute,
Neatpik, R2-D2, Rosie and WALL-E. Each carries its own lines. Neatpik rewords
a subset of the messages and leaves the rest in the catalog's English; the
other seven cover every message a persona can voice, in their own manner. A
message a persona has no line for falls back to Neatpik's, then to the
catalog's English. You can write persona files of your own too.

Here is each one telling you three things need you, the way the inbox prints
it under the header:

| Id | Emoji | Name | Sample line |
|---|---|---|---|
| `neatpik` | 🧹 | Neatpik | 3 things are waiting for you |
| `rosie` | 🧽 | Rosie | 3 chores waiting, hon. Sleeves up |
| `wall-e` | 🌱 | WALL-E | Ooh. 3 things. Found them for you |
| `marvin` | ☔️ | Marvin | 3 things need you, and this will all end in tears |
| `r2-d2` | 📡 | R2-D2 | Dweet-bwoo-wee! [3 things need you] |
| `johnny-5` | 🤖 | Johnny 5 | 3 things need you! Read it all twice. Wonderful! |
| `hal-9000` | 🔴 | HAL 9000 | 3 things need you, Dave. I'm afraid I can't do this for you |
| `miss-minute` | ⏰️ | Miss Minute | 3 things need you, sugar, and the clock's a-tickin' |

A persona cannot change facts, paths, counts, commands, fixes or the commit
guard. Help, commit messages, the journal and anything stored in your home
repo keep their neutral wording. Ordinary piped command output also stays
neutral; `persona show` and `persona preview` deliberately display their named
voice even through a pipe. On a terminal, the active persona speaks where a
message allows it; the shell nudge and desktop notifications use that voice
even though they run in the background. [The file format and checks](/reference/personas/) explain what a
voice is allowed to change.

## Choose a voice

From inside your home repo:

```sh
homebot persona use neatpik
homebot persona use none
```

`use` checks the choice and writes `persona:` in `homebot.yml`, preserving
comments. `none` selects the neutral voice, including for the nudge. The next
`homebot save` carries the config change; `use` does not make a commit. It
also rewrites the shell nudge, prompt symbol and count at once. When
`HOMEBOT_PERSONA` names another persona in that shell, `use` says the variable
still wins there, and the caches it rewrites take that persona's voice.
`homebot setup --persona <id>` also writes `persona:`, and it judges the choice
the way `use` does: the persona must exist and load, and a refusal says so
before setup changes anything on the machine. A persona file that is lost or
broken later shows up as `persona_broken` in the inbox.

`HOMEBOT_PERSONA=<id>` overrides `persona:` in the current process. An empty
value counts as unset. `HOMEBOT_PERSONA=none` selects neutral without changing
the repo. An unusable selection falls back to Neatpik; a broken selection from
`homebot.yml` also becomes an inbox item. An unusable environment selection
warns once on a terminal and makes no shared inbox item.

`homebot --plain <command>` or `HOMEBOT_PLAIN=1` uses neutral wording for that
run, even in `persona show` and `persona preview`. `NO_COLOR=1` removes colours
but keeps the chosen voice. `HOMEBOT_THEME=light` or `dark` chooses a palette;
it does not change the words. [Giving homebot a command name](/naming/)
uses the separate `homebot name` command and `name:` key; a persona does not
create that name.

## Write your own

```sh
homebot persona new tester
homebot persona validate tester
homebot persona preview tester
homebot persona preview tester --event inbox.nudge --all-variants
homebot persona ids
```

`new` creates `$XDG_CONFIG_HOME/homebot/personas/tester.toml` and will not
overwrite a file. `--from <id>` copies an existing persona's name, emoji,
tagline, `extends`, `lang`, colours and lines into the new file, under the new
ID. It refuses a persona with an error in its chain. Without `--from`, the
scaffold lists voice message IDs as comments. `ids` shows each voice ID, its
English text and its placeholders. `validate` also accepts a `.toml` path;
`preview` shows light and dark output without sending a notification or
changing your repo. With `--event`, a slot the inbox fills with another message
or a count is filled the same way (the nudge's urgency, a notification's item
title and "+N more", an optional detail like "last unpacked"); any other slot
shows a generic sample such as `<profile>`.

If `homebot.yml` cannot be read, `persona list` and a bare `persona validate`
still list or check the personas, report the config error the way `use` does,
and exit 1. They mark no persona as the current one while the config is broken,
since which one the repo selects is not known. `HOMEBOT_PERSONA`, when set,
still decides.

Findings name a file in the config directory relative to it
(`personas/tester.toml`), and any other file by its full path.

Persona files live in each machine's config directory. To take yours to
another machine, keep it in the home repo with `homebot pack` and let homebot
link it there. A machine that sees `persona: tester` before receiving
`tester.toml` speaks as Neatpik in the terminal, keeps the shell nudge, the prompt
symbol and desktop notifications neutral (the prompt shows 🚚), and shows a
`persona_broken` inbox item; run
`homebot persona validate tester` on that machine to inspect the problem.

Before adding `persona:` to a home repo shared with other machines, update
**every machine that reads it** to a homebot build that has the `persona`
commands. An older build either rejects the key, and so does its background
helper, or ignores it and stays neutral (`homebot · laptop · …`).
`HOMEBOT_PERSONA` set in one shell also affects the shared nudge and symbol
caches when a background refresh inherits that environment, so a shell
may show that voice after the setting is removed until the next refresh.
