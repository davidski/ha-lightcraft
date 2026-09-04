package main

import (
	"encoding/json"
	"fmt"
	"sort"
)

type ConfigKind string

const (
	Scenes      ConfigKind = "scenes"
	Scripts     ConfigKind = "scripts"
	Automations ConfigKind = "automations"
	Helpers     ConfigKind = "helpers"
	Colors      ConfigKind = "colors"
)

type Config struct {
	Kind ConfigKind
	Data any
}

type Bundle struct {
	Files      map[ConfigKind]Config
	Hash       string
	SourceHash string // full HA YAML fingerprint captured at import
}

func (b Bundle) Kinds() []ConfigKind {
	result := make([]ConfigKind, 0, len(b.Files))
	for kind := range b.Files {
		result = append(result, kind)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func mapKeys(values map[string]map[string]any) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func canonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("canonicalize config: %w", err)
	}
	return data, nil
}
