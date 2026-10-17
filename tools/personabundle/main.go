// Command personabundle gives the Fable author one current, deterministic
// inventory of messages that a persona may reword.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

type placeholder struct {
	Name     string       `json:"name"`
	Type     catalog.Type `json:"type"`
	Optional bool         `json:"optional"`
	Value    *string      `json:"value"`
	Meaning  string       `json:"meaning"`
	Forms    string       `json:"forms"`
}

type entry struct {
	ID           string        `json:"id"`
	Kind         string        `json:"kind"`
	Class        string        `json:"class"`
	Text         string        `json:"text"`
	Append       string        `json:"append"`
	Placeholders []placeholder `json:"placeholders"`
	Notes        string        `json:"notes"`
	Variants     string        `json:"variants"`
}

func main() {
	catalogPath := flag.String("catalog", "internal/msg/catalog.yaml", "message catalog")
	manyPath := flag.String("many", "internal/persona/fable/many.txt", "ids needing 3–5 variants")
	outPath := flag.String("out", "internal/persona/fable/entries.json", "output JSON")
	flag.Parse()
	data, err := bundle(*catalogPath, *manyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "personabundle:", err)
		os.Exit(1)
	}
	old, err := os.ReadFile(*outPath)
	if err == nil && bytes.Equal(old, data) {
		return
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "personabundle:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "personabundle:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "personabundle:", err)
		os.Exit(1)
	}
}

func bundle(catalogPath, manyPath string) ([]byte, error) {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return nil, err
	}
	file, err := catalog.Load(data)
	if err != nil {
		return nil, err
	}
	data, err = os.ReadFile(manyPath)
	if err != nil {
		return nil, err
	}
	many := map[string]bool{}
	for lineNo, raw := range strings.Split(string(data), "\n") {
		id := strings.TrimSpace(raw)
		if id == "" || strings.HasPrefix(id, "#") {
			continue
		}
		if many[id] {
			return nil, fmt.Errorf("%s:%d: duplicate id %q", manyPath, lineNo+1, id)
		}
		many[id] = true
	}
	out := make([]entry, 0, len(file.Messages))
	for _, msg := range file.Messages {
		if !msg.Persona {
			continue
		}
		variants := "1-2"
		if many[msg.ID] {
			variants = "3-5"
		}
		item := entry{ID: msg.ID, Kind: msg.Kind, Class: msg.Class, Text: msg.Text,
			Append: msg.Append, Notes: msg.Notes, Variants: variants,
			Placeholders: make([]placeholder, 0, len(msg.Placeholders))}
		for _, p := range msg.Placeholders {
			item.Placeholders = append(item.Placeholders, placeholder{
				Name: p.Name, Type: p.Type, Optional: p.Optional, Value: p.Value,
				Meaning: p.Meaning, Forms: p.Forms,
			})
		}
		out = append(out, item)
	}
	slices.SortFunc(out, func(a, b entry) int { return strings.Compare(a.ID, b.ID) })
	data, err = json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
