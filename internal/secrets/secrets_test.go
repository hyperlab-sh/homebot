package secrets

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/crypto/ssh"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// fixture is a sandbox with test keys, as the TypeScript tests' beforeEach.
type fixture struct {
	t   *testing.T
	sb  *testutil.Box
	ctx *app.Context
	// ids is the private key TestKeys made, parsed.
	ids []age.Identity
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{})
	ids, err := age.ParseIdentities(strings.NewReader(testutil.TestKeys(t, sb.Root)))
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, sb: sb, ctx: sb.Ctx, ids: ids}
}

// p is the path of rel under private/.
func (f *fixture) p(rel string) string {
	return filepath.Join(f.sb.Root, "private", filepath.FromSlash(rel))
}

func (f *fixture) write(rel, content string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.p(rel)), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(f.p(rel), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) read(rel string) string {
	f.t.Helper()
	data, err := os.ReadFile(f.p(rel))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) remove(rel string) {
	f.t.Helper()
	if err := os.Remove(f.p(rel)); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) exists(rel string) bool {
	_, err := os.Lstat(f.p(rel))
	return err == nil
}

// states maps each secret to its state, as the TypeScript tests' states().
func (f *fixture) states() map[string]State {
	f.t.Helper()
	list, err := List(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]State{}
	for _, s := range list {
		out[s.Rel] = s.State
	}
	return out
}

func (f *fixture) wantStates(want map[string]State) {
	f.t.Helper()
	if diff := cmp.Diff(want, f.states()); diff != "" {
		f.t.Errorf("states (-want +got):\n%s", diff)
	}
}

// otherMachine simulates another machine: fresh state, same repo files.
func (f *fixture) otherMachine() {
	f.t.Helper()
	if err := os.RemoveAll(f.ctx.StateDir); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) recordsFile() string {
	return filepath.Join(f.ctx.StateDir, "secrets.json")
}

func (f *fixture) saveRecords() []byte {
	f.t.Helper()
	data, err := os.ReadFile(f.recordsFile())
	if err != nil {
		f.t.Fatal(err)
	}
	return data
}

func (f *fixture) restoreRecords(data []byte) {
	f.t.Helper()
	if err := os.WriteFile(f.recordsFile(), data, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) seal() []string {
	f.t.Helper()
	r, err := Seal(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	return r.Sealed
}

func (f *fixture) unlock(opts UnlockOptions) UnlockResult {
	f.t.Helper()
	r, err := unlockWith(f.ctx, f.ids, opts)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

// withPassphrase answers every passphrase prompt with pass.
func (f *fixture) withPassphrase(pass string) {
	f.ctx.Env = f.ctx.Env.With(map[string]string{"HOMEBOT_TEST_PASSPHRASE": pass})
}

// open decrypts the file at path with ids.
func open(t *testing.T, path string, ids []age.Identity) string {
	t.Helper()
	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := age.Decrypt(bytes.NewReader(sealed), ids...)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// wantUserError checks err is a *fail.UserError with the text of want.
func wantUserError(t *testing.T, ctx *app.Context, err error, want msg.Text) {
	t.Helper()
	var userErr *fail.UserError
	if !errors.As(err, &userErr) {
		t.Fatalf("err = %v (%T), want a UserError", err, err)
	}
	if text := voice.Plain(ctx, want); userErr.Msg != text {
		t.Errorf("err = %q, want %q", userErr.Msg, text)
	}
}

func eq[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("%s (-want +got):\n%s", what, diff)
	}
}

// ── seal ──

func TestSealEncryptsNewPlaintextAndRecordsIt(t *testing.T) {
	f := newFixture(t)
	f.write("common/netrc", "machine x password y")
	f.wantStates(map[string]State{"common/netrc": New})
	eq(t, "sealed", f.seal(), []string{"common/netrc"})
	if !f.exists("common/netrc.age") {
		t.Fatal("no common/netrc.age")
	}
	if strings.Contains(f.read("common/netrc.age"), "password") {
		t.Error("the .age file holds the plaintext")
	}
	if got := open(t, f.p("common/netrc.age"), f.ids); got != "machine x password y" {
		t.Errorf("the .age file decrypts to %q", got)
	}
	f.wantStates(map[string]State{"common/netrc": Sealed})
}

func TestSealDoesNothingWhenNothingChanged(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "1")
	f.seal()
	before := f.read("common/a.age")
	eq(t, "second seal", f.seal(), []string(nil))
	if f.read("common/a.age") != before {
		t.Error("the .age file churned")
	}
}

func TestSealReencryptsAfterALocalEdit(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "1")
	f.seal()
	f.write("common/a", "2")
	f.wantStates(map[string]State{"common/a": Modified})
	eq(t, "sealed", f.seal(), []string{"common/a"})
	if got := open(t, f.p("common/a.age"), f.ids); got != "2" {
		t.Errorf("the .age file decrypts to %q", got)
	}
}

func TestSealIgnoresTheReposOwnFiles(t *testing.T) {
	f := newFixture(t)
	f.write("README.md", "docs")
	f.write("common/.gitkeep", "")
	f.write("common/README.md", "docs")
	f.write("linux/.gitignore", "*")
	f.write(".age", "nameless")
	f.write("common/.age", "nameless")
	f.wantStates(map[string]State{})
}

func TestSealTreatsMetaNamesBelowTheTopAsSecrets(t *testing.T) {
	f := newFixture(t)
	f.write("common/recipients.txt", "a secret that happens to have this name")
	f.write("common/identity.age", "sealed elsewhere")
	f.wantStates(map[string]State{"common/recipients.txt": New, "common/identity": Locked})
}

func TestSealWorksWithoutThePassphrase(t *testing.T) {
	f := newFixture(t)
	f.remove("identity.age")
	f.write("linux/token", "t")
	eq(t, "sealed", f.seal(), []string{"linux/token"})
}

func TestSealRefusesWithoutKeys(t *testing.T) {
	f := newFixture(t)
	f.remove("recipients.txt")
	f.write("common/a", "1")
	_, err := Seal(f.ctx)
	wantUserError(t, f.ctx, err, msg.SecretsErrorNoRecipients())
	if !strings.Contains(err.Error(), "homebot keys init") {
		t.Errorf("err = %q, want it to say homebot keys init", err)
	}
	if f.exists("common/a.age") {
		t.Error("sealed without keys")
	}
}

func TestSealWithBadRecipientsWritesNothing(t *testing.T) {
	f := newFixture(t)
	f.write("recipients.txt", "not a public key\n")
	f.write("common/a", "1")
	_, err := Seal(f.ctx)
	if err == nil || !strings.HasPrefix(err.Error(), "Could not encrypt private/common/a.\n  age: ") {
		t.Fatalf("err = %v, want the encrypt failure with age's reason", err)
	}
	if f.exists("common/a.age") {
		t.Error("wrote a .age file")
	}
}

func TestSealKeepsWhatItSealedBeforeAFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable files")
	}
	f := newFixture(t)
	f.write("common/a", "1")
	f.write("common/b", "2")
	if err := os.Chmod(f.p("common/b"), 0o000); err != nil {
		t.Fatal(err)
	}
	r, err := Seal(f.ctx)
	if err == nil || !strings.HasPrefix(err.Error(), "Could not encrypt private/common/b.") {
		t.Fatalf("err = %v, want the failure for common/b", err)
	}
	eq(t, "sealed", r.Sealed, []string{"common/a"})
	if err := os.Chmod(f.p("common/b"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.wantStates(map[string]State{"common/a": Sealed, "common/b": New})
}

func TestSealLeavesNoTempFiles(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "1")
	f.write("linux/b", "2")
	f.seal()
	f.write("common/a", "3")
	f.seal()
	err := filepath.WalkDir(filepath.Join(f.sb.Root, "private"), func(path string, _ os.DirEntry, err error) error {
		if strings.Contains(filepath.Base(path), ".tmp-") {
			t.Errorf("temp file left: %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ── unlock ──

func TestUnlockDecryptsLockedSecretsWithOwnerOnlyPermissions(t *testing.T) {
	f := newFixture(t)
	f.write("common/ssh/id", "KEY")
	f.seal()
	f.remove("common/ssh/id")
	f.otherMachine()
	f.wantStates(map[string]State{"common/ssh/id": Locked})
	r := f.unlock(UnlockOptions{})
	eq(t, "unlocked", r.Unlocked, []string{"common/ssh/id"})
	if got := f.read("common/ssh/id"); got != "KEY" {
		t.Errorf("plaintext = %q", got)
	}
	info, err := os.Stat(f.p("common/ssh/id"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
	f.wantStates(map[string]State{"common/ssh/id": Sealed})
}

func TestUnlockKeepsBinarySecretsByteForByte(t *testing.T) {
	f := newFixture(t)
	blob := string([]byte{0, 255, 1, 128, 10, 13, 0xc3, 0x28})
	f.write("common/blob", blob)
	f.seal()
	f.remove("common/blob")
	f.unlock(UnlockOptions{})
	if got := f.read("common/blob"); got != blob {
		t.Errorf("plaintext = %q, want %q", got, blob)
	}
}

func TestUnlockPicksUpASecretChangedOnAnotherMachine(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "old")
	f.seal()
	// The other machine edits and seals; we pull its .age.
	mine := f.read("common/a")
	f.write("common/a", "new")
	before := f.saveRecords()
	f.seal()
	f.restoreRecords(before)
	f.write("common/a", mine)
	f.wantStates(map[string]State{"common/a": Upstream})
	f.unlock(UnlockOptions{})
	if got := f.read("common/a"); got != "new" {
		t.Errorf("plaintext = %q, want new", got)
	}
	f.wantStates(map[string]State{"common/a": Sealed})
}

// conflict leaves common/a changed here ("mine") and in the repo ("theirs").
func (f *fixture) conflict() {
	f.t.Helper()
	f.write("common/a", "base")
	f.seal()
	records := f.saveRecords()
	f.write("common/a", "theirs")
	f.seal()
	f.restoreRecords(records)
	f.write("common/a", "mine")
}

func TestUnlockNeverOverwritesALocalEditThatConflicts(t *testing.T) {
	f := newFixture(t)
	f.conflict()
	f.wantStates(map[string]State{"common/a": Conflict})
	eq(t, "sealed", f.seal(), []string(nil))
	r := f.unlock(UnlockOptions{})
	eq(t, "conflicts", r.Conflicts, []string{"common/a"})
	if got := f.read("common/a"); got != "mine" {
		t.Errorf("plaintext = %q, want mine", got)
	}
	// And the person decides.
	f.unlock(UnlockOptions{Theirs: true})
	if got := f.read("common/a"); got != "theirs" {
		t.Errorf("plaintext = %q, want theirs", got)
	}
}

func TestLockOursSealsAConflictWithTheLocalVersion(t *testing.T) {
	f := newFixture(t)
	f.conflict()
	r, err := Lock(f.ctx, LockOptions{Ours: true})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "sealed", r.Sealed, []string{"common/a"})
	f.remove("common/a")
	f.unlock(UnlockOptions{})
	if got := f.read("common/a"); got != "mine" {
		t.Errorf("plaintext = %q, want mine", got)
	}
}

// Unlock calls unverified plaintext that differs a conflict and offers
// lock --ours; the TypeScript's lock --ours then sealed nothing.
func TestLockOursSealsUnverifiedPlaintextWithTheLocalVersion(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "committed")
	f.write("common/b", "untouched")
	f.seal()
	f.otherMachine()
	f.write("common/a", "local")
	eq(t, "conflicts", f.unlock(UnlockOptions{}).Conflicts, []string{"common/a"})
	r, err := Lock(f.ctx, LockOptions{Ours: true})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "sealed", r.Sealed, []string{"common/a"})
	if got := open(t, f.p("common/a.age"), f.ids); got != "local" {
		t.Errorf("common/a.age decrypts to %q, want local", got)
	}
	f.wantStates(map[string]State{"common/a": Sealed, "common/b": Sealed})
	eq(t, "plain lock", f.seal(), []string(nil))
}

func TestLockWithoutOursLeavesUnverifiedPlaintext(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "committed")
	f.seal()
	before := f.read("common/a.age")
	f.otherMachine()
	f.write("common/a", "local")
	eq(t, "sealed", f.seal(), []string(nil))
	if f.read("common/a.age") != before {
		t.Error("lock overwrote the repo's version")
	}
}

func TestUnlockVerifiesPlaintextItHasNoRecordOf(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "same")
	f.seal()
	f.otherMachine()
	f.wantStates(map[string]State{"common/a": Unverified})
	eq(t, "verified", f.unlock(UnlockOptions{}).Verified, []string{"common/a"})
	f.wantStates(map[string]State{"common/a": Sealed})
}

func TestUnlockUnverifiedPlaintextThatDiffersIsAConflict(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "committed")
	f.seal()
	f.otherMachine()
	f.write("common/a", "local")
	eq(t, "conflicts", f.unlock(UnlockOptions{}).Conflicts, []string{"common/a"})
	if got := f.read("common/a"); got != "local" {
		t.Errorf("plaintext = %q, want local", got)
	}
	f.wantStates(map[string]State{"common/a": Unverified})
}

func TestUnlockTheirsOverwritesUnverifiedPlaintextThatDiffers(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "committed")
	f.seal()
	f.otherMachine()
	f.write("common/a", "local")
	eq(t, "unlocked", f.unlock(UnlockOptions{Theirs: true}).Unlocked, []string{"common/a"})
	if got := f.read("common/a"); got != "committed" {
		t.Errorf("plaintext = %q, want committed", got)
	}
	info, err := os.Stat(f.p("common/a"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
	f.wantStates(map[string]State{"common/a": Sealed})
}

func TestUnlockWithAWrongIdentityFailsLoudly(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "x")
	f.seal()
	f.remove("common/a")
	wrong, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	_, err = unlockWith(f.ctx, []age.Identity{wrong}, UnlockOptions{})
	if err == nil || !strings.HasPrefix(err.Error(), "Could not decrypt private/common/a.age.\n  age: ") {
		t.Fatalf("err = %v, want the decrypt failure with age's reason", err)
	}
	if f.exists("common/a") {
		t.Error("wrote plaintext")
	}
}

func TestUnlockKeepsWhatItDidBeforeAFailure(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "1")
	f.write("common/b", "2")
	f.seal()
	f.remove("common/a")
	f.remove("common/b")
	f.write("common/b.age", "damaged")
	r, err := unlockWith(f.ctx, f.ids, UnlockOptions{})
	if err == nil || !strings.HasPrefix(err.Error(), "Could not decrypt private/common/b.age.") {
		t.Fatalf("err = %v, want the failure for common/b", err)
	}
	eq(t, "unlocked", r.Unlocked, []string{"common/a"})
	f.wantStates(map[string]State{"common/a": Sealed, "common/b": Locked})
}

func TestUnlockWritesNoTempFilesOutsidePrivate(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "1")
	f.seal()
	f.remove("common/a")
	f.unlock(UnlockOptions{})
	entries, err := os.ReadDir(f.p("common"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	eq(t, "private/common", names, []string{"a", "a.age"})
}

// ── wipe ──

func TestWipeRemovesOnlyPlaintextThatIsSafelySealed(t *testing.T) {
	f := newFixture(t)
	f.write("common/sealed", "s")
	f.seal()
	f.write("common/fresh", "f")
	r, err := wipe(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "wipe", r, LockResult{Wiped: []string{"common/sealed"}, Kept: []string{"common/fresh"}})
	if f.exists("common/sealed") {
		t.Error("sealed plaintext is still there")
	}
	if !f.exists("common/sealed.age") {
		t.Error("the .age file is gone")
	}
	if !f.exists("common/fresh") {
		t.Error("unsealed plaintext was removed")
	}
}

func TestLockWipeSealsFirst(t *testing.T) {
	f := newFixture(t)
	f.write("common/sealed", "s")
	f.seal()
	f.write("common/fresh", "f")
	r, err := Lock(f.ctx, LockOptions{Wipe: true})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "lock --wipe", r, LockResult{Sealed: []string{"common/fresh"}, Wiped: []string{"common/fresh", "common/sealed"}})
	f.wantStates(map[string]State{"common/fresh": Locked, "common/sealed": Locked})
}

func TestLockWipeKeepsConflictsEvenWithOurs(t *testing.T) {
	f := newFixture(t)
	f.conflict()
	r, err := Lock(f.ctx, LockOptions{Ours: true, Wipe: true})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "lock --ours --wipe", r, LockResult{Kept: []string{"common/a"}})
	if got := f.read("common/a"); got != "mine" {
		t.Errorf("plaintext = %q, want mine", got)
	}
}

// ── status ──

func TestStatusCountsStatesInTheOrderListMeetsThem(t *testing.T) {
	f := newFixture(t)
	f.write("common/c", "sealed")
	f.seal()
	f.write("common/a", "new")
	f.write("common/b", "new")
	f.write("linux/d.age", "locked")
	counts, err := Status(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "counts", counts, Counts{{New, 2}, {Sealed, 1}, {Locked, 1}})
	if counts.Of(New) != 2 || counts.Of(Conflict) != 0 {
		t.Errorf("Of = %d new, %d conflict", counts.Of(New), counts.Of(Conflict))
	}
}

func TestStatusOfAnEmptyPrivateIsNothing(t *testing.T) {
	f := newFixture(t)
	counts, err := Status(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Errorf("counts = %v", counts)
	}
}

func TestKeysExistNeedsBothFiles(t *testing.T) {
	f := newFixture(t)
	if !KeysExist(f.ctx) {
		t.Error("KeysExist = false with both files")
	}
	f.remove("identity.age")
	if KeysExist(f.ctx) {
		t.Error("KeysExist = true without identity.age")
	}
	sb := testutil.Sandbox(t, testutil.Options{})
	if KeysExist(sb.Ctx) {
		t.Error("KeysExist = true in a repo without keys")
	}
}

// ── the private key ──

func TestUnlockAsksForThePassphrase(t *testing.T) {
	f := newFixture(t)
	f.write("common/a", "1")
	f.seal()
	f.remove("common/a")
	f.withPassphrase(testutil.TestPassphrase)
	r, err := Unlock(f.ctx, UnlockOptions{})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "unlocked", r.Unlocked, []string{"common/a"})
}

func TestUnlockWithAWrongPassphraseSaysSo(t *testing.T) {
	for _, pass := range []string{"wrong", ""} {
		f := newFixture(t)
		f.withPassphrase(pass)
		_, err := Unlock(f.ctx, UnlockOptions{})
		wantUserError(t, f.ctx, err, msg.SecretsErrorPassphrase())
	}
}

func TestUnlockWithoutIdentity(t *testing.T) {
	f := newFixture(t)
	f.remove("identity.age")
	_, err := Unlock(f.ctx, UnlockOptions{})
	wantUserError(t, f.ctx, err, msg.SecretsErrorNoIdentity())
}

// noTerminal makes asking for a passphrase fail: no hook, no terminal.
func noTerminal(t *testing.T) {
	t.Helper()
	saved := ttyPath
	ttyPath = filepath.Join(t.TempDir(), "no-tty")
	t.Cleanup(func() { ttyPath = saved })
}

func TestUnlockReportsAnUnreadableIdentityWithoutAsking(t *testing.T) {
	notPassphrase := func(t *testing.T) []byte {
		id, err := age.GenerateX25519Identity()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := encrypt(&b, []byte(id.String()+"\n"), id.Recipient()); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	cases := map[string]func(t *testing.T) []byte{
		"the oracle's placeholder": func(*testing.T) []byte { return []byte("placeholder: the scenarios never decrypt\n") },
		"empty":                    func(*testing.T) []byte { return nil },
		"a plaintext key":          func(*testing.T) []byte { return []byte("AGE-SECRET-KEY-1QQQ\n") },
		"sealed to a public key":   notPassphrase,
		"truncated header":         func(*testing.T) []byte { return []byte("age-encryption.org/v1\n-> scrypt") },
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			noTerminal(t)
			if err := os.WriteFile(f.p("identity.age"), content(t), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Unlock(f.ctx, UnlockOptions{})
			wantUserError(t, f.ctx, err, msg.SecretsErrorIdentityUnreadable())
		})
	}
}

func TestUnlockReportsAPassphraseSealedFileThatHoldsNoKey(t *testing.T) {
	f := newFixture(t)
	r, err := age.NewScryptRecipient(testutil.TestPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	r.SetWorkFactor(testutil.TestWorkFactor)
	var b bytes.Buffer
	if err := encrypt(&b, []byte("hello\n"), r); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.p("identity.age"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	f.withPassphrase(testutil.TestPassphrase)
	_, err = Unlock(f.ctx, UnlockOptions{})
	wantUserError(t, f.ctx, err, msg.SecretsErrorIdentityUnreadable())
}

func TestUnlockWithoutATerminal(t *testing.T) {
	f := newFixture(t)
	noTerminal(t)
	_, err := Unlock(f.ctx, UnlockOptions{})
	wantUserError(t, f.ctx, err, msg.SecretsErrorNoTerminal())

	notATTY := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notATTY, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ttyPath = notATTY
	_, err = Unlock(f.ctx, UnlockOptions{})
	wantUserError(t, f.ctx, err, msg.SecretsErrorNoTerminal())
}

func TestUnlockWithNothingToDoStillSavesRecords(t *testing.T) {
	f := newFixture(t)
	f.withPassphrase(testutil.TestPassphrase)
	r, err := Unlock(f.ctx, UnlockOptions{})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "result", r, UnlockResult{})
	if got := string(f.saveRecords()); got != "{}\n" {
		t.Errorf("secrets.json = %q, want {}", got)
	}
}

// ── keys init ──

// withNewKeyHooks answers the passphrase prompts and lowers the work factor.
func (f *fixture) withNewKeyHooks(pass string) {
	f.ctx.Env = f.ctx.Env.With(map[string]string{
		"HOMEBOT_TEST_PASSPHRASE":         pass,
		"HOMEBOT_TEST_SCRYPT_WORK_FACTOR": "10",
	})
}

func emptyRepo(t *testing.T) *fixture {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{})
	return &fixture{t: t, sb: sb, ctx: sb.Ctx}
}

// header returns the scrypt stanza's arguments of the age file at path.
func scryptArgs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 || lines[0] != "age-encryption.org/v1" {
		t.Fatalf("%s is not an age file", path)
	}
	fields := strings.Fields(lines[1])
	if len(fields) != 4 || fields[0] != "->" || fields[1] != "scrypt" {
		t.Fatalf("%s: first stanza %q, want scrypt", path, lines[1])
	}
	return fields[2:]
}

func TestInitKeysCreatesAKeyPairTheRepoCanUse(t *testing.T) {
	f := emptyRepo(t)
	f.withNewKeyHooks("a new passphrase")
	public, err := InitKeys(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(public, "age1") {
		t.Errorf("public key = %q", public)
	}
	if got := f.read("recipients.txt"); got != public+"\n" {
		t.Errorf("recipients.txt = %q, want %q", got, public+"\n")
	}
	if args := scryptArgs(t, f.p("identity.age")); args[1] != "10" {
		t.Errorf("work factor = %s, want the test's 10", args[1])
	}
	id, err := age.NewScryptIdentity("a new passphrase")
	if err != nil {
		t.Fatal(err)
	}
	key := open(t, f.p("identity.age"), []age.Identity{id})
	lines := strings.Split(key, "\n")
	if len(lines) != 4 || lines[0] != "# created: 2026-01-01T00:00:00Z" || lines[1] != "# public key: "+public ||
		!strings.HasPrefix(lines[2], "AGE-SECRET-KEY-1") || lines[3] != "" {
		t.Errorf("identity.age holds %q, want age-keygen's layout", key)
	}
	if !KeysExist(f.ctx) {
		t.Error("KeysExist = false after InitKeys")
	}

	f.write("common/a", "round trip")
	if _, err := Seal(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.remove("common/a")
	r, err := Unlock(f.ctx, UnlockOptions{})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "unlocked", r.Unlocked, []string{"common/a"})
	if got := f.read("common/a"); got != "round trip" {
		t.Errorf("plaintext = %q", got)
	}
}

func TestInitKeysUsesAgesDefaultWorkFactor(t *testing.T) {
	if testing.Short() {
		t.Skip("scrypt at the default work factor takes a moment")
	}
	f := emptyRepo(t)
	f.withPassphrase("a new passphrase")
	if _, err := InitKeys(f.ctx); err != nil {
		t.Fatal(err)
	}
	if args := scryptArgs(t, f.p("identity.age")); args[1] != "18" {
		t.Errorf("work factor = %s, want age's default 18", args[1])
	}
}

func TestInitKeysMakesUpAPassphraseForAnEmptyAnswer(t *testing.T) {
	f := emptyRepo(t)
	f.withNewKeyHooks("")
	if _, err := InitKeys(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := age.NewScryptIdentity(""); err == nil {
		t.Fatal("age accepts an empty passphrase, so this test proves nothing")
	}
	id, err := age.NewScryptIdentity("x")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := os.ReadFile(f.p("identity.age"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := age.Decrypt(bytes.NewReader(sealed), id); err == nil {
		t.Error("identity.age opens with a guess")
	}
}

func TestMadeUpPassphrasesAreTenListWords(t *testing.T) {
	list := words()
	if len(list) != 2048 || list[0] != "abandon" || list[2047] != "zoo" {
		t.Fatalf("word list: %d words, %q to %q", len(list), list[0], list[len(list)-1])
	}
	seen := map[string]bool{}
	for range 20 {
		p := madeUpPassphrase()
		parts := strings.Split(p, "-")
		if len(parts) != 10 {
			t.Fatalf("%q has %d words", p, len(parts))
		}
		for _, w := range parts {
			if !slices.Contains(list, w) {
				t.Errorf("%q is not a list word", w)
			}
		}
		seen[p] = true
	}
	if len(seen) != 20 {
		t.Errorf("20 passphrases, %d different", len(seen))
	}
}

func TestInitKeysRefusesWhenKeysExist(t *testing.T) {
	cases := map[string]func(f *fixture){
		"identity.age":   func(f *fixture) { f.write("identity.age", "x") },
		"recipients.txt": func(f *fixture) { f.write("recipients.txt", "x") },
		"a dangling identity link": func(f *fixture) {
			if err := os.Symlink(filepath.Join(f.sb.Dir, "nowhere"), f.p("identity.age")); err != nil {
				f.t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := emptyRepo(t)
			setup(f)
			f.withNewKeyHooks("pass")
			_, err := InitKeys(f.ctx)
			wantUserError(t, f.ctx, err, msg.SecretsErrorKeysExist())
		})
	}
}

func TestInitKeysNamesTheFileItCouldNotSave(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into read-only folders")
	}
	f := emptyRepo(t)
	if err := os.MkdirAll(f.p(""), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.p(""), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.p(""), 0o755) })
	f.withNewKeyHooks("pass")
	_, err := InitKeys(f.ctx)
	var userErr *fail.UserError
	want := voice.Plain(f.ctx, msg.SecretsErrorSaveKey("private/identity.age")) + "\n  "
	if !errors.As(err, &userErr) || !strings.HasPrefix(err.Error(), want) || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want %q and the reason", err, want)
	}
	if f.exists("identity.age") || f.exists("recipients.txt") {
		t.Error("saved keys")
	}
}

// D14: keys init writes private/.gitignore when there is none, so the
// plaintext later packed or unlocked into private/ is never staged.
func TestInitKeysWritesPrivateGitignoreWhenMissing(t *testing.T) {
	f := emptyRepo(t)
	f.remove(".gitignore")
	f.withNewKeyHooks("pass")
	if _, err := InitKeys(f.ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.read(".gitignore"); got != PrivateGitignore {
		t.Errorf("private/.gitignore = %q, want %q", got, PrivateGitignore)
	}
}

func TestInitKeysKeepsAnExistingPrivateGitignore(t *testing.T) {
	for name, setup := range map[string]func(f *fixture){
		"a file of the person's own": func(f *fixture) { f.write(".gitignore", "# mine\n*\n!*.age\n") },
		"a link": func(f *fixture) {
			f.write("../my-gitignore", "# linked\n")
			f.remove(".gitignore")
			if err := os.Symlink("../my-gitignore", f.p(".gitignore")); err != nil {
				f.t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := emptyRepo(t)
			setup(f)
			before, _ := os.Lstat(f.p(".gitignore"))
			want := f.read(".gitignore")
			f.withNewKeyHooks("pass")
			if _, err := InitKeys(f.ctx); err != nil {
				t.Fatal(err)
			}
			after, _ := os.Lstat(f.p(".gitignore"))
			if got := f.read(".gitignore"); got != want || before.Mode() != after.Mode() {
				t.Errorf("private/.gitignore = %q (%v), want it kept as %q (%v)", got, after.Mode(), want, before.Mode())
			}
		})
	}
}

func TestInitKeysStopsWhenItCannotWritePrivateGitignore(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into read-only folders")
	}
	f := emptyRepo(t)
	f.remove(".gitignore")
	if err := os.Chmod(f.p(""), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.p(""), 0o755) })
	f.withNewKeyHooks("pass")
	_, err := InitKeys(f.ctx)
	var userErr *fail.UserError
	want := voice.Plain(f.ctx, msg.SecretsErrorSaveGitignore("private/.gitignore")) + "\n  "
	if !errors.As(err, &userErr) || !strings.HasPrefix(err.Error(), want) || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want %q and the reason", err, want)
	}
	if f.exists("identity.age") || f.exists("recipients.txt") {
		t.Error("saved keys without private/.gitignore")
	}
}

func TestInitKeysKeepsTheKeyWhenRecipientsWasSavedBeforeAnError(t *testing.T) {
	f := emptyRepo(t)
	f.withNewKeyHooks("pass")
	orig := publishFile
	t.Cleanup(func() { publishFile = orig })
	publishFile = func(path string, data []byte, perm fs.FileMode) error {
		if err := orig(path, data, perm); err != nil {
			return err
		}
		if filepath.Base(path) == "recipients.txt" {
			return errors.New("sync private: input/output error")
		}
		return nil
	}
	public, err := InitKeys(f.ctx)
	if err != nil {
		t.Fatalf("err = %v, want success: both files are in place", err)
	}
	if got := f.read("recipients.txt"); got != public+"\n" {
		t.Errorf("recipients.txt = %q", got)
	}
	if !f.exists("identity.age") {
		t.Fatal("identity.age removed while recipients.txt names its key")
	}
}

func TestInitKeysRemovesTheKeyWhenRecipientsWasNotSaved(t *testing.T) {
	f := emptyRepo(t)
	f.withNewKeyHooks("pass")
	orig := publishFile
	t.Cleanup(func() { publishFile = orig })
	publishFile = func(path string, data []byte, perm fs.FileMode) error {
		if filepath.Base(path) == "recipients.txt" {
			return errors.New("disk full")
		}
		return orig(path, data, perm)
	}
	_, err := InitKeys(f.ctx)
	var userErr *fail.UserError
	want := voice.Plain(f.ctx, msg.SecretsErrorSaveKey("private/recipients.txt"))
	if !errors.As(err, &userErr) || !strings.HasPrefix(userErr.Msg, want) {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if f.exists("identity.age") || f.exists("recipients.txt") {
		t.Error("left half a key pair behind")
	}
}

func TestInitKeysWithoutATerminalSavesNothing(t *testing.T) {
	f := emptyRepo(t)
	noTerminal(t)
	_, err := InitKeys(f.ctx)
	wantUserError(t, f.ctx, err, msg.SecretsErrorNoTerminal())
	if f.exists("identity.age") || f.exists("recipients.txt") {
		t.Error("saved keys")
	}
}

func TestInitKeysRejectsABadTestWorkFactor(t *testing.T) {
	for _, v := range []string{"0", "31", "ten", ""} {
		f := emptyRepo(t)
		f.ctx.Env = f.ctx.Env.With(map[string]string{"HOMEBOT_TEST_SCRYPT_WORK_FACTOR": v, "HOMEBOT_TEST_PASSPHRASE": "p"})
		if _, err := InitKeys(f.ctx); err == nil || !strings.Contains(err.Error(), "HOMEBOT_TEST_SCRYPT_WORK_FACTOR") {
			t.Errorf("%q: err = %v", v, err)
		}
	}
}

// TestReleaseBuildIgnoresTheTestHooks plays a release build, where
// testing.Testing is false: the hooks are set but nothing answers the
// prompt, and the work factor stays age's.
func TestReleaseBuildIgnoresTheTestHooks(t *testing.T) {
	saved := inTest
	inTest = func() bool { return false }
	t.Cleanup(func() { inTest = saved })
	noTerminal(t)

	f := newFixture(t)
	f.withNewKeyHooks(testutil.TestPassphrase)
	if _, ok := testPassphrase(f.ctx.Env); ok {
		t.Error("testPassphrase answered in a release build")
	}
	if _, lowered, err := testWorkFactor(f.ctx.Env); lowered || err != nil {
		t.Errorf("testWorkFactor = %v, %v in a release build", lowered, err)
	}
	_, err := Unlock(f.ctx, UnlockOptions{})
	wantUserError(t, f.ctx, err, msg.SecretsErrorNoTerminal())

	repo := emptyRepo(t)
	repo.withNewKeyHooks(testutil.TestPassphrase)
	_, err = InitKeys(repo.ctx)
	wantUserError(t, repo.ctx, err, msg.SecretsErrorNoTerminal())
	if repo.exists("identity.age") {
		t.Error("InitKeys saved a key")
	}
}

// ── the repo's own files ──

func TestListSkipsSecretsNamedAfterTheReposOwnFiles(t *testing.T) {
	f := newFixture(t)
	for _, rel := range []string{
		// Plaintext that would seal over identity.age.
		"identity",
		// .age files that would unlock over the repo's own files.
		"recipients.txt.age", "README.md.age", ".gitignore.age", ".gitkeep.age", "identity.age.age",
		"common/README.md.age", "linux/.gitignore.age", "common/.gitkeep.age",
	} {
		f.write(rel, "x")
	}
	f.write("common/a", "a real secret")
	f.wantStates(map[string]State{"common/a": New})
}

// TestListSkipsTheReposOwnFilesInAnyCase: on a case-insensitive
// filesystem, macOS's by default, private/Identity.age is
// private/identity.age and readme.md is README.md. The names are checked
// without case on every system, so a repo means the same on all of them.
func TestListSkipsTheReposOwnFilesInAnyCase(t *testing.T) {
	f := newFixture(t)
	for _, rel := range []string{
		// Plaintext that would seal over identity.age.
		"Identity", "IDENTITY",
		// Plaintext that is one of the repo's own files.
		"common/Readme.md", "linux/.GitIgnore", "common/.GITKEEP",
		// .age files that would unlock over the repo's own files.
		"readme.md.age", "Recipients.TXT.age", ".GitIgnore.age", ".GITKEEP.age", "Identity.AGE.age",
		"common/readme.MD.age", "linux/.gitIgnore.age", ".AGE",
	} {
		f.write(rel, "x")
	}
	f.write("common/a", "a real secret")
	f.wantStates(map[string]State{"common/a": New})
}

func TestReservedIgnoresCase(t *testing.T) {
	for rel, want := range map[string]bool{
		"identity.age": true, "Identity.age": true, "IDENTITY.AGE": true,
		"recipients.txt": true, "Recipients.Txt": true,
		"README.md": true, "readme.md": true, "ReadMe.MD": true,
		".gitignore": true, ".GITIGNORE": true, ".GitKeep": true, ".Age": true,
		"common/readme.md": true, "linux/.GitIgnore": true,
		// identity.age and recipients.txt are reserved at the top only.
		"common/Identity.age": false, "linux/RECIPIENTS.TXT": false,
		"identity": false, "readme": false, "readme.md.bak": false,
	} {
		if got := reserved(rel); got != want {
			t.Errorf("reserved(%q) = %v, want %v", rel, got, want)
		}
	}
	l := newFixture(t).ctx.Layout()
	for _, name := range []string{"Identity.age", "IDENTITY.AGE", "Recipients.txt", "readme.md", "common/README.MD"} {
		if !reservedTarget(l, filepath.Join(l.Private, filepath.FromSlash(name))) {
			t.Errorf("reservedTarget(private/%s) = false", name)
		}
	}
	if reservedTarget(l, filepath.Join(l.Private, "common", "identity.age")) {
		t.Error("reservedTarget(private/common/identity.age) = true")
	}
}

func TestLockLeavesIdentityAgeAloneBesidePlaintextNamedIdentity(t *testing.T) {
	for _, plain := range []string{"identity", "Identity", "IDENTITY"} {
		for name, opts := range map[string]LockOptions{"lock": {}, "lock --ours": {Ours: true}, "lock --wipe": {Wipe: true}} {
			t.Run(plain+"/"+name, func(t *testing.T) {
				f := newFixture(t)
				before := f.read("identity.age")
				f.write(plain, "AGE-SECRET-KEY-1 pasted here by mistake\n")
				r, err := Lock(f.ctx, opts)
				if err != nil {
					t.Fatal(err)
				}
				eq(t, "result", r, LockResult{})
				if f.read("identity.age") != before {
					t.Fatal("identity.age changed")
				}
				eq(t, "scrypt stanza", scryptArgs(t, f.p("identity.age"))[1], strconv.Itoa(testutil.TestWorkFactor))
				if !f.exists(plain) {
					t.Error("the plaintext was wiped")
				}
				if got := names(t, f.p("")); slices.ContainsFunc(got, func(n string) bool { return n != "identity.age" && strings.EqualFold(n, "identity.age") }) {
					t.Errorf("sealed beside identity.age: %v", got)
				}
				f.withPassphrase(testutil.TestPassphrase)
				if _, err := Unlock(f.ctx, UnlockOptions{}); err != nil {
					t.Errorf("unlock after lock: %v", err)
				}
			})
		}
	}
}

// TestUnlockLeavesTheReadmeAloneBesideASecretNamedInAnotherCase: a Linux
// machine could seal private/readme.md; on a Mac unlocking it would write
// over README.md.
func TestUnlockLeavesTheReadmeAloneBesideASecretNamedInAnotherCase(t *testing.T) {
	f := newFixture(t)
	f.write("README.md", "the repo's readme\n")
	f.write("readme.md.age", f.sealed("a secret sealed on Linux\n"))
	f.write("common/Readme.md.age", f.sealed("another\n"))
	f.wantStates(map[string]State{})
	for _, opts := range []UnlockOptions{{}, {Theirs: true}} {
		eq(t, "unlocked", f.unlock(opts), UnlockResult{})
	}
	if got := f.read("README.md"); got != "the repo's readme\n" {
		t.Errorf("README.md became %q", got)
	}
	if got := names(t, f.p("")); slices.Contains(got, "readme.md") {
		t.Errorf("unlocked readme.md: %v", got)
	}
	if got := names(t, f.p("common")); slices.Contains(got, "Readme.md") {
		t.Errorf("unlocked common/Readme.md: %v", got)
	}
}

// names lists the entries of dir as the filesystem spells them.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestSealOneNeverWritesOverTheReposOwnFiles(t *testing.T) {
	f := newFixture(t)
	recipients := []age.Recipient{f.ids[0].(*age.X25519Identity).Recipient()}
	before := f.read("identity.age")
	f.write("identity", "plaintext")
	_, err := sealOne(f.ctx, "identity", recipients)
	wantUserError(t, f.ctx, err, msg.SecretsErrorReserved("private/identity.age"))
	if f.read("identity.age") != before {
		t.Error("identity.age changed")
	}

	for _, rel := range []string{"Identity", "IDENTITY", "iDeNtItY"} {
		f.write(rel, "plaintext")
		_, err = sealOne(f.ctx, rel, recipients)
		wantUserError(t, f.ctx, err, msg.SecretsErrorReserved("private/"+rel+".age"))
		if f.read("identity.age") != before {
			t.Errorf("%s: identity.age changed", rel)
		}
	}

	f.write("../outside", "plaintext")
	_, err = sealOne(f.ctx, "../outside", recipients)
	wantUserError(t, f.ctx, err, msg.SecretsErrorReserved("outside.age"))
	if _, err := os.Lstat(filepath.Join(f.sb.Root, "outside.age")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("sealed outside private/: %v", err)
	}
}

func TestUnlockOneNeverWritesOverTheReposOwnFiles(t *testing.T) {
	for _, rel := range []string{
		"recipients.txt", "identity.age", "README.md", ".gitignore", ".gitkeep",
		"common/README.md", "linux/.gitignore", "../outside",
		"Recipients.TXT", "IDENTITY.AGE", "readme.md", ".GitIgnore", "common/Readme.md", "linux/.GITKEEP",
	} {
		t.Run(rel, func(t *testing.T) {
			f := newFixture(t)
			f.write(rel, "the repo's own\n")
			var sealed bytes.Buffer
			if err := encrypt(&sealed, []byte("a secret\n"), f.ids[0].(*age.X25519Identity).Recipient()); err != nil {
				t.Fatal(err)
			}
			f.write(rel+".age", sealed.String())
			for _, st := range []State{Locked, Upstream, Unverified} {
				_, _, err := unlockOne(f.ctx, rel, st, f.ids, true)
				var userErr *fail.UserError
				if !errors.As(err, &userErr) || !strings.HasPrefix(userErr.Msg, "Refusing to write over ") {
					t.Fatalf("%s: err = %v, want the refusal", st, err)
				}
				if got := f.read(rel); got != "the repo's own\n" {
					t.Fatalf("%s: written over: %q", st, got)
				}
			}
		})
	}
}

func TestListSkipsTempFilesACrashLeft(t *testing.T) {
	f := newFixture(t)
	f.write("common/a.tmp-1x2y3z", "half a plaintext")
	f.write("common/a.age.tmp-0", "half a seal")
	f.write("linux/token.tmp-zzzzzzz", "x")
	f.write("common/b", "real")
	f.wantStates(map[string]State{"common/b": New})
	eq(t, "sealed", f.seal(), []string{"common/b"})
	for _, rel := range []string{"common/a.tmp-1x2y3z.age", "common/a.age", "linux/token.tmp-zzzzzzz.age"} {
		if f.exists(rel) {
			t.Errorf("sealed a temp file: %s", rel)
		}
	}
}

// TestUnlockLeavesAPlaintextWithATempNameAlone is the re-review's repro: a
// .age file whose secret has a temp file's name was listed without its
// plaintext, so unlock wrote the repo's version over a local edit.
func TestUnlockLeavesAPlaintextWithATempNameAlone(t *testing.T) {
	f := newFixture(t)
	sealed := f.sealed("the repo's version\n")
	f.write("common/vpn.tmp-old", "a local edit\n")
	f.write("common/vpn.tmp-old.age", sealed)
	f.write("linux/token.tmp-0.age", sealed)
	f.wantStates(map[string]State{})
	for _, opts := range []UnlockOptions{{}, {Theirs: true}, {}} {
		eq(t, "unlocked", f.unlock(opts), UnlockResult{})
	}
	if got := f.read("common/vpn.tmp-old"); got != "a local edit\n" {
		t.Errorf("the local edit became %q", got)
	}
	if f.exists("linux/token.tmp-0") {
		t.Error("unlocked a .age file with a temp file's name")
	}
	eq(t, "sealed", f.seal(), []string(nil))
	if f.read("common/vpn.tmp-old.age") != sealed {
		t.Error("sealed over the repo's .age file")
	}
}

// sealed returns text sealed to the fixture's key, as another machine
// would have sealed it.
func (f *fixture) sealed(text string) string {
	f.t.Helper()
	var b bytes.Buffer
	if err := encrypt(&b, []byte(text), f.ids[0].(*age.X25519Identity).Recipient()); err != nil {
		f.t.Fatal(err)
	}
	return b.String()
}

// ── damaged files ──

// damages are the ways a .age file is damaged in transit: cut short, or a
// byte of its payload flipped.
var damages = []string{"truncated", "payload flipped"}

// damage returns sealed damaged as how says.
func damage(sealed []byte, how string) []byte {
	if how == "truncated" {
		return sealed[:len(sealed)-10]
	}
	flipped := bytes.Clone(sealed)
	flipped[len(flipped)-1] ^= 0x01
	return flipped
}

func TestUnlockLeavesPlaintextAloneWhenTheAgeFileIsDamaged(t *testing.T) {
	setups := map[string]struct {
		opts  UnlockOptions
		setup func(f *fixture)
		state State
	}{
		"upstream":           {UnlockOptions{}, func(*fixture) {}, Upstream},
		"upstream, theirs":   {UnlockOptions{Theirs: true}, func(*fixture) {}, Upstream},
		"conflict, theirs":   {UnlockOptions{Theirs: true}, func(f *fixture) { f.write("common/a", "local edit") }, Conflict},
		"unverified, theirs": {UnlockOptions{Theirs: true}, func(f *fixture) { f.otherMachine(); f.write("common/a", "local edit") }, Unverified},
	}
	for name, s := range setups {
		for _, how := range damages {
			t.Run(name+", "+how, func(t *testing.T) {
				f := newFixture(t)
				f.write("common/a", "base")
				f.seal()
				f.write("common/a.age", string(damage([]byte(f.read("common/a.age")), how)))
				s.setup(f)
				f.wantStates(map[string]State{"common/a": s.state})
				before := f.read("common/a")
				_, err := unlockWith(f.ctx, f.ids, s.opts)
				if err == nil || !strings.HasPrefix(err.Error(), "Could not decrypt private/common/a.age.\n  age: ") {
					t.Fatalf("err = %v, want the decrypt failure with age's reason", err)
				}
				if got := f.read("common/a"); got != before {
					t.Errorf("plaintext = %q, want %q kept", got, before)
				}
				f.wantStates(map[string]State{"common/a": s.state})
			})
		}
	}
}

// sealedKey returns a private key file sealed with the test passphrase.
func sealedKey(t *testing.T) []byte {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	r, err := age.NewScryptRecipient(testutil.TestPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	r.SetWorkFactor(testutil.TestWorkFactor)
	var b bytes.Buffer
	if err := encrypt(&b, []byte(id.String()+"\n"), r); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestUnlockReportsADamagedIdentityAfterTheRightPassphrase(t *testing.T) {
	sealed := sealedKey(t)
	cases := map[string][]byte{}
	for _, how := range damages {
		cases[how] = damage(sealed, how)
	}
	// The nonce follows the header; cut it short.
	header := bytes.Index(sealed, []byte("\n--- "))
	end := header + 1 + bytes.IndexByte(sealed[header+1:], '\n') + 1
	cases["nonce cut short"] = sealed[:end+5]
	// A well-formed header MAC that does not match.
	mac := bytes.Clone(sealed)
	copy(mac[header+len("\n--- "):], strings.Repeat("A", 43))
	cases["bad header MAC"] = mac
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if err := os.WriteFile(f.p("identity.age"), content, 0o644); err != nil {
				t.Fatal(err)
			}
			f.withPassphrase(testutil.TestPassphrase)
			_, err := Unlock(f.ctx, UnlockOptions{})
			wantUserError(t, f.ctx, err, msg.SecretsErrorIdentityUnreadable())
		})
	}
	// The same file, whole, with a wrong passphrase.
	f := newFixture(t)
	if err := os.WriteFile(f.p("identity.age"), sealed, 0o644); err != nil {
		t.Fatal(err)
	}
	f.withPassphrase("not it")
	_, err := Unlock(f.ctx, UnlockOptions{})
	wantUserError(t, f.ctx, err, msg.SecretsErrorPassphrase())
}

// ── recipients ──

func TestSealRefusesRecipientsWithoutAKey(t *testing.T) {
	for name, content := range map[string]string{
		"empty":         "",
		"blank lines":   "\n\n",
		"comments only": "# the public key used to be here\n\n# age1...\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.write("recipients.txt", content)
			f.write("common/a", "1")
			_, err := Seal(f.ctx)
			if err == nil || err.Error() != voice.Record(f.ctx, msg.SecretsErrorEncrypt("common/a"))+"\n  "+voice.Record(f.ctx, msg.SecretsErrorEmptyRecipients()) {
				t.Errorf("err = %v, want secret path and recipient reason", err)
			}
			if f.exists("common/a.age") {
				t.Error("wrote a .age file")
			}
		})
	}
}

func TestSealRecipientFailureNamesFirstOfTwoSecrets(t *testing.T) {
	f := newFixture(t)
	f.write("recipients.txt", "")
	f.write("common/a", "one")
	f.write("common/b", "two")
	_, err := Seal(f.ctx)
	if err == nil || !strings.Contains(err.Error(), "private/common/a") || strings.Contains(err.Error(), "private/common/b") {
		t.Errorf("seal error = %v, want first candidate path only", err)
	}
}

func TestSealToEveryRecipient(t *testing.T) {
	f := newFixture(t)
	other, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	ours := f.ids[0].(*age.X25519Identity).Recipient()
	f.write("recipients.txt", "# this machine\n"+ours.String()+"\n\n# the laptop\n"+other.Recipient().String()+"\n")
	f.write("common/a", "for both")
	f.seal()
	for name, id := range map[string]age.Identity{"first": f.ids[0], "second": other} {
		if got := open(t, f.p("common/a.age"), []age.Identity{id}); got != "for both" {
			t.Errorf("%s key decrypts %q", name, got)
		}
	}
}

// sshKey returns a new ed25519 SSH key pair: the authorized_keys line of
// its public key, with a comment, and the identity that decrypts for it.
func sshKey(t *testing.T) (string, age.Identity) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	id, err := agessh.NewEd25519Identity(priv)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(sshPub)), "\n") + " me@laptop", id
}

// rsaSSHKey makes an ssh-rsa key of bits and returns its authorized_keys
// line and its private half as an age identity.
func rsaSSHKey(t *testing.T, bits int) (string, age.Identity) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	id, err := agessh.NewRSAIdentity(priv)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(sshPub)), "\n") + " me@laptop", id
}

// unsupportedSSHKeys are well-formed ssh public keys of the types age
// cannot encrypt to, by type.
func unsupportedSSHKeys(t *testing.T) map[string]string {
	t.Helper()
	line := func(keyType string, key any) string {
		return keyType + " " + base64.StdEncoding.EncodeToString(ssh.Marshal(key)) + " me@key"
	}
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaPub, err := ssh.NewPublicKey(&ecdsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	point, err := ecdsaKey.PublicKey.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	edPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"ecdsa-sha2-nistp256": strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(ecdsaPub)), "\n") + " me@laptop",
		"sk-ssh-ed25519@openssh.com": line("sk-ssh-ed25519@openssh.com", struct {
			Name string
			Key  []byte
			App  string
		}{"sk-ssh-ed25519@openssh.com", edPub, "ssh:"}),
		"sk-ecdsa-sha2-nistp256@openssh.com": line("sk-ecdsa-sha2-nistp256@openssh.com", struct {
			Name, Curve string
			Key         []byte
			App         string
		}{"sk-ecdsa-sha2-nistp256@openssh.com", "nistp256", point.Bytes(), "ssh:"}),
		"ssh-dss": line("ssh-dss", struct {
			Name       string
			P, Q, G, Y *big.Int
		}{"ssh-dss", big.NewInt(23), big.NewInt(11), big.NewInt(4), big.NewInt(8)}),
	}
}

func TestSealToAnSSHRSAKey(t *testing.T) {
	f := newFixture(t)
	line, rsaID := rsaSSHKey(t, 2048)
	ours := f.ids[0].(*age.X25519Identity).Recipient()
	f.write("recipients.txt", ours.String()+"\n"+line+"\n")
	f.write("common/a", "for both")
	f.seal()
	for name, id := range map[string]age.Identity{"age": f.ids[0], "ssh-rsa": rsaID} {
		if got := open(t, f.p("common/a.age"), []age.Identity{id}); got != "for both" {
			t.Errorf("the %s key decrypts %q", name, got)
		}
	}
}

// TestSealRefusesSSHKeysAgeCannotEncryptTo: the age CLI skipped them with a
// warning and sealed to the other keys; homebot never seals to fewer keys
// than the file lists.
func TestSealRefusesSSHKeysAgeCannotEncryptTo(t *testing.T) {
	keys := unsupportedSSHKeys(t)
	keys["ssh-rsa"], _ = rsaSSHKey(t, 1024)
	for keyType, line := range keys {
		t.Run(keyType, func(t *testing.T) {
			f := newFixture(t)
			ours := f.ids[0].(*age.X25519Identity).Recipient()
			f.write("recipients.txt", ours.String()+"\n# the old laptop\n"+line+"\n")
			f.write("common/a", "1")
			r, err := Seal(f.ctx)
			want := voice.Record(f.ctx, msg.SecretsErrorEncrypt("common/a")) + "\n  " + voice.Record(f.ctx, msg.SecretsErrorUnsupportedSSHKey(3, keyType))
			if err == nil || err.Error() != want {
				t.Errorf("err = %v, want %q", err, want)
			}
			eq(t, "sealed", r.Sealed, []string(nil))
			if f.exists("common/a.age") {
				t.Error("wrote a .age file")
			}
			f.wantStates(map[string]State{"common/a": New})
		})
	}
}

func TestUnsupportedSSHKeyIsTheAgeCLIsTest(t *testing.T) {
	ed, _ := sshKey(t)
	rsaBig, _ := rsaSSHKey(t, 2048)
	ecdsaLine := unsupportedSSHKeys(t)["ecdsa-sha2-nistp256"]
	ecdsaBlob := strings.Fields(ecdsaLine)[1]
	for line, want := range map[string]string{
		ed:                                    "",
		rsaBig:                                "",
		ecdsaLine:                             "ecdsa-sha2-nistp256",
		"ecdsa-sha2-nistp256 " + ecdsaBlob:    "ecdsa-sha2-nistp256",
		"# " + ecdsaLine:                      "",
		"ssh-dss " + ecdsaBlob:                "",
		"ecdsa-sha2-nistp256 not-base64":      "",
		"ecdsa-sha2-nistp256":                 "",
		"ecdsa-sha2-nistp256  " + ecdsaBlob:   "",
		"age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq": "",
		"":                                    "",
	} {
		got, ok := unsupportedSSHKey(line)
		if got != want || ok != (want != "") {
			t.Errorf("unsupportedSSHKey(%.40q) = %q, %v, want %q", line, got, ok, want)
		}
	}
}

func TestSealToAnSSHKey(t *testing.T) {
	t.Run("beside an age key", func(t *testing.T) {
		f := newFixture(t)
		line, sshID := sshKey(t)
		ours := f.ids[0].(*age.X25519Identity).Recipient()
		f.write("recipients.txt", ours.String()+"\n"+line+"\n")
		f.write("common/a", "for both")
		f.seal()
		for name, id := range map[string]age.Identity{"age": f.ids[0], "ssh": sshID} {
			if got := open(t, f.p("common/a.age"), []age.Identity{id}); got != "for both" {
				t.Errorf("the %s key decrypts %q", name, got)
			}
		}
	})
	t.Run("alone", func(t *testing.T) {
		f := newFixture(t)
		line, sshID := sshKey(t)
		f.write("recipients.txt", "# ssh only\n"+line+"\n")
		f.write("common/a", "for ssh")
		f.seal()
		if got := open(t, f.p("common/a.age"), []age.Identity{sshID}); got != "for ssh" {
			t.Errorf("the ssh key decrypts %q", got)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		f := newFixture(t)
		ours := f.ids[0].(*age.X25519Identity).Recipient()
		f.write("recipients.txt", ours.String()+"\nssh-ed25519 not-base64\n")
		f.write("common/a", "1")
		_, err := Seal(f.ctx)
		if err == nil || !strings.HasPrefix(err.Error(), "Could not encrypt private/common/a.\n  age: error at line 2: ") {
			t.Fatalf("err = %v, want age's reason with the line", err)
		}
		if f.exists("common/a.age") {
			t.Error("wrote a .age file")
		}
	})
	t.Run("a bad age key keeps its line number", func(t *testing.T) {
		f := newFixture(t)
		line, _ := sshKey(t)
		f.write("recipients.txt", line+"\n# comment\nage1notakey\n")
		f.write("common/a", "1")
		_, err := Seal(f.ctx)
		if err == nil || !strings.HasPrefix(err.Error(), "Could not encrypt private/common/a.\n  age: error at line 3: ") {
			t.Fatalf("err = %v, want age's reason at line 3", err)
		}
	})
}

func TestOnlyAgesOwnErrorsAreLabelledAsAges(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable files")
	}
	f := newFixture(t)
	f.write("common/a", "1")
	if err := os.Chmod(f.p("common/a"), 0o000); err != nil {
		t.Fatal(err)
	}
	_, err := Seal(f.ctx)
	if err == nil || !strings.HasPrefix(err.Error(), "Could not encrypt private/common/a.\n  ") || strings.Contains(err.Error(), "\n  age: ") {
		t.Errorf("err = %v, want the read error unlabelled", err)
	}
	if err := os.Chmod(f.p("common/a"), 0o644); err != nil {
		t.Fatal(err)
	}

	f.seal()
	f.remove("common/a")
	if err := os.Chmod(f.p("common/a.age"), 0o000); err != nil {
		t.Fatal(err)
	}
	_, err = unlockWith(f.ctx, f.ids, UnlockOptions{})
	if err == nil || !strings.HasPrefix(err.Error(), "Could not decrypt private/common/a.age.\n  ") || strings.Contains(err.Error(), "\n  age: ") {
		t.Errorf("err = %v, want the read error unlabelled", err)
	}
}
