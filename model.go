package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type ConfigKind string

const (
	Scenes      ConfigKind = "scenes" // migration input only; never part of a saved modern draft
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

func nativeDraftKinds(bundle Bundle) []ConfigKind {
	result := make([]ConfigKind, 0, len(bundle.Files))
	for _, kind := range bundle.Kinds() {
		if kind != Scenes && kind != Colors {
			result = append(result, kind)
		}
	}
	return result
}

func nativeDraftName(kind ConfigKind, filePaths map[ConfigKind][]string) string {
	if path, ok := packagePath(filePaths); ok {
		return path
	}
	if paths := filePaths[kind]; len(paths) > 0 {
		return strings.Join(paths, ", ")
	}
	return filenameForKind(kind)
}

func packagePath(filePaths map[ConfigKind][]string) (string, bool) {
	var path string
	for _, kind := range []ConfigKind{Scripts, Automations, Helpers} {
		paths := filePaths[kind]
		if len(paths) != 1 || strings.TrimSpace(paths[0]) == "" {
			return "", false
		}
		if path == "" {
			path = paths[0]
		} else if path != paths[0] {
			return "", false
		}
	}
	return path, path != ""
}

func localFilePaths(filePaths map[ConfigKind][]string) map[ConfigKind][]string {
	if len(filePaths) == 0 {
		return filePaths
	}
	paths := make(map[ConfigKind][]string, len(filePaths))
	for kind, values := range filePaths {
		paths[kind] = append([]string(nil), values...)
	}
	if packageFile, ok := packagePath(filePaths); ok {
		localPackage := filepath.Base(filepath.Clean(packageFile))
		for _, kind := range []ConfigKind{Scripts, Automations, Helpers} {
			paths[kind] = []string{localPackage}
		}
	}
	return paths
}

func canonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("canonicalize config: %w", err)
	}
	return data, nil
}

func cloneBundle(bundle Bundle) (Bundle, error) {
	result := Bundle{Files: map[ConfigKind]Config{}, SourceHash: bundle.SourceHash}
	for kind, config := range bundle.Files {
		data, err := canonicalJSON(config.Data)
		if err != nil {
			return Bundle{}, err
		}
		var copy any
		if err := json.Unmarshal(data, &copy); err != nil {
			return Bundle{}, err
		}
		result.Files[kind] = Config{Kind: config.Kind, Data: copy}
	}
	return result, nil
}
