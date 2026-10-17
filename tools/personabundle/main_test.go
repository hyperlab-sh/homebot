package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleCurrent(t *testing.T) {
	root := filepath.Join("..", "..")
	want, err := bundle(filepath.Join(root, "internal/msg/catalog.yaml"), filepath.Join(root, "internal/persona/fable/many.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "internal/persona/fable/entries.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("internal/persona/fable/entries.json is stale; run go generate ./internal/persona")
	}
}

func TestBundleRejectsDuplicateManyID(t *testing.T) {
	dir := t.TempDir()
	catalog := filepath.Join("..", "..", "internal/msg/catalog.yaml")
	many := filepath.Join(dir, "many.txt")
	if err := os.WriteFile(many, []byte("inbox.nudge\ninbox.nudge\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle(catalog, many); err == nil {
		t.Fatal("duplicate many id accepted")
	}
}
