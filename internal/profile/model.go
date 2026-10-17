// Package profile reads profiles (manifests/profiles/*.yml), flattens them
// into the steps that apply to this machine, and hashes what they would do.
package profile

import (
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// StepSource says where a step came from, for error messages.
type StepSource struct {
	File string
	// Index is the zero-based position in the file's steps list.
	Index int
}

// Step is one entry of a profile: an action and its arguments.
type Step struct {
	// Action is the name of the action to run, e.g. "brew" or "fonts".
	Action string
	Args   []string
	// On limits the step to these systems. Nil means everywhere.
	On []app.OS
	// Name is an optional label shown instead of "action args".
	Name   string
	Source StepSource
}

// Profile is one parsed profile file.
type Profile struct {
	Name        string
	File        string
	Description string
	Include     []string
	Steps       []Step
}

// DescribeStep is how a step is named in progress output.
func DescribeStep(s Step) string {
	if s.Name != "" {
		return s.Name
	}
	if len(s.Args) > 0 {
		return s.Action + " " + strings.Join(s.Args, ", ")
	}
	return s.Action
}
