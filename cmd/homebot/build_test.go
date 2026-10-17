package main

import (
	"runtime/debug"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/cli"
)

func TestResolveBuild(t *testing.T) {
	vcs := &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-10-01T12:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	cases := []struct {
		name                  string
		version, commit, date string
		info                  *debug.BuildInfo
		want                  cli.Build
	}{
		{"ldflags win", "1.2.3", "abc", "2026-09-01", vcs, cli.Build{Version: "1.2.3", Commit: "abc", Date: "2026-09-01"}},
		{"vcs fills the gaps", "1.2.3", "", "", vcs, cli.Build{Version: "1.2.3", Commit: "0123456789abcdef", Date: "2026-10-01T12:00:00Z"}},
		{"devel is dev", "", "", "", vcs, cli.Build{Version: "dev", Commit: "0123456789abcdef", Date: "2026-10-01T12:00:00Z"}},
		{"module version without v", "", "", "", &debug.BuildInfo{Main: debug.Module{Version: "v0.4.0"}}, cli.Build{Version: "0.4.0"}},
		{"empty module version", "", "", "", &debug.BuildInfo{}, cli.Build{Version: "dev"}},
		{"no build info", "", "", "", nil, cli.Build{Version: "dev"}},
		{"no build info, ldflags", "2.0.0", "c", "d", nil, cli.Build{Version: "2.0.0", Commit: "c", Date: "d"}},
	}
	for _, tc := range cases {
		if got := resolveBuild(tc.version, tc.commit, tc.date, tc.info); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// The release build stamps main.channel with -ldflags; the update core only
// trusts a build whose channel is "release", so currentBuild must carry it
// through and an unstamped build must not claim it.
func TestCurrentBuildCarriesTheChannelStamp(t *testing.T) {
	old := channel
	t.Cleanup(func() { channel = old })

	channel = ""
	if got := currentBuild().Channel; got != "" {
		t.Fatalf("unstamped build channel = %q, want empty", got)
	}
	channel = "release"
	if got := currentBuild().Channel; got != "release" {
		t.Fatalf("stamped build channel = %q, want release", got)
	}
}
