package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBundleCanonicalizesMapOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scripts.yaml"), []byte("b: 2\na: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts.yaml"), []byte("a: 1\nb: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != second.Hash {
		t.Fatalf("hash changed for equivalent YAML: %s != %s", first.Hash, second.Hash)
	}
}

func TestBundleAtUsesConfiguredHomeAssistantTree(t *testing.T) {
	dir := t.TempDir()
	paths := map[ConfigKind][]string{
		Scripts:     {"scripts/holiday_lighting_designer.yaml"},
		Automations: {"automations/holiday_lighting_designer.yaml"},
		Helpers:     {"input_select/holiday_lighting_designer.yaml"},
	}
	bundle := Bundle{Files: map[ConfigKind]Config{
		Scripts:     {Kind: Scripts, Data: map[string]any{"holiday": map[string]any{"alias": "Holiday"}}},
		Automations: {Kind: Automations, Data: []any{map[string]any{"id": "holiday", "alias": "Holiday"}}},
		Helpers:     {Kind: Helpers, Data: map[string]any{"holiday": map[string]any{"name": "Holiday"}}},
		Colors:      {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}},
	}}
	if err := SaveBundleAt(dir, bundle, paths); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"scripts/holiday_lighting_designer.yaml",
		"automations/holiday_lighting_designer.yaml",
		"input_select/holiday_lighting_designer.yaml",
		"colors.yaml",
	} {
		if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
			t.Fatalf("missing proposed file %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts.yaml")); !os.IsNotExist(err) {
		t.Fatalf("legacy root scripts.yaml exists: %v", err)
	}
	loaded, err := LoadBundleAt(dir, paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundleEntries(loaded)) != len(bundleEntries(bundle)) || len(colorDefinitions(loaded)) != 1 {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestBundleAtUsesSingleHomeAssistantPackage(t *testing.T) {
	dir := t.TempDir()
	paths := map[ConfigKind][]string{
		Scripts:     {"packages/ha_lightcraft.yaml"},
		Automations: {"packages/ha_lightcraft.yaml"},
		Helpers:     {"packages/ha_lightcraft.yaml"},
	}
	bundle := Bundle{Files: map[ConfigKind]Config{
		Scripts:     {Kind: Scripts, Data: map[string]any{"holiday": map[string]any{"alias": "Holiday"}}},
		Automations: {Kind: Automations, Data: []any{map[string]any{"id": "holiday"}}},
		Helpers:     {Kind: Helpers, Data: map[string]any{"holiday": map[string]any{"name": "Holiday"}}},
	}}
	if err := SaveBundleAt(dir, bundle, paths); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ha_lightcraft.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, key := range []string{"automation:", "input_select:", "script:"} {
		if !strings.Contains(text, key) {
			t.Fatalf("package missing %q: %s", key, text)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts.yaml")); !os.IsNotExist(err) {
		t.Fatalf("per-kind draft file exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "packages/ha_lightcraft.yaml")); !os.IsNotExist(err) {
		t.Fatalf("nested package draft file exists: %v", err)
	}
	loaded, err := LoadBundleAt(dir, paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundleEntries(loaded)) != len(bundleEntries(bundle)) {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestColorsStayBesideTheDataRoot(t *testing.T) {
	root := t.TempDir()
	proposed := filepath.Join(root, "proposed")
	paths := map[ConfigKind][]string{Scripts: {"scripts/holiday.yaml"}}
	bundle := Bundle{Files: map[ConfigKind]Config{
		Scripts: {Kind: Scripts, Data: map[string]any{"holiday": map[string]any{"alias": "Holiday"}}},
		Colors:  {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}},
	}}
	if err := SaveBundleAtWithReferences(proposed, root, bundle, paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(proposed, "colors.yaml")); !os.IsNotExist(err) {
		t.Fatalf("proposed colors.yaml exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "colors.yaml")); err != nil {
		t.Fatalf("root colors.yaml missing: %v", err)
	}
	loaded, err := LoadBundleAtWithReferences(proposed, root, paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(colorDefinitions(loaded)) != 1 {
		t.Fatalf("loaded colors = %#v", colorDefinitions(loaded))
	}
}

func TestLoadBundleDoesNotFallbackToPreMoveColorCatalog(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	proposed := filepath.Join(dataRoot, "proposed")
	configDir := filepath.Join(root, "project")
	paths := map[ConfigKind][]string{Scripts: {"scripts/holiday.yaml"}}
	bundle := Bundle{Files: map[ConfigKind]Config{
		Scripts: {Kind: Scripts, Data: map[string]any{"holiday": map[string]any{"alias": "Holiday"}}},
		Colors:  {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}},
	}}
	if err := SaveBundleAtWithReferences(proposed, dataRoot, bundle, paths); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadBundleAtWithReferences(proposed, configDir, paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(colorDefinitions(loaded)) != 0 {
		t.Fatalf("pre-move colors were loaded: %#v", colorDefinitions(loaded))
	}
}

func TestDiffDetectsChangedConfig(t *testing.T) {
	old := Bundle{Files: map[ConfigKind]Config{Scripts: {Kind: Scripts, Data: map[string]any{"x": 1}}}}
	next := Bundle{Files: map[ConfigKind]Config{Scripts: {Kind: Scripts, Data: map[string]any{"x": 2}}}}
	changes, err := Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != Scripts {
		t.Fatalf("unexpected changes: %#v", changes)
	}
}

func TestDraftPreservesSourceFingerprint(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{SourceHash: "full-ha-hash", Files: map[ConfigKind]Config{Scripts: {Kind: Scripts, Data: []any{}}}}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SourceHash != bundle.SourceHash {
		t.Fatalf("source hash = %q", loaded.SourceHash)
	}
}

func TestXYValuesNormalizeToThreeDigits(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "colors.yaml"), []byte("blue:\n  name: Blue\n  x: 0.3107010688491798\n  y: 0.2086338447\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts.yaml"), []byte("lighting_sequence_blue:\n  alias: Blue\n  variables:\n    sequence_data:\n      repeat: true\n      steps:\n        - name: Blue\n          xy: [0.3107010688491798, 0.2086338447]\n          brightness: 255\n          hold: 1\n          transition: 0\n      \n  sequence: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	bundle, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	color := colorDefinitions(bundle)["blue"]
	if color.X != 0.311 || color.Y != 0.209 {
		t.Fatalf("color XY = %.12f, %.12f, want three-digit values", color.X, color.Y)
	}
	step := colorSequences(bundle)["blue"].Steps[0]
	if step.XY[0] != 0.311 || step.XY[1] != 0.209 {
		t.Fatalf("sequence XY = %.12f, %.12f, want three-digit values", step.XY[0], step.XY[1])
	}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"colors.yaml", "scripts.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, filename))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "0.3107010688491798") || strings.Contains(string(data), "0.2086338447") {
			t.Fatalf("%s retained over-precise XY values:\n%s", filename, data)
		}
	}
}

func TestAutomationYAMLOrdersIdentityKeysFirst(t *testing.T) {
	data, err := marshalConfigYAML(Automations, []any{map[string]any{
		"action":      []any{map[string]any{"service": "light.turn_on"}},
		"description": "Holiday lights",
		"alias":       "Holiday",
		"id":          "holiday",
		"mode":        "single",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "- id: holiday\n  alias: Holiday\n  description: Holiday lights\n"
	if !strings.HasPrefix(string(data), want) {
		t.Fatalf("automation YAML =\n%s\nwant prefix =\n%s", data, want)
	}
	if !strings.Contains(string(data), "  action:\n") || !strings.Contains(string(data), "  mode: single\n") {
		t.Fatalf("automation YAML dropped fields:\n%s", data)
	}
}
