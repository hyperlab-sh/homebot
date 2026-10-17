package app

import (
	"io"
	"log/slog"

	"charm.land/log/v2"
	"github.com/charmbracelet/colorprofile"
)

// newDebugLogger returns a logger that writes debug lines to w when
// HOMEBOT_DEBUG is set to anything but "0", and discards them otherwise.
func newDebugLogger(env Env, w io.Writer) *slog.Logger {
	if !on(env.Get("HOMEBOT_DEBUG")) {
		return slog.New(slog.DiscardHandler)
	}
	l := log.NewWithOptions(w, log.Options{Level: log.DebugLevel, Prefix: "homebot"})
	// log detects colour from the process environment; use the Context's.
	l.SetColorProfile(colorprofile.Detect(w, env.Environ()))
	return slog.New(l)
}

// on reads a switch variable such as HOMEBOT_DEBUG: set to anything but "0".
func on(v string) bool { return v != "" && v != "0" }
