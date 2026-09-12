package main

import (
	"context"
	"testing"
)

func legacyBundleForTest() (Bundle, Bundle) {
	legacyScript := map[string]any{
		"sequence":  []any{},
		"variables": map[string]any{"holiday_colors": map[string]any{"Christmas": []any{"red", "white"}}},
	}
	source := Bundle{Files: map[ConfigKind]Config{
		Scripts: {Kind: Scripts, Data: map[string]any{holidayScriptID: legacyScript}},
		Scenes: {Kind: Scenes, Data: []any{
			map[string]any{"id": "red", "name": "Red", "entities": map[string]any{"light.one": map[string]any{"state": "on", "rgb_color": []any{255, 0, 0}, "brightness": 180}}},
			map[string]any{"id": "white", "name": "White", "entities": map[string]any{"light.one": map[string]any{"state": "on", "rgb_color": []any{255, 255, 255}, "brightness": 180}}},
		}},
	}}
	modern := Bundle{Files: map[ConfigKind]Config{
		Scripts:     {Kind: Scripts, Data: map[string]any{holidayScriptID: legacyScript}},
		Automations: {Kind: Automations, Data: []any{}},
		Helpers:     {Kind: Helpers, Data: map[string]any{}},
	}}
	return modern, source
}

func TestUpgradeLegacyInfrastructureCreatesModernSequence(t *testing.T) {
	modern, source := legacyBundleForTest()
	upgraded, err := upgradeLegacyInfrastructure(modern, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := upgraded.Files[Scenes]; ok {
		t.Fatal("legacy scenes leaked into modern draft")
	}
	if _, ok := upgraded.Files[Scripts].Data.(map[string]any)[holidayScriptID+"_legacy_backup"]; ok {
		t.Fatal("legacy script backup should not be added")
	}
	sequences := colorSequences(upgraded)
	if len(sequences) != 1 || len(sequences["christmas"].Steps) != 2 {
		t.Fatalf("sequences = %#v", sequences)
	}
	colors := colorDefinitions(upgraded)
	if len(colors) != 2 || colors["red"].X <= 0 || colors["white"].Y <= 0 {
		t.Fatalf("colors = %#v", colors)
	}
}

func TestUpgradeLegacyInfrastructureDeduplicatesNormalizedColors(t *testing.T) {
	modern, source := legacyBundleForTest()
	script := source.Files[Scripts].Data.(map[string]any)[holidayScriptID].(map[string]any)
	variables := script["variables"].(map[string]any)
	colors := variables["holiday_colors"].(map[string]any)
	colors["Christmas"] = []any{"red", "white", "white"}

	upgraded, err := upgradeLegacyInfrastructure(modern, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(colorDefinitions(upgraded)) != 2 {
		t.Fatalf("colors = %#v", colorDefinitions(upgraded))
	}
	if len(colorSequences(upgraded)["christmas"].Steps) != 3 {
		t.Fatalf("sequence steps = %#v", colorSequences(upgraded)["christmas"].Steps)
	}
}

func TestNativeYAMLReadLegacyKeepsScenesOutOfModernImport(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "scripts.yaml", "holiday_lights:\n  sequence: []\n  variables:\n    holiday_colors:\n      Christmas: [red]\n")
	writeNative(t, dir, "scenes.yaml", "- id: red\n  name: Red\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: ".", FilePaths: map[ConfigKind][]string{Scenes: {"scenes.yaml"}}}
	modern, err := store.Import(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := modern.Files[Scenes]; ok {
		t.Fatal("modern import included legacy scenes")
	}
	legacy, err := store.ReadLegacy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := legacy.Files[Scenes]; !ok {
		t.Fatal("legacy read did not include scenes")
	}
}

func TestImportedBundleAutomaticallyUpgradesLegacyLighting(t *testing.T) {
	remote := t.TempDir()
	writeNative(t, remote, "scripts.yaml", "holiday_lights:\n  sequence: []\n  variables:\n    holiday_colors:\n      Christmas: [red]\n")
	writeNative(t, remote, "scenes.yaml", "- id: red\n  name: Red\n  entities:\n    light.one:\n      state: on\n      rgb_color: [255, 0, 0]\n      brightness: 180\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: remote}, ConfigDir: ".", FilePaths: map[ConfigKind][]string{Scenes: {"scenes.yaml"}, Scripts: {"scripts.yaml"}}}
	full, err := store.Pull(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := importedBundleWithLegacyUpgrade(context.Background(), store, full, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := upgraded.Files[Scenes]; ok {
		t.Fatal("legacy scenes leaked into automatically upgraded draft")
	}
	if _, ok := upgraded.Files[Scripts].Data.(map[string]any)[holidayScriptID+"_legacy_backup"]; ok {
		t.Fatal("legacy script backup should not be added")
	}
	if _, ok := colorSequences(upgraded)["christmas"]; !ok {
		t.Fatal("automatic upgrade did not create a color sequence")
	}
	if upgraded.SourceHash != full.Hash {
		t.Fatalf("source hash = %q, want %q", upgraded.SourceHash, full.Hash)
	}
}
