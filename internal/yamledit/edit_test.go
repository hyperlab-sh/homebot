package yamledit

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type editCase struct {
	name string
	src  string
	path string // dot-separated
	arg  string
	want string
}

func pathOf(p string) []string {
	if p == "" {
		return nil
	}
	return strings.Split(p, ".")
}

func TestSetScalar(t *testing.T) {
	cases := []editCase{
		{
			name: "replaces a plain value and keeps the trailing comment",
			src:  "# top\nprofiles:\n  default: desk   # the usual\n  hosts: {}\n",
			path: "profiles.default", arg: "laptop",
			want: "# top\nprofiles:\n  default: laptop   # the usual\n  hosts: {}\n",
		},
		{
			name: "replaces a double-quoted value",
			src:  "persona: \"tester\" # voice\nother: 1\n",
			path: "persona", arg: "none",
			want: "persona: none # voice\nother: 1\n",
		},
		{
			name: "replaces a single-quoted value with an escaped quote",
			src:  "persona: 'it''s'\nz: 1\n",
			path: "persona", arg: "x",
			want: "persona: x\nz: 1\n",
		},
		{
			name: "replaces a value that is not a string",
			src:  "limits:\n  maxFileMB: 50\n",
			path: "limits.maxFileMB", arg: "20x",
			want: "limits:\n  maxFileMB: 20x\n",
		},
		{
			name: "replaces inside a flow mapping",
			src:  "profiles:\n  hosts: { forge: workstation, mac: laptop }  # two\n",
			path: "profiles.hosts.forge", arg: "desk",
			want: "profiles:\n  hosts: { forge: desk, mac: laptop }  # two\n",
		},
		{
			name: "replaces the last line of a file with no final newline",
			src:  "a: 1\nb: old",
			path: "b", arg: "new",
			want: "a: 1\nb: new",
		},
		{
			name: "fills an empty value",
			src:  "a:   # nothing yet\nb: 1\n",
			path: "a", arg: "x",
			want: "a: x   # nothing yet\nb: 1\n",
		},
		{
			name: "replaces an explicit null",
			src:  "a: null # none\nb: ~\n",
			path: "b", arg: "x",
			want: "a: null # none\nb: x\n",
		},
		{
			name: "survives non-ASCII text before the target on its line",
			src:  "café: \"é\"   # naïve\nb: 1\n",
			path: "café", arg: "x",
			want: "café: x   # naïve\nb: 1\n",
		},
		{
			name: "adds a key at the end of the top-level mapping, before trailing comments",
			src:  "# header\na: 1   # one\n\nb: 2\n\n# footer\n",
			path: "c", arg: "three",
			want: "# header\na: 1   # one\n\nb: 2\nc: three\n\n# footer\n",
		},
		{
			name: "adds a key after a nested block, not inside it",
			src:  "profiles:\n  default: desk\n  hosts:\n    forge: ws\nlimits:\n  maxFileMB: 5\n",
			path: "persona", arg: "tester",
			want: "profiles:\n  default: desk\n  hosts:\n    forge: ws\nlimits:\n  maxFileMB: 5\npersona: tester\n",
		},
		{
			name: "adds a key to a nested mapping with its indentation",
			src:  "profiles:\n    default: desk\n    hosts:\n        forge: ws   # primary\n\n# next\nlimits: {}\n",
			path: "profiles.hosts.mac", arg: "laptop",
			want: "profiles:\n    default: desk\n    hosts:\n        forge: ws   # primary\n        mac: laptop\n\n# next\nlimits: {}\n",
		},
		{
			name: "creates a missing parent mapping, learning the file's indent step",
			src:  "limits:\n    maxFileMB: 5\n",
			path: "profiles.hosts.forge", arg: "ws",
			want: "limits:\n    maxFileMB: 5\nprofiles:\n    hosts:\n        forge: ws\n",
		},
		{
			name: "creates a missing parent below an existing key",
			src:  "profiles:\n  default: desk\n",
			path: "profiles.hosts.forge", arg: "ws",
			want: "profiles:\n  default: desk\n  hosts:\n    forge: ws\n",
		},
		{
			name: "puts a new key after the comment-separated end of a trailing block scalar",
			src:  "note: |\n  line one\n  # not a comment\n\nz: 1\n",
			path: "after", arg: "x",
			want: "note: |\n  line one\n  # not a comment\n\nz: 1\nafter: x\n",
		},
		{
			name: "adds after a last entry that is a block scalar",
			src:  "a: 1\nnote: |\n  line one\n  # kept\n\n# footer\n",
			path: "b", arg: "two",
			want: "a: 1\nnote: |\n  line one\n  # kept\nb: two\n\n# footer\n",
		},
		{
			name: "adds after a last entry that is a sequence at its key's indent",
			src:  "list:\n- a\n- b\n# end\n",
			path: "c", arg: "one",
			want: "list:\n- a\n- b\nc: one\n# end\n",
		},
		{
			name: "adds after a last entry that is a multi-line flow collection",
			src:  "list: [\n  a,\n  b,\n]\n# end\n",
			path: "c", arg: "one",
			want: "list: [\n  a,\n  b,\n]\nc: one\n# end\n",
		},
		{
			name: "adds to a flow mapping",
			src:  "profiles:\n  hosts: { forge: ws }\n",
			path: "profiles.hosts.mac", arg: "laptop",
			want: "profiles:\n  hosts: { forge: ws, mac: laptop }\n",
		},
		{
			name: "adds to an empty flow mapping",
			src:  "profiles:\n  hosts: {}\n  default: d\n",
			path: "profiles.hosts.mac", arg: "laptop",
			want: "profiles:\n  hosts: {mac: laptop}\n  default: d\n",
		},
		{
			name: "adds to a flow mapping with a trailing comma",
			src:  "h: {a: 1, }\n",
			path: "h.b", arg: "two",
			want: "h: {a: 1, b: two }\n",
		},
		{
			name: "creates nested keys inside a flow mapping",
			src:  "h: {a: 1}\n",
			path: "h.b.c", arg: "two",
			want: "h: {a: 1, b: {c: two}}\n",
		},
		{
			name: "creates a block under a key with an empty value",
			src:  "profiles:\n  hosts:   # none yet\nlimits: {}\n",
			path: "profiles.hosts.forge", arg: "ws",
			want: "profiles:\n  hosts:   # none yet\n    forge: ws\nlimits: {}\n",
		},
		{
			name: "creates a block under an explicit null",
			src:  "profiles:\n  hosts: ~ # none yet\n",
			path: "profiles.hosts.forge", arg: "ws",
			want: "profiles:\n  hosts: # none yet\n    forge: ws\n",
		},
		{
			name: "fills an empty top-level key",
			src:  "profiles:\nlimits: {}\n",
			path: "profiles.default", arg: "desk",
			want: "profiles:\n  default: desk\nlimits: {}\n",
		},
		{
			name: "writes into an empty file",
			src:  "",
			path: "persona", arg: "tester",
			want: "persona: tester\n",
		},
		{
			name: "writes nested keys into an empty file",
			src:  "",
			path: "a.b", arg: "c",
			want: "a:\n  b: c\n",
		},
		{
			name: "writes into a comment-only file after the comments",
			src:  "# yaml-language-server: $schema=x\n# nothing here\n",
			path: "persona", arg: "tester",
			want: "# yaml-language-server: $schema=x\n# nothing here\npersona: tester\n",
		},
		{
			name: "writes into a comment-only file with no final newline",
			src:  "# nothing",
			path: "a", arg: "b",
			want: "# nothing\na: b\n",
		},
		{
			name: "adds after the last line when the file has no final newline",
			src:  "a: 1\nb: 2",
			path: "c", arg: "three",
			want: "a: 1\nb: 2\nc: three",
		},
		{
			name: "quotes a value that YAML 1.1 reads as a boolean",
			src:  "a: 1\n",
			path: "b", arg: "yes",
			want: "a: 1\nb: \"yes\"\n",
		},
		{
			name: "quotes a value that looks like a number",
			src:  "a: 1\n",
			path: "a", arg: "123",
			want: "a: \"123\"\n",
		},
		{
			name: "quotes a value with a colon and a space",
			src:  "a: 1\n",
			path: "b", arg: "x: y",
			want: "a: 1\nb: \"x: y\"\n",
		},
		{
			name: "quotes an empty value",
			src:  "a: 1\n",
			path: "b", arg: "",
			want: "a: 1\nb: \"\"\n",
		},
		{
			name: "quotes and escapes a value with quotes and a newline",
			src:  "a: 1\n",
			path: "b", arg: "say \"hi\"\nnow",
			want: "a: 1\nb: \"say \\\"hi\\\"\\nnow\"\n",
		},
		{
			name: "quotes a value with flow punctuation only in flow context",
			src:  "h: {a: 1}\np: 2\n",
			path: "h.b", arg: "x,y",
			want: "h: {a: 1, b: \"x,y\"}\np: 2\n",
		},
		{
			name: "keeps a plain value with a hash and no space",
			src:  "a: 1\n",
			path: "b", arg: "x#y",
			want: "a: 1\nb: x#y\n",
		},
		{
			name: "CRLF: replaces in place",
			src:  "a: 1\r\nb: old # c\r\n",
			path: "b", arg: "new",
			want: "a: 1\r\nb: new # c\r\n",
		},
		{
			name: "CRLF: inserts with CRLF line endings",
			src:  "a: 1\r\n\r\n# end\r\n",
			path: "p.q", arg: "v",
			want: "a: 1\r\np:\r\n  q: v\r\n\r\n# end\r\n",
		},
		{
			name: "CRLF: appends to a file with no final newline",
			src:  "a: 1\r\nb: 2",
			path: "c", arg: "three",
			want: "a: 1\r\nb: 2\r\nc: three",
		},
		{
			name: "tabs and odd spacing elsewhere are untouched",
			src:  "a:\t1\nb:    2\n\n\n  # indented comment\nc: 3\n",
			path: "b", arg: "nine",
			want: "a:\t1\nb:    nine\n\n\n  # indented comment\nc: 3\n",
		},
		{
			name: "anchored values keep their anchor",
			src:  "a: &x old\nb: *x\n",
			path: "a", arg: "new",
			want: "a: &x new\nb: *x\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SetScalar([]byte(c.src), pathOf(c.path), c.arg)
			if err != nil {
				t.Fatalf("SetScalar: %v", err)
			}
			if diff := cmp.Diff(c.want, string(got)); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetScalarAwkwardKey(t *testing.T) {
	got, err := SetScalar([]byte("a: 1\n"), []string{"my key: x"}, "v")
	if err != nil {
		t.Fatal(err)
	}
	if want := "a: 1\n\"my key: x\": v\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetScalarErrors(t *testing.T) {
	cases := []struct {
		name, src, path, want string
	}{
		{"empty path", "a: 1\n", "", "non-empty path"},
		{"path through a scalar", "a: 1\n", "a.b", "a is not a mapping"},
		{"path through a sequence", "a: [1]\n", "a.b", "a is not a mapping"},
		{"target is a mapping", "a:\n  b: 1\n", "a", "a is not a scalar"},
		{"target is a sequence", "a: [1]\n", "a", "a is not a scalar"},
		{"target is a block scalar", "a: |\n  text\n", "a", "block scalar"},
		{"invalid YAML", "a: b\n  c: d\n", "a", "invalid YAML"},
		{"more than one document", "a: 1\n---\nb: 2\n", "a", "more than one"},
		{"root is a sequence", "- a\n", "x", "the document is not a mapping"},
		{"empty value inside a flow mapping", "h: {a: }\n", "h.a.b", "empty value inside a flow mapping"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := []byte(c.src)
			_, err := SetScalar(src, pathOf(c.path), "v")
			if err == nil {
				t.Fatal("SetScalar succeeded, want an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to contain %q", err, c.want)
			}
			if string(src) != c.src {
				t.Errorf("SetScalar modified its input")
			}
		})
	}
}

func TestAppendToSequence(t *testing.T) {
	cases := []editCase{
		{
			name: "appends to an indented block sequence, keeping comments",
			src:  "ignore:\n  - \"*.log\"   # logs\n  - tmp\n\n# end\n",
			path: "ignore", arg: "cache",
			want: "ignore:\n  - \"*.log\"   # logs\n  - tmp\n  - cache\n\n# end\n",
		},
		{
			name: "appends to a sequence written at its key's indent",
			src:  "ignore:\n- a\n- b\nnext: 1\n",
			path: "ignore", arg: "c",
			want: "ignore:\n- a\n- b\n- c\nnext: 1\n",
		},
		{
			name: "appends after a multi-line last item",
			src:  "exports:\n  - name: a\n    out: x\n  - name: b\n    out: y\n    # trailing\nlimits: {}\n",
			path: "exports", arg: "z",
			want: "exports:\n  - name: a\n    out: x\n  - name: b\n    out: y\n  - z\n    # trailing\nlimits: {}\n",
		},
		{
			name: "appends to a nested sequence",
			src:  "a:\n  b:\n    - 1\n    - 2\n  c: 3\n",
			path: "a.b", arg: "x",
			want: "a:\n  b:\n    - 1\n    - 2\n    - x\n  c: 3\n",
		},
		{
			name: "appends to a flow sequence",
			src:  "ignore: [a, b]   # two\nnext: 1\n",
			path: "ignore", arg: "c",
			want: "ignore: [a, b, c]   # two\nnext: 1\n",
		},
		{
			name: "appends to an empty flow sequence",
			src:  "ignore: []\n",
			path: "ignore", arg: "c",
			want: "ignore: [c]\n",
		},
		{
			name: "appends to a multi-line flow sequence",
			src:  "ignore: [\n  a,\n  b,\n]\nnext: 1\n",
			path: "ignore", arg: "c",
			want: "ignore: [\n  a,\n  b, c\n]\nnext: 1\n",
		},
		{
			name: "quotes a flow item with a comma",
			src:  "ignore: [a]\n",
			path: "ignore", arg: "b,c",
			want: "ignore: [a, \"b,c\"]\n",
		},
		{
			name: "quotes a block item that starts with an asterisk",
			src:  "ignore:\n  - a\n",
			path: "ignore", arg: "*.tmp",
			want: "ignore:\n  - a\n  - \"*.tmp\"\n",
		},
		{
			name: "fills an empty key with a sequence",
			src:  "ignore:   # none\nnext: 1\n",
			path: "ignore", arg: "a",
			want: "ignore:   # none\n  - a\nnext: 1\n",
		},
		{
			name: "creates the sequence at the end of the mapping",
			src:  "# decisions\nversion: 1\n\n# end\n",
			path: "ignore", arg: "a",
			want: "# decisions\nversion: 1\nignore:\n  - a\n\n# end\n",
		},
		{
			name: "creates a missing parent and the sequence",
			src:  "version: 1\n",
			path: "triage.ignore", arg: "a",
			want: "version: 1\ntriage:\n  ignore:\n    - a\n",
		},
		{
			name: "creates the sequence inside a flow mapping",
			src:  "h: {a: 1}\n",
			path: "h.list", arg: "x",
			want: "h: {a: 1, list: [x]}\n",
		},
		{
			name: "creates a sequence in an empty file",
			src:  "",
			path: "ignore", arg: "a",
			want: "ignore:\n  - a\n",
		},
		{
			name: "appends to a root sequence",
			src:  "- a\n- b\n",
			path: "", arg: "c",
			want: "- a\n- b\n- c\n",
		},
		{
			name: "starts a root sequence in an empty file",
			src:  "",
			path: "", arg: "c",
			want: "- c\n",
		},
		{
			name: "no final newline",
			src:  "ignore:\n  - a",
			path: "ignore", arg: "b",
			want: "ignore:\n  - a\n  - b",
		},
		{
			name: "CRLF",
			src:  "x: 1\r\nignore:\r\n  - a\r\n# end\r\n",
			path: "ignore", arg: "b",
			want: "x: 1\r\nignore:\r\n  - a\r\n  - b\r\n# end\r\n",
		},
		{
			name: "CRLF: creates the sequence",
			src:  "x: 1\r\n",
			path: "ignore", arg: "b",
			want: "x: 1\r\nignore:\r\n  - b\r\n",
		},
		{
			name: "non-ASCII items",
			src:  "ignore:\n  - é\n",
			path: "ignore", arg: "ü",
			want: "ignore:\n  - é\n  - ü\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := AppendToSequence([]byte(c.src), pathOf(c.path), c.arg)
			if err != nil {
				t.Fatalf("AppendToSequence: %v", err)
			}
			if diff := cmp.Diff(c.want, string(got)); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

func TestAppendToSequenceErrors(t *testing.T) {
	cases := []struct{ name, src, path, want string }{
		{"target is a scalar", "a: 1\n", "a", "a is not a sequence"},
		{"target is a mapping", "a:\n  b: 1\n", "a", "a is not a sequence"},
		{"path through a scalar", "a: 1\n", "a.b", "a is not a mapping"},
		{"root is a mapping", "a: 1\n", "", "not a sequence"},
		{"invalid YAML", "a: [\n", "a", "invalid YAML"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := AppendToSequence([]byte(c.src), pathOf(c.path), "v")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

// Only the edited bytes may differ: the output is the input with one
// contiguous region replaced (or inserted).
func TestEditsOnlyAddOrReplaceTheTarget(t *testing.T) {
	src := "# header  \n\nprofiles:   \n  default:   desk\t# tab\n  hosts: { forge:   ws }\n\n\nlist:\n -   a\n -   b   \n# footer"
	for _, c := range []struct {
		name string
		run  func() ([]byte, error)
	}{
		{"set existing", func() ([]byte, error) { return SetScalar([]byte(src), []string{"profiles", "default"}, "x") }},
		{"set flow", func() ([]byte, error) { return SetScalar([]byte(src), []string{"profiles", "hosts", "mac"}, "y") }},
		{"set new top-level", func() ([]byte, error) { return SetScalar([]byte(src), []string{"persona"}, "m") }},
		{"append", func() ([]byte, error) { return AppendToSequence([]byte(src), []string{"list"}, "c") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := c.run()
			if err != nil {
				t.Fatal(err)
			}
			got := string(out)
			prefix := 0
			for prefix < len(src) && prefix < len(got) && src[prefix] == got[prefix] {
				prefix++
			}
			suffix := 0
			for suffix < len(src)-prefix && suffix < len(got)-prefix && src[len(src)-1-suffix] == got[len(got)-1-suffix] {
				suffix++
			}
			removed := src[prefix : len(src)-suffix]
			added := got[prefix : len(got)-suffix]
			if len(removed) > 12 || len(added) > 24 {
				t.Errorf("changed region too large: %q -> %q", removed, added)
			}
			if src[:prefix]+removed+src[len(src)-suffix:] != src || got[:prefix]+added+got[len(got)-suffix:] != got {
				t.Error("changed region does not account for the difference")
			}
		})
	}
}

func TestCRLFFlowAndNullParents(t *testing.T) {
	cases := []struct {
		name string
		run  func() ([]byte, error)
		want string
	}{
		{"flow mapping insert", func() ([]byte, error) {
			return SetScalar([]byte("h: {a: b}\r\nz: 1\r\n"), []string{"h", "c"}, "d")
		}, "h: {a: b, c: d}\r\nz: 1\r\n"},
		{"multi-line flow sequence", func() ([]byte, error) {
			return AppendToSequence([]byte("l: [\r\n  a,\r\n]\r\nz: 1\r\n"), []string{"l"}, "b")
		}, "l: [\r\n  a, b\r\n]\r\nz: 1\r\n"},
		{"empty key gets a mapping", func() ([]byte, error) {
			return SetScalar([]byte("hosts:\r\nz: 1\r\n"), []string{"hosts", "forge"}, "ws")
		}, "hosts:\r\n  forge: ws\r\nz: 1\r\n"},
		{"explicit null gets a mapping", func() ([]byte, error) {
			return SetScalar([]byte("hosts: ~ # c\r\nz: 1\r\n"), []string{"hosts", "forge"}, "ws")
		}, "hosts: # c\r\n  forge: ws\r\nz: 1\r\n"},
		{"empty key gets a sequence", func() ([]byte, error) {
			return AppendToSequence([]byte("l:\r\nz: 1\r\n"), []string{"l"}, "a")
		}, "l:\r\n  - a\r\nz: 1\r\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.run()
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, string(got)); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}
