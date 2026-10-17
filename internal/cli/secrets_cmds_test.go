package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// secretsSandbox is a sandbox whose passphrase prompts get pass, with a low
// scrypt work factor for new keys.
func secretsSandbox(t *testing.T, pass string) *testutil.Box {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{})
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{
		"HOMEBOT_TEST_PASSPHRASE":         pass,
		"HOMEBOT_TEST_SCRYPT_WORK_FACTOR": "10",
	})
	return sb
}

// secretsRun runs homebot with args in sb and returns stdout and the error.
func secretsRun(t *testing.T, sb *testutil.Box, args ...string) (string, error) {
	t.Helper()
	sb.Stdout.Reset()
	sb.Stderr.Reset()
	root := NewRoot(sb.Ctx, Build{Version: "test"})
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	if sb.Stderr.Len() > 0 {
		t.Errorf("stderr = %q", sb.Stderr)
	}
	return sb.Stdout.String(), err
}

func secretsWrite(t *testing.T, sb *testutil.Box, rel, content string) {
	t.Helper()
	sb.Files(map[string]string{"home/.home/private/" + rel: content})
}

func TestKeysWithoutInitShowsItsUsage(t *testing.T) {
	for _, args := range [][]string{{"keys"}, {"keys", "frobnicate"}} {
		sb := testutil.Sandbox(t, testutil.Options{})
		out, err := secretsRun(t, sb, args...)
		var userErr *fail.UserError
		if !errors.As(err, &userErr) || err.Error() != "Usage: homebot keys init" {
			t.Errorf("%v: err = %v, want the usage", args, err)
		}
		if out != "" {
			t.Errorf("%v: stdout = %q", args, out)
		}
	}
}

func TestKeysInitPrintsThePublicKeyAndWhatToCommit(t *testing.T) {
	sb := secretsSandbox(t, "a passphrase")
	out, err := secretsRun(t, sb, "keys", "init")
	if err != nil {
		t.Fatal(err)
	}
	recipients, err := os.ReadFile(filepath.Join(sb.Root, "private", "recipients.txt"))
	if err != nil {
		t.Fatal(err)
	}
	public := strings.TrimSuffix(string(recipients), "\n")
	if !regexp.MustCompile(`^age1[a-z0-9]{58}$`).MatchString(public) {
		t.Fatalf("recipients.txt holds %q", recipients)
	}
	want := "Creating your secrets key. homebot asks for a passphrase next.\n" +
		"Leave it empty and homebot makes up a strong one and prints it. Save it in your\n" +
		"password manager: without it, the secrets in this repo can't be recovered.\n" +
		"\n" +
		"\n" +
		"Keys created. The public key:\n" +
		"  " + public + "\n" +
		"Commit private/identity.age and private/recipients.txt:\n" +
		"  homebot save\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "a passphrase\n") || strings.Contains(out, "AGE-SECRET-KEY") {
		t.Error("stdout holds the passphrase or the private key")
	}

	_, err = secretsRun(t, sb, "keys", "init")
	if err == nil || !strings.HasPrefix(err.Error(), "Keys already exist") {
		t.Errorf("second keys init: err = %v", err)
	}
}

func TestLockSaysWhatItSealed(t *testing.T) {
	sb := secretsSandbox(t, testutil.TestPassphrase)
	testutil.TestKeys(t, sb.Root)

	out, err := secretsRun(t, sb, "lock")
	if err != nil || out != "Nothing to seal.\n" {
		t.Errorf("nothing to seal: %q, %v", out, err)
	}

	secretsWrite(t, sb, "common/b", "2")
	secretsWrite(t, sb, "common/a", "1")
	out, err = secretsRun(t, sb, "lock")
	if err != nil || out != "sealed private/common/a, private/common/b\n" {
		t.Errorf("lock: %q, %v", out, err)
	}

	secretsWrite(t, sb, "linux/c", "3")
	out, err = secretsRun(t, sb, "lock", "--wipe")
	if err != nil || out != "Sealed 1, removed 3 plaintext files.\n" {
		t.Errorf("lock --wipe: %q, %v", out, err)
	}
}

func TestLockWipeNamesWhatItKept(t *testing.T) {
	sb := secretsSandbox(t, testutil.TestPassphrase)
	testutil.TestKeys(t, sb.Root)
	secretsWrite(t, sb, "common/a", "base")
	if _, err := secretsRun(t, sb, "lock"); err != nil {
		t.Fatal(err)
	}
	// Forgetting the records makes common/a unverified: not safely sealed.
	if err := os.RemoveAll(sb.Ctx.StateDir); err != nil {
		t.Fatal(err)
	}
	out, err := secretsRun(t, sb, "lock", "--wipe")
	want := "Sealed 0, removed 0 plaintext files.\n" +
		"kept private/common/a (not safely sealed; resolve it first)\n"
	if err != nil || out != want {
		t.Errorf("lock --wipe: %q, %v; want %q", out, err, want)
	}
}

func TestUnlockSaysWhatItUnlockedVerifiedAndLeft(t *testing.T) {
	sb := secretsSandbox(t, testutil.TestPassphrase)
	testutil.TestKeys(t, sb.Root)

	out, err := secretsRun(t, sb, "unlock")
	if err != nil || out != "unlocked 0 secrets\n" {
		t.Errorf("nothing to unlock: %q, %v", out, err)
	}

	secretsWrite(t, sb, "common/a", "same")
	secretsWrite(t, sb, "common/b", "committed")
	secretsWrite(t, sb, "common/c", "locked")
	if _, err := secretsRun(t, sb, "lock", "--wipe"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sb.Ctx.StateDir); err != nil {
		t.Fatal(err)
	}
	secretsWrite(t, sb, "common/a", "same")
	secretsWrite(t, sb, "common/b", "local")
	out, err = secretsRun(t, sb, "unlock")
	want := "unlocked 1 secret, verified 1\n" +
		"conflict private/common/b: changed here and in the repo\n" +
		"Keep this machine's versions:\n" +
		"  homebot lock --ours\n" +
		"Or take the repo's:\n" +
		"  homebot unlock --theirs\n" +
		"linked 3 secrets\n"
	if err != nil || out != want {
		t.Errorf("unlock: %v\n%s\nwant:\n%s", err, out, want)
	}

	out, err = secretsRun(t, sb, "unlock", "--theirs")
	if err != nil || out != "unlocked 1 secret\n" {
		t.Errorf("unlock --theirs: %q, %v", out, err)
	}
}

func TestUnlockWithAWrongPassphrase(t *testing.T) {
	sb := secretsSandbox(t, "not it")
	testutil.TestKeys(t, sb.Root)
	out, err := secretsRun(t, sb, "unlock")
	if err == nil || err.Error() != "Could not decrypt private/identity.age (wrong passphrase?)" {
		t.Errorf("err = %v", err)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
}

func TestUnlockLinksTheSecretsIntoHome(t *testing.T) {
	sb := secretsSandbox(t, testutil.TestPassphrase)
	testutil.TestKeys(t, sb.Root)
	secretsWrite(t, sb, "common/netrc", "machine example.com\n")
	if _, err := secretsRun(t, sb, "lock", "--wipe"); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sb.Home, ".netrc")
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("~/.netrc before unlock: %v", err)
	}

	out, err := secretsRun(t, sb, "unlock")
	if err != nil || out != "unlocked 1 secret\nlinked 1 secret\n" {
		t.Fatalf("unlock: %q, %v", out, err)
	}
	dest, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("~/.netrc is not a link: %v", err)
	}
	if want := filepath.Join(sb.Root, "private", "common", "netrc"); dest != want {
		t.Errorf("~/.netrc -> %s, want %s", dest, want)
	}
	if data, err := os.ReadFile(link); err != nil || string(data) != "machine example.com\n" {
		t.Errorf("~/.netrc reads %q, %v", data, err)
	}

	// Linked already: nothing more to say.
	out, err = secretsRun(t, sb, "unlock")
	if err != nil || out != "unlocked 0 secrets\n" {
		t.Errorf("second unlock: %q, %v", out, err)
	}
}

func TestUnlockNeverReplacesARealFileInHome(t *testing.T) {
	sb := secretsSandbox(t, testutil.TestPassphrase)
	testutil.TestKeys(t, sb.Root)
	secretsWrite(t, sb, "common/netrc", "from the repo\n")
	if _, err := secretsRun(t, sb, "lock", "--wipe"); err != nil {
		t.Fatal(err)
	}
	// The same content: an interactive link would adopt it, unlock does not.
	sb.Files(map[string]string{"home/.netrc": "from the repo\n"})
	out, err := secretsRun(t, sb, "unlock")
	if err != nil || out != "unlocked 1 secret\n" {
		t.Errorf("unlock: %q, %v", out, err)
	}
	info, err := os.Lstat(filepath.Join(sb.Home, ".netrc"))
	if err != nil || !info.Mode().IsRegular() {
		t.Errorf("~/.netrc: %v, %v; want the real file kept", info, err)
	}
}

func TestUnlockPrintsWhatItDidBeforeAFailure(t *testing.T) {
	sb := secretsSandbox(t, testutil.TestPassphrase)
	testutil.TestKeys(t, sb.Root)
	secretsWrite(t, sb, "common/a", "1")
	secretsWrite(t, sb, "common/b", "2")
	if _, err := secretsRun(t, sb, "lock", "--wipe"); err != nil {
		t.Fatal(err)
	}
	secretsWrite(t, sb, "common/b.age", "damaged")
	out, err := secretsRun(t, sb, "unlock")
	if err == nil || !strings.HasPrefix(err.Error(), "Could not decrypt private/common/b.age.\n  age: ") {
		t.Errorf("err = %v, want the failure for common/b", err)
	}
	if out != "unlocked 1 secret\n" {
		t.Errorf("stdout = %q, want what was unlocked before the failure", out)
	}
	// Linking waits for an unlock that succeeds.
	if _, err := os.Lstat(filepath.Join(sb.Home, ".a")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("~/.a: %v, want it not linked", err)
	}
}

func TestLockPrintsWhatItDidBeforeAFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable files")
	}
	for _, args := range [][]string{{"lock"}, {"lock", "--wipe"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			sb := secretsSandbox(t, testutil.TestPassphrase)
			testutil.TestKeys(t, sb.Root)
			secretsWrite(t, sb, "common/a", "1")
			secretsWrite(t, sb, "common/b", "2")
			b := filepath.Join(sb.Root, "private", "common", "b")
			if err := os.Chmod(b, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(b, 0o644) })
			out, err := secretsRun(t, sb, args...)
			if err == nil || !strings.HasPrefix(err.Error(), "Could not encrypt private/common/b.\n  ") {
				t.Errorf("err = %v, want the failure for common/b", err)
			}
			if err != nil && strings.Contains(err.Error(), "\n  age: ") {
				t.Errorf("err = %q: a read error is not age's", err)
			}
			want := "sealed private/common/a\n"
			if len(args) > 1 {
				want = "Sealed 1, removed 0 plaintext files.\n"
			}
			if out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestLockPrintsNothingWhenItFailsFirst(t *testing.T) {
	sb := secretsSandbox(t, testutil.TestPassphrase)
	testutil.TestKeys(t, sb.Root)
	secretsWrite(t, sb, "recipients.txt", "not a key\n")
	secretsWrite(t, sb, "common/a", "1")
	out, err := secretsRun(t, sb, "lock")
	if err == nil || !strings.HasPrefix(err.Error(), "Could not encrypt private/common/a.\n  age: ") {
		t.Errorf("err = %v", err)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
}

func TestSecretsStyleLeadStylesOnlyTheLead(t *testing.T) {
	c := testutil.Sandbox(t, testutil.Options{}).Ctx
	st := ui.Styles(c)
	wiped := msg.LockStatusWiped(2, 3)
	cases := []struct {
		name  string
		style ui.Style
		text  msg.Text
		sep   string
		want  string
	}{
		{"the first word, then a count", st.OK, msg.UnlockSuccessUnlocked(3), " ",
			st.OK.Render("unlocked") + " " + st.Count.Render("3") + " secrets"},
		{"the first word, then a path", st.Error, msg.UnlockErrorConflict("common/b"), " ",
			st.Error.Render("conflict") + " private/" + st.Path.Render("common/b") + ": changed here and in the repo"},
		{"the first sentence", st.OK, msg.KeysSuccessCreated(), ". ",
			st.OK.Render("Keys created.") + " The public key:"},
		{"no separator: all of it, values styled too", st.OK, wiped, ". ",
			st.OK.Render(voice.Render(c, wiped))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := secretsStyleLead(c, tc.style, tc.text, tc.sep)
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
			if plain := voice.Plain(c, tc.text); ansi.Strip(got) != plain {
				t.Errorf("text %q, want %q", ansi.Strip(got), plain)
			}
		})
	}
}
