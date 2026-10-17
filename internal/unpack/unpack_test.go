package unpack_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/actions"
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/profile"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/unpack"
)

const workstation = "home/.home/manifests/profiles/workstation.yml"

func sandbox(t *testing.T, files map[string]string) *testutil.Box {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{})
	sb.Files(files)
	return sb
}

func ptr(n int) *int { return &n }

func read(t *testing.T, sb *testutil.Box, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sb.Dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(sb *testutil.Box, rel string) bool {
	_, err := os.Lstat(filepath.Join(sb.Dir, filepath.FromSlash(rel)))
	return err == nil
}

func unpacked(t *testing.T, sb *testutil.Box) *state.Unpack {
	t.Helper()
	s, err := state.Load(sb.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	return s.Unpack
}

// userError is the text of err as it is stored (Error(), neutral all the way
// down), once err is known to hold a user error. A step error is a user error
// wrapped with the resume hint, so the hint is in Error(), not in the user
// error's own Msg.
func userError(t *testing.T, err error) string {
	t.Helper()
	var ue *fail.UserError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v (%T), want a user error", err, err)
	}
	return err.Error()
}

// TestUnpack ports test/inbox-exports.test.ts "unpack".
func TestUnpack(t *testing.T) {
	t.Run("runs steps in order and records the profile", func(t *testing.T) {
		sb := sandbox(t, map[string]string{
			workstation:                            "steps:\n  - run: echo one >> out.txt\n  - mark: two\n",
			"home/.home/manifests/actions/mark.sh": "# Append a word\necho \"$1 $HOMEBOT_OS\" >> out.txt\n",
		})
		if err := unpack.Run(sb.Ctx, "", unpack.Options{}); err != nil {
			t.Fatal(err)
		}
		if got := read(t, sb, "home/.home/manifests/out.txt"); got != "one\ntwo linux\n" {
			t.Errorf("out.txt = %q", got)
		}
		u := unpacked(t, sb)
		if u == nil || u.Profile != "workstation" {
			t.Fatalf("state.unpack = %+v", u)
		}
		steps, err := profile.ResolveFor(sb.Ctx, "workstation")
		if err != nil {
			t.Fatal(err)
		}
		if u.Hash != profile.StepsHash(steps) {
			t.Errorf("hash = %q, want %q", u.Hash, profile.StepsHash(steps))
		}
		if u.At != sb.Ctx.Now().UnixMilli() {
			t.Errorf("at = %d, want %d", u.At, sb.Ctx.Now().UnixMilli())
		}
		wantOut := "Unpacking workstation on testhost (linux): 2 steps\n" +
			"[1/2] run echo one >> out.txt\n[2/2] mark two\nUnpacked workstation.\n"
		if sb.Stdout.String() != wantOut {
			t.Errorf("stdout = %q, want %q", sb.Stdout, wantOut)
		}
	})

	t.Run("run steps get the HOMEBOT_* environment", func(t *testing.T) {
		sb := sandbox(t, map[string]string{workstation: "steps:\n  - run: echo \"$HOMEBOT_OS/$HOMEBOT_HOST/$HOMEBOT_ARCH\" > env.txt\n"})
		if err := unpack.Run(sb.Ctx, "", unpack.Options{}); err != nil {
			t.Fatal(err)
		}
		if got, want := read(t, sb, "home/.home/manifests/env.txt"), "linux/testhost/"+sb.Ctx.Arch+"\n"; got != want {
			t.Errorf("env.txt = %q, want %q", got, want)
		}
	})

	t.Run("stops at a failing step and says how to resume", func(t *testing.T) {
		sb := sandbox(t, map[string]string{workstation: "steps:\n  - run: 'true'\n  - run: 'exit 4'\n  - run: echo never > never.txt\n"})
		err := unpack.Run(sb.Ctx, "", unpack.Options{})
		want := "Step 2 failed: run: \"exit 4\" exited with 4\nFix it, then resume:\n  homebot unpack workstation --from 2"
		if got := userError(t, err); got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
		if exists(sb, "home/.home/manifests/never.txt") {
			t.Error("the step after the failure ran")
		}
		if u := unpacked(t, sb); u != nil {
			t.Errorf("state.unpack = %+v, want none", u)
		}
		if strings.Contains(sb.Stdout.String(), "Unpacked") {
			t.Errorf("stdout claims success: %q", sb.Stdout)
		}
	})

	t.Run("a script that exits non-zero names the step and says how to resume", func(t *testing.T) {
		sb := sandbox(t, map[string]string{
			workstation:                            "steps:\n  - fail: [a, b]\n",
			"home/.home/manifests/actions/fail.sh": "exit 3\n",
		})
		err := unpack.Run(sb.Ctx, "", unpack.Options{})
		want := "Step 1 (fail a, b) exited with 3.\nFix it, then resume:\n  homebot unpack workstation --from 1"
		if got := userError(t, err); got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})

	t.Run("--from skips earlier steps", func(t *testing.T) {
		sb := sandbox(t, map[string]string{workstation: "steps:\n  - run: 'exit 1'\n  - run: echo ok > ok.txt\n"})
		if err := unpack.Run(sb.Ctx, "", unpack.Options{From: ptr(2)}); err != nil {
			t.Fatal(err)
		}
		if !exists(sb, "home/.home/manifests/ok.txt") {
			t.Error("step 2 did not run")
		}
		want := "Unpacking workstation on testhost (linux): 2 steps\n[1/2] run exit 1  (skipped)\n[2/2] run echo ok > ok.txt\nUnpacked workstation.\n"
		if sb.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", sb.Stdout, want)
		}
	})

	t.Run("unknown actions fail clearly", func(t *testing.T) {
		sb := sandbox(t, map[string]string{workstation: "steps:\n  - teleport: x\n"})
		err := unpack.Run(sb.Ctx, "", unpack.Options{})
		if got, want := userError(t, err), "Step 1: no action named \"teleport\". See: homebot actions"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
		if got, want := sb.Stdout.String(), "Unpacking workstation on testhost (linux): 1 steps\n[1/1] teleport x\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
		if u := unpacked(t, sb); u != nil {
			t.Errorf("state.unpack = %+v, want none", u)
		}
	})

	t.Run("--dry-run runs nothing", func(t *testing.T) {
		sb := sandbox(t, map[string]string{workstation: "steps:\n  - run: echo x > x.txt\n  - teleport: y\n"})
		if err := unpack.Run(sb.Ctx, "", unpack.Options{DryRun: true}); err != nil {
			t.Fatal(err)
		}
		if exists(sb, "home/.home/manifests/x.txt") {
			t.Error("a dry run ran a step")
		}
		if u := unpacked(t, sb); u != nil {
			t.Errorf("a dry run recorded %+v", u)
		}
		want := "Would unpack workstation on testhost (linux): 2 steps\n[1/2] run echo x > x.txt\n[2/2] teleport y\n"
		if sb.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", sb.Stdout, want)
		}
	})

	t.Run("--dry-run with --from marks the earlier steps skipped", func(t *testing.T) {
		sb := sandbox(t, map[string]string{workstation: "steps:\n  - run: a\n  - run: b\n"})
		if err := unpack.Run(sb.Ctx, "", unpack.Options{DryRun: true, From: ptr(2)}); err != nil {
			t.Fatal(err)
		}
		want := "Would unpack workstation on testhost (linux): 2 steps\n[1/2] run a  (skipped)\n[2/2] run b\n"
		if sb.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", sb.Stdout, want)
		}
	})

	t.Run("unlock with nothing locked never asks for the passphrase", func(t *testing.T) {
		// no identity.age exists: asking would fail
		sb := sandbox(t, map[string]string{workstation: "steps:\n  - unlock\n"})
		if err := unpack.Run(sb.Ctx, "", unpack.Options{}); err != nil {
			t.Fatal(err)
		}
		if u := unpacked(t, sb); u == nil || u.Profile != "workstation" {
			t.Errorf("state.unpack = %+v", u)
		}
		if !strings.Contains(sb.Stdout.String(), "unlock: nothing to decrypt\n") {
			t.Errorf("stdout = %q", sb.Stdout)
		}
	})

	t.Run("unlock with locked secrets and no keys fails clearly", func(t *testing.T) {
		sb := sandbox(t, map[string]string{
			workstation:                       "steps:\n  - unlock\n",
			"home/.home/private/common/a.age": "x",
		})
		err := unpack.Run(sb.Ctx, "", unpack.Options{})
		got := userError(t, err)
		if !strings.HasPrefix(got, "Step 1 failed: ") || !strings.Contains(got, "keys init") ||
			!strings.HasSuffix(got, "\nFix it, then resume:\n  homebot unpack workstation --from 1") {
			t.Errorf("error = %q", got)
		}
	})

	t.Run("the link action links", func(t *testing.T) {
		sb := sandbox(t, map[string]string{
			workstation:                        "steps:\n  - link\n",
			"home/.home/autolink/common/zshrc": "z",
		})
		if err := unpack.Run(sb.Ctx, "", unpack.Options{}); err != nil {
			t.Fatal(err)
		}
		if got := read(t, sb, "home/.zshrc"); got != "z" {
			t.Errorf(".zshrc = %q", got)
		}
		if !strings.Contains(sb.Stdout.String(), "[1/1] link\nlink: 1 changed\n") {
			t.Errorf("stdout = %q", sb.Stdout)
		}
	})

	t.Run("the link action links what it can, then stops on layout errors", func(t *testing.T) {
		sb := sandbox(t, map[string]string{
			workstation:                          "steps:\n  - link\n  - run: echo after > after.txt\n",
			"home/.home/autolink/common/zshrc":   "z",
			"home/.home/autolink/common/a/(b)/c": "c",
		})
		err := unpack.Run(sb.Ctx, "", unpack.Options{})
		got := userError(t, err)
		if !strings.HasPrefix(got, "Step 1 failed: link: the repo breaks a layout rule:\n") || !strings.Contains(got, `rename "a" to "(a)"`) {
			t.Errorf("error = %q", got)
		}
		if !strings.HasSuffix(got, "\nFix it, then resume:\n  homebot unpack workstation --from 1") {
			t.Errorf("error does not end with the resume hint: %q", got)
		}
		if read(t, sb, "home/.zshrc") != "z" {
			t.Error("the link that could be made was not")
		}
		if exists(sb, "home/.home/manifests/after.txt") {
			t.Error("the step after the failing link ran")
		}
	})
}

func TestUnpackProfileChoice(t *testing.T) {
	t.Run("no profile anywhere asks which", func(t *testing.T) {
		sb := testutil.Sandbox(t, testutil.Options{Config: "profiles: {}\n"})
		err := unpack.Run(sb.Ctx, "", unpack.Options{})
		want := "Which profile? Pass one (homebot unpack workstation) or set profiles.default in homebot.yml."
		if got := userError(t, err); got != want {
			t.Errorf("error = %q", got)
		}
		if sb.Stdout.Len() != 0 {
			t.Errorf("stdout = %q", sb.Stdout)
		}
	})

	t.Run("a named profile wins over the default", func(t *testing.T) {
		sb := sandbox(t, map[string]string{
			workstation: "steps:\n  - run: echo ws\n",
			"home/.home/manifests/profiles/container.yml": "steps:\n  - run: echo ct\n",
		})
		if err := unpack.Run(sb.Ctx, "container", unpack.Options{}); err != nil {
			t.Fatal(err)
		}
		if u := unpacked(t, sb); u == nil || u.Profile != "container" {
			t.Errorf("state.unpack = %+v", u)
		}
	})

	t.Run("the host's own profile wins over profiles.default", func(t *testing.T) {
		sb := testutil.Sandbox(t, testutil.Options{Config: "profiles:\n  default: workstation\n  hosts:\n    testhost: server\n"})
		sb.Files(map[string]string{
			workstation: "steps: []\n",
			"home/.home/manifests/profiles/server.yml": "steps: []\n",
		})
		if err := unpack.Run(sb.Ctx, "", unpack.Options{}); err != nil {
			t.Fatal(err)
		}
		if u := unpacked(t, sb); u == nil || u.Profile != "server" {
			t.Errorf("state.unpack = %+v", u)
		}
	})

	t.Run("an unknown profile is the resolver's error", func(t *testing.T) {
		sb := sandbox(t, map[string]string{workstation: "steps: []\n"})
		err := unpack.Run(sb.Ctx, "missing", unpack.Options{})
		if got := err.Error(); !strings.Contains(got, `No profile named "missing"`) {
			t.Errorf("error = %q", got)
		}
	})
}

func TestUnpackFromRange(t *testing.T) {
	cases := []struct {
		name  string
		steps string
		from  int
		n     int // the number the message names
		ok    bool
	}{
		{"zero", "  - run: a\n  - run: b\n", 0, 2, false},
		{"negative", "  - run: a\n  - run: b\n", -3, 2, false},
		{"one past the end", "  - run: a\n  - run: b\n", 3, 2, false},
		{"the last step", "  - run: a\n  - run: b\n", 2, 2, true},
		{"the first step", "  - run: a\n  - run: b\n", 1, 2, true},
		{"an empty profile takes 1", "", 1, 0, true},
		{"an empty profile refuses 2", "", 2, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := sandbox(t, map[string]string{workstation: "steps:\n" + tc.steps})
			if tc.steps == "" {
				sb.Files(map[string]string{workstation: "steps: []\n"})
			}
			err := unpack.Run(sb.Ctx, "", unpack.Options{DryRun: true, From: ptr(tc.from)})
			if tc.ok {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			want := "--from must be between 1 and " + strconv.Itoa(tc.n)
			if got := userError(t, err); got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
			if sb.Stdout.Len() != 0 {
				t.Errorf("stdout = %q; the range is checked before the header", sb.Stdout)
			}
		})
	}
}

func TestUnpackKeepsOtherState(t *testing.T) {
	sb := sandbox(t, map[string]string{workstation: "steps: []\n"})
	if err := state.Update(sb.Ctx, func(s *state.State) { s.LastSync = 42 }); err != nil {
		t.Fatal(err)
	}
	if err := unpack.Run(sb.Ctx, "", unpack.Options{}); err != nil {
		t.Fatal(err)
	}
	s, err := state.Load(sb.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastSync != 42 || s.Unpack == nil {
		t.Errorf("state = %+v", s)
	}
}

func TestUnpackEmptyHostProfileIsNoProfile(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{Config: "profiles:\n  default: workstation\n  hosts:\n    testhost: \"\"\n"})
	sb.Files(map[string]string{workstation: "steps:\n  - run: echo ran > ran.txt\n"})
	err := unpack.Run(sb.Ctx, "", unpack.Options{})
	want := "Which profile? Pass one (homebot unpack workstation) or set profiles.default in homebot.yml."
	if got := userError(t, err); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if exists(sb, "home/.home/manifests/ran.txt") {
		t.Error("a step ran")
	}
}

// A Ctrl-C ends the run between steps: no later step runs, nothing is
// recorded, and the error is the cancel cause, which Execute turns into a
// silent exit with the signal's code.
func TestUnpackStopsAfterCtrlC(t *testing.T) {
	cases := []struct {
		name  string
		steps string
		opts  unpack.Options
	}{
		{"between steps", "steps:\n  - cancelnow\n  - run: echo two > two.txt\n", unpack.Options{}},
		{"after the last step", "steps:\n  - run: echo one > one.txt\n  - cancelnow\n", unpack.Options{}},
		{"in a dry run", "steps:\n  - run: echo two > two.txt\n", unpack.Options{DryRun: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := sandbox(t, map[string]string{workstation: tc.steps})
			ctx, cancel := context.WithCancelCause(context.Background())
			t.Cleanup(func() { cancel(nil) })
			c := sb.Ctx.WithContext(ctx)
			actions.StubInternal(t, "cancelnow", msg.ActionRunSummary(), func(*app.Context, []string) error {
				cancel(fail.Silently(130))
				return nil
			})
			if tc.opts.DryRun {
				cancel(fail.Silently(130))
			}

			err := unpack.Run(c, "", tc.opts)
			var exit *fail.Exit
			if !errors.As(err, &exit) || exit.Code != 130 {
				t.Fatalf("err = %v, want the exit 130 cause", err)
			}
			if exists(sb, "home/.home/manifests/two.txt") {
				t.Error("a step ran after Ctrl-C")
			}
			if u := unpacked(t, sb); u != nil {
				t.Errorf("state.unpack = %+v, want none", u)
			}
			if strings.Contains(sb.Stdout.String(), "Unpacked") {
				t.Errorf("stdout claims success: %q", sb.Stdout)
			}
		})
	}
}
