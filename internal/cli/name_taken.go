package cli

import (
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/selfname"
)

// nameTaken supplies D3's two lists that selfname cannot build itself.
func nameTaken(c *app.Context) (selfname.Taken, error) {
	plan, err := linker.PlanLinks(c)
	if err != nil {
		return selfname.Taken{}, err
	}
	bin := filepath.Join(c.Home, ".local", "bin")
	planned := make(map[string]string)
	for _, op := range plan {
		if filepath.Dir(op.Target) == bin {
			planned[filepath.Base(op.Target)] = op.Source
		}
	}
	return selfname.Taken{Reserved: reservedNames(), Planned: planned}, nil
}
