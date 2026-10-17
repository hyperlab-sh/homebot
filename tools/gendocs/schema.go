package main

import (
	"strings"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

type schema = map[string]any

const schemaVersion = "https://json-schema.org/draft/2020-12/schema"
const idPattern = `^[a-z][a-z0-9-]{0,31}$`

func object() schema {
	return schema{"type": "object", "properties": schema{}, "additionalProperties": false}
}

func homebotSchema() schema {
	root := object()
	root["$schema"] = schemaVersion
	root["$id"] = "https://homebot.sh/schema/homebot.json"
	root["title"] = "homebot.yml"
	// An empty YAML document is accepted by the loader as its defaults.
	root["type"] = []string{"object", "null"}
	for _, k := range config.Keys {
		parts := strings.Split(k.Path, ".")
		parent := root
		for i, part := range parts {
			if i == len(parts)-1 {
				parent["properties"].(schema)[part] = configValue(k)
				break
			}
			if part == "exports[]" {
				parent = parent["properties"].(schema)["exports"].(schema)["items"].(schema)
				continue
			}
			child, ok := parent["properties"].(schema)[part].(schema)
			if !ok {
				child = object()
				parent["properties"].(schema)[part] = child
			}
			parent = child
		}
	}
	// The loader requires all three fields on each export.
	exports := root["properties"].(schema)["exports"].(schema)
	exports["items"].(schema)["required"] = []string{"name", "out", "run"}
	return root
}

func configValue(k config.Key) schema {
	if k.Reserved {
		return schema{} // Forward-compatible key: any JSON/YAML value is accepted.
	}
	v := schema{"description": configText(k)}
	switch k.Kind {
	case config.Object:
		v["type"] = "object"
		v["properties"] = schema{}
		v["additionalProperties"] = false
	case config.Map:
		v["type"] = "object"
		v["patternProperties"] = schema{`.*`: schema{"type": "string"}}
		v["additionalProperties"] = false
	case config.List:
		item := schema{"type": "string"}
		if len(k.Allowed) > 0 {
			item["enum"] = k.Allowed
		}
		if k.Path == "exports" {
			item = object()
		}
		v["type"] = "array"
		v["items"] = item
		if k.Path == "triage.watch" || k.Path == "exports[].on" {
			// config.stringList accepts null, one string, or a list.
			v["type"] = []string{"array", "string", "null"}
			if k.Path == "triage.watch" {
				v["pattern"] = `/\.?\*$`
			} else {
				v["anyOf"] = []schema{
					{"type": "array", "items": item},
					{"type": "string", "enum": k.Allowed},
					{"type": "null"},
				}
				delete(v, "type")
				delete(v, "items")
			}
		}
	case config.String:
		v["type"] = "string"
		if len(k.Allowed) > 0 {
			v["enum"] = k.Allowed
		}
	case config.Bool:
		v["type"] = "boolean"
	case config.Number:
		v["type"] = "number"
	}
	switch k.Path {
	case "triage.watch":
		v["items"].(schema)["pattern"] = `/\.?\*$`
	case "exports[].name", "exports[].out", "exports[].run", "exports[].profiles":
		if k.Kind == config.String {
			v["minLength"] = 1
		} else if k.Path == "exports[].profiles" {
			v["items"].(schema)["minLength"] = 1
		}
	case "name":
		v["pattern"] = idPattern
		v["minLength"] = 2
	case "persona":
		v["pattern"] = idPattern
	case "limits.maxFileMB":
		v["exclusiveMinimum"] = 0
	}
	return v
}

func configText(k config.Key) string {
	return voice.Record(nil, config.Description(k))
}

func personaSchema() schema {
	root := object()
	root["$schema"] = schemaVersion
	root["$id"] = "https://homebot.sh/schema/persona.json"
	root["title"] = "homebot persona.toml"
	root["required"] = []string{"schema", "name", "emoji"}
	props := root["properties"].(schema)
	for _, f := range persona.Fields() {
		v := schema{"type": f.Type, "description": f.Description}
		switch f.Name {
		case "schema":
			v["const"] = 1
		case "id", "extends":
			v["pattern"] = idPattern
		case "name":
			v["minLength"], v["maxLength"] = 1, 32
		case "emoji":
			v["minLength"] = 1
			v["pattern"] = `^[^\x00-\x7f]+$`
		case "tagline":
			v["maxLength"] = 100
			v["pattern"] = "^[^\\n\\r$`]*$"
		case "colors":
			v = object()
			v["description"] = f.Description
			for _, token := range []string{"accent", "ok", "muted"} {
				pair := object()
				colour := schema{"oneOf": []schema{
					{"type": "string", "pattern": `^#[0-9a-fA-F]{6}$`},
					{"type": "string", "pattern": `^(?:0|[1-9][0-9]?|1[0-9]{2}|2[0-4][0-9]|25[0-5])$`},
					{"type": "integer", "minimum": 0, "maximum": 255},
				}}
				pair["properties"].(schema)["light"] = colour
				pair["properties"].(schema)["dark"] = colour
				v["properties"].(schema)[token] = pair
			}
		case "lines":
			v = object()
			v["description"] = f.Description
			for _, e := range msg.Entries() {
				if e.Persona {
					v["properties"].(schema)[e.ID] = schema{"type": "array", "minItems": 1, "items": schema{"type": "string"}}
				}
			}
		}
		props[f.Name] = v
	}
	return root
}
