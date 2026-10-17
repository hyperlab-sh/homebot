package secrets

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// PrivateDocs are the plain files private/ may commit, and only directly
// under it: private/.gitignore lets them through there, and the commit guard
// allows them there. Everything else committed from private/ is a sealed
// *.age file.
var PrivateDocs = []string{"README.md", ".gitignore", ".gitkeep", "recipients.txt"}

// PrivateGitignore is private/.gitignore as homebot writes it: everything is
// ignored but folders (so .age files at any depth stay reachable), *.age
// files, and PrivateDocs directly under private/. The docs patterns are
// anchored with a leading slash: unanchored, git would stage a README.md or
// .gitignore at any depth, and the guard refuses those as plaintext, so
// every save would be refused.
const PrivateGitignore = "*\n!*/\n!*.age\n!/README.md\n!/.gitignore\n!/.gitkeep\n!/recipients.txt\n"

// EnsurePrivateGitignore writes private/.gitignore when there is none. One
// that exists, whatever it holds and whatever it is, is never touched.
func EnsurePrivateGitignore(ctx *app.Context) error {
	p := filepath.Join(ctx.Layout().Private, ".gitignore")
	info, err := fsx.LstatIfExists(p)
	if err != nil || info != nil {
		return err
	}
	// PublishFile never replaces a file: one that appeared since the Lstat wins.
	if err := publishFile(p, []byte(PrivateGitignore), 0o666); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w%s", fail.Say(msg.SecretsErrorSaveGitignore(shownPath(ctx, p))), fail.Failure("", err.Error(), ""))
	}
	return nil
}
