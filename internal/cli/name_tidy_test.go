package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/inbox"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func setConfigName(t *testing.T, b *testutil.Box, name string) {
	t.Helper()
	text := readNameFile(t, b)
	lines := strings.Split(text, "\n")
	var out []string
	for _, line := range lines {
		if !strings.HasPrefix(line, "name:") {
			out = append(out, line)
		}
	}
	if name != "" {
		out = append(out, "name: "+name)
	}
	if err := os.WriteFile(filepath.Join(b.Root, "homebot.yml"), []byte(strings.Join(out, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tidyName(t *testing.T, b *testutil.Box) {
	t.Helper()
	if code := execute(b.Ctx, "tidy", "--quiet"); code != 0 {
		t.Fatalf("tidy exit %d: %s", code, b.Stderr.String())
	}
}

func nameItem(t *testing.T, b *testutil.Box, id string) *inbox.Item {
	t.Helper()
	items, err := inbox.Collect(b.Ctx, nameTaken)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == id {
			return &item
		}
	}
	return nil
}

func TestTidyNameFromConfigRenamesAndTurnsOff(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	old := filepath.Join(helper.LocalBin(b.Ctx), "zork")
	setConfigName(t, b, "zara")
	tidyName(t, b)
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Errorf("old link remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(helper.LocalBin(b.Ctx), "zara")); err != nil {
		t.Errorf("new link missing: %v", err)
	}
	if rec, ok := selfname.Record(b.Ctx); !ok || rec.Name != "zara" || rec.Old != "" {
		t.Errorf("record after rename = %+v, %v", rec, ok)
	}
	setConfigName(t, b, "")
	tidyName(t, b)
	if _, ok := selfname.Record(b.Ctx); ok {
		t.Error("record remains after config turned name off")
	}
	if _, err := os.Lstat(filepath.Join(helper.LocalBin(b.Ctx), "zara")); !os.IsNotExist(err) {
		t.Errorf("named link remains: %v", err)
	}
}

func TestTidyBlockedNameKeepsOldLinkAndReportsIt(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	setConfigName(t, b, "zara")
	foreign := filepath.Join(helper.LocalBin(b.Ctx), "zara")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	tidyName(t, b)
	if rec, ok := selfname.Record(b.Ctx); !ok || rec.Name != "zork" {
		t.Errorf("old record lost: %+v, %v", rec, ok)
	}
	if _, err := os.Lstat(filepath.Join(helper.LocalBin(b.Ctx), "zork")); err != nil {
		t.Errorf("old link lost: %v", err)
	}
	if it := nameItem(t, b, "name_blocked"); it == nil || !strings.Contains(it.Detail, foreign) {
		t.Errorf("name_blocked item = %+v", it)
	}
	if data, _ := os.ReadFile(foreign); string(data) != "foreign" {
		t.Errorf("foreign file changed: %q", data)
	}
}

func TestTidyUsesCLIReservedAndPlannedNames(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		prepare      func(*testutil.Box)
	}{
		{name: "save", reason: "homebot uses it as a command"},
		{name: "zara", reason: "home repo already places", prepare: func(b *testutil.Box) {
			b.Files(map[string]string{"home/.home/autolink/common/local/bin/zara": "repo program"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := nameBox(t)
			if code, _, stderr := runName(t, b, "zork"); code != 0 {
				t.Fatalf("set: %d %s", code, stderr)
			}
			if tc.prepare != nil {
				tc.prepare(b)
			}
			setConfigName(t, b, tc.name)
			tidyName(t, b)
			if rec, ok := selfname.Record(b.Ctx); !ok || rec.Name != "zork" {
				t.Errorf("old record lost: %+v, %v", rec, ok)
			}
			if it := nameItem(t, b, "name_blocked"); it == nil || !strings.Contains(it.Detail, tc.reason) {
				t.Errorf("name_blocked = %+v", it)
			}
		})
	}
}

func TestTidyNameKeepsAdoptedLinkWhenConfigOff(t *testing.T) {
	b := nameBox(t)
	old := filepath.Join(helper.LocalBin(b.Ctx), "zork")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(b.Bin, "homebot"), old); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("adopt: %d %s", code, stderr)
	}
	setConfigName(t, b, "")
	tidyName(t, b)
	if _, err := os.Lstat(old); err != nil {
		t.Errorf("adopted link removed: %v", err)
	}
}

func TestTidyNameWithoutBinMakesNothingAndAddsItem(t *testing.T) {
	b := nameBox(t)
	setConfigName(t, b, "zork")
	tidyName(t, b)
	if _, err := os.Stat(helper.LocalBin(b.Ctx)); !os.IsNotExist(err) {
		t.Errorf("helper created bin: %v", err)
	}
	if it := nameItem(t, b, "name_no_bin"); it == nil || !slices.Equal(it.Fix.Args, []string{"name", "zork"}) {
		t.Errorf("name_no_bin item = %+v", it)
	}
}

func TestTidyFinishesRecordedHalfRename(t *testing.T) {
	for _, step := range []int{2, 3} {
		t.Run(string(rune('0'+step)), func(t *testing.T) {
			b := nameBox(t)
			if code, _, stderr := runName(t, b, "zork"); code != 0 {
				t.Fatalf("set: %d %s", code, stderr)
			}
			setConfigName(t, b, "zara")
			previous := selfname.AfterStep
			selfname.AfterStep = func(got int) error {
				if got == step {
					return errors.New("simulated crash")
				}
				return nil
			}
			t.Cleanup(func() { selfname.AfterStep = previous })
			tidyName(t, b)
			selfname.AfterStep = previous
			tidyName(t, b)
			if rec, ok := selfname.Record(b.Ctx); !ok || rec.Name != "zara" || rec.Old != "" {
				t.Errorf("record after recovery = %+v, %v", rec, ok)
			}
			if _, err := os.Lstat(filepath.Join(helper.LocalBin(b.Ctx), "zork")); !os.IsNotExist(err) {
				t.Errorf("old link remains: %v", err)
			}
		})
	}
}

func TestTidyNameReportsForeignTempAndRepairsAfterRemoval(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	link := filepath.Join(helper.LocalBin(b.Ctx), "zork")
	oldTarget, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	newBin := filepath.Join(b.Dir, "new-bin")
	if err := os.Mkdir(newBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newBin, "homebot"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": newBin + string(os.PathListSeparator) + b.Bin})
	temp := filepath.Join(helper.LocalBin(b.Ctx), ".zork.homebot-tmp")
	if err := os.WriteFile(temp, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	tidyName(t, b)
	if got, err := os.Readlink(link); err != nil || got != oldTarget {
		t.Errorf("current link changed: %q, %v", got, err)
	}
	if it := nameItem(t, b, "name_blocked"); it == nil || !strings.Contains(it.Detail, temp) {
		t.Errorf("temp blocker item = %+v", it)
	}
	if data, _ := os.ReadFile(temp); string(data) != "foreign" {
		t.Errorf("foreign temp changed: %q", data)
	}
	if err := os.Remove(temp); err != nil {
		t.Fatal(err)
	}
	tidyName(t, b)
	if got, err := os.Readlink(link); err != nil || got == oldTarget {
		t.Errorf("stale link not repaired: %q, %v", got, err)
	}
}

func TestTidyRepairsDanglingOwnedNameLink(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	oldHomebot := filepath.Join(b.Bin, "homebot")
	if err := os.Remove(oldHomebot); err != nil {
		t.Fatal(err)
	}
	newBin := filepath.Join(b.Dir, "new-bin")
	if err := os.Mkdir(newBin, 0o755); err != nil {
		t.Fatal(err)
	}
	newHomebot := filepath.Join(newBin, "homebot")
	if err := os.WriteFile(newHomebot, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": newBin + string(os.PathListSeparator) + b.Bin})
	tidyName(t, b)
	if got, err := os.Readlink(filepath.Join(helper.LocalBin(b.Ctx), "zork")); err != nil || got != newHomebot {
		t.Errorf("dangling link repair = %q, %v; want %q", got, err, newHomebot)
	}
}

func TestInboxNameShadowingLevels(t *testing.T) {
	for _, earlier := range []bool{false, true} {
		t.Run(map[bool]string{false: "ours earlier", true: "other earlier"}[earlier], func(t *testing.T) {
			b := nameBox(t)
			if code, _, stderr := runName(t, b, "zork"); code != 0 {
				t.Fatalf("set: %d %s", code, stderr)
			}
			other := filepath.Join(b.Dir, "other")
			if err := os.Mkdir(other, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(other, "zork"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			path := helper.LocalBin(b.Ctx) + string(os.PathListSeparator) + other
			if earlier {
				path = other + string(os.PathListSeparator) + helper.LocalBin(b.Ctx)
			}
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": path + string(os.PathListSeparator) + b.Bin})
			it := nameItem(t, b, "name_shadowed")
			want := inbox.Urgent
			if earlier {
				want = inbox.Todo
			}
			if it == nil || it.Level != want {
				t.Errorf("name_shadowed = %+v, want %s", it, want)
			}
		})
	}
}

func TestInboxNameHelperPathOnlyWhenShellWillFindIt(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if it := nameItem(t, b, "name_helper_path"); it != nil {
		t.Fatalf("without rc line: %+v", it)
	}
	if err := os.WriteFile(filepath.Join(b.Home, ".zshrc"), []byte("export PATH=\"$HOME/.local/bin:$PATH\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := helper.RecordedPath(b.Ctx)
	if it := nameItem(t, b, "name_helper_path"); it == nil {
		t.Error("missing name_helper_path")
	}
	if after, _ := helper.RecordedPath(b.Ctx); after != before {
		t.Errorf("inbox changed helper PATH: %q -> %q", before, after)
	}
}

func TestNamedHelperNotificationAndShellRefreshUseWorkingName(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	path := helper.LocalBin(b.Ctx) + string(os.PathListSeparator) + b.Bin
	helperCtx := b.Ctx.Fork(map[string]string{"PATH": path})
	if err := helper.Install(helperCtx); err != nil {
		t.Fatal(err)
	}
	recorded, ok := helper.RecordedPath(helperCtx)
	if !ok || !helper.PathHasLocalBin(recorded, helperCtx) {
		t.Fatalf("helper PATH = %q, %v", recorded, ok)
	}
	runCtx := b.Ctx.Fork(map[string]string{"PATH": recorded})
	title, body := inbox.NotificationText(runCtx, []inbox.Item{{ID: "sample", Level: inbox.Urgent, Title: "Needs help"}})
	if !strings.Contains(title, "zork needs you") || !strings.Contains(body, "Run: zork") {
		t.Errorf("helper notification = %q, %q", title, body)
	}
	shellCtx := b.Ctx.Fork(map[string]string{"PATH": path})
	if _, err := inbox.Refresh(shellCtx, inbox.Options{NameTaken: nameTaken}); err != nil {
		t.Fatal(err)
	}
	nudge, err := os.ReadFile(filepath.Join(b.Ctx.CacheDir, "nudge"))
	if err != nil || !strings.Contains(string(nudge), "zork") {
		t.Errorf("shell nudge = %q, %v", nudge, err)
	}
}

func TestNameLinkChangesGetJournalLines(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	if code, _, stderr := runName(t, b, "zara"); code != 0 {
		t.Fatalf("rename: %d %s", code, stderr)
	}
	if code, _, stderr := runName(t, b, "--off"); code != 0 {
		t.Fatalf("off: %d %s", code, stderr)
	}
	entries, err := state.ReadJournal(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, e := range entries {
		joined += e.Msg + "\n"
	}
	for _, want := range []string{"made this machine's name link", "removed this machine's name link"} {
		if !strings.Contains(joined, want) {
			t.Errorf("journal missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "zork") || strings.Contains(joined, "zara") {
		t.Errorf("machine name persisted in journal: %s", joined)
	}
}
