package config

import (
	"strings"

	"github.com/hyperlab-sh/homebot/internal/msg"
)

// KeyKind describes the shape of one homebot.yml value.
type KeyKind string

const (
	String KeyKind = "string"
	Bool   KeyKind = "bool"
	Number KeyKind = "number"
	List   KeyKind = "list"
	Map    KeyKind = "map"
	Object KeyKind = "object"
	Any    KeyKind = "any"
)

// Key describes one configuration path. Defaults are the values Default()
// applies when a path is absent; an empty Default means there is no scalar
// default. DocID points to the catalog help entry used by generated docs.
type Key struct {
	Path     string
	Kind     KeyKind
	Allowed  []string
	Default  string
	DocID    string
	Added    string
	Reserved bool
}

// Keys is the source of accepted paths, editor schema data and reference docs.
// Order is the order used in unknown-key diagnostics.
var Keys = []Key{
	{Path: "profiles", Kind: Object, DocID: "config.doc.profiles", Added: "0.1.0"},
	{Path: "profiles.default", Kind: String, DocID: "config.doc.profiles.default", Added: "0.1.0"},
	{Path: "profiles.hosts", Kind: Map, Default: "{}", DocID: "config.doc.profiles.hosts", Added: "0.1.0"},
	{Path: "triage", Kind: Object, DocID: "config.doc.triage", Added: "0.1.0"},
	{Path: "triage.watch", Kind: List, Default: "[~/.config/*, ~/.*]", DocID: "config.doc.triage.watch", Added: "0.1.0"},
	{Path: "autosave", Kind: Object, DocID: "config.doc.autosave", Added: "0.1.0"},
	{Path: "autosave.commit", Kind: String, Allowed: []string{"off", "hourly", "daily"}, Default: "off", DocID: "config.doc.autosave.commit", Added: "0.1.0"},
	{Path: "autosave.push", Kind: Bool, Default: "false", DocID: "config.doc.autosave.push", Added: "0.1.0"},
	{Path: "exports", Kind: List, Default: "[]", DocID: "config.doc.exports", Added: "0.1.0"},
	{Path: "exports[].name", Kind: String, DocID: "config.doc.exports.name", Added: "0.1.0"},
	{Path: "exports[].on", Kind: List, Allowed: []string{"linux", "macos"}, Default: "[linux, macos]", DocID: "config.doc.exports.on", Added: "0.1.0"},
	{Path: "exports[].out", Kind: String, DocID: "config.doc.exports.out", Added: "0.1.0"},
	{Path: "exports[].run", Kind: String, DocID: "config.doc.exports.run", Added: "0.1.0"},
	{Path: "exports[].profiles", Kind: List, DocID: "config.doc.exports.profiles", Added: "0.1.0"},
	{Path: "limits", Kind: Object, DocID: "config.doc.limits", Added: "0.1.0"},
	{Path: "limits.maxFileMB", Kind: Number, Default: "50", DocID: "config.doc.limits.max_file_mb", Added: "0.1.0"},
	{Path: "name", Kind: String, DocID: "config.doc.name", Added: "0.1.0"},
	{Path: "persona", Kind: String, DocID: "config.doc.persona", Added: "0.1.0"},
	{Path: "update", Kind: String, Allowed: []string{"off", "notify", "auto"}, Default: "off", DocID: "config.doc.update", Added: "0.1.0"},
	{Path: "ai", Kind: Any, DocID: "config.doc.ai", Added: "future", Reserved: true},
}

// schemaDocs keeps generated catalog accessors live until the reference-page
// generator consumes them. It also lets docs read the table without knowing
// accessor names or reconstructing catalog ids.
var schemaDocs = map[string]msg.Text{
	"config.doc.profiles":           msg.ConfigDocProfiles(),
	"config.doc.profiles.default":   msg.ConfigDocProfilesDefault(),
	"config.doc.profiles.hosts":     msg.ConfigDocProfilesHosts(),
	"config.doc.triage":             msg.ConfigDocTriage(),
	"config.doc.triage.watch":       msg.ConfigDocTriageWatch(),
	"config.doc.autosave":           msg.ConfigDocAutosave(),
	"config.doc.autosave.commit":    msg.ConfigDocAutosaveCommit(),
	"config.doc.autosave.push":      msg.ConfigDocAutosavePush(),
	"config.doc.exports":            msg.ConfigDocExports(),
	"config.doc.exports.name":       msg.ConfigDocExportsName(),
	"config.doc.exports.on":         msg.ConfigDocExportsOn(),
	"config.doc.exports.out":        msg.ConfigDocExportsOut(),
	"config.doc.exports.run":        msg.ConfigDocExportsRun(),
	"config.doc.exports.profiles":   msg.ConfigDocExportsProfiles(),
	"config.doc.limits":             msg.ConfigDocLimits(),
	"config.doc.limits.max_file_mb": msg.ConfigDocLimitsMaxFileMb(),
	"config.doc.name":               msg.ConfigDocName(),
	"config.doc.persona":            msg.ConfigDocPersona(),
	"config.doc.update":             msg.ConfigDocUpdate(),
	"config.doc.ai":                 msg.ConfigDocAi(),
}

// Description returns the neutral catalog description for a schema row.
func Description(key Key) msg.Text { return schemaDocs[key.DocID] }

func reservedKeys() []string {
	var out []string
	for _, key := range Keys {
		if key.Reserved {
			out = append(out, key.Path)
		}
	}
	return out
}

// children lists the permitted direct children of path, in schema order.
// The reserved ai key is accepted but not suggested in errors or docs.
func children(path string) []string {
	if strings.HasPrefix(path, "exports[") {
		path = "exports[]"
	}
	var out []string
	for _, key := range Keys {
		if key.Reserved {
			continue
		}
		parent, name, hasParent := strings.Cut(key.Path, ".")
		if path == "" && !hasParent {
			out = append(out, parent)
		} else if hasParent && parent == path {
			out = append(out, name)
		}
	}
	return out
}
