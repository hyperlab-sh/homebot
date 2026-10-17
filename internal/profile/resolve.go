package profile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// ResolveEnv is what a profile is resolved against.
type ResolveEnv struct {
	ProfilesDir string
	OS          app.OS
	Host        string
	Arch        string
}

// ListProfiles returns the names of the profiles in the repo, sorted. A
// directory that can't be read, like a missing one, has none; loading a
// profile reports the real cause.
func ListProfiles(ctx *app.Context) []string {
	names, _ := listProfilesIn(ctx.Layout().Profiles)
	return names
}

// listProfilesIn lists the *.yml files in dir, without the extension. A
// missing directory has none; any other read error is returned.
func listProfilesIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".yml"); ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func loadProfile(dir, name string) (*Profile, error) {
	file := filepath.Join(dir, name+".yml")
	if exists(file) {
		text, err := os.ReadFile(file)
		if err != nil {
			return nil, fail.User(err.Error())
		}
		return Parse(string(text), file, name)
	}
	if old := filepath.Join(dir, name+".yaml"); exists(old) {
		return nil, fail.Say(msg.ManifestErrorOldExtension(old, name))
	}
	known, err := listProfilesIn(dir)
	if err != nil {
		return nil, fail.User(err.Error())
	}
	if len(known) > 0 {
		return nil, fail.Say(msg.ManifestErrorNoProfileAvailable(name, dir, strings.Join(known, ", ")))
	}
	return nil, fail.Say(msg.ManifestErrorNoProfile(name, dir))
}

// Var is one variable available to {name} placeholders.
type Var struct {
	Name, Value string
}

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// Interpolate replaces {name} in text with the variable of that name. where
// locates the text in the error for an unknown variable, which lists the
// available ones in the order given.
func Interpolate(text string, vars []Var, where string) (string, error) {
	var failure error
	out := placeholder.ReplaceAllStringFunc(text, func(m string) string {
		key := m[1 : len(m)-1]
		for _, v := range vars {
			if v.Name == key {
				return v.Value
			}
		}
		if failure == nil {
			names := make([]string, len(vars))
			for i, v := range vars {
				names[i] = "{" + v.Name + "}"
			}
			failure = fail.Say(msg.ManifestErrorUnknownVariable(where, "{"+key+"}", strings.Join(names, ", ")))
		}
		return m
	})
	return out, failure
}

// Resolve flattens a profile into the steps that apply to this machine:
// includes first (each profile at most once), steps filtered by "on",
// variables filled in.
func Resolve(env ResolveEnv, name string) ([]Step, error) {
	vars := []Var{{"os", string(env.OS)}, {"host", env.Host}, {"arch", env.Arch}}
	out := []Step{}
	done := map[string]bool{}

	var visit func(profileName string, stack []string) error
	visit = func(profileName string, stack []string) error {
		if slices.Contains(stack, profileName) {
			chain := append(slices.Clone(stack), profileName)
			return fail.Say(msg.ManifestErrorLoop(strings.Join(chain, " -> ")))
		}
		if done[profileName] {
			return nil
		}
		p, err := loadProfile(env.ProfilesDir, profileName)
		if err != nil {
			return err
		}
		for _, inc := range p.Include {
			if err := visit(inc, append(slices.Clone(stack), profileName)); err != nil {
				return err
			}
		}
		done[profileName] = true
		for _, step := range p.Steps {
			if step.On != nil && !slices.Contains(step.On, env.OS) {
				continue
			}
			where := fail.Say(msg.ManifestWhereStep(step.Source.File, step.Source.Index+1)).Error()
			args := make([]string, len(step.Args))
			for i, a := range step.Args {
				if args[i], err = Interpolate(a, vars, where); err != nil {
					return err
				}
			}
			step.Args = args
			out = append(out, step)
		}
		return nil
	}
	if err := visit(name, nil); err != nil {
		return nil, err
	}
	return out, nil
}

// DefaultProfile is the profile this machine unpacks when none is named: the
// one profiles.hosts gives for its host, else profiles.default. A nil
// ctx.Config means the defaults, which name none.
func DefaultProfile(ctx *app.Context) (string, bool) {
	cfg := ctx.Config
	if cfg == nil {
		cfg = config.Default()
	}
	if name, ok := cfg.Profiles.Hosts[ctx.Host]; ok {
		return name, true
	}
	return cfg.Profiles.Default, cfg.Profiles.Default != ""
}

// ResolveFor resolves the named profile for the machine ctx describes.
func ResolveFor(ctx *app.Context, name string) ([]Step, error) {
	return Resolve(ResolveEnv{ProfilesDir: ctx.Layout().Profiles, OS: ctx.OS, Host: ctx.Host, Arch: ctx.Arch}, name)
}
