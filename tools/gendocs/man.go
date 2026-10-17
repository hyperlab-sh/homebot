package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/cli"
)

var releaseVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]*$`)

func writeMan(root *cobra.Command, out, version string, when time.Time) error {
	if !releaseVersion.MatchString(version) {
		return fmt.Errorf("invalid release version %q", version)
	}
	page := manPage(root, version, when)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "homebot.1"), page, 0o644); err != nil {
		return err
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	gz.ModTime = time.Time{} // reproducible gzip header: mtime zero, no name
	if _, err := gz.Write(page); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "homebot.1.gz"), compressed.Bytes(), 0o644)
}

func manPage(root *cobra.Command, version string, when time.Time) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, ".TH HOMEBOT 1 \"%s\" \"homebot %s\" \"User Commands\"\n", when.UTC().Format("2006-01-02"), version)
	fmt.Fprintln(&b, ".SH NAME")
	fmt.Fprintf(&b, "homebot \\- %s\n", roff(root.Short, false))
	fmt.Fprintln(&b, ".SH SYNOPSIS")
	fmt.Fprintln(&b, "homebot [command] [options]")
	fmt.Fprintln(&b, ".SH DESCRIPTION")
	fmt.Fprintln(&b, roff(root.Short, false))
	fmt.Fprintln(&b, ".SH COMMANDS")
	for _, cmd := range cli.ManEntries(root) {
		fmt.Fprintln(&b, ".SS", roff(cmd.Name, false))
		fmt.Fprintln(&b, ".TP")
		fmt.Fprintln(&b, "Usage:", roff(cmd.Usage, true))
		fmt.Fprintln(&b, roff(cmd.Description, false))
		for _, option := range cmd.Options {
			fmt.Fprintln(&b, ".TP")
			fmt.Fprintln(&b, roff(option.Label, true))
			fmt.Fprintln(&b, roff(option.Meaning, false))
		}
		fmt.Fprintln(&b, ".TP")
		fmt.Fprintln(&b, `\-h, \-\-help`)
		fmt.Fprintln(&b, "Show this command's help.")
		if len(cmd.Examples) > 0 {
			fmt.Fprintln(&b, ".PP")
			fmt.Fprintln(&b, "Examples:")
			for _, example := range cmd.Examples {
				fmt.Fprintln(&b, ".TP")
				fmt.Fprintln(&b, roff(example.Label, true))
				fmt.Fprintln(&b, roff(example.Meaning, false))
			}
		}
	}
	fmt.Fprintln(&b, ".SH SEE ALSO")
	fmt.Fprintln(&b, "https://homebot.sh/docs/")
	return []byte(b.String())
}

// roff escapes input text so no catalog line can turn into a roff request.
// Option names receive typographic hyphens that render as literal dashes.
func roff(s string, option bool) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.ReplaceAll(line, `\`, `\e`)
		if option {
			line = strings.ReplaceAll(line, "-", `\-`)
		}
		if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "'") {
			line = `\&` + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
