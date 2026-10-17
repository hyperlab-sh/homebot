package triage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/voice"
	"github.com/hyperlab-sh/homebot/internal/yamledit"
)

// LoadIgnores reads decisions.yml's ignore list; nil when the file is absent.
// A decisions.yaml left from an older homebot is refused, so its decisions
// are not lost quietly. A file that is not YAML, or whose ignore is not a
// list of strings, is a user error.
func LoadIgnores(c *app.Context) ([]string, error) {
	file := c.Layout().Decisions
	src, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		old := strings.TrimSuffix(file, ".yml") + ".yaml"
		if _, err := os.Stat(old); err == nil {
			return nil, fail.User(voice.Plain(c, msg.TriageErrorOldFile(old)))
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	doc, err := parseYAML(file, src)
	if err != nil {
		return nil, err
	}
	m, ok := doc.(map[string]any)
	if !ok || m["ignore"] == nil {
		return nil, nil
	}
	items, ok := m["ignore"].([]any)
	if !ok {
		return nil, fail.User(voice.Plain(c, msg.TriageErrorIgnoreList(file)))
	}
	list := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fail.User(voice.Plain(c, msg.TriageErrorIgnoreList(file)))
		}
		list[i] = s
	}
	return list, nil
}

var parseErrPos = regexp.MustCompile(`^\[(\d+):(\d+)\] (.*)`)

// parseYAML reads src, the text of file, into maps, lists and scalars. A
// syntax error names the file, line and column: "<file>:<line>:<col>:
// invalid YAML: <what>".
func parseYAML(file string, src []byte) (any, error) {
	invalid := func(err error) error {
		first, _, _ := strings.Cut(err.Error(), "\n")
		line, col := 1, 1
		if m := parseErrPos.FindStringSubmatch(first); m != nil {
			line, _ = strconv.Atoi(m[1])
			col, _ = strconv.Atoi(m[2])
			first = m[3]
		}
		return fail.Say(msg.ConfigErrorYAML(fmt.Sprintf("%s:%d:%d", file, line, col), first))
	}
	if _, err := parser.ParseBytes(src, 0); err != nil {
		return nil, invalid(err)
	}
	var doc any
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, invalid(err)
	}
	return doc, nil
}

// AddIgnore adds path to decisions.yml's ignore list. A new file gets the
// header and the list, as the TypeScript wrote it. In a file that has one
// the path is appended, and every other byte (comments, order, quoting) is
// left as the person wrote it; a path already listed changes nothing.
func AddIgnore(c *app.Context, path string) error {
	list, err := LoadIgnores(c)
	if err != nil {
		return err
	}
	if slices.Contains(list, path) {
		return nil
	}
	file := c.Layout().Decisions
	src, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		text := voice.Record(c, msg.TriageDecisionsHeader()) + "\nignore:\n  - " + yamlString(path) + "\n"
		return fsx.WriteFileAtomic(file, []byte(text), 0o644)
	}
	if err != nil {
		return err
	}
	out, err := yamledit.AppendToSequence(src, []string{"ignore"}, path)
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(file, out, 0o644)
}

var plainPath = regexp.MustCompile(`^[~\w./*@+-][\w./*@+ -]*$`)

// yamlString is s as a YAML scalar: plain when it is a simple path, a JSON
// string otherwise. Unlike the TypeScript, a path ending in a space is
// quoted, since plain YAML would drop the space.
func yamlString(s string) string {
	if plainPath.MatchString(s) && !strings.HasSuffix(s, " ") {
		return s
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(b.String(), "\n")
}

// isIgnored says whether key is in ignores, as written or by glob.
func isIgnored(key string, ignores []string) bool {
	return slices.ContainsFunc(ignores, func(pattern string) bool {
		return pattern == key || globMatch(pattern, key)
	})
}

// globMatch matches key against pattern as Bun.Glob did in the TypeScript:
// * and ** match names starting with a dot, {a,b} and [a-z] work, a
// backslash escapes, a ] right after [ (or [! and [^) is a member of the
// class, a pattern ending in /** does not match the folder itself, and a
// pattern ending in / matches only a path ending in /. A pattern that is not
// a valid glob matches nothing.
//
// A leading ! is a literal character, deliberately: Bun read it as negation,
// so in the TypeScript one "!x" entry ignored everything else.
func globMatch(pattern, key string) bool {
	if strings.HasSuffix(pattern, "/") && !strings.HasSuffix(key, "/") {
		return false
	}
	pattern = classBracketFirst(pattern)
	match := func(p string) bool {
		ok, err := doublestar.Match(p, key)
		return err == nil && ok
	}
	// doublestar's "a/**" matches "a" itself; Bun's does not, but does
	// match "a/".
	if prefix, ok := strings.CutSuffix(pattern, "/**"); ok {
		return match(prefix+"/*/**") || strings.HasSuffix(key, "/") && match(prefix+"/")
	}
	return match(pattern)
}

// classBracketFirst escapes a ] that opens a character class ("[]a]",
// "[!]a]"), which Bun reads as a member and doublestar as an empty class.
func classBracketFirst(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		b.WriteByte(ch)
		switch ch {
		case '\\':
			if i+1 < len(pattern) {
				i++
				b.WriteByte(pattern[i])
			}
		case '[':
			j := i + 1
			if j < len(pattern) && (pattern[j] == '!' || pattern[j] == '^') {
				b.WriteByte(pattern[j])
				j++
			}
			if j < len(pattern) && pattern[j] == ']' {
				b.WriteString(`\]`)
				j++
			}
			i = j - 1
		}
	}
	return b.String()
}
