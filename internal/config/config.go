// Package config loads exon.toml. Each step owns one [section] and decodes it
// into its own struct, so adding a step never changes this package.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/bitbynik/exon/internal/usererr"
)

// FileName is the config file at the root of a config repo.
const FileName = "exon.toml"

// File is a parsed exon.toml.
type File struct {
	path     string
	sections map[string]map[string]any
}

// Load parses the file at path. known lists the valid section names; any
// other top-level key is an error, so typos don't silently do nothing.
func Load(path string, known []string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, usererr.New("no "+FileName+" found", "The config repo needs "+FileName+" at its root. Expected: "+path, err)
		}
		return nil, usererr.New("cannot read "+FileName, "", err)
	}
	return parse(path, data, known)
}

func parse(path string, data []byte, known []string) (*File, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		if de, ok := errors.AsType[*toml.DecodeError](err); ok {
			row, col := de.Position()
			return nil, usererr.New(
				fmt.Sprintf("%s line %d, column %d: %s", path, row, col, de.Error()),
				"Fix the TOML syntax there.\n"+de.String(), err)
		}
		return nil, usererr.New(path+": invalid TOML", "", err)
	}

	f := &File{path: path, sections: map[string]map[string]any{}}
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		if !slices.Contains(known, name) {
			return nil, usererr.New(
				fmt.Sprintf("%s: unknown section %q", path, name),
				"Valid sections: "+strings.Join(known, ", "), nil)
		}
		table, ok := raw[name].(map[string]any)
		if !ok {
			return nil, usererr.New(fmt.Sprintf("%s: %q must be a [section]", path, name), "", nil)
		}
		f.sections[name] = table
	}
	return f, nil
}

// Section returns the named section. A missing section is empty, not an error.
func (f *File) Section(name string) Section {
	return Section{file: f.path, name: name, raw: f.sections[name]}
}

// Section is one [table] of exon.toml.
type Section struct {
	file string
	name string
	raw  map[string]any
}

// Decode fills v (a pointer to a struct with `toml` tags) from the section.
// Unknown keys and wrong types are errors naming the section and key.
func (s Section) Decode(v any) error {
	if len(s.raw) == 0 {
		return nil
	}
	data, err := toml.Marshal(s.raw)
	if err != nil {
		return fmt.Errorf("re-encode [%s]: %w", s.name, err)
	}
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if sme, ok := errors.AsType[*toml.StrictMissingError](err); ok {
			keys := make([]string, 0, len(sme.Errors))
			for _, e := range sme.Errors {
				keys = append(keys, strings.Join(e.Key(), "."))
			}
			return usererr.New(
				fmt.Sprintf("%s [%s]: unknown key %s", s.file, s.name, strings.Join(keys, ", ")),
				"Check the spelling against docs/configuration.md.", nil)
		}
		return usererr.New(fmt.Sprintf("%s [%s]: %v", s.file, s.name, err), "", err)
	}
	return nil
}
