package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

// Catalog loads internal/msg/catalog.yaml, in file order, for checks that
// need more than the generated accessors give, such as validating a persona.
// Only tests read the YAML: the binary carries the generated accessors, not
// the file, so it is read from the source tree, found from this file's own
// path. Every call returns a fresh copy.
func Catalog(t testing.TB) []catalog.Entry {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("testutil: cannot find the source tree")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(here), "..", "msg", "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := catalog.Load(data)
	if err != nil {
		t.Fatalf("the catalog does not load: %v", err)
	}
	return f.Messages
}
