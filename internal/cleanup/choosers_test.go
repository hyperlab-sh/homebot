package cleanup

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// asked is the one backup of a box where ~/.gitconfig was in the way, and
// the question about it, first of two.
func asked(b *box) (Backup, Question) {
	b.backedUp(conflict{".gitconfig", "from repo\n", "from forge\n"})
	got := b.only()
	return got, Question{Index: 1, Total: 2, Diff: backupDiff(b.Ctx, got, true), CanUse: true}
}

// fzfRecorder is an fzf stub that keeps its stdin and arguments next to
// itself, then prints reply.
func fzfRecorder(reply string) string {
	return `cat > "$(dirname "$0")/stdin"; printf '%s\n' "$@" > "$(dirname "$0")/args"; echo "` + reply + `"`
}

func (b *box) stubFile(name string) string { return b.read(filepath.Join(b.Bin, name)) }

func firstWords(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if f := strings.Fields(l); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

func TestFzfGetsTheChoicesOnStdinAndTheDiffInItsPreview(t *testing.T) {
	b := newBox(t)
	got, q := asked(b)
	b.WithStubs(map[string]string{"fzf": fzfRecorder("use     copy it")})
	answer, err := fzfChooser(b.Ctx)(got, q)
	if err != nil {
		t.Fatal(err)
	}
	if answer != Use {
		t.Errorf("answer = %q", answer)
	}
	if words := strings.Join(firstWords(b.stubFile("stdin")), " "); words != "delete keep use merge" {
		t.Errorf("menu = %q", words)
	}
	args := b.stubFile("args")
	contains(t, args, "~/.gitconfig"+stamp+"  (1 of 2)")
	preview := filepath.Join(b.Ctx.CacheDir, "cleanup-preview.diff")
	contains(t, args, "cat "+preview)
	contains(t, ansi.Strip(b.read(preview)), "-from forge")
}

func TestFzfNoUseLineWhenItCannotWorkAndEscMeansQuit(t *testing.T) {
	b := newBox(t)
	got, q := asked(b)
	b.WithStubs(map[string]string{"fzf": `cat > "$(dirname "$0")/stdin"; exit 130`})
	q.CanUse, q.Diff = false, ""
	answer, err := fzfChooser(b.Ctx)(got, q)
	if err != nil || answer != Quit {
		t.Errorf("answer = %q, %v; want quit", answer, err)
	}
	lacks(t, b.stubFile("stdin"), "use")
	contains(t, b.read(filepath.Join(b.Ctx.CacheDir, "cleanup-preview.diff")), "Identical")
}

func TestFzfAnEmptyOrOddPickMeansQuit(t *testing.T) {
	for _, reply := range []string{"something else", ""} {
		b := newBox(t)
		got, q := asked(b)
		b.WithStubs(map[string]string{"fzf": `cat >/dev/null; echo "` + reply + `"`})
		if answer, err := fzfChooser(b.Ctx)(got, q); err != nil || answer != Quit {
			t.Errorf("%q: answer = %q, %v; want quit", reply, answer, err)
		}
	}
}

// selected is what the accessible select answers when the user types input.
func selected(t *testing.T, b *box, got Backup, q Question, input string) Answer {
	t.Helper()
	b.Ctx.Stdin = strings.NewReader(input)
	answer, err := selectChooser(b.Ctx, true)(got, q)
	if err != nil {
		t.Fatalf("%q: %v", input, err)
	}
	return answer
}

// The TypeScript asked for a letter without fzf; the Go port asks with a huh
// select, whose accessible mode reads a number instead.
func TestSelectNumbersPickEnterKeepsAndUseIsNotOfferedWhenItCannotWork(t *testing.T) {
	b := newBox(t)
	got, q := asked(b)
	for input, want := range map[string]Answer{
		"1\n": Delete,
		"2\n": Keep,
		"\n":  Keep,
		"":    Keep, // a closed stdin keeps too
		"3\n": Use,
		"5\n": Quit,
		// End of input after a wrong entry keeps, as a closed stdin does.
		"x\n": Keep,
		"9\n": Keep,
		"0\n": Keep,
		"x":   Keep,
	} {
		if answer := selected(t, b, got, q, input); answer != want {
			t.Errorf("%q: answer = %q, want %q", input, answer, want)
		}
	}
	cannot := q
	cannot.CanUse = false
	b.Stdout.Reset()
	if answer := selected(t, b, got, cannot, "4\nx\n3\n"); answer != Quit {
		t.Errorf("without use: answer = %q, want quit", answer)
	}
	out := ansi.Strip(b.Stdout.String())
	lacks(t, out, "use ")
	contains(t, out, "Invalid")
}

// failingReader serves its text, then fails every read with err.
type failingReader struct {
	text string
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.text == "" {
		return 0, f.err
	}
	n := copy(p, f.text)
	f.text = f.text[n:]
	return n, nil
}

// A read error that is not io.EOF, an EIO from a terminal that hung up say,
// ends the input like EOF: Enter forever, so the select keeps.
func TestSelectAReadErrorEndsTheInputAndKeeps(t *testing.T) {
	r := &byteReader{r: &failingReader{text: "9\n", err: syscall.EIO}}
	var read []byte
	for range 4 {
		p := make([]byte, 8)
		n, err := r.Read(p)
		if err != nil || n != 1 {
			t.Fatalf("Read = %d, %v; want one byte and no error", n, err)
		}
		read = append(read, p[0])
	}
	if string(read) != "9\n\n\n" {
		t.Errorf("read %q, want the text, then empty lines", read)
	}

	b := newBox(t)
	got, q := asked(b)
	for _, input := range []string{"", "x\n", "9\n", "0\n", "x"} {
		b.Ctx.Stdin = &failingReader{text: input, err: syscall.EIO}
		if answer, err := selectChooser(b.Ctx, true)(got, q); err != nil || answer != Keep {
			t.Errorf("%q then EIO: answer = %q, %v; want keep", input, answer, err)
		}
	}
}

func TestSelectShowsTheDiffThenTheQuestion(t *testing.T) {
	b := newBox(t)
	got, q := asked(b)
	selected(t, b, got, q, "2\n")
	out := ansi.Strip(b.Stdout.String())
	diff := strings.Index(out, "-from forge")
	title := strings.Index(out, "[1/2] ~/.gitconfig"+stamp)
	if diff < 0 || title < diff {
		t.Errorf("printed %q, want the diff, then the title", out)
	}
	for _, line := range []string{"1. delete", "2. keep", "3. use", "4. merge", "5. quit"} {
		contains(t, out, line)
	}
	b.Stdout.Reset()
	q.Diff = ""
	selected(t, b, got, q, "2\n")
	contains(t, ansi.Strip(b.Stdout.String()), "Identical")
}

func TestDefaultChooserIsFzfWhenItIsInstalled(t *testing.T) {
	b := newBox(t)
	got, q := asked(b)
	b.pinPath()
	b.WithStubs(map[string]string{"fzf": fzfRecorder("delete  remove")})
	if answer, err := DefaultChooser(b.Ctx)(got, q); err != nil || answer != Delete {
		t.Errorf("answer = %q, %v", answer, err)
	}
	if err := os.Remove(filepath.Join(b.Bin, "fzf")); err != nil {
		t.Fatal(err)
	}
	// Without fzf, and with stdout not a terminal, the select asks on plain
	// lines; so it does with ACCESSIBLE set.
	for _, env := range []map[string]string{{}, {"ACCESSIBLE": "1"}} {
		b.Ctx.Env = b.Ctx.Env.With(env)
		b.Ctx.Stdin = strings.NewReader("1\n")
		b.Stdout.Reset()
		if answer, err := DefaultChooser(b.Ctx)(got, q); err != nil || answer != Delete {
			t.Errorf("without fzf %v: answer = %q, %v", env, answer, err)
		}
		contains(t, ansi.Strip(b.Stdout.String()), "Enter a number")
	}
}
