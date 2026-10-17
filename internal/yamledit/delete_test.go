package yamledit

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDeleteScalar(t *testing.T) {
	cases := []editCase{
		{
			name: "removes a top-level key and its trailing comment, keeps the other comments",
			src:  "# top\nprofiles:\n  default: a\n# about the name\nname: zork # mine\n# tail\nlimits:\n  maxFileMB: 5\n",
			path: "name",
			want: "# top\nprofiles:\n  default: a\n# about the name\n# tail\nlimits:\n  maxFileMB: 5\n",
		},
		{
			name: "removes the first key",
			src:  "name: zork\nprofiles:\n  default: a\n",
			path: "name",
			want: "profiles:\n  default: a\n",
		},
		{
			name: "removes the last line of a file with no final newline",
			src:  "profiles:\n  default: a\nname: zork",
			path: "name",
			want: "profiles:\n  default: a",
		},
		{
			name: "last key keeps the final newline",
			src:  "profiles:\n  default: a\nname: zork\n",
			path: "name",
			want: "profiles:\n  default: a\n",
		},
		{
			name: "last key keeps CRLF and the final newline",
			src:  "a: 1\r\nname: zork\r\n",
			path: "name",
			want: "a: 1\r\n",
		},
		{
			name: "last nested key keeps the final newline",
			src:  "a:\n  b: 1\n  name: zork\n",
			path: "a.name",
			want: "a:\n  b: 1\n",
		},
		{
			name: "file starting with a document marker",
			src:  "---\nname: zork\n",
			path: "name",
			want: "---\n",
		},
		{
			name: "multi-line flow mapping, last member",
			src:  "h: {\n  a: 1,\n  name: zork\n}\n",
			path: "h.name",
			want: "h: {\n  a: 1\n}\n",
		},
		{
			name: "multi-line flow mapping, last member with a trailing comma",
			src:  "h: {\n  a: 1,\n  name: zork,\n}\n",
			path: "h.name",
			want: "h: {\n  a: 1,\n}\n",
		},
		{
			name: "multi-line flow mapping, middle member",
			src:  "h: {\n  a: 1,\n  name: zork,\n  b: 2\n}\n",
			path: "h.name",
			want: "h: {\n  a: 1,\n  b: 2\n}\n",
		},
		{
			name: "flow mapping, empty last member",
			src:  "h: {a: 1, name: }\n",
			path: "h.name",
			want: "h: {a: 1}\n",
		},
		{
			name: "removes the only key",
			src:  "name: zork\n",
			path: "name",
			want: "",
		},
		{
			name: "removes a quoted value",
			src:  "a: 1\nname: \"zork\"\nb: 2\n",
			path: "name",
			want: "a: 1\nb: 2\n",
		},
		{
			name: "removes an empty value",
			src:  "a: 1\nname:\nb: 2\n",
			path: "name",
			want: "a: 1\nb: 2\n",
		},
		{
			name: "removes a nested key",
			src:  "profiles:\n  default: a\n  extra: b # c\n  hosts: {}\n",
			path: "profiles.extra",
			want: "profiles:\n  default: a\n  hosts: {}\n",
		},
		{
			name: "keeps CRLF line endings",
			src:  "a: 1\r\nname: zork\r\nb: 2\r\n",
			path: "name",
			want: "a: 1\r\nb: 2\r\n",
		},
		{
			name: "a missing key changes nothing",
			src:  "# only\na: 1\n",
			path: "name",
			want: "# only\na: 1\n",
		},
		{
			name: "a missing key in an empty file changes nothing",
			src:  "",
			path: "name",
			want: "",
		},
		{
			name: "a missing nested parent changes nothing",
			src:  "a: 1\n",
			path: "profiles.default",
			want: "a: 1\n",
		},
		{
			name: "flow mapping, middle member",
			src:  "h: { a: 1, name: zork, b: 2 } # c\n",
			path: "h.name",
			want: "h: { a: 1, b: 2 } # c\n",
		},
		{
			name: "flow mapping, last member",
			src:  "h: { a: 1, name: zork }\n",
			path: "h.name",
			want: "h: { a: 1 }\n",
		},
		{
			name: "flow mapping, only member",
			src:  "h: {name: zork}\n",
			path: "h.name",
			want: "h: {}\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := []byte(c.src)
			got, err := DeleteScalar(in, pathOf(c.path))
			if err != nil {
				t.Fatalf("DeleteScalar: %v", err)
			}
			if diff := cmp.Diff(c.want, string(got)); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
			if string(in) != c.src {
				t.Errorf("DeleteScalar modified its input")
			}
			again, err := DeleteScalar(got, pathOf(c.path))
			if err != nil || string(again) != string(got) {
				t.Errorf("deleting twice: %q, %v; want the same text and no error", again, err)
			}
		})
	}
}

func TestDeleteScalarErrors(t *testing.T) {
	cases := []struct {
		name, src, path, want string
	}{
		{"empty path", "a: 1\n", "", "non-empty path"},
		{"target is a mapping", "a:\n  b: 1\n", "a", "a is not a scalar"},
		{"target is a sequence", "a: [1]\n", "a", "a is not a scalar"},
		{"path through a scalar", "a: 1\n", "a.b", "a is not a mapping"},
		{"invalid YAML", "a: b\n  c: d\n", "a", "invalid YAML"},
		{"root is a sequence", "- a\n", "x", "the document is not a mapping"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DeleteScalar([]byte(c.src), pathOf(c.path))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

// Set then delete gives back the original text exactly.
func TestSetDeleteRoundTrip(t *testing.T) {
	for _, src := range []string{
		"profiles:\n  default: a\n",
		"# keep\nprofiles:\n  default: a # c\n\n# end\n",
		"a: 1",
		"a: 1\n",
		"",
		"---\nprofiles:\n  default: a\n",
		"profiles:\r\n  default: a\r\n",
	} {
		set, err := SetScalar([]byte(src), []string{"name"}, "zork")
		if err != nil {
			t.Fatalf("SetScalar(%q): %v", src, err)
		}
		if !strings.Contains(string(set), "name: zork") {
			t.Fatalf("SetScalar(%q) = %q, no name", src, set)
		}
		back, err := DeleteScalar(set, []string{"name"})
		if err != nil {
			t.Fatalf("DeleteScalar(%q): %v", set, err)
		}
		want := src
		if string(back) != want {
			t.Errorf("set then delete of %q gave %q, want %q", src, back, want)
		}
	}
}
