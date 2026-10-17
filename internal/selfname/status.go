package selfname

import (
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
)

// Status works out the effective name, and why it is homebot when a name
// is recorded (D2). It reads the disk every time; Effective remembers it.
//
// The recorded name is the effective name only when ~/.local/bin/<name>
// resolves to the same file as proc.SelfPath, and the first <name> on the
// Context's PATH is that same file too. Files are compared with
// os.SameFile, never by their spelling.
func Status(c *app.Context) State {
	rec, st := readRecord(c)
	switch st {
	case noRecord:
		return State{Name: self, Why: NoRecord}
	case badRecord:
		return State{Name: self, Why: BadRecord, Path: recordPath(c)}
	}
	s := State{Name: self, Rec: rec, HasRecord: true}
	if rec.Pending {
		s.Why, s.Path = LinkBroken, linkPath(c, rec.Name)
		return s
	}
	me := proc.SelfPath(c)
	link := linkPath(c, rec.Name)
	if !sameFile(link, me) {
		s.Why, s.Path = LinkBroken, link
		return s
	}
	first, ok := firstOnPath(c.Env, rec.Name)
	if !ok {
		s.Why = NotOnPath
		return s
	}
	if !sameFile(first, me) {
		s.Why, s.Path = Shadowed, first
		return s
	}
	s.Name = rec.Name
	return s
}

type effectiveKey struct{}

// Effective is the name homebot prints for itself (D2): the recorded name
// when it works on this machine, else homebot. It is worked out once per
// Context (app.Once).
func Effective(c *app.Context) string {
	return app.Once(c, effectiveKey{}, func() string { return Status(c).Name })
}
