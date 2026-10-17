package msg_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

// The printed command is selected at render time. This list is the registered
// command vocabulary, including hidden commands and option-only root calls.
var literalCommand = regexp.MustCompile(`\bhomebot (?:(?:actions|action|cleanup|export|inbox|keys|link|lock|next|pack|persona|refresh|remote|save|setup|shell|tidy|triage|unlink|unlock|unpack|inventory|version)\b|--[a-z]|-[a-z]|[<\[])`)

func TestCatalogCommandsUseSelf(t *testing.T) {
	if !literalCommand.MatchString("run homebot save") || literalCommand.MatchString("run {self} save") {
		t.Fatal("the command detector does not distinguish the fixture")
	}
	data, err := os.ReadFile("catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := catalog.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range f.Messages {
		// Errors currently use the neutral command because voice.Plain also
		// produces stored error text. The guard, scripts, shell snippets and
		// unit descriptions must be machine-neutral too.
		if e.Kind == "error" || e.Kind == "commit" ||
			strings.HasPrefix(e.ID, "guard.") || strings.HasPrefix(e.ID, "script.") ||
			strings.HasPrefix(e.ID, "shell.") || strings.HasPrefix(e.ID, "helper.unit.") ||
			strings.HasPrefix(e.ID, "triage.decisions.") || e.ID == "remote.repo_description" {
			continue
		}
		for field, value := range map[string]string{"text": e.Text, "append": e.Append} {
			if strings.Contains(value, "eval") && strings.Contains(value, "homebot shell") {
				continue
			}
			if literalCommand.MatchString(value) || (field == "append" && value == " homebot") {
				t.Errorf("%s %s has a literal command: %q", e.ID, field, value)
			}
		}
		for _, p := range e.Placeholders {
			if value, fixed := p.Fixed(); fixed && literalCommand.MatchString(value) {
				t.Errorf("%s value %s has a literal command: %q", e.ID, p.Name, value)
			}
		}
	}
}
