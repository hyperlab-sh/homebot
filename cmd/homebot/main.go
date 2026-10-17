// Command homebot keeps a home directory in order from a git repo of dotfiles.
package main

import (
	"context"
	"os"
	"os/user"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/cli"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:]))
}

// run is the whole program: it builds the Context from the process, runs the
// command line and returns the exit code. The testscript harness calls it as
// the homebot command.
func run(ctx context.Context, args []string) int {
	opts := processOptions()
	c, err := app.New(opts)
	if err != nil {
		return reportStartup(opts, err)
	}
	ctx, stop := stopOnSignal(ctx)
	defer stop()
	return cli.Execute(ctx, c, currentBuild(), args)
}

// reportStartup prints the error app.New returned as every other error is
// printed (cli.Report), through a Context made only for that, and returns
// the exit code.
func reportStartup(o app.Options, err error) int {
	cli.Report(app.ForReport(o), err)
	return cli.ExitCode(err)
}

// processOptions reads what a Context needs from the running process. It is
// the only place homebot reads the process environment, clock and streams.
func processOptions() app.Options {
	env := app.ParseEnviron(os.Environ())
	o := app.Options{
		Env:    env,
		Now:    time.Now,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	o.Cwd, _ = os.Getwd()
	o.Exe, _ = os.Executable()
	o.Host, _ = os.Hostname()
	if env.Get("HOME") == "" {
		if u, err := user.Current(); err == nil {
			o.Home = u.HomeDir
		}
	}
	return o
}
