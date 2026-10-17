---
title: "Secrets"
description: "age-encrypted secrets in private/, sealed automatically, and a commit guard that refuses leaks."
---

# Secrets

`private/` works exactly like `autolink/` (same layers, same
[linking rules](/linking/), except it never folds), but git only ever sees
encrypted `*.age` files.

| File | Committed? | What it is |
|---|---|---|
| `private/common/netrc` | **never** | plaintext, linked to `~/.netrc` |
| `private/common/netrc.age` | yes | the same, encrypted with age |
| `private/recipients.txt` | yes | public key: anything can **seal** without a passphrase |
| `private/identity.age` | yes | private key, encrypted with **your passphrase**; needed only to **unlock** |

::: callout danger "Lose the passphrase, lose the secrets"
There is no recovery. Keep the passphrase in the password manager.
:::

## Commands

```sh
homebot keys init                    # once, ever: make the key pair, pick a passphrase
homebot pack ~/.netrc --private      # move a secret in, link it back, seal it
homebot unlock                       # decrypt everything (passphrase asked once)
homebot lock                         # seal what changed; --wipe also deletes plaintext
```

Sealing is automatic: the commit guard and the hourly helper encrypt changed
plaintext on their own, because sealing needs only the public key.

`pack --private` links the file back before it seals it, so `~/.netrc` keeps
working even when sealing fails; it seals even when the link fails, and
reports what failed.

You do not need a separate `age` executable; homebot has age encryption built
in. Edit the plaintext that `~/.netrc` links to; `private/.gitignore` keeps
git from ever seeing it. `homebot keys init`,
`homebot pack --private` and `homebot save` write that file when `private/`
has none, and never change one that exists. The next commit, whether
`homebot save`, the autosave or a plain `git commit`, seals the plaintext into
the `.age` file next to it and commits that. The guard's refusal of plaintext
(step 2 below) is the safety net for a file force-added with `git add -f`, or
let through by a `.gitignore` that was edited.

## Two machines, one secret

Each machine keeps hashes of what it last sealed or unlocked, so homebot knows
**where** a change came from.

| State | Means | Fix |
|---|---|---|
| `new` / `modified` | plaintext changed here | sealed automatically |
| `locked` | `.age` exists, no plaintext here yet | `homebot unlock` |
| `upstream` | `.age` changed on another machine | `homebot unlock` |
| `conflict` | changed here **and** elsewhere | `homebot unlock --theirs` or `homebot lock --ours` |

A pull never silently overwrites a local edit; conflicts land in the inbox as urgent.

## The commit guard

`homebot setup` writes a small shell shim to `$XDG_DATA_HOME/homebot/hooks/pre-commit` (`~/.local/share/homebot/hooks/pre-commit` by default) and sets the home repo's local `core.hooksPath` to that folder. The shim runs `homebot hook pre-commit`. Before every commit the guard:

::: steps
1. Seals changed secrets and stages the `.age` files.
2. **Refuses** plaintext from `private/`, even if force-added (`git add -f`). When git does not ignore the file, so every save would stage it again, the refusal names the `.gitignore` that lets it through (or `private/.gitignore` when nothing ignores it) and says to fix that file, beside the `git rm --cached` command.
3. **Refuses** a `*.age` file in `private/` that does not hold age-encrypted data.
4. **Refuses** `private` staged as a symlink, a file or a submodule, and a commit that deletes every sealed file in `private/` and leaves none. Both happen when `private/` is moved, emptied or replaced by a link, and pushing them would take every secret away from your other machines.
5. **Refuses** any file over `limits.maxFileMB` (50 MB; GitHub hard-fails at 100).
6. **Refuses** anything `gitleaks` flags in the staged changes.
7. **Refuses** everything if `gitleaks` isn't installed. No scanner, no commit.
:::

The guard compares `private`, `.age` and the names `private/` may commit
(`README.md`, `.gitignore`, `.gitkeep`, `recipients.txt`, only directly in
`private/`) without regard to case, as git does on macOS: there `x.AGE` is a
sealed file, and must hold age data.

`homebot save` runs the same checks itself before it commits, so a save never
depends on the hook being installed: it needs `gitleaks` even on a machine
where `homebot setup` never ran. If the hourly helper cannot install the
guard, it skips that run's autosave.

The shim looks for the `homebot` binary on `PATH`, then where it was when `homebot setup` ran, then in `/opt/homebrew/bin`, `/usr/local/bin` and `~/.local/bin`, so a GUI git client with a minimal `PATH` still finds it. If it finds none, it refuses the commit. After the guard passes, the shim runs the repo's own `.git/hooks/pre-commit` if there is one; `core.hooksPath` would otherwise skip it. Other hooks in `.git/hooks` do not run.

If the guard does not run, check `git config core.hooksPath` in the home repo and that the shim exists and is executable. `homebot setup` repairs both, and the inbox flags a guard that is not installed.

A refused automatic commit becomes an urgent inbox item with the reason.

::: callout warning "Don't bypass it"
`git commit --no-verify` works, and is exactly how secrets end up on GitHub.
If the guard is wrong, fix the guard.

The one exception is removing every secret from the repo on purpose. The
guard refuses that commit, and homebot has no flag for it: run
`git commit --no-verify` yourself, in the home repo. The refusal says so.
:::
