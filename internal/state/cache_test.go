package state

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestWriteSymbolAcceptsOnlyEmoji(t *testing.T) {
	c := testutil.Sandbox(t, testutil.Options{}).Ctx
	path := filepath.Join(c.CacheDir, "symbol")
	for _, bad := range []string{"$(touch pwned)%F{red}", "\x1b]52;c;eA==\x07", "\u202e", ""} {
		if err := WriteSymbol(c, "🏳️‍🌈"); err != nil {
			t.Fatal(err)
		}
		if err := WriteSymbol(c, bad); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("bad symbol %q left file: %v", bad, err)
		}
	}
	if err := WriteSymbol(c, "🏳️‍🌈"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "🏳️‍🌈\n" {
		t.Fatalf("symbol = %q, %v", got, err)
	}
}

func TestPromptEscapeIsLiteralUnderPromptSubst(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "pwned")
	for _, input := range []string{"$(touch " + sentinel + ")", "`id`", "%F{red}x%f", "\\", "$HOME"} {
		cmd := exec.Command("zsh", "-fc", `setopt prompt_subst; print -rP -- "$INPUT"`)
		cmd.Env = append(os.Environ(), "INPUT="+promptEscape(input))
		out, err := cmd.Output()
		if err != nil || string(out) != input+"\n" {
			t.Errorf("promptEscape(%q): got %q, %v", input, out, err)
		}
		if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
			t.Fatalf("prompt executed payload: %v", err)
		}
	}
}

func TestWriteNudgeKeepsEmojiAndMakesTextSafeAndOneLine(t *testing.T) {
	c := testutil.Sandbox(t, testutil.Options{}).Ctx
	path := filepath.Join(c.CacheDir, "nudge")
	if err := WriteNudge(c, "👩‍💻", "hello\u200d\x1b\u202e\nworld"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "👩‍💻 hello world\n" {
		t.Errorf("nudge = %q", got)
	}
	if err := WriteNudge(c, "invalid", "ready"); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != "🚚 ready\n" {
		t.Errorf("invalid emoji fallback = %q, %v", got, err)
	}
	if err := WriteNudge(c, "👩‍💻", ""); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(got)) != "" {
		t.Errorf("empty nudge = %q, %v", got, err)
	}
}
