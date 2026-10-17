package pack

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestPackMovesAFolderIntoAutolinkCommonAndLinksItBackPlain(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".config/nvim/init.lua": "vim"})
	r := b.pack(b.h(".config/nvim"), Options{})
	if want := b.r("autolink/common/config/nvim"); r.To != want {
		t.Errorf("To = %q, want %q", r.To, want)
	}
	if r.From != b.h(".config/nvim") || r.Sealed || r.SealedCount != 0 {
		t.Errorf("Result = %+v", r)
	}
	if got := b.readlink(b.h(".config/nvim")); got != r.To {
		t.Errorf("link = %q, want %q", got, r.To)
	}
	if got := b.read(b.h(".config/nvim/init.lua")); got != "vim" {
		t.Errorf("init.lua = %q", got)
	}
}

func TestPackExpandsTildeAndHonoursLayer(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".zprofile": "z"})
	r := b.pack("~/.zprofile", Options{Layer: "linux"})
	if want := b.r("autolink/linux/zprofile"); r.To != want {
		t.Errorf("To = %q, want %q", r.To, want)
	}
	if r.From != b.h(".zprofile") {
		t.Errorf("From = %q", r.From)
	}
}

func TestPackResolvesRelativePathsAgainstTheContextCwdAndCleansThem(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".config/app/a": "a", ".config/other/b": "b", ".tool": "t"})
	// The process's own working directory is somewhere else entirely.
	b.Ctx.Cwd = b.h(".config")
	r := b.pack("app/", Options{})
	if want := b.r("autolink/common/config/app"); r.To != want {
		t.Errorf("app/: To = %q, want %q", r.To, want)
	}
	r = b.pack("./other/../other", Options{})
	if want := b.r("autolink/common/config/other"); r.To != want {
		t.Errorf("other: To = %q, want %q", r.To, want)
	}
	r = b.pack("../.tool", Options{})
	if want := b.r("autolink/common/tool"); r.To != want {
		t.Errorf(".tool: To = %q, want %q", r.To, want)
	}
	if r.From != b.h(".tool") {
		t.Errorf("From = %q", r.From)
	}
}

func TestPackPrivateMovesIntoPrivateSealsItAndLinksThePlaintext(t *testing.T) {
	b := newBox(t, false)
	testutil.TestKeys(t, b.Root)
	b.home(map[string]string{".netrc": "password"})
	r := b.pack(b.h(".netrc"), Options{Private: true})
	if want := b.r("private/common/netrc"); r.To != want {
		t.Errorf("To = %q, want %q", r.To, want)
	}
	if !r.Sealed || r.SealedCount != 1 {
		t.Errorf("Sealed = %v, SealedCount = %d, want true, 1", r.Sealed, r.SealedCount)
	}
	if !b.exists(b.r("private/common/netrc.age")) {
		t.Error("no netrc.age")
	}
	if got := b.readlink(b.h(".netrc")); got != r.To {
		t.Errorf("link = %q, want %q", got, r.To)
	}
	if got := b.read(b.h(".netrc")); got != "password" {
		t.Errorf("netrc through the link = %q", got)
	}
}

func TestPackPrivateCountsEverythingSealedUnderAPackedFolder(t *testing.T) {
	b := newBox(t, false)
	testutil.TestKeys(t, b.Root)
	b.home(map[string]string{".vault/id_a": "a", ".vault/sub/id_b": "b"})
	r := b.pack(b.h(".vault"), Options{Private: true, Layer: "linux"})
	if want := b.r("private/linux/vault"); r.To != want {
		t.Errorf("To = %q, want %q", r.To, want)
	}
	if r.SealedCount != 2 {
		t.Errorf("SealedCount = %d, want 2", r.SealedCount)
	}
	for _, p := range []string{"private/linux/vault/id_a.age", "private/linux/vault/sub/id_b.age"} {
		if !b.exists(b.r(p)) {
			t.Errorf("no %s", p)
		}
	}
}

func TestPackPrivateWithoutKeysRefusesBeforeMovingAnything(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".netrc": "password"})
	_, err := Pack(b.Ctx, b.h(".netrc"), Options{Private: true})
	wantUserError(t, err, `^Create keys before packing secrets:\n  homebot keys init$`)
	if !b.isRegular(b.h(".netrc")) {
		t.Error("the file is not where it was")
	}
	if b.exists(b.r("private/common/netrc")) {
		t.Error("the file moved anyway")
	}
}

func TestPackRefusesBadTargets(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".x": ""})
	// A real file that is not under the sandbox home.
	b.Files(map[string]string{"outside": "x"})
	outside := filepath.Join(b.Dir, "outside")
	for _, c := range []struct {
		path string
		o    Options
		re   string
	}{
		{b.h(".nope"), Options{}, `^~/\.nope does not exist\.$`},
		{outside, Options{}, `^Only things inside your home folder can be packed \(got ` + regexp.QuoteMeta(outside) + `\)\.$`},
		{b.Root, Options{}, `^~/\.home is \(or contains\) the home repo itself\.$`},
		{b.r("autolink/common"), Options{}, `^~/\.home/autolink/common is \(or contains\) the home repo itself\.$`},
		{b.Home, Options{}, `^~ is \(or contains\) the home repo itself\.$`},
		{b.h(".x"), Options{Layer: "windows"}, `^Unknown layer "windows" \(use common, linux, macos\)\.$`},
		{b.h(".x"), Options{Layer: "../x"}, `^Unknown layer "\.\./x"`},
	} {
		_, err := Pack(b.Ctx, c.path, c.o)
		wantUserError(t, err, c.re)
	}
	if !b.isRegular(b.h(".x")) {
		t.Error(".x was touched")
	}
}

func TestPackRefusesThingsAlreadyManagedOrInsideManagedFolders(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".config/app/a": "a"})
	b.pack(b.h(".config/app"), Options{})
	_, err := Pack(b.Ctx, b.h(".config/app"), Options{})
	wantUserError(t, err, `^~/\.config/app is already managed\.$`)
	b.home(map[string]string{".config/app/b": "b"})
	_, err = Pack(b.Ctx, b.h(".config/app/b"), Options{})
	wantUserError(t, err, `^~/\.config/app/b is inside ~/\.config/app, which is already managed\. It is in the repo already\.$`)
}

func TestPackRefusesForeignSymlinks(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{"real": "r"})
	if err := os.Symlink(b.h("real"), b.h(".link")); err != nil {
		t.Fatal(err)
	}
	_, err := Pack(b.Ctx, b.h(".link"), Options{})
	wantUserError(t, err, `^~/\.link is a symlink\. Pack the real file it points to instead\.$`)
	if b.exists(b.r("autolink/common/link")) {
		t.Error("the link was packed")
	}
}

func TestPackASingleFileGetsAMarkedParentSoTheAppsOtherFilesStayOut(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".claude/statusline-command.sh": "s", ".claude/sessions/x": "x"})
	r := b.pack(b.h(".claude/statusline-command.sh"), Options{})
	if want := b.r("autolink/common/(claude)/statusline-command.sh"); r.To != want {
		t.Errorf("To = %q, want %q", r.To, want)
	}
	if !b.isDir(b.h(".claude")) {
		t.Error("~/.claude is not a real folder")
	}
	if got := b.readlink(b.h(".claude/statusline-command.sh")); got != r.To {
		t.Errorf("link = %q, want %q", got, r.To)
	}
	if b.exists(b.r("autolink/common/(claude)/sessions")) {
		t.Error("sessions went into the repo")
	}
}

func TestPackRefusesSomethingAlreadyInTheRepoInAnyLayer(t *testing.T) {
	b := newBox(t, false)
	b.repo(map[string]string{"autolink/linux/x": "repo"})
	b.home(map[string]string{".x": "home"})
	_, err := Pack(b.Ctx, b.h(".x"), Options{Layer: "linux"})
	wantUserError(t, err, `^autolink/linux/x is already in the repo\. Pack the new things inside it instead\.$`)
	_, err = Pack(b.Ctx, b.h(".x"), Options{Layer: "common"})
	wantUserError(t, err, `already in the repo`)
	if got := b.read(b.h(".x")); got != "home" {
		t.Errorf(".x = %q", got)
	}
}

func TestPackRefusesAFolderAlreadyInTheRepoUnderItsMarkedSpelling(t *testing.T) {
	b := newBox(t, false)
	b.repo(map[string]string{"autolink/common/(claude)/a": "a"})
	b.home(map[string]string{".claude/b": "b"})
	_, err := Pack(b.Ctx, b.h(".claude"), Options{})
	wantUserError(t, err, `^autolink/common/\(claude\) is already in the repo`)
}

func TestPackStopsWhenAPrivateSealFailsAndKeepsTheSecretOnlyUnderPrivate(t *testing.T) {
	notRoot(t)
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	// Keys exist but cannot seal.
	b.repo(map[string]string{"private/recipients.txt": "this is not a key\n"})
	b.home(map[string]string{".netrc": "machine x password canary-4c1d9e07"})
	r, err := Pack(b.Ctx, b.h(".netrc"), Options{Private: true})
	if err == nil {
		t.Fatal("Pack succeeded with a recipients file that is not a key")
	}
	if strings.Contains(err.Error(), "canary-4c1d9e07") {
		t.Errorf("the error shows the secret: %v", err)
	}
	if r.To != b.r("private/common/netrc") || r.From != b.h(".netrc") {
		t.Errorf("Result = %+v", r)
	}
	b.wantPlaintextOnlyIn(t, "canary-4c1d9e07", "private/common/netrc")
	// Linked before the seal, so the secret still works from $HOME.
	if got := b.read(b.h(".netrc")); got != "machine x password canary-4c1d9e07" {
		t.Errorf("~/.netrc through the link = %q", got)
	}
}

// F2: a recipients.txt that lists a key age cannot encrypt to (an ecdsa ssh
// key) makes the seal fail after the move. The secret must still be in
// $HOME, linked and working, not only in the repo.
func TestPackPrivateSealFailureLeavesTheItemWorkingInHome(t *testing.T) {
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	b.repo(map[string]string{"private/recipients.txt": string(ssh.MarshalAuthorizedKey(pub))})
	b.home(map[string]string{".netrc": "machine x password canary-6a2e91b4", ".vault/key": "canary-6a2e91b4"}) // gitleaks:allow (test canary, not a secret)
	// A file is linked itself; a folder stays a real folder in $HOME, its
	// files linked one by one.
	for _, tc := range []struct{ path, to, file, target string }{
		{".netrc", "private/common/netrc", ".netrc", "private/common/netrc"},
		{".vault", "private/common/vault", ".vault/key", "private/common/vault/key"},
	} {
		r, err := Pack(b.Ctx, b.h(tc.path), Options{Private: true})
		wantUserError(t, err, `Could not encrypt private/`)
		if !strings.Contains(err.Error(), "ecdsa-sha2-nistp256") {
			t.Errorf("%s: error = %v, want recipient reason", tc.path, err)
		}
		if r.Sealed || r.SealedCount != 0 || r.To != b.r(tc.to) {
			t.Errorf("%s: Result = %+v, want nothing sealed", tc.path, r)
		}
		if got := b.readlink(b.h(tc.file)); got != b.r(tc.target) {
			t.Errorf("%s: link = %q, want %q", tc.file, got, b.r(tc.target))
		}
		if got := b.read(b.h(tc.file)); !strings.Contains(got, "canary-6a2e91b4") {
			t.Errorf("%s through the link = %q", tc.file, got)
		}
		if b.exists(b.r(tc.target + ".age")) {
			t.Errorf("%s: a sealed copy exists", tc.path)
		}
	}
	if strings.Contains(b.Stdout.String()+b.Stderr.String(), "canary-") {
		t.Error("the secret was printed")
	}
}

// D14: pack --private writes private/.gitignore when there is none, before
// anything lands in private/, so git never sees the plaintext.
func TestPackPrivateWritesPrivateGitignoreWhenMissing(t *testing.T) {
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	if err := os.Remove(b.r("private/.gitignore")); err != nil {
		t.Fatal(err)
	}
	b.home(map[string]string{".netrc": "machine x password canary-0d5c7e19"})
	b.pack(b.h(".netrc"), Options{Private: true})
	if got := b.read(b.r("private/.gitignore")); got != secrets.PrivateGitignore {
		t.Errorf("private/.gitignore = %q, want %q", got, secrets.PrivateGitignore)
	}
	b.wantPlaintextOnlyIn(t, "canary-0d5c7e19", "private/common/netrc")
}

func TestPackPrivateKeepsAnExistingPrivateGitignore(t *testing.T) {
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	mine := "# mine\n*\n!*/\n!*.age\n"
	b.repo(map[string]string{"private/.gitignore": mine})
	b.home(map[string]string{".netrc": "password"})
	b.pack(b.h(".netrc"), Options{Private: true})
	if got := b.read(b.r("private/.gitignore")); got != mine {
		t.Errorf("private/.gitignore = %q, want it kept as %q", got, mine)
	}
}

// Plain pack never touches private/.
func TestPackPlainWritesNoPrivateGitignore(t *testing.T) {
	b := newBox(t, true)
	if err := os.Remove(b.r("private/.gitignore")); err != nil {
		t.Fatal(err)
	}
	b.home(map[string]string{".tool": "x"})
	b.pack(b.h(".tool"), Options{})
	if b.exists(b.r("private/.gitignore")) {
		t.Error("plain pack wrote private/.gitignore")
	}
}

func TestPackPrivateLinkBackFailureKeepsTheSecretOnlyUnderPrivate(t *testing.T) {
	notRoot(t)
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	b.home(map[string]string{".gnupg-extra/token": "canary-8b02f6aa"})
	dir := b.h(".gnupg-extra")
	// The move works; then nothing can be created in the home folder.
	rename := func(from, to string) error {
		if err := os.Rename(from, to); err != nil {
			return err
		}
		return os.Chmod(dir, 0o500)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	r, err := packWith(b.Ctx, b.h(".gnupg-extra/token"), Options{Private: true}, rename)
	if err == nil {
		t.Fatal("Pack succeeded with no way to link the file back")
	}
	if strings.Contains(err.Error(), "canary-8b02f6aa") {
		t.Errorf("the error shows the secret: %v", err)
	}
	if !r.Sealed {
		t.Errorf("the secret was sealed before the link failed, Result = %+v", r)
	}
	if !b.exists(b.r("private/common/(gnupg-extra)/token.age")) {
		t.Error("no sealed copy")
	}
	b.wantPlaintextOnlyIn(t, "canary-8b02f6aa", "private/common/(gnupg-extra)/token")
}

func TestPackPrivateKeepsThePlaintextOutOfEveryTrackedPlace(t *testing.T) {
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	b.home(map[string]string{".netrc": "machine x password canary-1f77c3d2", ".vault/deep/key": "canary-1f77c3d2"}) // gitleaks:allow (test canary, not a secret)
	b.pack(b.h(".netrc"), Options{Private: true})
	b.pack(b.h(".vault"), Options{Private: true, Layer: "linux"})
	b.GitIn(b.Root, "add", "-A")
	staged := b.GitIn(b.Root, "ls-files", "--cached")
	for _, want := range []string{"private/common/netrc.age", "private/linux/vault/deep/key.age"} {
		if !strings.Contains(staged, want+"\n") {
			t.Errorf("%s is not staged:\n%s", want, staged)
		}
	}
	for _, line := range strings.Fields(staged) {
		if strings.HasPrefix(line, "private/") && !strings.HasSuffix(line, ".age") && !slices.Contains([]string{"private/.gitignore", "private/recipients.txt", "private/identity.age"}, line) {
			t.Errorf("%s is staged", line)
		}
	}
	for _, rel := range filesHolding(t, b.Root, "canary-1f77c3d2") {
		if !strings.HasPrefix(rel, "private/") || strings.HasSuffix(rel, ".age") {
			t.Errorf("%s holds the secret", rel)
		}
	}
	if strings.Contains(b.Stdout.String()+b.Stderr.String(), "canary-") {
		t.Error("the secret was printed")
	}
}

// wantPlaintextOnlyIn checks the secret is held by one file in the repo,
// under private/, and that git, asked to stage everything, does not take it.
func (b *box) wantPlaintextOnlyIn(t *testing.T, secret, rel string) {
	t.Helper()
	if got := filesHolding(t, b.Root, secret); !slices.Equal(got, []string{filepath.FromSlash(rel)}) {
		t.Errorf("files holding the secret = %q, want only %q", got, rel)
	}
	if strings.Contains(b.Stdout.String()+b.Stderr.String(), secret) {
		t.Error("the secret was printed")
	}
	b.GitIn(b.Root, "add", "-A")
	if staged := b.GitIn(b.Root, "ls-files", "--cached"); strings.Contains(staged, rel+"\n") {
		t.Errorf("git staged %s:\n%s", rel, staged)
	}
	if out := b.GitIn(b.Root, "diff", "--cached"); strings.Contains(out, secret) {
		t.Error("the staged diff holds the secret")
	}
}

// wantNoFilesUnder checks nothing but folders is under rel in the repo.
func (b *box) wantNoFilesUnder(t *testing.T, rel string) {
	t.Helper()
	_ = filepath.WalkDir(b.r(rel), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			t.Errorf("%s is in the repo", p)
		}
		return nil
	})
}

// The sandbox is a Linux machine, which never links the macos layer. Packing
// there would move the file and leave it unlinkable, so it is refused first.
func TestPackRefusesALayerThisSystemDoesNotLinkAndMovesNothing(t *testing.T) {
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	b.home(map[string]string{".zprofile": "z", ".netrc": "canary-2d6e8f01"})
	for _, c := range []struct {
		path string
		o    Options
	}{
		{b.h(".zprofile"), Options{Layer: "macos"}},
		{b.h(".netrc"), Options{Layer: "macos", Private: true}},
	} {
		r, err := Pack(b.Ctx, c.path, c.o)
		wantUserError(t, err, `^The macos layer is not linked on linux, so a file packed there could not be linked back \(use common, linux\)\.$`)
		if r != (Result{}) {
			t.Errorf("Result = %+v", r)
		}
		if !b.isRegular(c.path) {
			t.Errorf("%s is not where it was", c.path)
		}
	}
	for _, rel := range []string{"autolink/macos", "private/macos"} {
		b.wantNoFilesUnder(t, rel)
	}
	if got := filesHolding(t, b.Root, "canary-2d6e8f01"); len(got) != 0 {
		t.Errorf("the secret reached the repo: %q", got)
	}
}

func TestPackOnMacosRefusesTheLinuxLayerAndTakesMacos(t *testing.T) {
	b := &box{Box: testutil.Sandbox(t, testutil.Options{OS: app.MacOS}), t: t}
	b.home(map[string]string{".zprofile": "z", ".bashrc": "b"})
	_, err := Pack(b.Ctx, b.h(".bashrc"), Options{Layer: "linux"})
	wantUserError(t, err, `^The linux layer is not linked on macos, so .* \(use common, macos\)\.$`)
	if !b.isRegular(b.h(".bashrc")) {
		t.Error(".bashrc moved")
	}
	r := b.pack(b.h(".zprofile"), Options{Layer: "macos"})
	if r.To != b.r("autolink/macos/zprofile") || b.readlink(b.h(".zprofile")) != r.To {
		t.Errorf("Result = %+v", r)
	}
}

// A built-in folder with nothing in it is moved but yields no link, so the
// path is not back where it was.
func TestPackAnEmptyBuiltInFolderEndsInLinkFailed(t *testing.T) {
	b := newBox(t, false)
	if err := os.MkdirAll(b.h(".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := Pack(b.Ctx, b.h(".ssh"), Options{})
	wantUserError(t, err, `^Moved ~/\.ssh into the repo but could not link it back\. Link it with:\n  homebot link$`)
	if r.To != b.r("autolink/common/ssh") || !b.isDir(r.To) {
		t.Errorf("Result = %+v", r)
	}
	if b.exists(b.h(".ssh")) {
		t.Error("~/.ssh is back")
	}
}

func TestPackPrivateRefusesNamesThePrivateFolderDoesNotIgnoreBeforeMovingAnything(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string // relative to $HOME
		path  string            // packed, relative to $HOME
		shown string            // named in the error
		dirs  []string          // extra empty folders
	}{
		{"README.md deep", map[string]string{".vault/a/b/README.md": "r", ".vault/key": "k"}, ".vault", "~/.vault/a/b/README.md", nil},
		{".gitignore", map[string]string{".vault/.gitignore": "!*", ".vault/key": "k"}, ".vault", "~/.vault/.gitignore", nil},
		{".gitkeep", map[string]string{".vault/sub/.gitkeep": "", ".vault/key": "k"}, ".vault", "~/.vault/sub/.gitkeep", nil},
		{"recipients.txt", map[string]string{".vault/recipients.txt": "x", ".vault/key": "k"}, ".vault", "~/.vault/recipients.txt", nil},
		{"an .age file", map[string]string{".vault/x.age": "x", ".vault/key": "k"}, ".vault", "~/.vault/x.age", nil},
		{"the item is an .age file", map[string]string{".vault-extra/x.age": "x"}, ".vault-extra/x.age", "~/.vault-extra/x.age", nil},
		{"the item is a README.md", map[string]string{".vault-extra/README.md": "x"}, ".vault-extra/README.md", "~/.vault-extra/README.md", nil},
		{".git folder", map[string]string{".vault/key": "k"}, ".vault", "~/.vault/proj/.git", []string{".vault/proj/.git"}},
		// The top-level item is judged by the name it gets in the repo, which
		// has no leading dot.
		{"top-level .README.md lands as README.md", map[string]string{".README.md": "x"}, ".README.md", "~/.README.md", nil},
		{"top-level .recipients.txt lands as recipients.txt", map[string]string{".recipients.txt": "x"}, ".recipients.txt", "~/.recipients.txt", nil},
		{"a .GitIgnore beside a secret", map[string]string{".vault/sub/.GitIgnore": "!*", ".vault/key": "k"}, ".vault", "~/.vault/sub/.GitIgnore", nil},
		// Case does not matter: git on macOS ignores it.
		{"readme.md", map[string]string{".vault/readme.md": "r", ".vault/key": "k"}, ".vault", "~/.vault/readme.md", nil},
		{"x.AGE", map[string]string{".vault/x.AGE": "x", ".vault/key": "k"}, ".vault", "~/.vault/x.AGE", nil},
		{"Recipients.TXT", map[string]string{".vault/Recipients.TXT": "x", ".vault/key": "k"}, ".vault", "~/.vault/Recipients.TXT", nil},
		{"nested .GitIgnore", map[string]string{".vault/a/b/.GitIgnore": "!*", ".vault/key": "k"}, ".vault", "~/.vault/a/b/.GitIgnore", nil},
		{".GIT file", map[string]string{".vault/proj/.GIT": "gitdir: x", ".vault/key": "k"}, ".vault", "~/.vault/proj/.GIT", nil},
		{".GIT folder", map[string]string{".vault/key": "k"}, ".vault", "~/.vault/proj/.GIT", []string{".vault/proj/.GIT"}},
		{".git file", map[string]string{".vault/proj/.git": "gitdir: x", ".vault/key": "k"}, ".vault", "~/.vault/proj/.git", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t, true)
			testutil.TestKeys(t, b.Root)
			b.home(c.files)
			for _, d := range c.dirs {
				if err := os.MkdirAll(b.h(d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			before := filesHolding(t, b.Home, "")
			r, err := Pack(b.Ctx, b.h(c.path), Options{Private: true})
			wantUserError(t, err, `^`+regexp.QuoteMeta(c.shown)+` can't go into private/`)
			if r != (Result{}) {
				t.Errorf("Result = %+v", r)
			}
			if after := filesHolding(t, b.Home, ""); !slices.Equal(before, after) {
				t.Errorf("home changed:\n before %q\n after  %q", before, after)
			}
			if !b.isRegular(b.h(c.path)) && !b.isDir(b.h(c.path)) {
				t.Errorf("%s is gone", c.path)
			}
			b.wantNoFilesUnder(t, "private/common")
		})
	}
}

// ~/.gitignore lands in the repo as gitignore, a name private/ ignores, so it
// is no reason to refuse.
func TestPackPrivateTakesADotGitignoreBecauseItLandsWithoutTheDot(t *testing.T) {
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	b.home(map[string]string{".gitignore": "canary-6d2b80e5"})
	r := b.pack(b.h(".gitignore"), Options{Private: true})
	if r.To != b.r("private/common/gitignore") || !r.Sealed {
		t.Errorf("Result = %+v", r)
	}
	b.wantPlaintextOnlyIn(t, "canary-6d2b80e5", "private/common/gitignore")
}

// A link is judged by its name, not by what it points at.
func TestPackPrivateRefusesASymlinkNamedReadmeInsideTheSource(t *testing.T) {
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	b.home(map[string]string{".vault/key": "k"})
	if err := os.Symlink("key", b.h(".vault/README.md")); err != nil {
		t.Fatal(err)
	}
	r, err := Pack(b.Ctx, b.h(".vault"), Options{Private: true})
	wantUserError(t, err, `^~/\.vault/README\.md can't go into private/`)
	if r != (Result{}) || !b.isDir(b.h(".vault")) {
		t.Errorf("Result = %+v", r)
	}
	b.wantNoFilesUnder(t, "private/common")
}

// The same names are fine without --private: nothing there ignores them.
func TestPackPlainTakesTheNamesPrivateRefuses(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".vault/README.md": "r", ".vault/.gitignore": "x", ".vault/k.age": "k"})
	r := b.pack(b.h(".vault"), Options{})
	if b.read(filepath.Join(r.To, "README.md")) != "r" {
		t.Errorf("README.md is not in %s", r.To)
	}
}
