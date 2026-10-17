---
title: "Hacking on homebot"
description: "Build, test and change the tool."
---

# Hacking on homebot

Go 1.26 or newer to build (releases use Go 1.27.2). Runtime tools are `git`
and `gitleaks`; age encryption is built into homebot.
Lives in its own repo, separate from any home repo.

```sh
cd homebot
go build ./cmd/homebot    # ./homebot: one self-contained binary
go build -o ~/.local/bin/homebot ./cmd/homebot    # or build straight onto your PATH
go vet ./...
```

## The commit guard

The commit guard is the `homebot hook pre-commit` command. A shim at
`$XDG_DATA_HOME/homebot/hooks/pre-commit` runs whichever `homebot` it finds
first (`PATH`, then where `homebot setup` recorded it). The home repo's
`core.hooksPath` points to that data directory's `hooks` folder, so rebuild and
reinstall the binary for guard changes to take effect. The shim itself is
rewritten by `homebot setup`.

## Tests

Go and release tests run in containers, never on your own machine. Each test uses a
temporary sandbox rather than your home repo.

```sh
scripts/docker-test.sh --context NAME       # the whole gate: generate (no diff), build, vet,
                                            # go test, golangci-lint, shellcheck
scripts/docker-test.sh --context NAME go-test ./internal/cli -run TestExit -v
scripts/docker-test.sh --context NAME lint
scripts/docker-test.sh --context NAME generate   # go generate; changed files are copied back
scripts/docker-test.sh --context NAME update ./internal/msg -run TestCatalogSnapshot
scripts/release-test.sh --context NAME snapshot
scripts/release-test.sh --context NAME all
go run ./tools/gendocs completions -out /tmp/homebot-completions
go run ./tools/gendocs man -out /tmp/homebot-man -version 0.1.0 -date 2026-01-01T00:00:00Z
go run ./tools/gendocs docs -check
```

The scripts ship the worktree (including uncommitted changes) into fresh
containers. Neither script has a default host: both take `--context NAME`, a
Docker context (`docker context ls` lists yours) on a machine you control. The
README's "Running the tests" has the details. To regenerate the catalog after
editing `internal/msg/catalog.yaml`, run `scripts/docker-test.sh --context NAME generate` and
review the generated files. `gendocs docs` rewrites the generated reference
pages and schemas, and `gendocs docs -check` fails when they differ from the
command tree, config schema and message catalog.

The `testdata/parity` scripts hold the output of the earlier TypeScript
implementation, byte for byte except where a `# deliberate:` line says what
changed, and `testdata/script` the Go port's own
end-to-end scripts.

## Map

| Where | What |
|---|---|
| `cmd/homebot` | the binary: build info, signals, the end-to-end test harness |
| `internal/cli` | commands, argument checking, help pages, output |
| `internal/linker` | name rules, merge, fold, conflicts, layout errors, prune |
| `internal/pack` | where a `$HOME` path goes in the repo; moving it there |
| `internal/secrets` | age seal/unlock, per-machine hashes |
| `internal/guard` | pre-commit guard and its shim (`pre-commit.sh`) |
| `internal/inbox` | items, nudge cache, notifications |
| `internal/tidy` | the hourly chores |
| `internal/helper` | systemd timer / launchd agent |
| `internal/unpack`, `internal/actions`, `internal/profile` | profiles and actions; `internal/actions/scripts/*.sh` are the built-in `brew`, `apt-repos`, `apt`, `flatpak`, `fonts` |
| `internal/exports` | package snapshots |
| `internal/triage` | new-file questions, `decisions.yml` |
| `internal/save`, `internal/gitx`, `internal/remote` | commit, pull, push, the remote |
| `internal/shell` | `homebot.zsh`: nudge line, `HOMEBOT_COUNT`, Spaceship section |
| `internal/msg` | every message homebot prints, from `catalog.yaml` |
| `internal/persona` | persona files: built-ins in `builtin/<id>/persona.toml`, loading, validation, selection |
| `internal/voice` | renders a message in the active persona, or neutral for stored text; variant rotation |
| `tools/gendocs` | shell completions, the man page, the generated reference sections and the JSON Schemas in `docs/landing/schema/` |
| `tools/personabundle` | writes `internal/persona/fable/entries.json`, the list of voiced messages for persona authors |

## Messages and personas

Every message lives in `internal/msg/catalog.yaml`; code never writes English
of its own. After changing the catalog, run both generators and commit what
they change:

```sh
go generate ./internal/msg/...    # messages_gen.go: one typed accessor per entry
go generate ./internal/persona    # internal/persona/fable/entries.json, from the catalog and internal/persona/fable/many.txt
```

The gate's `generate` step fails when either leaves a diff, and
`TestBundleCurrent` fails on a stale `entries.json`. `internal/persona/fable/many.txt`
lists the message IDs whose persona lines need three to five variants.

- `persona: true` marks an entry a persona may reword. The catalog's
  `conventions.persona` says which entries get it; one the rule picks may stay
  fixed only with `persona: false` and a note starting `Not voiced:`. Help,
  commit messages, labels, choices, facts and the commit guard are never
  voiced.
- A voiced entry keeps every fact in a placeholder, a fixed placeholder
  (`value:`) or its `append`, so a persona line cannot change a path, count or
  command.
- `{self}` is the command word: the effective name (see `homebot name`) in
  printed output, `homebot` in stored text. `{bot}` is the command name when
  one is set and works, otherwise the persona's display name (`homebot` in the
  neutral voice).
- Text that is stored or sent away (commit messages, the journal, state, a
  forge repository's description) goes through `voice.Record`, which is always
  neutral. An ID that is only stored or sent away must not be
  `persona: true`: `TestRecordOnlyNotVoiced` fails when every direct use of a
  voiced ID is `voice.Record`, and the `storedOnly` list in
  `internal/msg/recordonly_test.go` guards the IDs that reach it indirectly.
- Text that is stored and also printed keeps both forms, and the voice is
  applied only when a line is printed. An error does it with
  `voice.Nest`, `NestWith`, `Attach` or `SayWith`: `Error()` is the neutral
  English to store, `voice.ErrorText` is the persona's. A linker `Op` keeps
  `Reason` (neutral, for anything stored) and `ReasonText` (the message not yet
  said, which `homebot link`, the inbox and the link action say in the voice).
  A failed tidy step keeps `tidy.Failure.Speech` beside the recorded message.
  A string placeholder takes plain text, so a fragment spliced into a voiced
  line (`layout.inner_error`'s inner reason, `layout.must_mark.every_layer`)
  stays English inside the persona's wording.

## Release checks

Nothing is released yet; these checks build throwaway snapshots and never
publish. `scripts/release-test.sh --context NAME MODE` runs a mode from
`scripts/release-test/modes/` in a Docker container on the host that context
names. `all` runs every mode except `cosign-matrix`, `packages` and
`brew-linux`. `packages` installs, upgrades and removes the `.deb` and `.rpm`
snapshots in fresh Debian, Ubuntu and Fedora containers; `brew-linux` installs
the generated cask with Linux Homebrew. Both need `--siblings` and the network,
so they run by name. The macOS cask and install-script checks run in the
manual `release-check` workflow on GitHub's macOS runners. CI's
`release-config` job runs `goreleaser check` and actionlint on every push and
pull request.

## The site branch

homebot.sh deploys from the `site` branch; `main` stays the only development
branch. The release workflow moves `site` to the tag after a final release
(`vX.Y.Z`, never a release candidate), by plain fast-forward: a `site` that is
ahead of the tag fails the job. The manual **Deploy site** workflow
(`.github/workflows/deploy-site.yml`, run from `main`) moves it to any commit on
`main` or any release tag, the latest final release by default, and can move it
back; the job summary shows the old and new commit.

## Conventions

- Errors the user can fix are `fail.User` errors and say **what to run**.
- Every chore in `tidy` must be unable to lose data. Anything else becomes an inbox item.
- Tests use `internal/testutil` to build a sandbox repo + `$HOME`; never the real ones.
- Config files are `.yml`. Old `.yaml` names get a rename hint, not silent support.
