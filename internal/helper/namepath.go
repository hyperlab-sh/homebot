package helper

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// LocalBin is the one directory in which a machine name can be linked.
func LocalBin(c *app.Context) string { return filepath.Join(c.Home, ".local", "bin") }

// PathHasLocalBin checks real PATH components, including alternate spellings
// of the same directory. Relative components are never evidence.
func PathHasLocalBin(path string, c *app.Context) bool {
	bin := LocalBin(c)
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if filepath.Clean(dir) == bin {
			return true
		}
		a, ae := os.Stat(dir)
		b, be := os.Stat(bin)
		if ae == nil && be == nil && os.SameFile(a, b) {
			return true
		}
	}
	return false
}

// ShellWillFindLocalBin checks the current process PATH or the chosen rc
// file. It uses the narrow, reviewed recognition of active installer-style
// statements; prose, comments and chained commands do not count.
func ShellWillFindLocalBin(c *app.Context) bool {
	return PathHasLocalBin(c.Env.Get("PATH"), c) || RCPutsLocalBinOnPath(c)
}

// rcCommand accepts one complete rc command. A later command or a comment
// mentioning the bin directory must not make a different PATH change count.
// This is deliberately a narrow recognizer, not a shell parser.
func rcCommand(line string) (string, bool) {
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '\\' && quote != '\'' {
			return "", false
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return strings.TrimSpace(line[:i]), true
		}
		if strings.ContainsRune(";&|<>", rune(ch)) {
			return "", false
		}
	}
	if quote != 0 {
		return "", false
	}
	return strings.TrimSpace(line), true
}

// RCPathLine recognizes the simple PATH forms written by setup and the front
// door, plus equivalent PATH components. Quoted prose, command substitutions,
// and chained commands are not evidence that a shell can run the name.
func RCPathLine(line, shell string) bool {
	command, ok := rcCommand(line)
	if !ok || command == "" || strings.Contains(command, "$(") || strings.ContainsRune(command, '`') {
		return false
	}
	if shell == "fish" {
		fields := strings.Fields(command)
		if len(fields) < 2 || fields[0] != "fish_add_path" {
			return false
		}
		for _, field := range fields[1:] {
			if rcLocalBin(field, shell) {
				return true
			}
		}
		return false
	}
	command = strings.TrimSpace(strings.TrimPrefix(command, "export "))
	key, value, ok := strings.Cut(command, "=")
	if !ok || strings.TrimSpace(key) != "PATH" {
		return false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	if strings.ContainsAny(value, "'\"\\ \t") {
		return false
	}
	for _, component := range strings.Split(value, ":") {
		if rcLocalBin(component, shell) {
			return true
		}
	}
	return false
}

func rcLocalBin(s, shell string) bool {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s == "$HOME/.local/bin" || shell != "fish" && s == "${HOME}/.local/bin"
}

// RCPutsLocalBinOnPath reads the startup file selected by setup for this
// shell. An unreadable or absent file provides no evidence.
func RCPutsLocalBinOnPath(c *app.Context) bool {
	file := RCFile(c)
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	shell := filepath.Base(c.Env.Get("SHELL"))
	for line := range strings.SplitSeq(string(data), "\n") {
		if RCPathLine(line, shell) {
			return true
		}
	}
	return false
}

// RCFile is the login shell's startup file setup may offer to extend.
func RCFile(c *app.Context) string {
	shell := filepath.Base(c.Env.Get("SHELL"))
	file := filepath.Join(c.Home, ".profile")
	switch shell {
	case "zsh":
		base := c.Env.Get("ZDOTDIR")
		if base == "" {
			base = c.Home
		}
		file = filepath.Join(base, ".zshrc")
	case "bash":
		file = filepath.Join(c.Home, ".bashrc")
		if c.OS == app.MacOS {
			file = filepath.Join(c.Home, ".bash_profile")
		}
	case "fish":
		file = filepath.Join(c.Home, ".config", "fish", "config.fish")
	}
	return file
}
