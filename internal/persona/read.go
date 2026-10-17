package persona

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"

	"github.com/hyperlab-sh/homebot/internal/app"
)

const maxFileBytes = 256 * 1024

//go:embed builtin/*/persona.toml
var builtinFiles embed.FS

func Builtins() []string {
	entries, err := builtinFiles.ReadDir("builtin")
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := builtinFiles.ReadFile("builtin/" + e.Name() + "/persona.toml"); err == nil {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids
}

type rawFile struct {
	Schema  any                 `toml:"schema"`
	ID      string              `toml:"id"`
	Name    string              `toml:"name"`
	Emoji   string              `toml:"emoji"`
	Tagline string              `toml:"tagline"`
	Extends string              `toml:"extends"`
	Lang    string              `toml:"lang"`
	Colors  any                 `toml:"colors"`
	Lines   map[string][]string `toml:"lines"`
}

// Parse decodes one persona file. Every diagnostic it returns carries path.
func Parse(path string, src []byte) (*File, []Diagnostic) {
	f, ds := parseAs(path, src)
	for i := range ds {
		ds[i].Path = path
	}
	return f, ds
}

// idFromPath is the id a file's name gives it: tester.toml is tester. A
// built-in lives in builtin/<id>/persona.toml, where the folder is the id;
// that layout is recognised only under builtin/, so a user's
// personas/persona.toml is the persona "persona", not "personas".
func idFromPath(path string) string {
	if strings.HasPrefix(filepath.ToSlash(path), "builtin/") && filepath.Base(path) == "persona.toml" {
		return filepath.Base(filepath.Dir(path))
	}
	return strings.TrimSuffix(filepath.Base(path), ".toml")
}

func parseAs(path string, src []byte) (*File, []Diagnostic) {
	var raw rawFile
	md, err := toml.Decode(string(src), &raw)
	if err != nil {
		var pe toml.ParseError
		if errors.As(err, &pe) {
			return nil, []Diagnostic{diagnostic("v1", pe.Message, pe.Position.Line, pe.Position.Col, "", 0)}
		}
		return nil, []Diagnostic{diagnostic("v1", err.Error(), 0, 0, "", 0)}
	}
	id := idFromPath(path)
	f := &File{ID: id, Name: raw.Name, Emoji: raw.Emoji, Tagline: raw.Tagline,
		Extends: raw.Extends, Lang: raw.Lang, Lines: raw.Lines, Path: path, declaredID: raw.ID}
	if raw.Schema != nil {
		f.schemaSet = true
		if n, ok := raw.Schema.(int64); ok && n == 1 {
			f.schema = 1
		}
	}
	if f.Lines == nil {
		f.Lines = map[string][]string{}
	}
	var ds []Diagnostic
	for _, key := range md.Undecoded() {
		if key[0] == "colors" {
			continue // an untyped value is read, and judged, by parseColors
		}
		ds = append(ds, diagnostic("v2", key.String(), 0, 0, "", 0))
	}
	ds = append(ds, parseColors(f, raw.Colors)...)
	return f, ds
}

// parseColors fills f.Colors from the decoded [colors] value. Anything that is
// not a table of tokens, each a table of light and dark values, is a
// diagnostic and never silently ignored (plan S3: unknown keys are errors; V9:
// a colour that is neither #RRGGBB nor 0-255).
func parseColors(f *File, colors any) []Diagnostic {
	var ds []Diagnostic
	if colors == nil {
		return nil
	}
	table, ok := colors.(map[string]any)
	if !ok {
		return []Diagnostic{diagnostic("v9", "colors", 0, 0, "", 0)}
	}
	tokens := make([]string, 0, len(table))
	for token := range table {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	for _, token := range tokens {
		var target *Pair
		switch token {
		case "accent":
			target = &f.Colors.Accent
		case "ok":
			target = &f.Colors.OK
		case "muted":
			target = &f.Colors.Muted
		default:
			ds = append(ds, diagnostic("v2", "colors."+token, 0, 0, "", 0))
			continue
		}
		pair, ok := table[token].(map[string]any)
		if !ok {
			ds = append(ds, diagnostic("v9", "colors."+token, 0, 0, "", 0))
			continue
		}
		sides := make([]string, 0, len(pair))
		for side := range pair {
			sides = append(sides, side)
		}
		sort.Strings(sides)
		for _, side := range sides {
			converted := ""
			switch x := pair[side].(type) {
			case string:
				converted = x
			case int64:
				converted = strconv.FormatInt(x, 10)
			default:
				ds = append(ds, diagnostic("v9", "colors."+token+"."+side, 0, 0, "", 0))
				continue
			}
			switch side {
			case "light":
				target.Light = converted
			case "dark":
				target.Dark = converted
			default:
				ds = append(ds, diagnostic("v2", "colors."+token+"."+side, 0, 0, "", 0))
			}
		}
	}
	return ds
}

func Read(ctx *app.Context, id string) (*File, []Diagnostic, error) {
	if !ValidID(id) || id == None {
		return nil, []Diagnostic{diagnostic("v4", id, 0, 0, "", 0)}, errors.New("invalid persona id")
	}
	if ctx.ConfigDir == "" {
		// No place for user files (app.ForReport): a path built from an
		// empty ConfigDir would be relative to the working directory. Only
		// the embedded personas exist, and an id that is not one of them
		// fails as an unknown id does.
		return readBuiltin(id)
	}
	path := filepath.Join(ctx.ConfigDir, "personas", id+".toml")
	if _, err := os.Lstat(path); err == nil {
		f, ds, err := ReadFile(ctx, path)
		if f != nil {
			f.Source = User
			for _, built := range Builtins() {
				if built == id {
					f.Source = UserOverridesBuiltin
					break
				}
			}
		}
		return f, ds, err
	} else if !errors.Is(err, os.ErrNotExist) {
		d := diagnostic("v28", path, 0, 0, "", 0)
		d.Path = path
		return nil, []Diagnostic{d}, err
	}
	return readBuiltin(id)
}

func readBuiltin(id string) (*File, []Diagnostic, error) {
	path := "builtin/" + id + "/persona.toml"
	data, err := builtinFiles.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	f, ds := Parse(path, data)
	if f != nil {
		f.Source = Builtin
	}
	return f, ds, nil
}

func ReadFile(_ *app.Context, path string) (*File, []Diagnostic, error) {
	bad := func(err error) (*File, []Diagnostic, error) {
		d := diagnostic("v28", path, 0, 0, "", 0)
		d.Path = path
		return nil, []Diagnostic{d}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return bad(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(path)
		if err != nil {
			return bad(err)
		}
	}
	if !info.Mode().IsRegular() {
		return bad(fmt.Errorf("persona: not a regular file: %s", path))
	}
	if info.Size() > maxFileBytes {
		return bad(fmt.Errorf("persona: file exceeds 256 KiB: %s", path))
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return bad(err)
	}
	fh := os.NewFile(uintptr(fd), path)
	defer func() { _ = fh.Close() }()
	opened, err := fh.Stat()
	if err != nil {
		return bad(err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return bad(fmt.Errorf("persona: file changed while opening: %s", path))
	}
	data, err := io.ReadAll(io.LimitReader(fh, maxFileBytes+1))
	if err != nil {
		return bad(err)
	}
	if len(data) > maxFileBytes {
		return bad(fmt.Errorf("persona: file exceeds 256 KiB: %s", path))
	}
	parsed, ds := Parse(path, data)
	return parsed, ds, nil
}
