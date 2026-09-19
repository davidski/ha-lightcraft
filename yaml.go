package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var fileKinds = map[string]ConfigKind{
	"scripts.yaml":      Scripts,
	"automations.yaml":  Automations,
	"input_select.yaml": Helpers,
	"colors.yaml":       Colors,
}

var errNoSupportedYAML = errors.New("no supported YAML files")

func emptyBundle() (Bundle, error) {
	return withHash(Bundle{Files: map[ConfigKind]Config{}})
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
	if len(bundle.Files) == 0 {
		return Bundle{}, fmt.Errorf("%w in %s", errNoSupportedYAML, dir)
	}
	if source, err := os.ReadFile(filepath.Join(dir, ".ha-source-hash")); err == nil {
		bundle.SourceHash = string(source)
	} else if !os.IsNotExist(err) {
		return Bundle{}, fmt.Errorf("read .ha-source-hash: %w", err)
	}
	return withHash(bundle)
}

func LoadBundleAt(dir string, filePaths map[ConfigKind][]string) (Bundle, error) {
	return LoadBundleAtWithReferences(dir, dir, filePaths)
}

func LoadBundleAtWithReferences(dir, colorsDir string, filePaths map[ConfigKind][]string) (Bundle, error) {
	if len(filePaths) == 0 {
		bundle, err := LoadBundle(dir)
		if errors.Is(err, errNoSupportedYAML) {
			return emptyBundle()
		}
		return bundle, err
	}
	localPaths := localFilePaths(filePaths)
	native, err := (NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: ".", FilePaths: localPaths}).ReadAll(context.Background())
	if err != nil {
		legacy, legacyErr := LoadBundle(dir)
		if legacyErr == nil {
			return legacy, nil
		}
		if errors.Is(err, errNoNativeYAML) && errors.Is(legacyErr, errNoSupportedYAML) {
			return emptyBundle()
		}
		return Bundle{}, legacyErr
	}
	colors, ok, err := loadColors(colorsDir)
	if err != nil {
		return Bundle{}, err
	}
	if ok {
		native.Files[Colors] = colors
	}
	return withHash(native)
}

func loadColors(dir string) (Config, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, filenameForKind(Colors)))
	if os.IsNotExist(err) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("read colors: %w", err)
	}
	var value any
	if err := yaml.Unmarshal(data, &value); err != nil {
		return Config{}, false, fmt.Errorf("parse colors: %w", err)
	}
	return Config{Kind: Colors, Data: normalizeYAML(value)}, true, nil
}

func loadBaselineBundle(dir string, filePaths map[ConfigKind][]string) (Bundle, error) {
	if len(filePaths) > 0 {
		store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: ".", FilePaths: localFilePaths(filePaths)}
		if bundle, err := store.ReadAll(context.Background()); err == nil {
			return bundle, nil
		}
	}
	bundle, err := LoadBundle(dir)
	if errors.Is(err, errNoSupportedYAML) {
		return emptyBundle()
	}
	return bundle, err
}

func SaveBundle(dir string, bundle Bundle) error {
	return SaveBundleAt(dir, bundle, nil)
}

func SaveBundleAt(dir string, bundle Bundle, filePaths map[ConfigKind][]string) error {
	return SaveBundleAtWithReferences(dir, dir, bundle, filePaths)
}

func SaveBundleAtWithReferences(dir, colorsDir string, bundle Bundle, filePaths map[ConfigKind][]string) error {
	if colorsDir == "" {
		colorsDir = dir
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create draft directory: %w", err)
	}
	filePaths = localFilePaths(filePaths)
	if packageFile, ok := packagePath(filePaths); ok {
		if err := validateRelativePath(packageFile); err != nil {
			return err
		}
		data, err := marshalPackageYAML(bundle)
		if err != nil {
			return fmt.Errorf("serialize package: %w", err)
		}
		path := filepath.Join(dir, packageFile)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create package parent: %w", err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return fmt.Errorf("write package: %w", err)
		}
	}
	for _, kind := range bundle.Kinds() {
		if _, ok := packagePath(filePaths); ok && kind != Colors {
			continue
		}
		data, err := marshalConfigYAML(kind, bundle.Files[kind].Data)
		if err != nil {
			return fmt.Errorf("serialize %s: %w", kind, err)
		}
		pathRoot := dir
		if kind == Colors && len(filePaths) > 0 {
			pathRoot = colorsDir
			if err := os.MkdirAll(pathRoot, 0o700); err != nil {
				return fmt.Errorf("create references directory: %w", err)
			}
		}
		path := filepath.Join(pathRoot, filenameForKind(kind))
		if paths := filePaths[kind]; len(paths) > 0 {
			if len(paths) != 1 {
				return fmt.Errorf("cannot save multi-file %s configuration", kind)
			}
			if err := validateRelativePath(paths[0]); err != nil {
				return err
			}
			path = filepath.Join(dir, paths[0])
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create draft parent for %s: %w", kind, err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
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

func marshalPackageYAML(bundle Bundle) ([]byte, error) {
	value := map[string]any{
		"automation":   packageData(bundle, Automations, []any{}),
		"input_select": packageData(bundle, Helpers, map[string]any{}),
		"script":       packageData(bundle, Scripts, map[string]any{}),
	}
	return yaml.Marshal(normalizeYAML(value))
}

func packageData(bundle Bundle, kind ConfigKind, empty any) any {
	if config, ok := bundle.Files[kind]; ok && config.Data != nil {
		return config.Data
	}
	return empty
}

func marshalConfigYAML(kind ConfigKind, value any) ([]byte, error) {
	data, err := yaml.Marshal(normalizeYAML(value))
	if err != nil || kind != Automations {
		return data, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	orderAutomationStanzas(&document)
	return yaml.Marshal(&document)
}

func orderAutomationStanzas(document *yaml.Node) {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return
	}
	list := document.Content[0]
	if list.Kind != yaml.SequenceNode {
		return
	}
	for _, stanza := range list.Content {
		if stanza.Kind != yaml.MappingNode {
			continue
		}
		ordered := make([]*yaml.Node, 0, len(stanza.Content))
		used := make([]bool, len(stanza.Content)/2)
		for _, name := range []string{"id", "alias", "description"} {
			for i := 0; i+1 < len(stanza.Content); i += 2 {
				if !used[i/2] && stanza.Content[i].Value == name {
					ordered = append(ordered, stanza.Content[i], stanza.Content[i+1])
					used[i/2] = true
					break
				}
			}
		}
		for i := 0; i+1 < len(stanza.Content); i += 2 {
			if !used[i/2] {
				ordered = append(ordered, stanza.Content[i], stanza.Content[i+1])
			}
		}
		stanza.Content = ordered
	}
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
		if xy, ok := result["xy"]; ok {
			result["xy"] = normalizeYAMLXY(xy)
		}
		if _, hasName := result["name"]; hasName {
			if x, ok := yamlFloat(result["x"]); ok {
				result["x"] = normalizeXY(x)
			}
			if y, ok := yamlFloat(result["y"]); ok {
				result["y"] = normalizeXY(y)
			}
		}
		return result
	case map[any]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			result[fmt.Sprint(key)] = normalizeYAML(item)
		}
		return normalizeYAML(result)
	case []any:
		for i := range value {
			value[i] = normalizeYAML(value[i])
		}
	}
	return value
}

func yamlFloat(value any) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	default:
		return 0, false
	}
}

func normalizeYAMLXY(value any) any {
	switch values := value.(type) {
	case []any:
		for i := 0; i < len(values) && i < 2; i++ {
			if number, ok := yamlFloat(values[i]); ok {
				values[i] = normalizeXY(number)
			}
		}
	case []float64:
		for i := 0; i < len(values) && i < 2; i++ {
			values[i] = normalizeXY(values[i])
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
