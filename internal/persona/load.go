package persona

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// Load resolves a selected persona: its file, the files it extends, then the
// embedded neatpik (plan S4). Only valid variants survive; a broken header in
// any file of the chain makes the choice unusable rather than silently
// selecting another persona, and every diagnostic and the error name the file
// they come from.
//
// The embedded neatpik ends every chain unless a file says extends = "none",
// which stops the chain there (plan S3). It is never replaced by a user's
// personas/neatpik.toml: that file shadows the built-in only as a choice
// (Load "neatpik", HOMEBOT_PERSONA=neatpik, persona: neatpik). Saying
// nothing and saying extends = "neatpik" (the default, plan S3) both mean the
// embedded copy, so a broken neatpik.toml makes neatpik unusable as a choice
// and leaves every other persona alone.
//
// The chain is at most four files (maxExtendsLevels) and has no cycle. The
// embedded neatpik is not one of the four, so a chain of four is accepted
// whether its last file says nothing, extends = "none" or extends = "neatpik",
// and a chain of five is refused. After extends = "none" the lines a file
// does not have come from the catalog's English, and unset colours from the
// terminal's own.
func Load(ctx *app.Context, id string) (*Voice, []Diagnostic, error) {
	return load(ctx, id, func() (*File, []Diagnostic, error) { return Read(ctx, id) })
}

// LoadFile is Load for a persona file at path rather than one found by id:
// the file is the first of the chain, with the id its name gives it, and the
// files it extends are found by id as for any persona. It is how a file is
// checked before it is installed (persona validate <file>), so the chain, the
// four-file cap and cycles are judged as they will be once it is in place.
func LoadFile(ctx *app.Context, path string) (*Voice, []Diagnostic, error) {
	return load(ctx, idFromPath(path), func() (*File, []Diagnostic, error) {
		f, ds, err := ReadFile(ctx, path)
		if f != nil {
			f.Source = User
		}
		return f, ds, err
	})
}

// load is Load, with the first file of the chain read by first.
func load(ctx *app.Context, id string, first func() (*File, []Diagnostic, error)) (*Voice, []Diagnostic, error) {
	if !ValidID(id) || id == None {
		d := diagnostic("v4", id, 0, 0, "", 0)
		return nil, []Diagnostic{d}, errors.New("persona: invalid id")
	}
	var chain []*File
	var findings []Diagnostic
	seen := map[string]bool{}
	embedded := false // the chain reached the embedded neatpik by name
	current, from := id, ""
	for current != "" && current != None {
		if current == Default && from != "" {
			// extends = "neatpik" is the implicit base spelled out: the
			// embedded copy, which ends the chain and is not a level.
			break
		}
		if seen[current] {
			d := diagnostic("v9_cycle", current, 0, 0, "", 0)
			d.Path = from
			return nil, append(findings, d), fmt.Errorf("persona: extends cycle at %s in %s", current, from)
		}
		seen[current] = true
		read := func() (*File, []Diagnostic, error) { return Read(ctx, current) }
		if len(chain) == 0 {
			read = first
		}
		f, parsed, err := read()
		findings = append(findings, parsed...)
		if err != nil {
			if len(parsed) == 0 {
				d := diagnostic("v9", current, 0, 0, "", 0)
				d.Path = from
				findings = append(findings, d)
			}
			return nil, findings, fmt.Errorf("persona: read %s: %w", current, err)
		}
		// Selecting neatpik with no user file of that name is the embedded
		// copy: the end of the chain, not a level.
		embedded = f != nil && f.Source == Builtin && f.ID == Default
		if !embedded && len(chain) >= maxExtendsLevels {
			d := diagnostic("v9_depth", current, 0, 0, "", 0)
			d.Path = from
			return nil, append(findings, d), fmt.Errorf("persona: %s extends %s, which is deeper than %d files", from, current, maxExtendsLevels)
		}
		if f == nil {
			return nil, findings, fmt.Errorf("persona: invalid header in %s", fileOf(parsed, from))
		}
		checked := Validate(ctx, f, ValidateOptions{})
		findings = append(findings, checked...)
		if headerError(parsed) || headerError(checked) {
			return nil, findings, fmt.Errorf("persona: invalid header in %s", f.Path)
		}
		chain = append(chain, f)
		if embedded {
			break
		}
		current, from = f.Extends, f.Path
	}
	if !embedded && current != None {
		f, ds, err := readBuiltin(Default)
		if err == nil && !headerError(ds) && !headerError(Validate(ctx, f, ValidateOptions{})) {
			chain = append(chain, f)
		}
	}
	v := &Voice{ID: id, Name: chain[0].Name, Emoji: chain[0].Emoji, Lang: language.English,
		Lines: map[string][]Line{}}
	if chain[0].Lang != "" {
		v.Lang, _ = language.Parse(chain[0].Lang)
	}
	for _, f := range chain {
		mergePair(&v.Colors.Accent, f.Colors.Accent)
		mergePair(&v.Colors.OK, f.Colors.OK)
		mergePair(&v.Colors.Muted, f.Colors.Muted)
		addLines(ctx, v, f)
	}
	return v, findings, nil
}

// addLines gives v the valid variants of f's lines, in f's language (English
// when f sets none), for every message id v has no line for yet: the first
// file down the chain to voice an id wins it.
func addLines(ctx *app.Context, v *Voice, f *File) {
	lang := language.English
	if f.Lang != "" {
		lang, _ = language.Parse(f.Lang)
	}
	for msgID, variants := range validVariants(ctx, f) {
		if len(v.Lines[msgID]) != 0 {
			continue
		}
		for _, line := range variants {
			v.Lines[msgID] = append(v.Lines[msgID], Line{Text: line, Lang: lang})
		}
	}
}

// maxExtendsLevels is the longest chain of files a persona may have (plan
// S4): the persona itself and the files it extends. The embedded neatpik
// that ends every chain is listed apart from them and does not count.
const maxExtendsLevels = 4

func headerError(ds []Diagnostic) bool {
	_, found := firstHeaderError(ds)
	return found
}

// firstHeaderError is the first diagnostic that makes a persona unusable.
func firstHeaderError(ds []Diagnostic) (Diagnostic, bool) {
	for _, d := range ds {
		if d.Severity == Error && d.LineID == "" && !d.inLines {
			return d, true
		}
	}
	return Diagnostic{}, false
}

// fileOf is the file the diagnostics are about, or fallback when they do not
// say.
func fileOf(ds []Diagnostic, fallback string) string {
	for _, d := range ds {
		if d.Path != "" {
			return d.Path
		}
	}
	return fallback
}

func mergePair(dst *Pair, src Pair) {
	if dst.Light == "" {
		dst.Light = src.Light
	}
	if dst.Dark == "" {
		dst.Dark = src.Dark
	}
}

func validVariants(ctx *app.Context, f *File) map[string][]string {
	bad := map[string]map[int]bool{}
	for _, d := range Validate(ctx, f, ValidateOptions{}) {
		// Unsafe keys are warnings to the user, but still cannot enter a
		// voice. Other warnings leave their variants available.
		if d.LineID == "" && !d.inLines {
			continue
		}
		if d.Severity != Error && (d.Variant != 0 || d.Text.ID() != "persona.diag.v0") {
			continue
		}
		if bad[d.LineID] == nil {
			bad[d.LineID] = map[int]bool{}
		}
		bad[d.LineID][d.Variant] = true
	}
	out := map[string][]string{}
	for id, variants := range f.Lines {
		if bad[id][0] {
			continue
		}
		for i, line := range variants {
			if bad[id][i+1] {
				if ctx.Debug != nil {
					ctx.Debug.Debug("persona: dropped invalid variant", "id", id, "variant", i+1)
				}
				continue
			}
			out[id] = append(out[id], line)
		}
	}
	return out
}

// List reports the headers of the embedded and user files, and for each id the
// findings of loading it: its whole chain, as Load, persona use and persona
// validate judge it, so a persona that extends a broken parent, closes a cycle
// or sits five files deep has errors here too. A user file with the same id
// replaces the built-in in the list, as it does for Read.
func List(ctx *app.Context) ([]*File, map[string][]Diagnostic, error) {
	ids := Builtins()
	var entries []os.DirEntry
	if ctx.ConfigDir != "" { // none: no user files, and no directory relative to the cwd
		var err error
		entries, err = os.ReadDir(filepath.Join(ctx.ConfigDir, "personas"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".toml")
		if ValidID(id) && id != None && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	files := make([]*File, 0, len(ids))
	findings := map[string][]Diagnostic{}
	for _, id := range ids {
		// The row shows this file's header; its findings are the chain's.
		f, _, _ := Read(ctx, id)
		_, ds, err := Load(ctx, id)
		findings[id] = ds
		if f != nil {
			files = append(files, f)
		}
		if err != nil && len(ds) == 0 {
			findings[id] = []Diagnostic{diagnostic("v28", id, 0, 0, "", 0)}
		}
	}
	return files, findings, nil
}
