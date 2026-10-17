package ui_test

import (
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

// Columns asks the Context whether stdout is a terminal, so the test seam
// decides even when stdout is a real one.
func TestColumnsAsksTheContextAboutTheTerminal(t *testing.T) {
	_, tty := testutil.OpenPTY(t)
	setColumns(t, tty, 72)
	if got := ui.Columns(onTerminal(t, tty, tty, tty, map[string]string{"HOMEBOT_TEST_TTY": "0"})); got != 80 {
		t.Errorf("a terminal the seam says is none: Columns = %d, want 80", got)
	}
	if got := ui.Columns(onTerminal(t, tty, tty, tty, nil)); got != 72 {
		t.Errorf("a 72-column terminal: Columns = %d, want 72", got)
	}
}

// The seam says terminal but stdout has no size: the default width.
func TestColumnsOnASeamTerminalWithoutASize(t *testing.T) {
	ctx := onTerminal(t, nil, nil, nil, map[string]string{"HOMEBOT_TEST_TTY": "1"})
	if got := ui.Columns(ctx); got != 80 {
		t.Errorf("Columns = %d, want 80", got)
	}
}
