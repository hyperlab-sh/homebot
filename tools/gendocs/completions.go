package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/cli"
)

func writeCompletions(root *cobra.Command, out string) error {
	files := []struct {
		name string
		gen  func(*bytes.Buffer) error
	}{
		{"homebot.bash", func(w *bytes.Buffer) error { return cli.Completion(root, "bash", w) }},
		{"_homebot", func(w *bytes.Buffer) error { return cli.Completion(root, "zsh", w) }},
		{"homebot.fish", func(w *bytes.Buffer) error { return cli.Completion(root, "fish", w) }},
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, file := range files {
		var b bytes.Buffer
		if err := file.gen(&b); err != nil {
			return fmt.Errorf("%s: %w", file.name, err)
		}
		if err := os.WriteFile(filepath.Join(out, file.name), b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}
