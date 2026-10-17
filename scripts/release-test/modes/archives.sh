#!/usr/bin/env bash
# Check the release asset contract, archive payloads, binary stamps and metadata.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

rt_require_uid 1000
rt_need_snapshots
cd "$RT_SRC"
want_layout=$(mktemp)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp" "$want_layout"' EXIT
cat >"$want_layout" <<'LAYOUT'
LICENSE
README.md
THIRD_PARTY_NOTICES
completions/_homebot
completions/homebot.bash
completions/homebot.fish
homebot
man/homebot.1
LAYOUT

for version in 0.0.1 0.0.2; do
  dist=$RT_VOL/snapshots/v$version
  [[ -d $dist ]] || rt_die "missing v$version snapshot"
  for os in linux darwin; do
    for arch in amd64 arm64; do
      archive=$dist/homebot_${version}_${os}_${arch}.tar.gz
      [[ -s $archive ]] || rt_die "missing $archive"
      tar -tzf "$archive" | sed 's|^\./||' | LC_ALL=C sort >"$tmp/layout"
      diff -u "$want_layout" "$tmp/layout" || rt_die "archive layout differs: $archive"
      mkdir -p "$tmp/unpack"
      tar -xzf "$archive" -C "$tmp/unpack"
      [[ -x $tmp/unpack/homebot ]] || rt_die "binary not executable: $archive"
      [[ -s $tmp/unpack/THIRD_PARTY_NOTICES ]] || rt_die "missing notices: $archive"
      grep -q github.com/spf13/cobra "$tmp/unpack/THIRD_PARTY_NOTICES" || rt_die "cobra notice missing: $archive"
      case $os/$arch in
        linux/amd64) file "$tmp/unpack/homebot" | grep -q 'ELF.*x86-64' ;;
        linux/arm64) file "$tmp/unpack/homebot" | grep -q 'ELF.*aarch64' ;;
        darwin/amd64) file "$tmp/unpack/homebot" | grep -q 'Mach-O.*x86_64' ;;
        darwin/arm64) file "$tmp/unpack/homebot" | grep -q 'Mach-O.*arm64' ;;
      esac || rt_die "wrong binary format: $archive"
      if [[ $os == linux && $arch == amd64 ]]; then
        [[ $("$tmp/unpack/homebot" version) == "$version" ]] || rt_die "wrong version: $archive"
        HOMEBOT_DEBUG=1 "$tmp/unpack/homebot" version >"$tmp/out" 2>"$tmp/debug"
        grep -Eq 'build version='${version}' commit=[0-9a-f]{40} date=[^ ]+ channel=release' "$tmp/debug" \
          || rt_die "missing release channel stamp: $archive"
        go version -m "$tmp/unpack/homebot" >"$tmp/buildinfo"
        grep -q -- '-trimpath=true' "$tmp/buildinfo" || rt_die "trimpath missing: $archive"
        if grep -q -- '-ldflags' "$tmp/buildinfo"; then rt_die "unexpected ldflags in build info: $archive"; fi
        if strings "$tmp/unpack/homebot" | grep -E -q '/home/tester|/work/'; then
          rt_die "build path leaked: $archive"
        fi
      fi
      rm -rf "$tmp/unpack"
      sbom=$dist/homebot_${version}_${os}_${arch}.tar.gz.sbom.json
      [[ -s $sbom ]] || rt_die "missing SBOM: $sbom"
      jq -e '.spdxVersion and (.packages | length > 0)' "$sbom" >/dev/null \
        || rt_die "invalid SPDX SBOM: $sbom"
    done
  done
  (
    cd "$dist"
    [[ -s checksums.txt ]] || rt_die "missing checksums.txt"
    # checksum.extra_files lists install.sh, which GoReleaser hashes from the
    # repo root but never copies into dist (release.extra_files uploads it from
    # there). Check every line against its file: dist first, install.sh from the
    # source tree, anything else is a gap.
    while read -r sum name || [[ -n ${sum:-} ]]; do
      if [[ -f $name ]]; then
        file=$name
      elif [[ $name == install.sh && -f $RT_SRC/install.sh ]]; then
        file=$RT_SRC/install.sh
      else
        rt_die "checksums.txt names a file that is not in dist: $name"
      fi
      [[ $(sha256sum "$file" | cut -d' ' -f1) == "$sum" ]] || rt_die "checksum mismatch: $name"
    done <checksums.txt
    grep -q '  install.sh$' checksums.txt || rt_die "install.sh absent from checksums"
    ruby -c homebrew/Casks/homebot.rb >/dev/null
    grep -q 'binary \"homebot\"' homebrew/Casks/homebot.rb
    grep -q 'OS.mac?' homebrew/Casks/homebot.rb
    if grep -q generate_completions_from_executable homebrew/Casks/homebot.rb; then
      rt_die "cask executes quarantined binary"
    fi
    grep -q 'manpage "man/homebot.1"' homebrew/Casks/homebot.rb
    grep -q 'bash_completion' homebrew/Casks/homebot.rb
    grep -q 'zsh_completion' homebrew/Casks/homebot.rb
    grep -q 'fish_completion' homebrew/Casks/homebot.rb
    grep -q 'on_linux' homebrew/Casks/homebot.rb
    # The cask templates the tag: url ".../releases/download/v#{version}/...".
    grep -qF 'releases/download/v#{version}/' homebrew/Casks/homebot.rb
    # ... so the URL no longer carries the version: check the version line.
    grep -qxF "  version \"$version\"" homebrew/Casks/homebot.rb \
      || rt_die "cask version is not $version"
    # GoReleaser renders the dependencies as a multi-line array and sorts them.
    # setup needs both: gitleaks for the commit guard, git 2.40+ (Xcode 16's
    # command-line tools ship 2.39.5). Exactly these two.
    tr -s ' \n' ' ' <homebrew/Casks/homebot.rb | grep -qF 'depends_on formula: [ "git", "gitleaks", ]' \
      || rt_die 'cask does not depend on exactly git and gitleaks'
  )
  for arch in amd64 arm64; do
    deb=$dist/homebot_${version}_${arch}.deb
    [[ -s $deb ]] || rt_die "missing package: $deb"
    dpkg-deb -f "$deb" Depends | grep -Fx 'git (>= 1:2.40)'
    dpkg-deb -f "$deb" Recommends | grep -Fx gitleaks
    dpkg-deb -f "$deb" Suggests | grep -Fx 'fzf, lazygit'
    dpkg-deb -c "$deb" | grep -q 'usr/share/doc/homebot/copyright'
    mkdir -p "$tmp/deb"
    dpkg-deb -x "$deb" "$tmp/deb"
    grep -q 'MIT License' "$tmp/deb/usr/share/doc/homebot/copyright"
    rm -rf "$tmp/deb"
    case $arch in amd64) rpm_arch=x86_64 ;; arm64) rpm_arch=aarch64 ;; esac
    rpmfile=$dist/homebot-$version-1.$rpm_arch.rpm
    [[ -s $rpmfile ]] || rt_die "missing package: $rpmfile"
    rpm -qp --requires "$rpmfile" | grep -Fx 'git >= 2.40'
    rpm -qp --recommends "$rpmfile" | grep -Fx gitleaks
    rpm -qp --suggests "$rpmfile" | grep -Fx fzf
    rpm -qp --suggests "$rpmfile" | grep -Fx lazygit
    rpm -qp --licensefiles "$rpmfile" | grep -Fx /usr/share/licenses/homebot/LICENSE
  done
done

# Parse the workflow rather than relying on text patterns for privilege and
# secret placement. The release job may not run repository scripts.
# No .git in the harness tree, so actionlint needs its config named (as in check.sh).
actionlint -config-file .github/actionlint.yaml .github/workflows/release.yml .github/workflows/deploy-site.yml
ruby -ryaml -ropen3 <<'RUBY'
w = YAML.load_file('.github/workflows/release.yml')
raise 'top-level permissions' unless w['permissions'] == {}
t = w.fetch('jobs').fetch('test')
r = w.fetch('jobs').fetch('release')
raise 'test permissions' unless t['permissions'] == {'contents' => 'read'}
raise 'release ordering' unless r['needs'] == 'test' && r['environment'] == 'release'
raise 'release permissions' unless r['permissions'] == {'contents' => 'write', 'id-token' => 'write'}
raise 'private guard not first' unless r['steps'].first['name'] == 'Require public repository'
check = r['steps'].find { |s| s['name'] == 'Require Homebrew tap token' }
raise 'secret check not after checkout' unless r['steps'].index(check) == 2
raise 'token check leaks value' unless check['run'].include?('test -n "$HOMEBREW_TAP_TOKEN_SET"')
raise 'token check expression' unless check.fetch('env').fetch('HOMEBREW_TAP_TOKEN_SET').include?("secrets.HOMEBREW_TAP_TOKEN != ''")
{'unset' => nil, 'empty' => '', 'set' => 'yes'}.each do |kind, value|
  env = {'HOMEBREW_TAP_TOKEN_SET' => value}
  out, status = Open3.capture2e(env, 'sh', '-c', check['run'])
  if kind == 'set'
    raise "set token rejected: #{out}" unless status.success? && out.empty?
  else
    raise "#{kind} token accepted" if status.success?
    raise 'missing error marker' unless out.include?('::error::HOMEBREW_TAP_TOKEN is not set')
  end
end
all = t['steps'] + r['steps']
all.each do |s|
  if s['uses']
    raise "unpinned action: #{s['uses']}" unless s['uses'].match?(/@[0-9a-f]{40}\z/)
    if s['uses'].start_with?('actions/checkout@')
      raise 'checkout persists credentials' unless s.fetch('with').fetch('persist-credentials') == false
    end
  end
end
raise 'release job runs repository script' if r['steps'].any? { |s| s.fetch('run', '').match?(/(^|\s)(\.\/|scripts\/)/) }
gr = r['steps'].find { |s| s.fetch('uses', '').start_with?('goreleaser/goreleaser-action@') }
raise 'goreleaser token scope' unless gr.fetch('env').keys.sort == %w[GITHUB_TOKEN HOMEBREW_TAP_TOKEN]
raise 'secret at job level' if t.key?('env') || r.key?('env')
rc = r['steps'].last
raise 'rc cask gate' unless rc['if'].include?("'-rc.'") && rc.fetch('with')['retention-days'] == 7
# The site job moves the `site` branch after a final release only: its own
# write scope, no secrets, no force, no repository scripts, pinned actions.
si = w.fetch('jobs').fetch('site')
raise 'site ordering' unless si['needs'] == 'release' && !si.key?('environment') && !si.key?('env')
raise 'site final-only gate' unless si['if'].include?("'-rc.'") && si['if'].include?('!contains(')
raise 'site permissions' unless si['permissions'] == {'contents' => 'write'}
raise 'site job reaches a secret' if si.to_s.include?('secrets.')
si['steps'].each do |s|
  raise "unpinned action: #{s['uses']}" if s['uses'] && !s['uses'].match?(/@[0-9a-f]{40}\z/)
  raise 'site job runs repository script' if s.fetch('run', '').match?(/(^|\s)(\.\/|scripts\/)/)
  raise 'site push forced' if s.fetch('run', '').match?(/--force|push\s+(-\w*f|\+)/)
end
raise 'site push missing' unless si['steps'].any? { |s| s.fetch('run', '').include?('^{commit}:refs/heads/site') }
raise 'site-only token persistence' unless (t['steps'] + r['steps']).none? { |s| s.dig('with', 'persist-credentials') == true }
# deploy-site.yml: manual only, never on forks, not from another branch.
ds = YAML.load_file('.github/workflows/deploy-site.yml')
raise 'deploy-site trigger' unless ds.fetch(true).keys == ['workflow_dispatch'] # YAML reads `on:` as true
raise 'deploy-site permissions' unless ds['permissions'] == {} && ds['jobs'].keys == ['deploy'] && ds['jobs']['deploy']['permissions'] == {'contents' => 'write'}
dj = ds['jobs']['deploy']
gate = dj['if'].delete(' ')
raise 'deploy-site gate' unless ["github.repository=='hyperlab-sh/homebot'", "github.event_name=='workflow_dispatch'", "github.ref=='refs/heads/main'"].all? { |x| gate.include?(x) }
raise 'deploy-site environment' if dj.key?('environment') || dj.key?('env')
dj['steps'].each do |s|
  raise "unpinned action: #{s['uses']}" if s['uses'] && !s['uses'].match?(/@[0-9a-f]{40}\z/)
  raise 'deploy-site interpolates the ref' if s.fetch('run', '').include?('${{')
  raise 'deploy-site must lease' if s.fetch('run', '').match?(/push[^\n]*--force(?!-with-lease)/)
end
raise 'deploy-site lease missing' unless dj['steps'].any? { |s| s.fetch('run', '').include?('--force-with-lease=refs/heads/site:') }
puts 'release workflow structure and token check: PASS'
RUBY

# The unstamped local build must not claim the release channel (the debug log
# quotes an empty value as channel="").
go build -o "$tmp/plain" ./cmd/homebot
HOMEBOT_DEBUG=1 "$tmp/plain" version >"$tmp/out" 2>"$tmp/debug"
grep -Eq 'build version=[^ ]+ commit=[^ ]* date=[^ ]* channel=(""|)($| )' "$tmp/debug" \
  || rt_die "plain build unexpectedly has a release channel"

# A repeated snapshot of the same inputs must have identical archive bytes.
mkdir -p "$RT_VOL/repro"
bash scripts/release-test/make-snapshots.sh "$RT_VOL/repro"
for version in 0.0.1 0.0.2; do
  for os in linux darwin; do
    for arch in amd64 arm64; do
      name="homebot_${version}_${os}_${arch}.tar.gz"
      cmp "$RT_VOL/snapshots/v$version/$name" "$RT_VOL/repro/v$version/$name" \
        || rt_die "non-reproducible archive: $name"
    done
  done
done
