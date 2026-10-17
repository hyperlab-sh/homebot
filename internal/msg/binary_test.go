package msg_test

import (
	"os/exec"
	"strings"
	"testing"
)

// catalog.yaml is the source of the generated accessors; the binary needs
// only those. Embedding the file would ship about 230 KB of YAML in every
// release for the tests' sake.
func TestReleaseBinaryDoesNotEmbedTheCatalog(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}} {{.EmbedFiles}}", "../../cmd/homebot")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "catalog.yaml") {
			t.Errorf("cmd/homebot embeds the catalog: %s", line)
		}
	}
	if !strings.Contains(string(out), "github.com/hyperlab-sh/homebot/internal/msg ") {
		t.Errorf("go list did not list internal/msg as a dependency:\n%s", out)
	}
}
