package gitx

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

const oid = "0123456789abcdef0123456789abcdef01234567"

func rec1(xy, path string) string {
	return "1 " + xy + " N... 100644 100644 100644 " + oid + " " + oid + " " + path
}

func rec2(xy, score, path, orig string) string {
	return "2 " + xy + " N... 100644 100644 100644 " + oid + " " + oid + " " + score + " " + path + "\x00" + orig
}

func TestParseStatus(t *testing.T) {
	head := "# branch.oid " + oid + "\x00# branch.head main\x00"
	tests := []struct {
		name string
		in   string
		want RepoStatus
	}{
		{"empty", "", RepoStatus{}},
		{"clean with upstream", head + "# branch.upstream origin/main\x00# branch.ab +0 -0\x00",
			RepoStatus{Branch: "main", Upstream: "origin/main"}},
		{"ahead and behind", head + "# branch.upstream origin/main\x00# branch.ab +3 -12\x00",
			RepoStatus{Branch: "main", Upstream: "origin/main", Ahead: 3, Behind: 12}},
		{"upstream gone is no upstream", head + "# branch.upstream origin/main\x00",
			RepoStatus{Branch: "main"}},
		{"detached", "# branch.oid " + oid + "\x00# branch.head (detached)\x00", RepoStatus{}},
		{"unborn branch", "# branch.oid (initial)\x00# branch.head main\x00", RepoStatus{Branch: "main"}},
		{"slashes in the branch name", "# branch.head feat/a-b\x00", RepoStatus{Branch: "feat/a-b"}},
		{"modified, with a space in the name", head + rec1(".M", "my file.txt") + "\x00",
			RepoStatus{Branch: "main", Changed: []string{"my file.txt"},
				Entries: []Entry{{Kind: EntryOrdinary, XY: ".M", Path: "my file.txt"}}}},
		{"non-ASCII", head + rec1("M.", "café/日本語.txt") + "\x00",
			RepoStatus{Branch: "main", Changed: []string{"café/日本語.txt"},
				Entries: []Entry{{Kind: EntryOrdinary, XY: "M.", Path: "café/日本語.txt"}}}},
		{"newline in the name", head + rec1("A.", "a\nb") + "\x00",
			RepoStatus{Branch: "main", Changed: []string{"a\nb"},
				Entries: []Entry{{Kind: EntryOrdinary, XY: "A.", Path: "a\nb"}}}},
		{"rename lists the new path and eats the old one", head + rec2("R.", "R100", "new name", "old\nname") + "\x00" + rec1(".M", "z") + "\x00",
			RepoStatus{Branch: "main", Changed: []string{"new name", "z"},
				Entries: []Entry{
					{Kind: EntryRenamed, XY: "R.", Path: "new name", OrigPath: "old\nname"},
					{Kind: EntryOrdinary, XY: ".M", Path: "z"}}}},
		{"copy", rec2("C.", "C75", "b", "a") + "\x00",
			RepoStatus{Changed: []string{"b"}, Entries: []Entry{{Kind: EntryRenamed, XY: "C.", Path: "b", OrigPath: "a"}}}},
		{"a path that looks like a record is not one", rec2("R.", "R100", "2 x y", "1 old") + "\x00" + rec1(".M", "ok") + "\x00",
			RepoStatus{Changed: []string{"2 x y", "ok"}, Entries: []Entry{
				{Kind: EntryRenamed, XY: "R.", Path: "2 x y", OrigPath: "1 old"},
				{Kind: EntryOrdinary, XY: ".M", Path: "ok"}}}},
		{"unmerged", "u UU N... 100644 100644 100644 100644 " + oid + " " + oid + " " + oid + " conflict file\x00",
			RepoStatus{Changed: []string{"conflict file"}, Entries: []Entry{{Kind: EntryUnmerged, XY: "UU", Path: "conflict file"}}}},
		{"untracked and ignored are entries, not changes", "? new file\x00! build/out\x00",
			RepoStatus{Entries: []Entry{{Kind: EntryUntracked, Path: "new file"}, {Kind: EntryIgnored, Path: "build/out"}}}},
		{"malformed rename still eats its old path", "2 XY short\x00old\x00" + rec1(".M", "z") + "\x00",
			RepoStatus{Changed: []string{"z"}, Entries: []Entry{{Kind: EntryOrdinary, XY: ".M", Path: "z"}}}},
		{"rename with the old path missing", rec2("R.", "R100", "n", "o"),
			RepoStatus{Changed: []string{"n"}, Entries: []Entry{{Kind: EntryRenamed, XY: "R.", Path: "n", OrigPath: "o"}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if d := cmp.Diff(tc.want, ParseStatus(tc.in)); d != "" {
				t.Errorf("(-want +got):\n%s", d)
			}
		})
	}
}

func TestParseRaw(t *testing.T) {
	const oid2 = "1111111111111111111111111111111111111111"
	const zeroID = "0000000000000000000000000000000000000000"
	meta := func(st string) string { return ":100644 100644 " + oid + " " + oid2 + " " + st }
	ch := func(c RawChange) RawChange {
		c.OldMode, c.NewMode, c.OldID, c.NewID = "100644", "100644", oid, oid2
		return c
	}
	tests := []struct {
		name string
		in   string
		want []RawChange
	}{
		{"empty", "", nil},
		{"added and modified", meta("A") + "\x00a b.txt\x00" + meta("M") + "\x00dir/é.txt\x00",
			[]RawChange{ch(RawChange{Status: "A", Path: "a b.txt"}), ch(RawChange{Status: "M", Path: "dir/é.txt"})}},
		{"rename takes two paths", meta("R100") + "\x00old\nname\x00new name\x00" + meta("M") + "\x00z\x00",
			[]RawChange{ch(RawChange{Status: "R", Score: "100", Path: "new name", OrigPath: "old\nname"}), ch(RawChange{Status: "M", Path: "z"})}},
		{"copy", meta("C050") + "\x00a\x00b\x00", []RawChange{ch(RawChange{Status: "C", Score: "050", Path: "b", OrigPath: "a"})}},
		{"a path starting with a colon is a path", meta("A") + "\x00:odd path\x00",
			[]RawChange{ch(RawChange{Status: "A", Path: ":odd path"})}},
		{"truncated rename is dropped", meta("R100") + "\x00old\x00", nil},
		{"modes and ids of an added gitlink", ":000000 160000 " + zeroID + " " + oid2 + " A\x00sub\x00",
			[]RawChange{{Status: "A", Path: "sub", OldMode: "000000", NewMode: "160000", OldID: zeroID, NewID: oid2}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if d := cmp.Diff(tc.want, ParseRaw(tc.in)); d != "" {
				t.Errorf("(-want +got):\n%s", d)
			}
		})
	}
}

func TestParseBatchCheck(t *testing.T) {
	specs := []string{"HEAD:a", "HEAD:no\nsuch", "HEAD:b c", "HEAD:gone", "HEAD:x missing\ny"}
	out := oid + " blob 12\x00HEAD:no\nsuch missing\x00" + oid + " tree 0\x00HEAD:gone missing\x00HEAD:x missing\ny missing\x00"
	got, err := ParseBatchCheck(out, specs)
	if err != nil {
		t.Fatal(err)
	}
	want := []Object{
		{Spec: "HEAD:a", ID: oid, Type: "blob", Size: 12},
		{Spec: "HEAD:no\nsuch", Missing: true},
		{Spec: "HEAD:b c", ID: oid, Type: "tree", Size: 0},
		{Spec: "HEAD:gone", Missing: true},
		{Spec: "HEAD:x missing\ny", Missing: true},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("(-want +got):\n%s", d)
	}
	if _, err := ParseBatchCheck(oid+" blob 1\x00", specs[:2]); err == nil {
		t.Error("a missing answer must be an error")
	}
	if _, err := ParseBatchCheck("HEAD:a ambiguous\x00", specs[:1]); err == nil {
		t.Error("an ambiguous answer must be an error")
	}
}

func TestParseLsRemote(t *testing.T) {
	got := ParseLsRemote(oid + "\tHEAD\n" + oid + "\trefs/heads/main\n\n")
	want := []RemoteRef{{oid, "HEAD"}, {oid, "refs/heads/main"}}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("(-want +got):\n%s", d)
	}
}
