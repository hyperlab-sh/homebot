package selfname

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func namedBox(t *testing.T) (*testutil.Box, string) {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{})
	binary := filepath.Join(b.Bin, "homebot")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Exe = binary
	return b, binary
}

func TestCheckRefusesNamesBeforeChangingDisk(t *testing.T) {
	b, _ := namedBox(t)
	for _, tc := range []struct {
		name   string
		taken  Taken
		reason Reason
	}{
		{"z", Taken{}, Syntax},
		{"homebot", Taken{}, Reserved},
		{"save", Taken{Reserved: []string{"save"}}, Reserved},
		{"alias", Taken{}, Shell},
		{"git", Taken{}, Tool},
		{"zork", Taken{Planned: map[string]string{"zork": "local/bin/zork"}}, InRepo},
	} {
		if got := Check(b.Ctx, tc.name, tc.taken); got.Reason != tc.reason {
			t.Errorf("Check(%q) = %q, want %q", tc.name, got.Reason, tc.reason)
		}
	}
	if _, err := os.Stat(filepath.Join(b.Home, ".local")); !os.IsNotExist(err) {
		t.Fatalf("Check changed ~/.local: %v", err)
	}
}

func TestLinkRenameAndUnlinkOnlyOurs(t *testing.T) {
	b, binary := namedBox(t)
	bin := filepath.Join(b.Home, ".local", "bin")
	if err := Link(b.Ctx, "zork"); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(bin, "zork")
	if !sameFile(first, binary) {
		t.Fatal("new link does not run homebot")
	}
	if rec, ok := Record(b.Ctx); !ok || rec.Name != "zork" || !rec.Made {
		t.Fatalf("record after Link = %+v, %v", rec, ok)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": bin + string(os.PathListSeparator) + b.Bin})
	if got := Status(b.Ctx); got.Name != "zork" {
		t.Fatalf("status = %+v", got)
	}
	if err := Link(b.Ctx, "zara"); err != nil {
		t.Fatal(err)
	}
	if exists(first) {
		t.Fatal("old link still exists after rename")
	}
	if !sameFile(filepath.Join(bin, "zara"), binary) {
		t.Fatal("renamed link does not run homebot")
	}
	if err := Unlink(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(bin, "zara")) {
		t.Fatal("our link remains after Unlink")
	}
	if _, ok := Record(b.Ctx); ok {
		t.Fatal("record remains after Unlink")
	}
}

func TestAdoptedLinkIsNeverRemoved(t *testing.T) {
	b, binary := namedBox(t)
	bin := filepath.Join(b.Home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "zork")
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	if got := Check(b.Ctx, "zork", Taken{}); !got.OK() || got.Existing != Adoptable {
		t.Fatalf("adoptable Check = %+v", got)
	}
	if err := Adopt(b.Ctx, "zork"); err != nil {
		t.Fatal(err)
	}
	if rec, ok := Record(b.Ctx); !ok || rec.Made {
		t.Fatalf("adopted record = %+v, %v", rec, ok)
	}
	if err := Unlink(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if !sameFile(link, binary) {
		t.Fatal("Unlink removed the user's link")
	}
}

func TestRenameOntoAdoptableLinkKeepsItUserOwned(t *testing.T) {
	b, binary := namedBox(t)
	if err := Link(b.Ctx, "zork"); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(b.Home, ".local", "bin")
	adopted := filepath.Join(bin, "zara")
	if err := os.Symlink(binary, adopted); err != nil {
		t.Fatal(err)
	}
	if err := Link(b.Ctx, "zara"); err != nil {
		t.Fatal(err)
	}
	if rec, ok := Record(b.Ctx); !ok || rec.Name != "zara" || rec.Made || rec.Old != "" || rec.Target != binary {
		t.Errorf("adopted rename record = %+v, %v", rec, ok)
	}
	if exists(filepath.Join(bin, "zork")) {
		t.Error("old owned link remains")
	}
	if err := Unlink(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if !sameFile(adopted, binary) {
		t.Error("adopted link changed on unlink")
	}
}

func TestForeignLinkIsUntouched(t *testing.T) {
	b, _ := namedBox(t)
	bin := filepath.Join(b.Home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(bin, "zork")
	if err := os.WriteFile(file, []byte("mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Check(b.Ctx, "zork", Taken{}); got.Reason != Foreign {
		t.Fatalf("foreign Check = %+v", got)
	}
	if err := Link(b.Ctx, "zork"); err == nil {
		t.Fatal("Link replaced a foreign file")
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "mine" {
		t.Fatalf("foreign file changed: %q, %v", data, err)
	}
}

func TestCheckFindsLaterPathAndSyncedBin(t *testing.T) {
	b, _ := namedBox(t)
	first := filepath.Join(b.Dir, "first")
	second := filepath.Join(b.Dir, "second")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(second, "zork")
	if err := os.WriteFile(other, []byte("mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": first + string(os.PathListSeparator) + second})
	if got := Check(b.Ctx, "zork", Taken{}); got.Reason != OnPath || got.Path != other {
		t.Fatalf("second PATH folder = %+v", got)
	}
	if err := os.Remove(other); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(b.Dir, "elsewhere")
	if err := os.Mkdir(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(b.Home, ".local")
	if err := os.Symlink(elsewhere, local); err != nil {
		t.Fatal(err)
	}
	if got := Check(b.Ctx, "zork", Taken{}); got.Reason != SyncedBin || got.Path != local {
		t.Fatalf("synced ~/.local = %+v", got)
	}
}

func TestLinkDoesNotReplaceRaceWinnerOrForeignTemp(t *testing.T) {
	b, _ := namedBox(t)
	bin := filepath.Join(b.Home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bin, "zork")
	beforeMake = func(got string) {
		if got != path {
			t.Errorf("make path = %q", got)
		}
		if rec, ok := Record(b.Ctx); !ok || rec.Made || !rec.Pending || rec.Name != "zork" {
			t.Errorf("pending claim was not recorded before make: %+v, %v", rec, ok)
		}
		if err := os.WriteFile(path, []byte("race winner"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeMake = func(string) {} })
	var se *Error
	if err := Link(b.Ctx, "zork"); !errors.As(err, &se) || se.Reason != Foreign {
		t.Fatalf("Link after race = %v, want Foreign", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "race winner" {
		t.Fatalf("race winner changed: %q, %v", data, err)
	}
	if _, ok := Record(b.Ctx); ok {
		t.Fatal("record written for a link that was not made")
	}
}

func TestPreclaimDoesNotOwnAnotherActorsLink(t *testing.T) {
	b, binary := namedBox(t)
	AfterStep = func(step int) error {
		if step == 1 {
			return errors.New("crash after preclaim")
		}
		return nil
	}
	t.Cleanup(func() { AfterStep = func(int) error { return nil } })
	if err := Link(b.Ctx, "zork"); err == nil {
		t.Fatal("missing simulated crash")
	}
	rec, ok := Record(b.Ctx)
	if !ok || !rec.Pending || rec.Made || rec.Target != binary {
		t.Fatalf("preclaim = %+v, %v", rec, ok)
	}
	bin := filepath.Join(b.Home, ".local", "bin")
	path := filepath.Join(bin, "zork")
	other := filepath.Join(b.Dir, "other", "homebot")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	AfterStep = func(int) error { return nil }
	var se *Error
	if err := Link(b.Ctx, "zork"); !errors.As(err, &se) || se.Reason != Foreign {
		t.Fatalf("recovery = %v, want Foreign", err)
	}
	if raw, err := os.Readlink(path); err != nil || raw != other {
		t.Fatalf("other actor's link changed: %q, %v", raw, err)
	}
	if err := Unlink(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.Readlink(path); err != nil || raw != other {
		t.Fatalf("Unlink removed other actor's link: %q, %v", raw, err)
	}
}

// The child reconstructs a context in a distinct process, sharing only the
// on-disk state and the kernel lock with its peer.
func TestSelfnameTransactionChild(t *testing.T) {
	if os.Getenv("HOMEBOT_N2_CHILD") != "1" {
		return
	}
	home := os.Getenv("HOMEBOT_N2_HOME")
	bin := os.Getenv("HOMEBOT_N2_BIN")
	c := &app.Context{
		Home: home, StateDir: os.Getenv("HOMEBOT_N2_STATE"),
		Exe: filepath.Join(bin, "homebot"),
		Env: app.NewEnv(map[string]string{"PATH": bin}),
	}
	if err := Link(c, os.Getenv("HOMEBOT_N2_NAME")); err != nil {
		t.Fatal(err)
	}
}

func TestTwoProcessesSerializeCompetingNames(t *testing.T) {
	b, _ := namedBox(t)
	state := b.Ctx.StateDir
	base := []string{
		"HOMEBOT_N2_CHILD=1", "HOMEBOT_N2_HOME=" + b.Home,
		"HOMEBOT_N2_BIN=" + b.Bin, "HOMEBOT_N2_STATE=" + state,
	}
	commands := make([]*exec.Cmd, 0, 2)
	for _, name := range []string{"zork", "zara"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSelfnameTransactionChild$")
		cmd.Env = append(os.Environ(), append(base, "HOMEBOT_N2_NAME="+name)...)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("child transaction: %v", err)
		}
	}
	rec, ok := Record(b.Ctx)
	if !ok || !rec.Made || rec.Pending || rec.Old != "" {
		t.Fatalf("final record = %+v, %v", rec, ok)
	}
	bin := filepath.Join(b.Home, ".local", "bin")
	for _, name := range []string{"zork", "zara"} {
		if exists(filepath.Join(bin, name)) != (rec.Name == name) {
			t.Fatalf("link %q does not match final owner %+v", name, rec)
		}
	}
}

func TestDetectedForeignReplacementFailsClosed(t *testing.T) {
	for _, operation := range []string{"unlink", "repoint", "temp"} {
		t.Run(operation, func(t *testing.T) {
			b, _ := namedBox(t)
			if err := Link(b.Ctx, "zork"); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(b.Home, ".local", "bin")
			link := filepath.Join(bin, "zork")
			tmp := filepath.Join(bin, ".zork.homebot-tmp")
			replace := func(path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("foreign"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			beforeRemove = func(path string) {
				if operation == "unlink" {
					replace(path)
				}
			}
			beforeRepoint = func(path string) {
				if operation == "repoint" {
					replace(path)
				}
			}
			beforeTempRemove = func(path string) {
				if operation == "temp" {
					replace(path)
				}
			}
			t.Cleanup(func() {
				beforeRemove = func(string) {}
				beforeRepoint = func(string) {}
				beforeTempRemove = func(string) {}
			})
			if operation != "unlink" {
				newBin := filepath.Join(b.Dir, "newbin")
				if err := os.Mkdir(newBin, 0o755); err != nil {
					t.Fatal(err)
				}
				newBinary := filepath.Join(newBin, "homebot")
				if err := os.WriteFile(newBinary, []byte("#!/bin/sh\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": newBin})
				if operation == "temp" {
					if err := os.Symlink(newBinary, tmp); err != nil {
						t.Fatal(err)
					}
				}
			}
			var err error
			if operation == "unlink" {
				err = Unlink(b.Ctx)
			} else {
				err = Link(b.Ctx, "zork")
			}
			var se *Error
			if !errors.As(err, &se) || (se.Reason != Foreign && se.Reason != TempBlocked) {
				t.Fatalf("operation = %v, want foreign/temp blocked", err)
			}
			foreignPath := link
			if operation == "temp" {
				foreignPath = tmp
			}
			if data, err := os.ReadFile(foreignPath); err != nil || string(data) != "foreign" {
				t.Fatalf("foreign replacement changed: %q, %v", data, err)
			}
		})
	}
}

func TestEffectiveFallsBackWhenNameIsShadowedOrRecordIsBad(t *testing.T) {
	b, _ := namedBox(t)
	if err := Link(b.Ctx, "zork"); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(b.Home, ".local", "bin")
	shadow := filepath.Join(b.Dir, "shadow")
	if err := os.Mkdir(shadow, 0o755); err != nil {
		t.Fatal(err)
	}
	shadowPath := filepath.Join(shadow, "zork")
	if err := os.WriteFile(shadowPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": shadow + string(os.PathListSeparator) + bin})
	if got := Status(b.Ctx); got.Name != "homebot" || got.Why != Shadowed || got.Path != shadowPath {
		t.Fatalf("shadowed status = %+v", got)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
	if got := Status(b.Ctx); got.Name != "homebot" || got.Why != NotOnPath {
		t.Fatalf("not-on-PATH status = %+v", got)
	}
	if err := os.WriteFile(recordPath(b.Ctx), []byte("name=Bad Name\nmade=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Status(b.Ctx); got.Name != "homebot" || got.Why != BadRecord {
		t.Fatalf("bad-record status = %+v", got)
	}
}

func TestRenameResumesAfterNewLinkIsRecorded(t *testing.T) {
	b, binary := namedBox(t)
	if err := Link(b.Ctx, "zork"); err != nil {
		t.Fatal(err)
	}
	AfterStep = func(step int) error {
		if step == 2 {
			return errors.New("simulated stop after new link")
		}
		return nil
	}
	t.Cleanup(func() { AfterStep = func(int) error { return nil } })
	if err := Link(b.Ctx, "zara"); err == nil {
		t.Fatal("rename did not stop at step 2")
	}
	rec, ok := Record(b.Ctx)
	if !ok || rec.Name != "zara" || !rec.Made || rec.Old != "zork" || !rec.OldMade {
		t.Fatalf("partial rename record = %+v, %v", rec, ok)
	}
	bin := filepath.Join(b.Home, ".local", "bin")
	if !sameFile(filepath.Join(bin, "zara"), binary) || !sameFile(filepath.Join(bin, "zork"), binary) {
		t.Fatal("both links must exist until rename is completed")
	}
	AfterStep = func(int) error { return nil }
	if err := Link(b.Ctx, "zara"); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(bin, "zork")) {
		t.Fatal("old link survived resumed rename")
	}
	rec, ok = Record(b.Ctx)
	if !ok || rec.Old != "" || !rec.Made {
		t.Fatalf("completed rename record = %+v, %v", rec, ok)
	}
}

func TestForeignTempNeverReplacesOurLink(t *testing.T) {
	b, oldBinary := namedBox(t)
	if err := Link(b.Ctx, "zork"); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(b.Home, ".local", "bin")
	link := filepath.Join(bin, "zork")
	tmp := filepath.Join(bin, ".zork.homebot-tmp")
	if err := os.WriteFile(tmp, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	newBin := filepath.Join(b.Dir, "newbin")
	if err := os.Mkdir(newBin, 0o755); err != nil {
		t.Fatal(err)
	}
	newBinary := filepath.Join(newBin, "homebot")
	if err := os.WriteFile(newBinary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": newBin})
	var se *Error
	if err := Link(b.Ctx, "zork"); !errors.As(err, &se) || se.Reason != TempBlocked {
		t.Fatalf("Link with foreign temp = %v, want TempBlocked", err)
	}
	if !sameFile(link, oldBinary) {
		t.Fatal("our old link was replaced")
	}
	if data, err := os.ReadFile(tmp); err != nil || string(data) != "mine" {
		t.Fatalf("foreign temp changed: %q, %v", data, err)
	}
}

func TestLinkNeedsExecutableHomebotTarget(t *testing.T) {
	b, binary := namedBox(t)
	if err := os.Chmod(binary, 0o644); err != nil {
		t.Fatal(err)
	}
	var se *Error
	if err := Link(b.Ctx, "zork"); !errors.As(err, &se) || se.Reason != NoHomebot {
		t.Fatalf("Link without executable binary = %v, want NoHomebot", err)
	}
	if _, ok := Record(b.Ctx); ok {
		t.Fatal("record written for a non-executable target")
	}
}
