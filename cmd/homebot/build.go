package main

import (
	"runtime/debug"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/cli"
)

// Set by the release build: -ldflags "-X main.version=… -X main.commit=… -X main.date=…".
var (
	version string
	commit  string
	date    string
	channel string
)

// currentBuild returns the version stamped by -ldflags, or what Go recorded
// in the binary when it was built without them.
func currentBuild() cli.Build {
	info, _ := debug.ReadBuildInfo()
	b := resolveBuild(version, commit, date, info)
	b.Channel = channel
	return b
}

func resolveBuild(version, commit, date string, info *debug.BuildInfo) cli.Build {
	b := cli.Build{Version: version, Commit: commit, Date: date}
	if info == nil {
		if b.Version == "" {
			b.Version = "dev"
		}
		return b
	}
	if b.Version == "" {
		b.Version = strings.TrimPrefix(info.Main.Version, "v")
		if b.Version == "" || b.Version == "(devel)" {
			b.Version = "dev"
		}
	}
	for _, s := range info.Settings {
		switch {
		case s.Key == "vcs.revision" && b.Commit == "":
			b.Commit = s.Value
		case s.Key == "vcs.time" && b.Date == "":
			b.Date = s.Value
		}
	}
	return b
}
