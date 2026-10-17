package ui

import (
	"regexp"
	"strings"
)

// Tildify writes path under home as ~/...; any other path stays as it is.
func Tildify(path, home string) string {
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// ExpandHome undoes Tildify: ~ and ~/... become paths under home.
func ExpandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return home + path[1:]
	}
	return path
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// ShellQuote quotes arg for a POSIX shell, leaving words that need no
// quoting bare.
func ShellQuote(arg string) string {
	if shellSafe.MatchString(arg) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}
