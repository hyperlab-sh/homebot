package app

import (
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// OS is an operating system homebot supports, named the way the repo's layer
// folders are.
type OS string

// The supported operating systems.
const (
	Linux OS = "linux"
	MacOS OS = "macos"
)

// AllOS lists every supported OS.
var AllOS = []OS{Linux, MacOS}

// Layers lists every layer folder: the one shared by all machines, then one
// per OS.
var Layers = []string{"common", string(Linux), string(MacOS)}

// DetectOS maps a Go GOOS value to the OS homebot calls it.
func DetectOS(goos string) (OS, error) {
	switch goos {
	case "darwin":
		return MacOS, nil
	case "linux":
		return Linux, nil
	}
	return "", unsupportedOS(goos)
}

func unsupportedOS(name string) error {
	return fail.Say(msg.ContextErrorPlatform(name))
}

// nodeArch names a GOARCH the way Node's os.arch() does, because the
// architecture reaches users' files through {arch} and HOMEBOT_ARCH, and the
// TypeScript build wrote x64, not amd64.
func nodeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	case "ppc64le":
		return "ppc64"
	case "mipsle":
		return "mipsel"
	}
	return goarch
}
