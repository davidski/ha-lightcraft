package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var fileKinds = map[string]ConfigKind{
	"scenes.yaml":       Scenes,
	"scripts.yaml":      Scripts,
	"automations.yaml":  Automations,
	"input_select.yaml": Helpers,
	"colors.yaml":       Colors,
}

func LoadBundle(dir string) (Bundle, error) {
	bundle := Bundle{Files: map[ConfigKind]Config{}}
	for filename, kind := range fileKinds {
		path := filepath.Join(dir, filename)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Bundle{}, fmt.Errorf("read %s: %w", path, err)
		}
		var value any
		if err := yaml.Unmarshal(data, &value); err != nil {
			return Bundle{}, fmt.Errorf("parse %s: %w", path, err)
		}
		bundle.Files[kind] = Config{Kind: kind, Data: normalizeYAML(value)}
	}
	legacyHelpers := filepath.Join(dir, "helpers.yaml")
	if _, exists := bundle.Files[Helpers]; !exists {
		data, err := os.ReadFile(legacyHelpers)
		if err == nil {
			var value any
			if err := yaml.Unmarshal(data, &value); err != nil {
				return Bundle{}, fmt.Errorf("parse helpers.yaml: %w", err)
			}
			bundle.Files[Helpers] = Config{Kind: Helpers, Data: normalizeYAML(value)}
		} else if !os.IsNotExist(err) {
			return Bundle{}, fmt.Errorf("read helpers.yaml: %w", err)
		}
	}
	if len(bundle.Files) == 0 {
		return Bundle{}, fmt.Errorf("no supported YAML files in %s", dir)
	}
	if source, err := os.ReadFile(filepath.Join(dir, ".ha-source-hash")); err == nil {
		bundle.SourceHash = string(source)
	} else if !os.IsNotExist(err) {
		return Bundle{}, fmt.Errorf("read .ha-source-hash: %w", err)
	}
	return withHash(bundle)
}

func SaveBundle(dir string, bundle Bundle) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create draft directory: %w", err)
	}
	for _, kind := range bundle.Kinds() {
		data, err := yaml.Marshal(bundle.Files[kind].Data)
		if err != nil {
			return fmt.Errorf("serialize %s: %w", kind, err)
		}
		if err := os.WriteFile(filepath.Join(dir, filenameForKind(kind)), data, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", kind, err)
		}
	}
	if bundle.SourceHash != "" {
		if err := os.WriteFile(filepath.Join(dir, ".ha-source-hash"), []byte(bundle.SourceHash), 0o600); err != nil {
			return fmt.Errorf("write .ha-source-hash: %w", err)
		}
	}
	return nil
}

func filenameForKind(kind ConfigKind) string {
	if kind == Helpers {
		return "input_select.yaml"
	}
	return string(kind) + ".yaml"
}

func normalizeYAML(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			result[key] = normalizeYAML(item)
		}
		return result
	case map[any]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			result[fmt.Sprint(key)] = normalizeYAML(item)
		}
		return result
	case []any:
		for i := range value {
			value[i] = normalizeYAML(value[i])
		}
	}
	return value
}

func withHash(bundle Bundle) (Bundle, error) {
	digest := sha256.New()
	for _, kind := range bundle.Kinds() {
		data, err := canonicalJSON(bundle.Files[kind].Data)
		if err != nil {
			return Bundle{}, err
		}
		digest.Write([]byte(kind))
		digest.Write(data)
	}
	bundle.Hash = hex.EncodeToString(digest.Sum(nil))
	return bundle, nil
}
