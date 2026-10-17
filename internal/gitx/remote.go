package gitx

import (
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
)

// RemoteRef is one ref a remote advertises.
type RemoteRef struct {
	ID   string
	Name string
}

// LsRemote lists the refs of remote (a name or URL), limited to patterns
// when given. It never prompts for credentials; opts can bound it.
func LsRemote(c *app.Context, dir string, opts proc.Options, remote string, patterns ...string) ([]RemoteRef, error) {
	r := RunNet(c, dir, opts, append([]string{"ls-remote", remote}, patterns...)...)
	if r.Code != 0 {
		return nil, failed(r, "ls-remote "+remote)
	}
	return ParseLsRemote(r.Stdout), nil
}

// ParseLsRemote parses "<id>\t<name>" lines.
func ParseLsRemote(out string) []RemoteRef {
	var refs []RemoteRef
	for _, line := range strings.Split(out, "\n") {
		if id, name, ok := strings.Cut(line, "\t"); ok {
			refs = append(refs, RemoteRef{ID: id, Name: name})
		}
	}
	return refs
}
