package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/cli"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestGeneratedFilesAreDeterministic(t *testing.T) {
	for _, mode := range []string{"completions", "man"} {
		t.Run(mode, func(t *testing.T) {
			var first map[string][]byte
			for i := range 2 {
				out := filepath.Join(t.TempDir(), "generated")
				args := []string{mode, "-out", out}
				if mode == "man" {
					args = append(args, "-version", "0.1.0", "-date", "2026-10-13T12:34:56Z")
				}
				if err := run(args); err != nil {
					t.Fatal(err)
				}
				entries, err := os.ReadDir(out)
				if err != nil {
					t.Fatal(err)
				}
				got := map[string][]byte{}
				for _, e := range entries {
					got[e.Name()], err = os.ReadFile(filepath.Join(out, e.Name()))
					if err != nil {
						t.Fatal(err)
					}
				}
				if i == 0 {
					first = got
				} else {
					if len(first) != len(got) {
						t.Fatalf("file count changed: %d to %d", len(first), len(got))
					}
					for name, want := range first {
						if !bytes.Equal(got[name], want) {
							t.Errorf("%s changed between runs", name)
						}
					}
				}
			}
		})
	}
}

func TestCompletionScriptsCallCobraAtRuntime(t *testing.T) {
	root, cleanup, err := newTree()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	out := t.TempDir()
	if err := writeCompletions(root, out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"homebot.bash", "_homebot", "homebot.fish"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("__complete")) {
			t.Errorf("%s does not call Cobra completion", name)
		}
	}
}

func TestGeneratedCompletionsEqualTheCLI(t *testing.T) {
	out := t.TempDir()
	if err := run([]string{"completions", "-out", out}); err != nil {
		t.Fatal(err)
	}
	for shell, name := range map[string]string{"bash": "homebot.bash", "zsh": "_homebot", "fish": "homebot.fish"} {
		sb := testutil.Sandbox(t, testutil.Options{})
		if code := cli.Execute(t.Context(), sb.Ctx, cli.Build{}, []string{"completion", shell}); code != 0 {
			t.Fatalf("%s: exit %d: %s", shell, code, sb.Stderr.String())
		}
		file, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(sb.Stdout.Bytes(), file) {
			t.Errorf("%s CLI script differs from gendocs", shell)
		}
	}
}

func TestManCoversVisibleCommandsAndVersion(t *testing.T) {
	root, cleanup, err := newTree()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	page := string(manPage(root, "0.1.0", time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)))
	for _, cmd := range root.Commands() {
		want := ".SS " + cmd.Name() + "\n"
		if !cmd.Hidden || cmd.Name() == "version" {
			if !strings.Contains(page, want) {
				t.Errorf("missing %s", cmd.Name())
			}
		} else if strings.Contains(page, want) {
			t.Errorf("hidden %s appeared", cmd.Name())
		}
	}
	for _, section := range []string{".SH NAME", ".SH SYNOPSIS", ".SH DESCRIPTION", ".SH COMMANDS", ".SH SEE ALSO", "https://homebot.sh/docs/"} {
		if !strings.Contains(page, section) {
			t.Errorf("missing %q", section)
		}
	}
	if !strings.Contains(page, `.TH HOMEBOT 1 "2026-10-13" "homebot 0.1.0"`) {
		t.Fatal("man header did not use the supplied date and version")
	}
}

func TestManExamplesStartWithCanonicalName(t *testing.T) {
	root, cleanup, err := newTree()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	total := 0
	for _, cmd := range cli.ManEntries(root) {
		for _, example := range cmd.Examples {
			total++
			if !strings.HasPrefix(example.Label, "homebot ") || strings.HasPrefix(example.Label, "homebot  ") {
				t.Errorf("%s example %q must start with exactly one \"homebot \"", cmd.Name, example.Label)
			}
		}
	}
	if total == 0 {
		t.Fatal("no man page examples to check")
	}
	// The rendered page carries the prefix too, after roff escaping.
	page := string(manPage(root, "0.1.0", time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)))
	lines := strings.Split(page, "\n")
	seen := 0
	for i, line := range lines {
		if line == "Examples:" {
			for j := i + 1; j+1 < len(lines) && lines[j] == ".TP"; j += 3 {
				seen++
				if !strings.HasPrefix(lines[j+1], "homebot ") {
					t.Errorf("rendered example %q lacks the homebot prefix", lines[j+1])
				}
			}
		}
	}
	if seen != total {
		t.Errorf("rendered %d examples, want %d", seen, total)
	}
}

func TestRoffEscaping(t *testing.T) {
	for _, tc := range []struct {
		in     string
		option bool
		want   string
	}{
		{`.a`, false, `\&.a`},
		{`'a`, false, `\&'a`},
		{`a\b`, false, `a\eb`},
		{`--dry-run`, true, `\-\-dry\-run`},
	} {
		if got := roff(tc.in, tc.option); got != tc.want {
			t.Errorf("roff(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestManGzipHeaderHasNoTimestampOrName(t *testing.T) {
	root, cleanup, err := newTree()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	out := t.TempDir()
	if err := writeMan(root, out, "0.1.0", time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "homebot.1.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data[4:8], []byte{0, 0, 0, 0}) || data[3]&8 != 0 {
		t.Fatal("gzip contains a timestamp or filename")
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	uncompressed, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := os.ReadFile(filepath.Join(out, "homebot.1"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(uncompressed, plain) {
		t.Fatal("gzip does not contain the man page")
	}
}
