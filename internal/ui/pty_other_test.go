//go:build !linux

package ui_test

import (
	"os"
	"testing"
)

func setColumns(t *testing.T, _ *os.File, _ uint16) {
	t.Helper()
}
