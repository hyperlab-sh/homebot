// Package testutil builds the sandboxes every homebot test runs in: a temp
// home with a home repo, an environment that hides the developer's own, and
// a Context pointing at both. Only tests import it.
package testutil

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Fixed values every sandbox runs with.
const (
	// TestNow is 2026-01-01T00:00:00Z in milliseconds, the sandbox clock.
	TestNow int64 = 1767225600000
	// TestSeed seeds the sandbox's Rand.
	TestSeed = 1
	// TestHost is the sandbox's host name.
	TestHost = "testhost"
	// GitDate is when every sandbox commit is made.
	GitDate = "2026-01-01T00:00:00Z"
)

// BaseConfig is the homebot.yml a sandbox gets unless the test gives one.
const BaseConfig = "profiles:\n  default: workstation\n"

// PrivateGitignore is the private/.gitignore of a home repo: only sealed
// files and a few plain ones directly under private/ are ever committed. It
// is secrets.PrivateGitignore, which homebot writes (testutil cannot import
// secrets, whose tests import testutil; a guard test checks they match).
const PrivateGitignore = "*\n!*/\n!*.age\n!/README.md\n!/.gitignore\n!/.gitkeep\n!/recipients.txt\n"

// GitConfig is the global git config of a sandbox, at $WORK/gitconfig.
const GitConfig = `[user]
	name = Test User
	email = test@example.com
[init]
	defaultBranch = main
[commit]
	gpgsign = false
`

// SandboxEnv returns the whole environment of a sandbox rooted at work. The
// PATH is $WORK/bin, then pathDirs, then /usr/bin:/bin, so tools the
// developer installed elsewhere never leak into a test.
//
// HOMEBOT_PERSONA is none: every sandbox, and so every parity golden,
// speaks in the neutral voice byte for byte. A test of a persona, the
// default neatpik included, deletes it first (an empty value counts as
// unset). It beats persona: in homebot.yml, so a test that sets that key
// must delete it too, or it passes whether or not the code works.
func SandboxEnv(work string, pathDirs ...string) map[string]string {
	path := append([]string{filepath.Join(work, "bin")}, pathDirs...)
	path = append(path, "/usr/bin", "/bin")
	return map[string]string{
		"HOME":                filepath.Join(work, "home"),
		"XDG_CONFIG_HOME":     filepath.Join(work, "xdg", "config"),
		"XDG_STATE_HOME":      filepath.Join(work, "xdg", "state"),
		"XDG_CACHE_HOME":      filepath.Join(work, "xdg", "cache"),
		"XDG_DATA_HOME":       filepath.Join(work, "xdg", "data"),
		"HOMEBOT_TEST_NOW":    strconv.FormatInt(TestNow, 10),
		"HOMEBOT_TEST_SEED":   strconv.Itoa(TestSeed),
		"HOMEBOT_TEST_HOST":   TestHost,
		"HOMEBOT_PERSONA":     "none",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   filepath.Join(work, "gitconfig"),
		"GIT_AUTHOR_DATE":     GitDate,
		"GIT_COMMITTER_DATE":  GitDate,
		"PATH":                strings.Join(path, string(filepath.ListSeparator)),
		"NO_COLOR":            "1",
		"TZ":                  "UTC",
		"LANG":                "C.UTF-8",
		"LC_ALL":              "C.UTF-8",
	}
}
