package persona

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// persona sits below voice and ui, which ask it which voice is in effect
// (plan S1, N1), and below fsx, which imports voice. It must never import
// them, directly or through another package.
func TestPersonaImportsNothingAboveIt(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go command is not on PATH")
	}
	out, err := exec.Command(goTool, "list", "-deps", "-f", "{{.ImportPath}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	const mod = "github.com/hyperlab-sh/homebot/internal/"
	var internal []string
	for _, p := range strings.Fields(string(out)) {
		if name, ok := strings.CutPrefix(p, mod); ok {
			internal = append(internal, name)
		}
	}
	for _, banned := range []string{"ui", "voice", "fsx", "state", "inbox", "cli"} {
		for _, p := range internal {
			if p == banned || strings.HasPrefix(p, banned+"/") {
				t.Errorf("persona imports %s (its internal dependencies: %v)", p, internal)
			}
		}
	}
	// What it may import, by the plan: app, config, msg, msg/catalog, proc,
	// and what those import.
	allowed := []string{"persona", "app", "config", "msg", "msg/catalog", "proc", "fail", "layout"}
	for _, p := range internal {
		if !slices.Contains(allowed, p) {
			t.Errorf("persona depends on %s, outside %v", p, allowed)
		}
	}
}
