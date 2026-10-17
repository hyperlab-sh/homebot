package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
	"github.com/hyperlab-sh/homebot/internal/yamledit"
)

func init() { register(KeepingOrder, "persona", newPersonaCmd) }

func newPersonaCmd(c *app.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use: "persona [command]", Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fail.User(voice.Plain(c, msg.PersonaErrorUsage()))
			}
			return personaList(c)
		},
	}
	cmd.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return personaList(c) }})
	cmd.AddCommand(&cobra.Command{Use: "show <id>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return personaShow(c, args[0]) }})
	cmd.AddCommand(&cobra.Command{Use: "use <id|none>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return personaUse(c, args[0]) }})
	var from string
	newCmd := &cobra.Command{Use: "new <id>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return personaNew(c, args[0], from) }}
	newCmd.Flags().StringVar(&from, "from", "", "")
	cmd.AddCommand(newCmd)
	cmd.AddCommand(&cobra.Command{Use: "validate [id|file]", Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error { return personaValidate(c, args) }})
	var event string
	var allVariants bool
	preview := &cobra.Command{Use: "preview <id>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return personaPreview(c, args[0], event, allVariants) }}
	preview.Flags().StringVar(&event, "event", "", "")
	preview.Flags().BoolVar(&allVariants, "all-variants", false, "")
	cmd.AddCommand(preview)
	cmd.AddCommand(&cobra.Command{Use: "ids", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return personaIDs(c) }})
	return cmd
}

func personaInvalidID(c *app.Context, id string) error {
	if id == persona.None {
		// A well-formed id that is reserved, not an invalid one.
		return fail.User(voice.Plain(c, msg.PersonaErrorReserved()))
	}
	return fail.User(voice.Plain(c, msg.PersonaErrorID(msg.StripUnsafe(id))))
}

// personaMissing is the "no persona named" error for a well-formed id that is
// neither a user file nor a built-in, and nil for every other id. Without it
// the loader's own wording reaches the user: it reports a missing file as an
// invalid header, which sends them looking inside a file that is not there.
func personaMissing(c *app.Context, id string) error {
	if _, _, err := persona.Read(c, id); errors.Is(err, fs.ErrNotExist) {
		return fail.User(voice.Plain(c, msg.PersonaErrorNotFound(msg.StripUnsafe(id))))
	}
	return nil
}

// personaDiagnostics prints the findings to stderr, as every other warning and
// error of the CLI goes, and reports whether any is an error. stdout keeps
// only what a command produced. Each finding starts with the file it belongs
// to: a persona is judged with the files it extends, and a finding in a parent
// must not read as one in the file the user named.
func personaDiagnostics(c *app.Context, ds []persona.Diagnostic) bool {
	hasError := false
	for _, d := range ds {
		level := "warning"
		if d.Severity == persona.Error {
			level, hasError = "error", true
		}
		loc := ""
		if d.Path != "" {
			loc = msg.StripUnsafe(personaPath(c, d.Path)) + ": "
		}
		if d.Line > 0 {
			loc += fmt.Sprintf("%d:%d: ", d.Line, d.Col)
		}
		if d.LineID != "" {
			loc += msg.StripUnsafe(d.LineID) + ": "
		}
		ui.Eprintln(c, voice.Render(c, msg.PersonaLabelDiagnostic(level, loc+msg.StripUnsafe(voice.Record(c, d.Text)))))
	}
	return hasError
}

// personaPath is how a finding names a persona file: relative to the config
// directory ("personas/cb.toml") when the file is under it, as the user keeps
// them, and absolute otherwise (a file validated from elsewhere). A path that
// is not absolute is left as it is.
func personaPath(c *app.Context, path string) string {
	if c.ConfigDir == "" || !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(c.ConfigDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

func personaHasErrors(ds []persona.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == persona.Error {
			return true
		}
	}
	return false
}

func personaList(c *app.Context) error {
	// A list outside a home repo is useful, so a missing repo is not an error
	// here. A repo whose homebot.yml cannot be loaded is: it is reported as
	// every other command reports it (the list is still printed, since the
	// personas do not depend on the config), and nothing is marked current,
	// because which persona the repo selects is not known.
	configErr := personaConfigError(c)
	files, ds, err := persona.List(c)
	if err != nil {
		return err
	}
	current := personaCurrent(c, configErr)
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.ID] = true
		mark := " "
		if f.ID == current {
			mark = "*"
		}
		if first := firstPersonaError(c, ds[f.ID], f.Path); first != "" {
			ui.Println(c, personaRow(voice.Render(c, msg.PersonaLabelBroken(mark, msg.StripUnsafe(f.ID), first))))
			continue
		}
		source := "built-in"
		switch f.Source {
		case persona.User:
			source = "user"
		case persona.UserOverridesBuiltin:
			source = "override"
		}
		ui.Println(c, personaRow(voice.Render(c, msg.PersonaLabelRow(mark, msg.StripUnsafe(f.ID), msg.StripUnsafe(f.Name), source, msg.StripUnsafe(f.Tagline)))))
	}
	for _, id := range slices.Sorted(func(yield func(string) bool) {
		for id := range ds {
			if !yield(id) {
				return
			}
		}
	}) {
		if !seen[id] {
			mark := " "
			if id == current {
				mark = "*"
			}
			ui.Println(c, personaRow(voice.Render(c, msg.PersonaLabelBroken(mark, msg.StripUnsafe(id), firstPersonaError(c, ds[id], "")))))
		}
	}
	return configErr
}

// personaConfigError is the error of locating the home repo's homebot.yml
// when there is a repo and its config does not load, and nil otherwise: no
// repo at all is a normal place to list or validate personas from, a broken
// config is not. It is the error persona use returns for the same file.
func personaConfigError(c *app.Context) error {
	err := c.LocateRoot()
	if err == nil || app.IsNoRoot(err) {
		return nil
	}
	return err
}

// personaCurrent is the id of the persona in use: HOMEBOT_PERSONA, else
// persona: in the config, else the default. It is "" when the config could
// not be loaded and the environment does not decide: the repo's choice is
// unknown, and the default is not a fact then.
func personaCurrent(c *app.Context, configErr error) string {
	if env := c.Env.Get("HOMEBOT_PERSONA"); env != "" {
		return env
	}
	if configErr != nil {
		return ""
	}
	if c.Config != nil && c.Config.Persona != "" {
		return c.Config.Persona
	}
	return persona.Default
}

// personaRow is a list row without the spaces a missing tagline leaves at its
// end.
func personaRow(row string) string { return strings.TrimRight(row, " ") }

// firstPersonaError is the first error among the findings of a persona, as the
// reason its list row gives. The findings are those of the whole chain, so one
// that belongs to another file than own, the persona's file, starts with that
// file's path: a row must not blame the persona for its parent's emoji.
// own is "" when the persona's own file is unknown, as when it did not parse;
// then every finding is taken to be its own.
func firstPersonaError(c *app.Context, ds []persona.Diagnostic, own string) string {
	for _, d := range ds {
		if d.Severity != persona.Error {
			continue
		}
		reason := msg.StripUnsafe(voice.Record(nil, d.Text))
		if own != "" && d.Path != "" && d.Path != own {
			reason = msg.StripUnsafe(personaPath(c, d.Path)) + ": " + reason
		}
		return reason
	}
	return ""
}

func personaShow(c *app.Context, id string) error {
	if !persona.ValidID(id) || id == persona.None {
		return personaInvalidID(c, id)
	}
	if err := personaMissing(c, id); err != nil {
		return err
	}
	v, ds, loadErr := persona.Load(c, id)
	personaDiagnostics(c, ds)
	if loadErr != nil {
		return fail.User(voice.Plain(c, msg.PersonaErrorUnusable(msg.StripUnsafe(id))))
	}
	f, _, err := persona.Read(c, id)
	if err != nil {
		return err
	}
	// A persona without a tagline has no dash to dangle after its name.
	header := msg.PersonaLabelHeaderUntagged(msg.StripUnsafe(f.Emoji), msg.StripUnsafe(f.Name))
	if f.Tagline != "" {
		header = msg.PersonaLabelHeader(msg.StripUnsafe(f.Emoji), msg.StripUnsafe(f.Name), msg.StripUnsafe(f.Tagline))
	}
	ui.Println(c, strings.TrimRight(voice.Render(c, header), " "))
	for _, pair := range []struct {
		token string
		value persona.Pair
	}{{"accent", v.Colors.Accent}, {"ok", v.Colors.OK}, {"muted", v.Colors.Muted}} {
		ui.Println(c, personaColorLine(c, pair.token, pair.value))
	}
	return personaShowSamples(c, id)
}

// personaColorLine is one colour of persona show. A colour the chain does not
// set is the terminal's own (extends = "none" with no [colors], say), and the
// line says so rather than print an empty value; a swatch is only drawn for a
// value there is.
func personaColorLine(c *app.Context, token string, value persona.Pair) string {
	if value.Light == "" && value.Dark == "" {
		return voice.Render(c, msg.PersonaLabelColorUnset(token))
	}
	side := func(v string) string {
		if v == "" {
			return voice.Plain(c, msg.PersonaLabelColorUnsetSide())
		}
		return v
	}
	line := voice.Render(c, msg.PersonaLabelColor(token, side(value.Light), side(value.Dark)))
	if c.StdoutIsTerminal() && !c.Plain && c.Env.Get("NO_COLOR") == "" {
		prefix := ""
		for _, v := range []string{value.Light, value.Dark} {
			if swatch := ui.Swatch(c, v); swatch != "" {
				prefix += swatch + " "
			}
		}
		line = prefix + line
	}
	return line
}

// personaUsable is nil when id can be chosen as the repo's persona: none, or
// a well-formed id of a persona that exists and loads without errors. Any
// other id is refused with the error persona use gives, after the findings
// that explain it. It is the one check behind persona use and setup --persona,
// so neither can write a persona: the next refresh would call broken.
func personaUsable(c *app.Context, id string) error {
	if id == persona.None {
		return nil
	}
	if !persona.ValidID(id) {
		return personaInvalidID(c, id)
	}
	if err := personaMissing(c, id); err != nil {
		return err
	}
	_, ds, err := persona.Load(c, id)
	if err != nil || personaHasErrors(ds) {
		personaDiagnostics(c, ds)
		return fail.User(voice.Plain(c, msg.PersonaErrorUnusable(msg.StripUnsafe(id))))
	}
	return nil
}

func personaUse(c *app.Context, id string) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	if err := personaUsable(c, id); err != nil {
		return err
	}
	path := filepath.Join(c.Root, config.FileName)
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	after, err := yamledit.SetScalar(before, []string{"persona"}, id)
	if err != nil {
		return err
	}
	parsed, err := config.Parse(after, path)
	if err != nil {
		return err
	}
	if err := fsx.WriteFileAtomic(path, after, info.Mode().Perm()); err != nil {
		return err
	}
	// The shell's nudge and prompt symbol are caches of the old choice: write
	// them again now, from the file just written, instead of leaving them to
	// the helper or the shell's 30-minute refresh.
	c.Config = parsed
	refreshInbox(c)
	fork := c.Fork(map[string]string{"HOMEBOT_PERSONA": id})
	fork.ForceVoice = true
	if id == persona.None {
		ui.Println(c, voice.Render(fork, msg.PersonaStatusNone()))
	} else {
		ui.Println(c, voice.Render(fork, msg.PersonaSuccessGreeting()))
		ui.Println(c, voice.Render(fork, msg.PersonaSuccessSelected(id)))
	}
	if env := c.Env.Get("HOMEBOT_PERSONA"); env != "" && env != id {
		ui.Println(c, voice.Render(c, msg.PersonaWarningEnv()))
	}
	return nil
}

func personaNew(c *app.Context, id, from string) error {
	if !persona.ValidID(id) || id == persona.None {
		return personaInvalidID(c, id)
	}
	var copied *persona.File
	if from != "" {
		if !persona.ValidID(from) || from == persona.None {
			return personaInvalidID(c, from)
		}
		if err := personaMissing(c, from); err != nil {
			return err
		}
		if _, ds, err := persona.Load(c, from); err != nil || personaHasErrors(ds) {
			personaDiagnostics(c, ds)
			return fail.User(voice.Plain(c, msg.PersonaErrorUnusable(from)))
		}
		var err error
		copied, _, err = persona.Read(c, from)
		if err != nil {
			return err
		}
	}
	dir := filepath.Join(c.ConfigDir, "personas")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, id+".toml")
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fail.User(voice.Plain(c, msg.PersonaErrorExists(path)))
	}
	if err != nil {
		return err
	}
	data := personaScaffold(id, copied)
	if _, err = fd.WriteString(data); err != nil {
		_ = fd.Close()
		_ = os.Remove(path)
		return err
	}
	if err = fd.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	ui.Println(c, voice.Render(c, msg.PersonaSuccessCreated(path)))
	ui.Println(c, voice.Command(c, msg.PersonaHintPack(path)))
	return nil
}

func personaScaffold(id string, from *persona.File) string {
	name, emoji, tagline, extends, lang := id, "🧹", "", "neatpik", ""
	colors := persona.Colors{}
	if from != nil {
		name, emoji, tagline, extends, lang, colors = from.Name, from.Emoji, from.Tagline, from.Extends, from.Lang, from.Colors
	}
	var b strings.Builder
	fmt.Fprintf(&b, "schema = 1\nid = %s\nname = %s\nemoji = %s\ntagline = %s\nextends = %s\n", strconv.Quote(id), strconv.Quote(name), strconv.Quote(emoji), strconv.Quote(tagline), strconv.Quote(extends))
	// The copied lines are in the copied persona's language: without its tag
	// they would be read as English.
	if lang != "" {
		fmt.Fprintf(&b, "lang = %s\n", strconv.Quote(lang))
	}
	b.WriteString("\n[colors]\n")
	for _, p := range []struct {
		name string
		pair persona.Pair
	}{{"accent", colors.Accent}, {"ok", colors.OK}, {"muted", colors.Muted}} {
		if p.pair.Light != "" || p.pair.Dark != "" {
			fmt.Fprintf(&b, "%s = { light = %s, dark = %s }\n", p.name, strconv.Quote(p.pair.Light), strconv.Quote(p.pair.Dark))
		}
	}
	b.WriteString("\n[lines]\n")
	if from != nil {
		for _, mid := range slices.Sorted(func(yield func(string) bool) {
			for mid := range from.Lines {
				if !yield(mid) {
					return
				}
			}
		}) {
			b.WriteString(strconv.Quote(mid) + " = [")
			for i, line := range from.Lines[mid] {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(strconv.Quote(line))
			}
			b.WriteString("]\n")
		}
	} else {
		for _, entry := range msg.Entries() {
			if !entry.Persona {
				continue
			}
			fmt.Fprintf(&b, "# %s = [%s]", strconv.Quote(entry.ID), strconv.Quote(entry.Text))
			if comment := scaffoldComment(entry); comment != "" {
				b.WriteString("  # " + comment)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func scaffoldComment(entry msg.Meta) string {
	var parts []string
	for _, s := range entry.Slots {
		parts = append(parts, fmt.Sprintf("{%s}: %s (%s)", s.Name, s.Meaning, s.Type))
	}
	return strings.ReplaceAll(strings.Join(parts, "; "), "\n", " ")
}

func personaValidate(c *app.Context, args []string) error {
	id := ""
	if len(args) > 0 {
		id = args[0]
	}
	// configErr is a homebot.yml that does not load. It is reported last, as
	// persona use reports it. Without an id on the command line, the persona to
	// validate is the one the repo selects, and a broken config does not say
	// which: only HOMEBOT_PERSONA can still decide, and with neither there is
	// nothing to validate but the config's error.
	var configErr error
	if id == "" {
		configErr = personaConfigError(c)
		id = personaCurrent(c, configErr)
		if id == "" {
			return configErr
		}
		// none is a valid choice with no file to validate: say so rather than
		// call the user's own config invalid. Named on the command line it is
		// still refused, as show, new and preview refuse it.
		if id == persona.None {
			neutral := c.Fork(map[string]string{"HOMEBOT_PERSONA": persona.None})
			ui.Println(c, voice.Render(neutral, msg.PersonaStatusNone()))
			return configErr
		}
	}
	// The whole chain is loaded, not just the one file: a file that extends a
	// missing or broken persona, closes a cycle or sits five files deep is
	// unusable, and validate must say so before the user installs it.
	var ds []persona.Diagnostic
	var err error
	if strings.Contains(id, "/") || strings.HasSuffix(id, ".toml") {
		path := id
		if !filepath.IsAbs(path) {
			path = filepath.Join(c.Cwd, path)
		}
		_, ds, err = persona.LoadFile(c, path)
	} else {
		if !persona.ValidID(id) || id == persona.None {
			return personaInvalidID(c, id)
		}
		if err := personaMissing(c, id); err != nil {
			return err
		}
		_, ds, err = persona.Load(c, id)
	}
	bad := personaDiagnostics(c, ds)
	if err != nil || bad {
		return fail.User(voice.Plain(c, msg.PersonaErrorUnusable(msg.StripUnsafe(id))))
	}
	ui.Println(c, voice.Render(c, msg.PersonaStatusValid(msg.StripUnsafe(id))))
	return configErr
}

func personaIDs(c *app.Context) error {
	for _, entry := range msg.Entries() {
		if !entry.Persona {
			continue
		}
		var slots []string
		for _, slot := range entry.Slots {
			// A slot with no fixed value ends at its meaning: no dangling ", ".
			parts := []string{string(slot.Type), slot.Meaning}
			if slot.Value != "" {
				parts = append(parts, slot.Value)
			}
			slots = append(slots, fmt.Sprintf("{%s}: %s", slot.Name, strings.Join(parts, ", ")))
		}
		// The label ends in its slots, so an entry without slots would end in
		// the space between them: trim it, and any space English ends in.
		line := voice.Render(c, msg.PersonaLabelIds(entry.ID, entry.Kind, entry.Text, strings.Join(slots, "; ")))
		ui.Println(c, strings.TrimRight(line, " "))
	}
	return nil
}
