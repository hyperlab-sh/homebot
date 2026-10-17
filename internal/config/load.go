package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// oldFileName is the pre-rename spelling Load points the user away from.
const oldFileName = "homebot.yaml"

// Load reads and validates root/homebot.yml. An old homebot.yaml with no
// homebot.yml gets a rename hint. Errors about the file's content are
// *fail.UserError and carry "<path>:<line>:<col>: ".
func Load(root string) (*Config, error) { return LoadDebug(root, nil) }

// LoadDebug is Load, and says on debug (nil discards) which ReservedKeys it
// ignored.
func LoadDebug(root string, debug *slog.Logger) (*Config, error) {
	file := filepath.Join(root, FileName)
	src, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		if _, oldErr := os.Stat(filepath.Join(root, oldFileName)); oldErr == nil {
			return nil, fail.Say(msg.ConfigErrorOldName(filepath.Join(root, oldFileName)))
		}
	}
	if err != nil {
		return nil, err
	}
	return parseIn(src, file, root, debug)
}

// Parse validates the text of a homebot.yml and applies the defaults. file is
// only used to prefix error messages. Without the repo it cannot look for
// symlinks among an export's out folders, as Load does.
func Parse(src []byte, file string) (*Config, error) {
	return parseIn(src, file, "", nil)
}

// parseIn is Parse, with the repo root when the file is root's homebot.yml.
func parseIn(src []byte, file, repo string, debug *slog.Logger) (*Config, error) {
	v := validator{file: file, root: repo, debug: debug}
	root, err := v.document(src)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if root == nil {
		return cfg, nil
	}
	if err := v.config(root, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validator checks one file. Its errors are fail.Say errors: Parse has no
// Context to say them with.
type validator struct {
	file string
	// root is the repo the file configures, or "" when unknown.
	root string
	// debug, when not nil, hears about ignored reserved keys.
	debug *slog.Logger
}

// ReservedKeys are top-level keys a coming release will give meaning to.
// homebot.yml is synced to every machine, so a build that predates them must
// not refuse a file that carries them: it accepts and ignores them, with no
// field and no effect. Their values are not validated here, and any YAML
// value (a map included) passes. A key leaves this list when its parser is added.
var ReservedKeys = reservedKeys()

// at is pos as errors name it, "<file>:<line>:<col>": the {file} or {where}
// that starts each config.error.* message.
func (v validator) at(pos token.Position) string {
	return fmt.Sprintf("%s:%d:%d", v.file, pos.Line, pos.Column)
}

// where is the {where} of a message about the value at path, such as
// exports[0]: "<file>:<line>:<col>: <path>".
func (v validator) where(pos token.Position, path string) string {
	return v.at(pos) + ": " + path
}

var parseErrPos = regexp.MustCompile(`^\[(\d+):(\d+)\] (.*)`)

// document parses src to the root node; nil means an empty document.
func (v validator) document(src []byte) (*node, error) {
	f, err := parser.ParseBytes(src, 0)
	if err != nil {
		first, _, _ := strings.Cut(err.Error(), "\n")
		pos := token.Position{Line: 1, Column: 1}
		if m := parseErrPos.FindStringSubmatch(first); m != nil {
			pos.Line, _ = strconv.Atoi(m[1])
			pos.Column, _ = strconv.Atoi(m[2])
			first = m[3]
		}
		return nil, fail.Say(msg.ConfigErrorYAML(v.at(pos), first))
	}
	var bodies []ast.Node
	for _, d := range f.Docs {
		if d.Body != nil {
			if _, isComment := d.Body.(*ast.CommentGroupNode); !isComment {
				bodies = append(bodies, d.Body)
			}
		}
	}
	if len(bodies) == 0 {
		return nil, nil
	}
	if len(bodies) > 1 {
		return nil, fail.Say(msg.ConfigErrorNotMapping(v.at(*bodies[1].GetToken().Position)))
	}
	n, err := build(bodies[0])
	if err != nil {
		return nil, fail.Say(msg.ConfigErrorYAML(v.at(*bodies[0].GetToken().Position), err.Error()))
	}
	if n.kind == kindNull {
		return nil, nil
	}
	if n.kind != kindMap {
		return nil, fail.Say(msg.ConfigErrorNotMapping(v.at(n.pos)))
	}
	return n, nil
}

// expectKeys rejects keys outside allowed. parent is "" at the top level.
func (v validator) expectKeys(n *node, parent string) error {
	allowed := children(parent)
	for _, e := range n.entries {
		if slices.Contains(allowed, e.key) || (parent == "" && slices.Contains(ReservedKeys, e.key)) {
			continue
		}
		keys := strings.Join(allowed, ", ")
		if parent == "" {
			return fail.Say(msg.ConfigErrorUnknownKey(v.at(e.keyPos), e.key, keys))
		}
		return fail.Say(msg.ConfigErrorUnknownKeyUnder(v.at(e.keyPos), e.key, parent, keys))
	}
	return nil
}

func (v validator) config(doc *node, cfg *Config) error {
	for _, e := range doc.entries {
		if e.key == "link" {
			return fail.Say(msg.ConfigErrorLinkGone(v.at(e.keyPos)))
		}
	}
	if err := v.expectKeys(doc, ""); err != nil {
		return err
	}
	if v.debug != nil {
		for _, e := range doc.entries {
			if slices.Contains(ReservedKeys, e.key) {
				v.debug.Debug("config: ignoring reserved key", "key", e.key, "file", v.file)
			}
		}
	}
	if p := doc.get("profiles"); p != nil {
		if err := v.profiles(p, &cfg.Profiles); err != nil {
			return err
		}
	}
	if t := doc.get("triage"); t != nil {
		if err := v.triage(t, &cfg.Triage); err != nil {
			return err
		}
	}
	if a := doc.get("autosave"); a != nil {
		if err := v.autosave(a, &cfg.Autosave); err != nil {
			return err
		}
	}
	if e := doc.get("exports"); e != nil {
		if err := v.exports(e, cfg); err != nil {
			return err
		}
	}
	if l := doc.get("limits"); l != nil {
		if err := v.limits(l, &cfg.Limits); err != nil {
			return err
		}
	}
	if n := doc.get("name"); n != nil {
		if err := v.name(n, cfg); err != nil {
			return err
		}
	}
	if p := doc.get("persona"); p != nil {
		if err := v.persona(p, cfg); err != nil {
			return err
		}
	}
	if u := doc.get("update"); u != nil {
		v.update(u, cfg)
	}
	return nil
}

func (v validator) update(n *node, cfg *Config) {
	if n.kind == kindNull {
		return
	}
	if n.kind == kindString {
		switch UpdateMode(n.str) {
		case UpdateOff, UpdateNotify, UpdateAuto:
			cfg.Update = UpdateMode(n.str)
			return
		}
	}
	cfg.Update = UpdateOff
	cfg.Warnings = append(cfg.Warnings, msg.ConfigWarningUpdate(valueSummary(n), v.file, n.pos.Line))
}

func valueSummary(n *node) string {
	var describe func(*node, int) string
	describe = func(n *node, depth int) string {
		if depth > 2 {
			return "…"
		}
		switch n.kind {
		case kindNull:
			return "null"
		case kindBool:
			return strconv.FormatBool(n.boolean)
		case kindNumber:
			return strconv.FormatFloat(n.num, 'g', -1, 64)
		case kindString:
			return strconv.Quote(msg.StripUnsafe(n.str))
		case kindList:
			parts := make([]string, 0, min(len(n.items), 3))
			for _, child := range n.items[:min(len(n.items), 3)] {
				parts = append(parts, describe(child, depth+1))
			}
			if len(n.items) > 3 {
				parts = append(parts, "…")
			}
			return "[" + strings.Join(parts, ", ") + "]"
		case kindMap:
			parts := make([]string, 0, min(len(n.entries), 3))
			for _, e := range n.entries[:min(len(n.entries), 3)] {
				parts = append(parts, msg.StripUnsafe(e.key)+": "+describe(e.val, depth+1))
			}
			if len(n.entries) > 3 {
				parts = append(parts, "…")
			}
			return "{" + strings.Join(parts, ", ") + "}"
		}
		return "unknown"
	}
	s := describe(n, 0)
	if len([]rune(s)) > 120 {
		s = string([]rune(s)[:119]) + "…"
	}
	return s
}

// persona checks only syntax. Whether the named persona exists or can be
// loaded belongs to the persona loader, which runs with the selected context.
func (v validator) persona(n *node, cfg *Config) error {
	if n.kind != kindString {
		return fail.Say(msg.ConfigErrorPersona(v.at(n.pos)))
	}
	if !ValidPersonaID(n.str) {
		return fail.Say(msg.ConfigErrorPersonaSyntax(v.at(n.pos), oneLine(n.str)))
	}
	cfg.Persona = n.str
	return nil
}

// oneLine is s as one printable line of an error: StripUnsafe removes the
// control characters but keeps \n, which a block scalar or a "te\nster"
// brings, and the error would run over two lines. Each newline is shown as
// the two characters \n instead.
func oneLine(s string) string {
	return strings.ReplaceAll(msg.StripUnsafe(s), "\n", `\n`)
}

// noName is the value of name: that means the same as leaving it out.
const noName = "homebot"

// name checks name:. It checks syntax only: reserved names, PATH and the
// machine are the CLI's to check (plan D3).
func (v validator) name(n *node, cfg *Config) error {
	if n.kind != kindString {
		return fail.Say(msg.ConfigErrorName(v.at(n.pos)))
	}
	if !ValidName(n.str) {
		return fail.Say(msg.ConfigErrorNameSyntax(v.at(n.pos), n.str))
	}
	if n.str != noName {
		cfg.Name = n.str
	}
	return nil
}

func (v validator) profiles(n *node, out *Profiles) error {
	if n.kind != kindMap {
		return fail.Say(msg.ConfigErrorProfiles(v.at(n.pos)))
	}
	if err := v.expectKeys(n, "profiles"); err != nil {
		return err
	}
	if d := n.get("default"); d != nil {
		if d.kind != kindString {
			return fail.Say(msg.ConfigErrorProfilesDefault(v.at(d.pos)))
		}
		out.Default = d.str
	}
	if h := n.get("hosts"); h != nil {
		if h.kind != kindMap {
			return fail.Say(msg.ConfigErrorProfilesHosts(v.at(h.pos)))
		}
		hosts := map[string]string{}
		for _, e := range h.entries {
			if e.val.kind != kindString {
				return fail.Say(msg.ConfigErrorProfilesHosts(v.at(h.pos)))
			}
			hosts[e.key] = e.val.str
		}
		out.Hosts = hosts
	}
	return nil
}

var watchSuffix = regexp.MustCompile(`/\.?\*$`)

func (v validator) triage(n *node, out *Triage) error {
	if n.kind != kindMap {
		return fail.Say(msg.ConfigErrorTriage(v.at(n.pos)))
	}
	if err := v.expectKeys(n, "triage"); err != nil {
		return err
	}
	w := n.get("watch")
	if w == nil {
		out.Watch = []string{}
		return nil
	}
	watch, err := v.stringList(w, "triage.watch")
	if err != nil {
		return err
	}
	for i, s := range watch {
		if !watchSuffix.MatchString(s) {
			at := w.pos
			if w.kind == kindList {
				at = w.items[i].pos
			}
			return fail.Say(msg.ConfigErrorTriageWatch(v.at(at), s))
		}
	}
	out.Watch = watch
	return nil
}

func (v validator) autosave(n *node, out *Autosave) error {
	if n.kind != kindMap {
		return fail.Say(msg.ConfigErrorAutosave(v.at(n.pos)))
	}
	if err := v.expectKeys(n, "autosave"); err != nil {
		return err
	}
	if c := n.get("commit"); c != nil {
		cadence := Cadence(c.str)
		if c.kind != kindString || (cadence != CadenceOff && cadence != CadenceHourly && cadence != CadenceDaily) {
			return fail.Say(msg.ConfigErrorAutosaveCommit(v.at(c.pos)))
		}
		out.Commit = cadence
	}
	if p := n.get("push"); p != nil {
		if p.kind != kindBool {
			return fail.Say(msg.ConfigErrorAutosavePush(v.at(p.pos)))
		}
		out.Push = p.boolean
	}
	return nil
}

func (v validator) exports(n *node, cfg *Config) error {
	if n.kind != kindList {
		return fail.Say(msg.ConfigErrorExports(v.at(n.pos)))
	}
	specs := []ExportSpec{}
	for i, e := range n.items {
		idx := strconv.Itoa(i)
		if e.kind != kindMap {
			return fail.Say(msg.ConfigErrorExportMapping(v.where(e.pos, "exports["+idx+"]")))
		}
		if err := v.expectKeys(e, "exports["+idx+"]"); err != nil {
			return err
		}
		for _, k := range []string{"name", "out", "run"} {
			if f := e.get(k); f == nil || f.kind != kindString || f.str == "" {
				return fail.Say(msg.ConfigErrorExportRequired(v.where(e.pos, "exports["+idx+"]"), k))
			}
		}
		spec := ExportSpec{
			Name: e.get("name").str,
			On:   []string{"linux", "macos"},
			Out:  e.get("out").str,
			Run:  e.get("run").str,
		}
		if err := ExportOutError(v.root, v.where(e.get("out").pos, "exports["+idx+"]"), spec.Name, spec.Out); err != nil {
			return err
		}
		if on := e.get("on"); on != nil {
			list, err := v.stringList(on, "exports["+idx+"].on")
			if err != nil {
				return err
			}
			for j, os := range list {
				if os != "linux" && os != "macos" {
					at := on.pos
					if on.kind == kindList {
						at = on.items[j].pos
					}
					return fail.Say(msg.ConfigErrorUnknownOS(v.where(at, "exports["+idx+"].on"), os))
				}
			}
			spec.On = list
		}
		if p := e.get("profiles"); p != nil {
			if p.kind != kindList {
				return fail.Say(msg.ConfigErrorExportProfiles(v.where(p.pos, "exports["+idx+"]")))
			}
			spec.Profiles = []string{}
			for _, item := range p.items {
				if item.kind != kindString || item.str == "" {
					return fail.Say(msg.ConfigErrorExportProfiles(v.where(p.pos, "exports["+idx+"]")))
				}
				spec.Profiles = append(spec.Profiles, item.str)
			}
		}
		specs = append(specs, spec)
	}
	// One name may appear once per kind of machine: `homebot export apt` runs
	// the apt export of this machine's profile.
	for i, spec := range specs {
		for _, other := range specs[:i] {
			if other.Name == spec.Name && overlap(other.On, spec.On) && overlap(other.Profiles, spec.Profiles) {
				return fail.Say(msg.ConfigErrorExportDuplicate(v.at(n.items[i].pos), i, spec.Name))
			}
		}
	}
	cfg.Exports = specs
	return nil
}

// overlap reports whether two selectors can match the same machine; nil
// means "every machine".
func overlap(a, b []string) bool {
	if a == nil || b == nil {
		return true
	}
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func (v validator) limits(n *node, out *Limits) error {
	if n.kind != kindMap {
		return fail.Say(msg.ConfigErrorLimits(v.at(n.pos)))
	}
	if err := v.expectKeys(n, "limits"); err != nil {
		return err
	}
	if mb := n.get("maxFileMB"); mb != nil {
		if mb.kind != kindNumber || math.IsNaN(mb.num) || mb.num <= 0 {
			return fail.Say(msg.ConfigErrorLimitsMax(v.at(mb.pos)))
		}
		out.MaxFileMB = mb.num
	}
	return nil
}

// stringList accepts a string, a list of strings, or null (an empty list).
func (v validator) stringList(n *node, where string) ([]string, error) {
	switch n.kind {
	case kindNull:
		return []string{}, nil
	case kindString:
		return []string{n.str}, nil
	case kindList:
		out := make([]string, 0, len(n.items))
		for _, item := range n.items {
			if item.kind != kindString {
				return nil, fail.Say(msg.ConfigErrorStringList(v.where(n.pos, where)))
			}
			out = append(out, item.str)
		}
		return out, nil
	}
	return nil, fail.Say(msg.ConfigErrorStringList(v.where(n.pos, where)))
}
