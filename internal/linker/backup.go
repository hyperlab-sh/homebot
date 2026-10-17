package linker

import (
	"context"
	"os"
	"regexp"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

// backupPattern matches a backup's name and captures the name it was moved
// aside from. dothaul, homebot's old name, wrote the same names with its own.
var backupPattern = regexp.MustCompile(`^(.+)\.(?:homebot|dothaul)-backup-\d{8}-\d{6}$`)

// BackupName is the name path is moved aside to at now:
// <path>.homebot-backup-YYYYMMDD-HHMMSS, in UTC.
func BackupName(path string, now time.Time) string {
	return path + ".homebot-backup-" + now.UTC().Format("20060102-150405")
}

// IsBackup reports whether name is a backup's name: <name>.homebot-backup-
// followed by 8 digits, a dash and 6 digits. Names dothaul wrote, with
// .dothaul-backup-, count too.
func IsBackup(name string) bool {
	return backupPattern.MatchString(name)
}

// BackupOf returns the name the backup name was moved aside from; ok is false
// when name is not a backup's.
func BackupOf(name string) (target string, ok bool) {
	m := backupPattern.FindStringSubmatch(name)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// BackupConflicts moves what is in the way of each conflict in plan aside,
// renamed with BackupName at ctx.Now(), so links can take its place. Nothing
// is deleted: when something already has the backup name, it stops with an
// error. Once ctx's command is told to stop (Ctrl-C) it moves nothing more
// and returns the cause. It returns the backups made, those made before a
// failure too.
func BackupConflicts(ctx *app.Context, plan Plan) ([]string, error) {
	now := ctx.Now()
	var moved []string
	for _, op := range plan.Only(Conflict) {
		if c := ctx.Context(); c.Err() != nil {
			return moved, context.Cause(c)
		}
		backup := BackupName(op.Target, now)
		there, err := fsx.LstatIfExists(backup)
		if err != nil {
			return moved, err
		}
		if there != nil {
			return moved, fail.Say(msg.LinkErrorBackupExists(ui.Tildify(op.Target, ctx.Home), ui.Tildify(backup, ctx.Home)))
		}
		if err := os.Rename(op.Target, backup); err != nil {
			return moved, err
		}
		moved = append(moved, backup)
	}
	return moved, nil
}
