package state

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// WriteNudge writes one safe line for the shell to print at startup.
func WriteNudge(ctx *app.Context, emoji, text string) error {
	if text == "" {
		return writeCacheFile(ctx, "nudge", "")
	}
	if !msg.ValidEmoji(emoji) {
		emoji = voice.Plain(ctx, msg.InboxNudgeMark())
	}
	text = strings.ReplaceAll(msg.StripUnsafe(text), "\n", " ")
	return writeCacheFile(ctx, "nudge", emoji+" "+text)
}

// WriteSymbol writes the persona emoji for the zsh prompt, or removes the
// file so the shell uses its neutral fallback.
func WriteSymbol(ctx *app.Context, emoji string) error {
	path := filepath.Join(ctx.CacheDir, "symbol")
	if !msg.ValidEmoji(emoji) {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return writeCacheFile(ctx, "symbol", promptEscape(emoji))
}

// promptEscape keeps even an unexpectedly accepted ASCII sequence literal
// under zsh PROMPT_SUBST.
func promptEscape(s string) string {
	return strings.NewReplacer("%", "%%", "\\", "\\\\", "$", "\\$", "`", "\\`").Replace(s)
}

// WriteCount writes the cache file starship reads: "<n>\n", "<n>!\n" when an
// item is urgent, and an empty file when n is 0.
func WriteCount(ctx *app.Context, n int, urgent bool) error {
	text := ""
	if n > 0 {
		mark := ""
		if urgent {
			mark = "!"
		}
		text = voice.Plain(ctx, msg.InboxCacheCount(n, mark))
	}
	return writeCacheFile(ctx, "count", text)
}

func writeCacheFile(ctx *app.Context, name, text string) error {
	if text != "" {
		text += "\n"
	}
	return fsx.WriteFileAtomic(filepath.Join(ctx.CacheDir, name), []byte(text), 0o666)
}
