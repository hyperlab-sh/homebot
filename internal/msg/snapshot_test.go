package msg_test

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

var update = flag.Bool("update", false, "rewrite testdata/catalog.snapshot")

// sample is one accessor call of samples_gen_test.go.
type sample struct {
	label string
	text  msg.Text
}

const snapshotPath = "testdata/catalog.snapshot"

const snapshotHeader = `# Every catalog message in the neutral voice, with sample values: counts 3,
# strings <placeholder>, paths /path/to/placeholder, durations "3h ago".
# Messages with a plural are rendered again with counts at 1 ("1h ago"), labelled
# "(1)". Each line is quoted, so trailing spaces show.
# Rewrite with: go test ./internal/msg -run TestCatalogSnapshot -update
`

// TestCatalogSnapshot renders every message, so a change to any of
// homebot's copy shows in one reviewable diff.
func TestCatalogSnapshot(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	var b strings.Builder
	b.WriteString(snapshotHeader)
	for _, s := range samples {
		got := voice.Plain(ctx, s.text)
		if styled := ansi.Strip(voice.Render(ctx, s.text)); styled != got {
			t.Errorf("%s: voice.Render has other words than voice.Plain: %q, %q", s.label, styled, got)
		}
		b.WriteString("\n" + s.label + "\n")
		for _, line := range strings.Split(got, "\n") {
			b.WriteString("  " + strconv.Quote(line) + "\n")
		}
	}
	if *update {
		if err := os.WriteFile(snapshotPath, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(filepath.FromSlash(snapshotPath))
	if err != nil {
		t.Fatalf("%v (create it with -update)", err)
	}
	if d := cmp.Diff(string(want), b.String()); d != "" {
		t.Errorf("catalog snapshot changed (-want +got); rerun with -update if intended:\n%s", d)
	}
}

func TestSamplesCoverTheCatalog(t *testing.T) {
	sampled := map[string]bool{}
	for _, s := range samples {
		id := s.text.ID()
		if s.label != id && s.label != id+" (1)" {
			t.Errorf("sample %q renders %s", s.label, id)
		}
		sampled[id] = true
	}
	entries := testutil.Catalog(t)
	if len(entries) != len(sampled) {
		t.Errorf("%d catalog entries, %d sampled", len(entries), len(sampled))
	}
	for _, e := range entries {
		if !sampled[e.ID] {
			t.Errorf("%s has no sample; run go generate", e.ID)
		}
	}
}
