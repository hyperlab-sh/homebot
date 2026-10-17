package cli

import (
	"os"
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// setupNamePath offers the shell line only for a working configured name.
// A new helper sees the process PATH with local/bin prepended only when the
// user accepted that line; an existing PATH order is preserved.
func setupNamePath(c *app.Context, name string) (*app.Context, bool, error) {
	if name == "" || helper.PathHasLocalBin(c.Env.Get("PATH"), c) {
		return c, false, nil
	}
	// The shell may already have the accepted rc line while this setup
	// process was started with an older PATH. Keep the rc file unchanged,
	// but let the helper inherit the PATH that a new shell will have.
	if helper.RCPutsLocalBinOnPath(c) {
		return setupWithLocalBin(c), false, nil
	}
	file := helper.RCFile(c)
	shown := ui.Tildify(file, c.Home)
	if !ui.Interactive(c) {
		ui.Println(c, voice.Render(c, msg.SetupHintNamePath(name, shown)))
		return c, false, nil
	}
	rel, link, ok := setupZshrcWritable(c, file)
	if !ok {
		if link != "" {
			ui.Println(c, voice.Render(c, msg.SetupWarningPathLink(shown, ui.Tildify(link, c.Home))))
		} else {
			ui.Println(c, voice.Render(c, msg.SetupHintNamePath(name, shown)))
		}
		return c, false, nil
	}
	if !setupYes(c, msg.SetupPromptNamePath(shown)) {
		ui.Println(c, voice.Render(c, msg.SetupHintNamePath(name, shown)))
		return c, false, nil
	}
	line := `export PATH="$HOME/.local/bin:$PATH"`
	if filepath.Base(c.Env.Get("SHELL")) == "fish" {
		line = `fish_add_path $HOME/.local/bin`
	}
	if err := setupAppendLine(file, line); err != nil {
		return c, false, err
	}
	ui.Println(c, voice.Render(c, msg.SetupStatusPathAdded(shown)))
	if rel != "" {
		ui.Println(c, voice.Render(c, msg.SetupStatusZshrcInRepo(rel)))
	}
	return setupWithLocalBin(c), true, nil
}

func setupWithLocalBin(c *app.Context) *app.Context {
	path := helper.LocalBin(c) + string(os.PathListSeparator) + c.Env.Get("PATH")
	return c.Fork(map[string]string{"PATH": path})
}
