package gitx

import (
	"strconv"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// EntryKind says which record of git status --porcelain=v2 an Entry is.
type EntryKind int

const (
	// EntryOrdinary is a changed tracked file (record "1").
	EntryOrdinary EntryKind = iota
	// EntryRenamed is a renamed or copied file (record "2").
	EntryRenamed
	// EntryUnmerged is a file with a merge conflict (record "u").
	EntryUnmerged
	// EntryUntracked is a file git does not track (record "?").
	EntryUntracked
	// EntryIgnored is an ignored file (record "!").
	EntryIgnored
)

// Entry is one changed path from git status.
type Entry struct {
	Kind EntryKind
	// XY is the two status letters, index then work tree; "" for untracked
	// and ignored entries.
	XY   string
	Path string
	// OrigPath is where a renamed entry came from.
	OrigPath string
}

// RepoStatus is where a repo stands against its upstream and what is
// uncommitted in it.
type RepoStatus struct {
	// Branch is the current branch; "" when HEAD is detached.
	Branch string
	// Upstream is the tracked branch, such as origin/main; "" when none, and
	// when the remote branch it tracked is gone.
	Upstream string
	Ahead    int
	Behind   int
	// Changed lists tracked files with uncommitted changes, staged or not.
	// A rename lists the new path.
	Changed []string
	// Entries are all the records git reported, in its order.
	Entries []Entry
}

// Status reads the branch, upstream, ahead/behind counts and uncommitted
// tracked files of the repo at dir. Untracked files are not looked at.
func Status(c *app.Context, dir string) (RepoStatus, error) {
	r := Run(c, dir, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=no")
	if r.Code != 0 {
		return RepoStatus{}, failed(r, "status")
	}
	return ParseStatus(r.Stdout), nil
}

// ParseStatus parses the output of git status --porcelain=v2 -z --branch.
func ParseStatus(out string) RepoStatus {
	var s RepoStatus
	var upstream string
	records := strings.Split(out, "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		switch {
		case rec == "":
		case strings.HasPrefix(rec, "# "):
			s.header(rec[2:], &upstream)
		case rec[0] == '1':
			// 1 XY sub mH mI mW hH hI path
			if f := strings.SplitN(rec, " ", 9); len(f) == 9 {
				s.add(Entry{Kind: EntryOrdinary, XY: f[1], Path: f[8]})
			}
		case rec[0] == '2':
			// 2 XY sub mH mI mW hH hI Xscore path, then the old path
			f := strings.SplitN(rec, " ", 10)
			orig := ""
			if i+1 < len(records) {
				i++
				orig = records[i]
			}
			if len(f) == 10 {
				s.add(Entry{Kind: EntryRenamed, XY: f[1], Path: f[9], OrigPath: orig})
			}
		case rec[0] == 'u':
			// u XY sub m1 m2 m3 mW h1 h2 h3 path
			if f := strings.SplitN(rec, " ", 11); len(f) == 11 {
				s.add(Entry{Kind: EntryUnmerged, XY: f[1], Path: f[10]})
			}
		case rec[0] == '?' && len(rec) > 2:
			s.Entries = append(s.Entries, Entry{Kind: EntryUntracked, Path: rec[2:]})
		case rec[0] == '!' && len(rec) > 2:
			s.Entries = append(s.Entries, Entry{Kind: EntryIgnored, Path: rec[2:]})
		}
	}
	return s
}

func (s *RepoStatus) add(e Entry) {
	s.Entries = append(s.Entries, e)
	s.Changed = append(s.Changed, e.Path)
}

func (s *RepoStatus) header(h string, upstream *string) {
	key, val, _ := strings.Cut(h, " ")
	switch key {
	case "branch.head":
		if val != "(detached)" {
			s.Branch = val
		}
	case "branch.upstream":
		// Only trusted once branch.ab confirms it: git prints the upstream
		// name even after the remote branch is gone.
		*upstream = val
	case "branch.ab":
		s.Upstream = *upstream
		// +ahead -behind
		a, b, _ := strings.Cut(val, " ")
		s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(a, "+"))
		s.Behind, _ = strconv.Atoi(strings.TrimPrefix(b, "-"))
	}
}
