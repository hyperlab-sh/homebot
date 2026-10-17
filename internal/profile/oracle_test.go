package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func oracle(t *testing.T, name string, into any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "oracle", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatal(err)
	}
}

func TestStepsHashOracle(t *testing.T) {
	var vectors []struct {
		Name  string
		Steps [][2]json.RawMessage
		Hash  string
	}
	oracle(t, "stepshash.json", &vectors)
	if len(vectors) == 0 {
		t.Fatal("no vectors")
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			steps := []Step{}
			for _, pair := range v.Steps {
				var s Step
				if err := json.Unmarshal(pair[0], &s.Action); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(pair[1], &s.Args); err != nil {
					t.Fatal(err)
				}
				steps = append(steps, s)
			}
			if got := StepsHash(steps); got != v.Hash {
				t.Errorf("StepsHash = %s, want %s", got, v.Hash)
			}
		})
	}
}

// Bun words its own YAML syntax errors; Go can only promise the part before them.
const yamlErrorPrefix = "profile.yml: invalid YAML: "

func TestYAMLScalarOracle(t *testing.T) {
	var vectors []struct {
		YAML  string   `json:"yaml"`
		Args  []string `json:"args"`
		Error string   `json:"error"`
	}
	oracle(t, "yaml-scalars.json", &vectors)
	if len(vectors) == 0 {
		t.Fatal("no vectors")
	}
	for _, v := range vectors {
		t.Run(v.YAML, func(t *testing.T) {
			p, err := Parse("steps:\n  - fonts: "+v.YAML+"\n", "profile.yml", "profile")
			if v.Error != "" {
				if err == nil {
					t.Fatalf("parsed to %v, want error %q", p.Steps[0].Args, v.Error)
				}
				if strings.HasPrefix(v.Error, yamlErrorPrefix) {
					if !strings.HasPrefix(err.Error(), yamlErrorPrefix) {
						t.Fatalf("error %q, want one starting %q", err, yamlErrorPrefix)
					}
					return
				}
				if err.Error() != v.Error {
					t.Fatalf("error %q, want %q", err, v.Error)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := v.Args
			if want == nil {
				want = []string{}
			}
			if diff := cmp.Diff(want, p.Steps[0].Args); diff != "" {
				t.Errorf("args (-want +got):\n%s", diff)
			}
		})
	}
}
