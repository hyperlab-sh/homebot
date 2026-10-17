package fsx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// entries lists the names in dir, to catch temp files left behind.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestSha256(t *testing.T) {
	for in, want := range map[string]string{
		"":    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"abc": "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	} {
		if got := Sha256([]byte(in)); got != want {
			t.Errorf("Sha256(%q) = %s, want %s", in, got, want)
		}
	}
	path := filepath.Join(t.TempDir(), "f")
	write(t, path, "abc")
	if got, err := FileSha256(path); err != nil || got != Sha256([]byte("abc")) {
		t.Errorf("FileSha256 = %s, %v", got, err)
	}
	if _, err := FileSha256(path + ".missing"); err == nil {
		t.Error("FileSha256 of a missing file gave no error")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "state.json")

	if err := WriteFileAtomic(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "one" {
		t.Errorf("content = %q", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	if err := WriteFileAtomic(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "two" {
		t.Errorf("content after a second write = %q", got)
	}
	if got := entries(t, filepath.Dir(path)); !slices.Equal(got, []string{"state.json"}) {
		t.Errorf("dir holds %q, want only state.json", got)
	}

	if err := WriteFileAtomic(filepath.Join(dir, "empty"), nil, 0o644); err != nil || read(t, filepath.Join(dir, "empty")) != "" {
		t.Errorf("empty write: %v", err)
	}
}

func TestWriteFileAtomicReplacesASymlinkNotItsTarget(t *testing.T) {
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target"), filepath.Join(dir, "link")
	write(t, target, "target")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(link, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink != 0 {
		t.Error("link is still a symlink")
	}
	if read(t, target) != "target" || read(t, link) != "new" {
		t.Errorf("target = %q, link = %q", read(t, target), read(t, link))
	}
}

func TestWriteFileAtomicFailureLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "taken")
	write(t, filepath.Join(target, "inside"), "x")
	if err := WriteFileAtomic(target, []byte("data"), 0o644); err == nil {
		t.Fatal("writing over a non-empty directory succeeded")
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"taken"}) {
		t.Errorf("dir holds %q after a failed write", got)
	}

	blocker := filepath.Join(dir, "file")
	write(t, blocker, "x")
	if err := WriteFileAtomic(filepath.Join(blocker, "child"), []byte("data"), 0o644); err == nil {
		t.Error("writing below a regular file succeeded")
	}
}

func TestWriteFileAtomicIgnoresALeftoverTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	leftovers := []string{path + ".tmp-", path + ".tmp-1", fmt.Sprintf("%s.tmp-%d-1", path, os.Getpid())}
	for _, name := range leftovers {
		write(t, name, "crashed")
	}
	for i := range 3 {
		if err := WriteFileAtomic(path, []byte(fmt.Sprint(i)), 0o644); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if got := read(t, path); got != "2" {
		t.Errorf("content = %q", got)
	}
	for _, name := range leftovers {
		if read(t, name) != "crashed" {
			t.Errorf("%s was touched", name)
		}
	}
	if got := len(entries(t, dir)); got != 1+len(leftovers) {
		t.Errorf("dir holds %d entries, want the file and the %d leftovers", got, len(leftovers))
	}
}

func TestWriteFileAtomicResetsTheModeEveryWrite(t *testing.T) {
	old := syscall.Umask(0o027)
	t.Cleanup(func() { syscall.Umask(old) })

	path := filepath.Join(t.TempDir(), "state")
	write(t, path, "x")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("y"), 0o666); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640: perm 0666 under umask 027, not the old 0600", info.Mode().Perm())
	}
	if err := WriteJSON(path, 1); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Errorf("WriteJSON mode = %v, want 0640", info.Mode().Perm())
	}
}

func TestWriteFileAtomicConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Go(func() { errs <- WriteFileAtomic(path, []byte(fmt.Sprint("writer ", i)), 0o644) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if got := read(t, path); !strings.HasPrefix(got, "writer ") {
		t.Errorf("content = %q, want one writer's whole text", got)
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"state"}) {
		t.Errorf("dir holds %q", got)
	}
}

func TestWriteJSONMatchesJavaScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	v := map[string]any{"b": []int{1, 2}, "a": "<x> & y", "c": map[string]any{}, "d": []string{}, "e": "\b\f\a\x7f"}
	if err := WriteJSON(path, v); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": \"<x> & y\",\n  \"b\": [\n    1,\n    2\n  ],\n  \"c\": {},\n  \"d\": [],\n  \"e\": \"\\b\\f\\u0007\x7f\"\n}\n"
	if got := read(t, path); got != want {
		t.Errorf("WriteJSON wrote\n%s\nwant\n%s", got, want)
	}
	if err := WriteJSON(path, func() {}); err == nil {
		t.Error("WriteJSON of a func gave no error")
	}
	if read(t, path) != want {
		t.Error("a failed WriteJSON changed the file")
	}
}

func TestWriteJSONKeepsLineSeparatorsRaw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	v := map[string]string{"sep": "a\u2028b\u2029c", "text": `\u2028 typed by hand`, "slash": `\`}
	if err := WriteJSON(path, v); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"sep\": \"a\u2028b\u2029c\",\n  \"slash\": \"\\\\\",\n  \"text\": \"\\\\u2028 typed by hand\"\n}\n"
	if got := read(t, path); got != want {
		t.Errorf("WriteJSON wrote %q, want %q", got, want)
	}
	var back map[string]string
	if err := ReadJSON(testutil.Sandbox(t, testutil.Options{}).Ctx, path, &back); err != nil || back["sep"] != v["sep"] || back["text"] != v["text"] || back["slash"] != v["slash"] {
		t.Errorf("round trip = %q, %v", back, err)
	}
}

func TestRawLineSeparators(t *testing.T) {
	for in, want := range map[string]string{
		`"\u2028"`:           "\"\u2028\"",
		`"\u2029\u2028"`:     "\"\u2029\u2028\"",
		`"\\u2028"`:          `"\\u2028"`,
		`"\\\u2028"`:         "\"\\\\\u2028\"",
		`"\u2027\u202a"`:     `"\u2027\u202a"`,
		`"\u202"`:            `"\u202"`,
		`"no escapes"`:       `"no escapes"`,
		`"\n\u2028\t"`:       "\"\\n\u2028\\t\"",
		`trailing \u202`:     `trailing \u202`,
		`ends in backslash\`: `ends in backslash\`,
	} {
		if got := string(RawLineSeparators([]byte(in))); got != want {
			t.Errorf("RawLineSeparators(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestReadJSON(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	c := sb.Ctx
	dir := filepath.Join(c.Home, ".local", "state", "homebot")
	type state struct{ N int }

	got := state{N: 7}
	if err := ReadJSON(c, filepath.Join(dir, "missing.json"), &got); err != nil || got.N != 7 {
		t.Errorf("missing file: %+v, %v; want the fallback kept and no error", got, err)
	}

	write(t, filepath.Join(dir, "ok.json"), `{"N": 3}`)
	if err := ReadJSON(c, filepath.Join(dir, "ok.json"), &got); err != nil || got.N != 3 {
		t.Errorf("valid file: %+v, %v", got, err)
	}

	// The catalog's words, with the path under $HOME shortened to ~.
	for name, text := range map[string]string{"bad.json": "{oops", "empty.json": ""} {
		path := filepath.Join(dir, name)
		write(t, path, text)
		err := ReadJSON(c, path, &got)
		want := "~/.local/state/homebot/" + name + " is not valid JSON. Fix or delete it (it is machine state, safe to delete)."
		if err == nil || err.Error() != want {
			t.Errorf("%s: err = %v, want %q", name, err, want)
		}
		var user *fail.UserError
		if !errors.As(err, &user) {
			t.Errorf("%s: %T is not a user error, so it would print with a stack of causes", name, err)
		}
	}

	// A path outside $HOME stays as it is.
	outside := filepath.Join(t.TempDir(), "bad.json")
	write(t, outside, "{oops")
	if err := ReadJSON(c, outside, &got); err == nil || !strings.HasPrefix(err.Error(), outside+" is not valid JSON") {
		t.Errorf("outside $HOME: err = %v", err)
	}

	if err := ReadJSON(c, dir, &got); err == nil || strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("reading a directory: err = %v, want the read error itself", err)
	}
}

func TestLstatIfExistsAndIsDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	write(t, file, "x")
	dirLink := filepath.Join(dir, "dir-link")
	if err := os.Symlink(dir, dirLink); err != nil {
		t.Fatal(err)
	}

	if info, err := LstatIfExists(filepath.Join(dir, "missing")); info != nil || err != nil {
		t.Errorf("missing: %v, %v", info, err)
	}
	if info, err := LstatIfExists(dirLink); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("a symlink must be reported as itself: %v, %v", info, err)
	}
	if _, err := LstatIfExists(filepath.Join(file, "below")); err == nil {
		t.Error("a path below a regular file must be an error, not missing")
	}

	for path, want := range map[string]bool{dir: true, dirLink: true, file: false, filepath.Join(dir, "missing"): false} {
		if got := IsDir(path); got != want {
			t.Errorf("IsDir(%s) = %v, want %v", path, got, want)
		}
	}
}

func TestWalkFiles(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"b.txt", "a/x", "a.txt", "a/z/deep", ".git/config", "sub/.git/HEAD", ".hidden", "B"} {
		write(t, filepath.Join(dir, rel), rel)
	}
	if err := os.Symlink(filepath.Join(dir, "a"), filepath.Join(dir, "link-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "b.txt"), filepath.Join(dir, "link-file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := WalkFiles(dir)
	want := []string{".hidden", "B", "a.txt", "a/x", "a/z/deep", "b.txt"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("WalkFiles = %q, %v; want %q", got, err, want)
	}

	if got, err := WalkFiles(filepath.Join(dir, "missing")); got != nil || err != nil {
		t.Errorf("missing dir: %q, %v", got, err)
	}
	if got, err := WalkFiles(filepath.Join(dir, "empty")); len(got) != 0 || err != nil {
		t.Errorf("empty dir: %q, %v", got, err)
	}
	if _, err := WalkFiles(filepath.Join(dir, "b.txt")); err == nil {
		t.Error("walking a regular file gave no error")
	}
}

func TestTreeSize(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a"), "12345")
	write(t, filepath.Join(dir, "sub", "b"), "123")

	if size, capped := TreeSize(filepath.Join(dir, "a"), TreeSizeLimit); size != 5 || capped {
		t.Errorf("file: %d, %v", size, capped)
	}
	if size, capped := TreeSize(filepath.Join(dir, "missing"), TreeSizeLimit); size != 0 || capped {
		t.Errorf("missing: %d, %v", size, capped)
	}

	if err := os.Symlink("a", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	size, capped := TreeSize(dir, TreeSizeLimit)
	if want := int64(8 + len("a")); size != want || capped {
		t.Errorf("tree: %d, %v; want %d: the files plus the link itself, not directories", size, capped, want)
	}

	if _, capped := TreeSize(dir, 5); capped {
		t.Error("5 entries with a limit of 5 reported capped")
	}
	if _, capped := TreeSize(dir, 4); !capped {
		t.Error("5 entries with a limit of 4 not capped")
	}
}

// A file only its owner may read gets parent folders only its owner may
// enter (final review security F8: unlock's plaintext under private/);
// other files get 0777 less the umask; folders that exist keep their mode.
func TestWriteFileAtomicMakesPrivateParents0700(t *testing.T) {
	dir := t.TempDir()
	mode := func(p string) os.FileMode {
		t.Helper()
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return st.Mode().Perm()
	}
	if err := os.Mkdir(filepath.Join(dir, "there"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "there"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(filepath.Join(dir, "there", "a", "b", "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"there/a", "there/a/b"} {
		if got := mode(filepath.Join(dir, d)); got != 0o700 {
			t.Errorf("%s is %o, want 700", d, got)
		}
	}
	if got := mode(filepath.Join(dir, "there")); got != 0o755 {
		t.Errorf("the folder that was there is %o now", got)
	}
	ref := filepath.Join(dir, "ref")
	if err := os.Mkdir(ref, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(filepath.Join(dir, "pub", "c", "file"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"pub", "pub/c"} {
		if got, want := mode(filepath.Join(dir, d)), mode(ref); got != want {
			t.Errorf("%s is %o, want %o", d, got, want)
		}
	}
}
