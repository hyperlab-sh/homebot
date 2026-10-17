package linker

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestBackupName(t *testing.T) {
	cases := []struct {
		name string
		path string
		now  time.Time
		want string
	}{
		{"UTC", "/h/.differ", time.Date(2026, 10, 11, 12, 34, 56, 0, time.UTC), "/h/.differ.homebot-backup-20261011-123456"},
		{"another zone is written in UTC", "/h/.zshrc", time.Date(2026, 10, 12, 1, 2, 3, 0, time.FixedZone("x", -5*3600)), "/h/.zshrc.homebot-backup-20261012-060203"},
		{"fractions of a second are dropped", "/h/nvim", time.Date(2026, 1, 2, 3, 4, 5, 999_000_000, time.UTC), "/h/nvim.homebot-backup-20260102-030405"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := BackupName(c.path, c.now)
			if got != c.want {
				t.Errorf("BackupName = %q, want %q", got, c.want)
			}
			if !IsBackup(got) {
				t.Errorf("IsBackup(%q) = false", got)
			}
		})
	}
}

func TestIsBackup(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		// test/cleanup-signing.test.ts: only <name>.homebot-backup-<8 digits>-<6 digits> counts
		{".gitconfig.homebot-backup-20261012-101500", true},
		{"nvim.homebot-backup-20261012-101500", true},
		{".gitconfig.homebot-backup-old", false},
		{".gitconfig.homebot-backup-20261012", false},
		{".homebot-backup-20261012-101500", false},
		{".gitconfig.homebot-backup-20261012-101500.swp", false},
		// dothaul, homebot's old name, made the same backups (plan 12.1)
		{".gitconfig.dothaul-backup-20261012-101500", true},
		{"nvim.dothaul-backup-20261012-101500", true},
		{".dothaul-backup-20261012-101500", false},
		{".gitconfig.dothaul-backup-old", false},
		{".gitconfig.other-backup-20261012-101500", false},
		{".gitconfig.homebot-backup-2026101-1015000", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsBackup(c.name); got != c.want {
			t.Errorf("IsBackup(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBackupOf(t *testing.T) {
	cases := []struct {
		name   string
		target string
		ok     bool
	}{
		{".gitconfig.homebot-backup-20261012-101500", ".gitconfig", true},
		{"a.b.homebot-backup-20261012-101500", "a.b", true},
		{"x.homebot-backup-20261012-101500.homebot-backup-20261013-000000", "x.homebot-backup-20261012-101500", true},
		{"nvim.dothaul-backup-20261012-101500", "nvim", true},
		{".gitconfig.homebot-backup-old", "", false},
	}
	for _, c := range cases {
		target, ok := BackupOf(c.name)
		if target != c.target || ok != c.ok {
			t.Errorf("BackupOf(%q) = %q, %v; want %q, %v", c.name, target, ok, c.target, c.ok)
		}
	}
}

func TestBackupConflictsNeverMovesOverAnOlderBackup(t *testing.T) {
	b := newBox(t)
	backup := b.h(".differ.homebot-backup-20260101-000000")
	b.Files(map[string]string{
		"home/.home/autolink/common/differ":           "repo",
		"home/.differ":                                "mine",
		"home/.differ.homebot-backup-20260101-000000": "older",
	})
	p := b.plan()
	b.wantKinds(p, map[string]Kind{".differ": Conflict})
	moved, err := BackupConflicts(b.Ctx, p)
	if err == nil {
		t.Fatal("BackupConflicts moved over an older backup")
	}
	if len(moved) != 0 {
		t.Errorf("moved = %v", moved)
	}
	b.contains(err.Error(), "Cannot move ~/.differ aside: ~/.differ.homebot-backup-20260101-000000 is already there. Try again in a second.")
	if got := b.read(b.h(".differ")); got != "mine" {
		t.Errorf(".differ = %q", got)
	}
	if got := b.read(backup); got != "older" {
		t.Errorf("the older backup = %q", got)
	}
}

func TestBackupConflictsReturnsTheBackupsMadeBeforeAFailure(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/differ": "repo",
		"home/.home/autolink/common/(ro)/b": "repo",
		"home/.differ":                      "mine",
		"home/.ro/b":                        "mine",
	})
	readOnly(t, b.h(".ro"))
	p := b.plan()
	b.wantKinds(p, map[string]Kind{".differ": Conflict, ".ro/b": Conflict})
	moved, err := BackupConflicts(b.Ctx, p)
	if err == nil {
		t.Fatal("BackupConflicts succeeded in a read-only folder")
	}
	want := []string{b.h(".differ.homebot-backup-20260101-000000")}
	if d := cmp.Diff(want, moved); d != "" {
		t.Errorf("moved (-want +got):\n%s", d)
	}
	if got := b.read(want[0]); got != "mine" {
		t.Errorf("the backup = %q", got)
	}
	if got := b.read(b.h(".ro/b")); got != "mine" {
		t.Errorf(".ro/b = %q", got)
	}
}
