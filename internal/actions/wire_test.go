package actions_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/actions"
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func plain(c *app.Context, t msg.Text) string { return voice.Plain(c, t) }

// stdout runs the action and returns what it printed and its error.
func stdout(r *rig, name string, args ...string) (string, error) {
	r.box.Stdout.Reset()
	err := actions.Run(r.box.Ctx, name, args)
	return r.box.Stdout.String(), err
}

func (r *rig) home(rel string) string { return filepath.Join(r.box.Home, rel) }

func isLink(p string) bool {
	info, err := os.Lstat(p)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func TestLinkAction(t *testing.T) {
	t.Run("links every tree and counts what changed", func(t *testing.T) {
		r := newRig(t)
		r.box.Files(map[string]string{
			"home/.home/autolink/common/zshrc":  "a",
			"home/.home/autolink/common/bashrc": "b",
		})
		out, err := stdout(r, "link")
		if err != nil {
			t.Fatal(err)
		}
		if want := plain(r.box.Ctx, msg.ActionLinkStatusChanged(2)) + "\n"; out != want || out != "link: 2 changed\n" {
			t.Errorf("out %q", out)
		}
		if !isLink(r.home(".zshrc")) || !isLink(r.home(".bashrc")) {
			t.Error("links not made")
		}
	})

	t.Run("says how many are in the way", func(t *testing.T) {
		r := newRig(t)
		r.box.Files(map[string]string{
			"home/.home/autolink/common/zshrc":  "repo",
			"home/.home/autolink/common/bashrc": "b",
			"home/.zshrc":                       "mine",
		})
		out, err := stdout(r, "link")
		if err != nil {
			t.Fatal(err)
		}
		want := plain(r.box.Ctx, msg.ActionLinkStatusChanged(1)) + plain(r.box.Ctx, msg.ActionLinkWarningInWay(1)) + "\n"
		if out != want || out != "link: 1 changed, 1 in the way (see: homebot link)\n" {
			t.Errorf("out %q", out)
		}
		if isLink(r.home(".zshrc")) {
			t.Error("the file in the way was replaced")
		}
	})

	t.Run("a layout error names its one reason", func(t *testing.T) {
		r := newRig(t)
		r.box.Files(map[string]string{"home/.home/autolink/linux/()/a": "a"})
		out, err := stdout(r, "link")
		var user *fail.UserError
		if !errors.As(err, &user) {
			t.Fatalf("err = %v", err)
		}
		want := plain(r.box.Ctx, msg.ActionLinkErrorLayout(1, `  "()" has no name inside`))
		if err.Error() != want || !strings.HasPrefix(want, "link: the repo breaks a layout rule:\n") {
			t.Errorf("err %q, want %q", err.Error(), want)
		}
		if out != "link: 0 changed\n" {
			t.Errorf("out %q", out)
		}
	})

	t.Run("a layout error names every reason", func(t *testing.T) {
		r := newRig(t)
		r.box.Files(map[string]string{
			"home/.home/autolink/common/(.dotted)/a": "a",
			"home/.home/autolink/linux/()/a":         "a",
		})
		_, err := stdout(r, "link")
		reasons := `  "(.dotted)": top-level names get their dot from homebot; rename it to "(dotted)"` + "\n" + `  "()" has no name inside`
		want := plain(r.box.Ctx, msg.ActionLinkErrorLayout(2, reasons))
		if err == nil || err.Error() != want || !strings.HasPrefix(want, "link: the repo breaks 2 layout rules:\n") {
			t.Errorf("err %v, want %q", err, want)
		}
	})

	// The error is kept by unpack (neutral); the person reads it in the voice,
	// the reasons inside it as well.
	t.Run("a layout error is stored neutral and printed in the persona's voice", func(t *testing.T) {
		r := newRig(t)
		yes := true
		r.box.Ctx.StdoutTerminal = &yes
		persona.Override(r.box.Ctx, &persona.Voice{ID: "hostile", Name: "Hostile", Lines: map[string][]persona.Line{
			"action.link.error.layout": {{Text: "{action} PERSONA LAYOUT {n}:\n  {reasons}", Lang: language.English}},
			"layout.empty_parens":      {{Text: "PERSONA EMPTY {marks}", Lang: language.English}},
		}})
		r.box.Files(map[string]string{
			"home/.home/autolink/common/(.dotted)/a": "a",
			"home/.home/autolink/linux/()/a":         "a",
		})
		_, err := stdout(r, "link")
		if err == nil {
			t.Fatal("no error")
		}

		reasons := `  "(.dotted)": top-level names get their dot from homebot; rename it to "(dotted)"` + "\n" + `  "()" has no name inside`
		if want := voice.Record(r.box.Ctx, msg.ActionLinkErrorLayout(2, reasons)); err.Error() != want {
			t.Errorf("stored Error() = %q, want the neutral %q", err.Error(), want)
		}

		said := voice.ErrorText(r.box.Ctx, err)
		for _, want := range []string{"link: PERSONA LAYOUT 2:", "PERSONA EMPTY ()"} {
			if !strings.Contains(said, want) {
				t.Errorf("printed error = %q, want %q in it", said, want)
			}
		}
		if strings.Contains(said, "has no name inside") {
			t.Errorf("printed error = %q still holds the neutral reason", said)
		}
	})

	t.Run("a named tree leaves the other alone", func(t *testing.T) {
		r := newRig(t)
		r.box.Files(map[string]string{
			"home/.home/autolink/common/zshrc":   "a",
			"home/.home/private/common/secretrc": "s",
		})
		out, err := stdout(r, "link", "autolink")
		if err != nil || out != "link: 1 changed\n" {
			t.Fatalf("out %q err %v", out, err)
		}
		if !isLink(r.home(".zshrc")) || exists(r.home(".secretrc")) {
			t.Error("link autolink touched the wrong tree")
		}
	})

	t.Run("an unknown tree is refused before anything is linked", func(t *testing.T) {
		r := newRig(t)
		r.box.Files(map[string]string{"home/.home/autolink/common/zshrc": "a"})
		_, err := stdout(r, "link", "autolink", "bogus")
		if err == nil || err.Error() != plain(r.box.Ctx, msg.ActionLinkErrorUnknownTree("bogus")) {
			t.Errorf("err %v", err)
		}
		if exists(r.home(".zshrc")) {
			t.Error("linked despite the bad argument")
		}
	})
}

// sealed gives the sandbox keys and sealed secrets, then removes their
// plaintext so they are locked.
func sealed(t *testing.T, r *rig, secrets map[string]string) {
	t.Helper()
	testutil.TestKeys(t, r.box.Root)
	in := map[string]string{}
	for rel, v := range secrets {
		in["home/.home/private/"+rel] = v
	}
	r.box.Files(in)
	if _, err := secretsSeal(r); err != nil {
		t.Fatal(err)
	}
}

func lock(t *testing.T, r *rig, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		if err := os.Remove(filepath.Join(r.box.Root, "private", rel)); err != nil {
			t.Fatal(err)
		}
	}
}

func withPassphrase(r *rig) {
	r.box.Ctx.Env = r.box.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_PASSPHRASE": testutil.TestPassphrase})
}

func TestUnlockAction(t *testing.T) {
	t.Run("asks for the passphrase and counts what it decrypted", func(t *testing.T) {
		r := newRig(t)
		sealed(t, r, map[string]string{"common/a": "1", "common/b": "2"})
		lock(t, r, "common/a", "common/b")

		// Without a passphrase or a terminal the prompt cannot be answered:
		// that it fails shows the action asked.
		if _, err := stdout(r, "unlock"); err == nil {
			t.Fatal("unlock decrypted without being able to ask for the passphrase")
		}

		withPassphrase(r)
		out, err := stdout(r, "unlock")
		if err != nil {
			t.Fatal(err)
		}
		if want := plain(r.box.Ctx, msg.ActionUnlockStatusDecrypted(2)) + "\n"; out != want || out != "unlock: 2 decrypted\n" {
			t.Errorf("out %q", out)
		}
		if b, _ := os.ReadFile(filepath.Join(r.box.Root, "private/common/a")); string(b) != "1" {
			t.Errorf("plaintext %q", b)
		}
	})

	t.Run("says how many conflict", func(t *testing.T) {
		r := newRig(t)
		sealed(t, r, map[string]string{"common/a": "base", "common/b": "x"})
		lock(t, r, "common/b")
		// a changed in the repo (another machine) and here.
		records := readFile(t, filepath.Join(r.box.Ctx.StateDir, "secrets.json"))
		r.box.Files(map[string]string{"home/.home/private/common/a": "theirs"})
		if _, err := secretsSeal(r); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(r.box.Ctx.StateDir, "secrets.json"), records)
		r.box.Files(map[string]string{"home/.home/private/common/a": "mine"})

		withPassphrase(r)
		out, err := stdout(r, "unlock")
		if err != nil {
			t.Fatal(err)
		}
		want := plain(r.box.Ctx, msg.ActionUnlockStatusDecrypted(1)) + plain(r.box.Ctx, msg.ActionUnlockWarningConflicts(1)) + "\n"
		if out != want || out != "unlock: 1 decrypted, 1 conflicts (see: homebot unlock)\n" {
			t.Errorf("out %q", out)
		}
		if got := readFile(t, filepath.Join(r.box.Root, "private/common/a")); string(got) != "mine" {
			t.Errorf("the local edit was overwritten: %q", got)
		}
	})

	t.Run("nothing to decrypt never asks", func(t *testing.T) {
		r := newRig(t)
		sealed(t, r, map[string]string{"common/a": "1"})
		// a is sealed and plaintext: nothing to do, and no passphrase or
		// terminal exists to ask on.
		out, err := stdout(r, "unlock")
		if err != nil || out != plain(r.box.Ctx, msg.ActionUnlockStatusNothing())+"\n" || out != "unlock: nothing to decrypt\n" {
			t.Errorf("out %q err %v", out, err)
		}
	})

	t.Run("a repo without secrets never asks", func(t *testing.T) {
		r := newRig(t)
		out, err := stdout(r, "unlock")
		if err != nil || out != "unlock: nothing to decrypt\n" {
			t.Errorf("out %q err %v", out, err)
		}
	})

	t.Run("does not link private into home", func(t *testing.T) {
		r := newRig(t)
		sealed(t, r, map[string]string{"common/secretrc": "1"})
		lock(t, r, "common/secretrc")
		withPassphrase(r)
		if _, err := stdout(r, "unlock"); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(r.box.Home)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() != ".home" && isLink(r.home(e.Name())) {
				t.Errorf("unlock made a link: %s", e.Name())
			}
		}
		if exists(r.home(".secretrc")) {
			t.Error("private/ was linked into home")
		}
	})
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func secretsSeal(r *rig) (secrets.SealResult, error) { return secrets.Seal(r.box.Ctx) }
