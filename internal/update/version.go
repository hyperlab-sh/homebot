// Package update plans and performs release upgrades without owning CLI text.
package update

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var ErrNotARelease = errors.New("not a release version")

type version struct {
	parts [3]string
	pre   []string
}

var semverPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
var pseudoPattern = regexp.MustCompile(`-(?:0\.)?[0-9]{14}-[0-9a-fA-F]{7,}$`)

func parseVersion(s string) (version, error) {
	if s == "dev" || pseudoPattern.MatchString(s) {
		return version{}, fmt.Errorf("%w: %q", ErrNotARelease, s)
	}
	m := semverPattern.FindStringSubmatch(s)
	if m == nil {
		return version{}, fmt.Errorf("%w: %q", ErrNotARelease, s)
	}
	var v version
	for i := range v.parts {
		v.parts[i] = m[i+1]
	}
	if m[4] != "" {
		v.pre = strings.Split(m[4], ".")
		for _, id := range v.pre {
			if numeric(id) && len(id) > 1 && id[0] == '0' {
				return version{}, fmt.Errorf("%w: %q", ErrNotARelease, s)
			}
		}
	}
	return v, nil
}

func numeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// Newer reports SemVer 2.0 precedence. Build metadata does not affect order.
func Newer(current, latest string) (bool, error) {
	a, err := parseVersion(current)
	if err != nil {
		return false, err
	}
	b, err := parseVersion(latest)
	if err != nil {
		return false, err
	}
	for i := range a.parts {
		if a.parts[i] != b.parts[i] {
			if len(a.parts[i]) != len(b.parts[i]) {
				return len(a.parts[i]) < len(b.parts[i]), nil
			}
			return a.parts[i] < b.parts[i], nil
		}
	}
	if len(a.pre) == 0 || len(b.pre) == 0 {
		return len(a.pre) > 0 && len(b.pre) == 0, nil
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		if x == y {
			continue
		}
		xn, yn := numeric(x), numeric(y)
		switch {
		case xn && !yn:
			return true, nil
		case !xn && yn:
			return false, nil
		case xn && yn:
			if len(x) != len(y) {
				return len(x) < len(y), nil
			}
			return x < y, nil
		default:
			return x < y, nil
		}
	}
	return len(a.pre) < len(b.pre), nil
}
