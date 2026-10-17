// Command gendocs generates release completions and the homebot man page.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/cli"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gendocs:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("choose completions, man or docs")
	}
	mode := args[0]
	if mode != "completions" && mode != "man" && mode != "docs" {
		return fmt.Errorf("unknown command %q", mode)
	}
	f := flag.NewFlagSet(mode, flag.ContinueOnError)
	if mode == "docs" {
		check := f.Bool("check", false, "fail if generated documentation differs")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() != 0 {
			return fmt.Errorf("docs takes no positional arguments")
		}
		return writeDocs(".", *check)
	}
	out := f.String("out", "", "output directory")
	version := f.String("version", "", "release version")
	date := f.String("date", "", "RFC3339 release date")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 || *out == "" {
		return fmt.Errorf("%s needs -out DIR and no positional arguments", mode)
	}
	if mode == "man" && (*version == "" || *date == "") {
		return fmt.Errorf("man needs -version VERSION and -date RFC3339")
	}
	if mode == "completions" && (*version != "" || *date != "") {
		return fmt.Errorf("completions does not take -version or -date")
	}
	root, cleanup, err := newTree()
	if err != nil {
		return err
	}
	defer cleanup()
	if mode == "completions" {
		return writeCompletions(root, *out)
	}
	when, err := time.Parse(time.RFC3339, *date)
	if err != nil {
		return fmt.Errorf("-date: %w", err)
	}
	return writeMan(root, *out, *version, when)
}

// newTree gives CLI builders a synthetic, isolated machine. No real HOME,
// process environment, repository, or clock participates in generation.
func newTree() (*cobra.Command, func(), error) {
	home, err := os.MkdirTemp("", "homebot-gendocs-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(home) }
	c, err := app.New(app.Options{
		Env:  app.NewEnv(map[string]string{"HOME": home}),
		Home: home, Cwd: home, OS: app.Linux, Host: "gendocs",
		Now: func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return cli.NewRoot(c, cli.Build{}), cleanup, nil
}
