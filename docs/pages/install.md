---
title: "Install and update"
description: "Install homebot, verify a release, and choose how updates work."
---

# Install and update

homebot v0.1.0 supports macOS and Linux. Run `homebot setup` after installing
it to configure the background helper and your home repo.

## What homebot needs

homebot needs git 2.40 or newer and `gitleaks`: the commit guard refuses every
commit when `gitleaks` is missing. `homebot setup` checks both before it
changes anything, and offers `brew install gitleaks` when Homebrew is on your
`PATH`. The Debian and RPM packages require git and recommend `gitleaks`; the
other install methods leave both to you. age encryption is built in, so there
is no `age` to install. These are optional: zsh (for the startup line and the
prompt count), `gh` or `glab` (`homebot remote` creates your backup repo with
one), and `fzf` and `lazygit` (nicer diffs and line merging in
`homebot cleanup`).

## Homebrew

```sh
brew install --cask hyperlab-sh/tap/homebot
homebot setup
```

The cask is for macOS only; on Linux use the installer script or a package.
The macOS release test exercises its installation, quarantine handling and
upgrade. Use the
fully qualified `hyperlab-sh/tap/homebot` name to select this tap's cask and its
trust path. `brew info --cask homebot` found no official cask with that name
on 2026-10-14; qualification still avoids ambiguity if one appears. Homebrew
6 and newer may require trust for a tap's cask; the fully qualified install
selects and trusts this cask. If you tap first, use
`brew tap hyperlab-sh/tap && brew trust --cask hyperlab-sh/tap/homebot` before
installing it.

The cask installs the zsh completion (`_homebot`) into Homebrew's
`site-functions` directory. zsh searches it only if Homebrew's `shellenv` runs
before `compinit` in your `.zshrc`; on Apple Silicon that line is
`eval "$(/opt/homebrew/bin/brew shellenv)"`. Homebrew's
[shell completion page](https://docs.brew.sh/Shell-Completion) has the details.
To check, run `echo ${_comps[homebot]:-missing}` in a new shell: `_homebot`
means `compinit` registered the completion, `missing` means it ran before
Homebrew's directory was on `$fpath`, or never ran.

The v0.1.0 macOS binary is unsigned and not notarized. The cask's install
hook clears the quarantine attribute from its installed binary. The release test checks the
attribute, including a cask without the hook as a negative control.

## Front door script

```sh
curl -fsSL https://homebot.sh/install | bash
```

The front door installs the latest release with the release installer below,
after installing Homebrew and the tools homebot needs, each with your OK, and
then starts `homebot setup`. Before it changes anything it downloads the
release's `install.sh` and `checksums.txt` from GitHub and checks the script's
sha256 against the checksums; with no release yet, or a mismatch, it stops and
changes nothing. It needs `bash` and is the only installer that takes
`--persona ID` and `--name NAME`, so you can pick the robot and the command
name up front, and `--version vX.Y.Z` installs that release instead of the
latest:

```sh
curl -fsSL https://homebot.sh/install | bash -s -- --persona ID --name NAME
```

What it does, step by step, is on the [docs home page](/). Read the script at
[homebot.sh/install](https://homebot.sh/install) before piping it to a shell.
You can skip Homebrew and the rest and use the release installer below, then
pass `--persona` and `--name` to `homebot setup` yourself.

## Release installer script

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/hyperlab-sh/homebot/releases/latest/download/install.sh | sh
```

The script is a release asset, covered by the release's signed checksums. To
pick a release or install folder, pass flags after `sh -s --`:

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/hyperlab-sh/homebot/releases/latest/download/install.sh | sh -s -- --version v0.1.0 --bin-dir "$HOME/.local/bin"
```

The script accepts `--version vX.Y.Z`, `--bin-dir DIR`, `--require-signature`
and `--max-time SECONDS`. It installs into `~/.local/bin` by default, checks
the release checksum, runs the new binary's version command before replacing
an older copy, and records an install receipt. With cosign 2.4.3 or newer it
also verifies the signature; otherwise it warns that the signature was not
checked. Pass `--require-signature` to refuse installation without that
verification. Run `homebot setup` when it
finishes. Run the script as your own user, never through `sudo`: it refuses a
sudo caller on macOS and Linux. A root shell with its own root-owned `HOME`
(for example in a container) installs into root's home.

The one-line method trusts GitHub and the network path to it to send the
intended script. The front door script above is served by homebot.sh instead,
so using it also trusts that server and Cloudflare. It checks the release
installer against the release's `checksums.txt`, but both come from the same
place, and it does not check the signature or pass `--require-signature`. For
high assurance, use the GitHub URL, not the front door: download the asset,
verify it as below, then run it with signature checking required:

```sh
sh install.sh --require-signature
```

## Debian, Ubuntu and Fedora packages

Get the `.deb` or `.rpm` for your architecture from the
[release page](https://github.com/hyperlab-sh/homebot/releases/latest). For
v0.1.0, the names are `homebot_0.1.0_amd64.deb` or
`homebot_0.1.0_arm64.deb`, and `homebot-0.1.0-1.x86_64.rpm` or
`homebot-0.1.0-1.aarch64.rpm`. Verify the download as below, then install
the matching file:

```sh
sudo apt install ./homebot_0.1.0_amd64.deb
# or, on Fedora:
sudo dnf install ./homebot-0.1.0-1.x86_64.rpm
```

These package managers need root for installation and upgrades. The
background helper can report an available version but will not run `sudo`.

## Build with Go

With Go 1.26 or newer:

```sh
go install github.com/hyperlab-sh/homebot/cmd/homebot@latest
```

Put `$(go env GOPATH)/bin` on your `PATH` (or `$GOBIN` if you set it), then
run `homebot setup`. A Go
install stays under your control: `update: auto` will not replace it.

Script and Go installs do not carry the packaged completion files. Run
`homebot completion bash`, `homebot completion zsh` or
`homebot completion fish` to print the script for your shell. For zsh, load it
after `compinit` and before the `eval "$(homebot shell zsh)"` line in your
`.zshrc`:

```zsh
autoload -Uz compinit && compinit
source <(homebot completion zsh)
eval "$(homebot shell zsh)"
```

See [Completion](/reference/inbox/#inbox-helper-shell-integration-completion) for why the order matters.

## Verify a release download

The [release page](https://github.com/hyperlab-sh/homebot/releases/latest)
provides `checksums.txt` and `checksums.txt.sigstore.json`. Download those
and the asset from the same release. Verify the checksum file's keyless
Sigstore bundle, bound to this repository's tagged release workflow:

```sh
# cosign 2.4.3 through 2.x
cosign verify-blob --new-bundle-format \
  --certificate-identity https://github.com/hyperlab-sh/homebot/.github/workflows/release.yml@refs/tags/v0.1.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --bundle checksums.txt.sigstore.json checksums.txt
```

With cosign 3.x, omit `--new-bundle-format`. The oldest version verified
against the release bundle format is 2.4.3. Debian trixie offers cosign
2.5.0 and Ubuntu 26.04 offers 2.6.2 in the release matrix check; the Fedora
44 package check found no
cosign package, so use an independently verified cosign binary there. After
verifying the bundle, compare your asset's SHA-256 with its **exact filename**
line in `checksums.txt`. For example, for the downloaded installer:

```sh
shasum -a 256 install.sh
grep '  install.sh$' checksums.txt
```

The two digests must agree. On Linux, `sha256sum install.sh` works too.
This verifies the bytes you run; merely reading a moving branch's source
file does not verify a release asset.

## Updates

`homebot version --check` checks for a release when you ask, and says so when
this copy is already current. The optional
`update:` key in `homebot.yml` controls what the background helper does:

| Value | Background behavior |
|---|---|
| `off` (default) | Makes no automatic release check. |
| `notify` | Checks at most daily (six hours after a failed check) and adds an inbox item for a newer release. |
| `auto` | Checks at most daily (six hours after a failed check); can upgrade a script install or this Homebrew cask after a release has been seen for 48 hours. Other methods get an inbox item with a manual command. |

An unknown value warns and acts as `off`. Automatic updates run only in the
background helper, never as part of an interactive `homebot tidy --notify`.
The Homebrew cask relies on the tap's SHA-256 checksums. A script install
requires cosign 2.4.3 or newer on the helper's recorded `PATH`; it verifies
the release signature and checksum before installing. If cosign is missing,
too old, or verification fails, it leaves the binary untouched and raises
an inbox item, and that item stays until the installed version catches up, even
if a later check can't reach GitHub. Debian, RPM, Homebrew formula, `go install`
and unknown installs are manual.

The helper captures your proxy and CA environment (`HTTPS_PROXY`,
`HTTP_PROXY`, `NO_PROXY`, their lowercase forms, `SSL_CERT_FILE` and
`SSL_CERT_DIR`) at setup. Run `homebot setup` again after changing those
values or installing cosign in a new PATH directory. If your home repo syncs
across machines, set `update:` only after **every** machine runs v0.1.0 or
later and has rerun `homebot setup`.

## Remove homebot

Run `homebot setup --remove` before removing its binary; this uninstalls the
background helper and removes a command-name link homebot owns. Then use
`brew uninstall --cask hyperlab-sh/tap/homebot`, `sudo apt remove homebot`,
or `sudo dnf remove homebot` for those install methods. For a script install,
remove the installed `homebot` from your chosen `--bin-dir` and its
`install-receipt` in `$XDG_DATA_HOME/homebot` (or
`~/.local/share/homebot`). A Go install's binary is in `$GOBIN` when set,
otherwise `$(go env GOPATH)/bin/homebot`. Your home repo and its files are separate;
removing the program does not delete them.
