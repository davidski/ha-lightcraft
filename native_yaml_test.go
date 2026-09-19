package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeYAMLImportSelectsOnlyRequestedEntries(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "automations.yaml", "- id: keep\n  name: Keep\n  entities: {}\n- id: other\n  name: Other\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	bundle, err := store.Import(context.Background(), []ConfigRef{{Kind: Automations, ID: "keep"}})
	if err != nil {
		t.Fatal(err)
	}
	automations := bundle.Files[Automations].Data.([]any)
	if len(automations) != 1 || automations[0].(map[string]any)["id"] != "keep" {
		t.Fatalf("bundle = %#v", bundle)
	}
}

func TestSSHFileTransportReadFileIgnoresStderr(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nprintf '%s\\n' 'Warning: Permanently added host' >&2\nprintf '%s\\n' 'automation: []'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	data, err := (SSHFileTransport{Host: "host"}).ReadFile(context.Background(), "/config/ha_lightcraft.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "automation: []\n" {
		t.Fatalf("data = %q", data)
	}
}

func TestSSHFileTransportReadFileMapsRemoteMissingFile(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nexit 44\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := (SSHFileTransport{Host: "host"}).ReadFile(context.Background(), "/config/missing.yaml")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestSSHFileTransportReadFileDoesNotMaskLocalSSHError(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nprintf '%s\\n' 'Warning: Identity file $/run/secrets/key not accessible: No such file or directory' >&2\nexit 255\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := (SSHFileTransport{Host: "host"}).ReadFile(context.Background(), "/config/missing.yaml")
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, local SSH failure was misclassified as missing file", err)
	}
	if !strings.Contains(err.Error(), "Identity file") {
		t.Fatalf("err = %v, missing SSH diagnostic", err)
	}
}

func TestNativeYAMLImportWithoutRefsSelectsAllEntries(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "automations.yaml", "- id: first\n  name: First\n  entities: {}\n- id: second\n  name: Second\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	bundle, err := store.Import(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	automations := bundle.Files[Automations].Data.([]any)
	if len(automations) != 2 || automations[0].(map[string]any)["id"] != "first" || bundle.SourceHash != bundle.Hash {
		t.Fatalf("bundle = %#v", bundle)
	}
}

func TestNativeYAMLPackageImportAndPublish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "packages", "ha_lightcraft.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("automation:\n  - id: holiday\n    alias: Holiday\ninput_select:\n  holiday:\n    name: Holiday\nscript:\n  holiday:\n    alias: Holiday\n    sequence: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := map[ConfigKind][]string{
		Scripts:     {"packages/ha_lightcraft.yaml"},
		Automations: {"packages/ha_lightcraft.yaml"},
		Helpers:     {"packages/ha_lightcraft.yaml"},
	}
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: ".", FilePaths: paths}
	refs := []ConfigRef{{Kind: Scripts, ID: "holiday"}, {Kind: Automations, ID: "holiday"}, {Kind: Helpers, ID: "holiday"}}
	imported, err := store.Import(context.Background(), refs)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := cloneBundle(imported)
	if err != nil {
		t.Fatal(err)
	}
	draft.Files[Scripts] = Config{Kind: Scripts, Data: map[string]any{"holiday": map[string]any{"alias": "Changed", "sequence": []any{}}}}
	draft, err = withHash(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(context.Background(), draft, imported, refs, nil, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, key := range []string{"automation:", "input_select:", "script:", "alias: Changed"} {
		if !strings.Contains(text, key) {
			t.Fatalf("package missing %q: %s", key, text)
		}
	}
	if strings.Contains(text, "homeassistant:") || strings.Contains(text, "package:") {
		t.Fatalf("package has wrapper: %s", text)
	}
}

func TestNativeYAMLPackageRejectsWrapper(t *testing.T) {
	dir := t.TempDir()
	writeNestedNative(t, dir, "packages/ha_lightcraft.yaml", "homeassistant:\n  packages: {}\n")
	paths := map[ConfigKind][]string{
		Scripts:     {"packages/ha_lightcraft.yaml"},
		Automations: {"packages/ha_lightcraft.yaml"},
		Helpers:     {"packages/ha_lightcraft.yaml"},
	}
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: ".", FilePaths: paths}
	if _, err := store.Import(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "only automation") {
		t.Fatalf("err = %v", err)
	}
}

func TestNativeYAMLPullCopiesConfiguredFilesAndLoadsTheSourceBaseline(t *testing.T) {
	remote := t.TempDir()
	source := t.TempDir()
	paths := map[ConfigKind][]string{
		Scenes:      {"scenes/holiday.yaml"},
		Scripts:     {"scripts/holiday.yaml"},
		Automations: {"automations/holiday.yaml"},
	}
	script := "holiday:\n  alias: Holiday\n  sequence: []\n"
	automation := "- id: holiday\n  alias: Holiday\n  trigger: {}\n"
	writeNestedNative(t, remote, paths[Scenes][0], "- id: legacy\n")
	writeNestedNative(t, remote, paths[Scripts][0], script)
	writeNestedNative(t, remote, paths[Automations][0], automation)
	writeNestedNative(t, source, paths[Scenes][0], "- id: stale-legacy\n")
	writeNestedNative(t, source, paths[Scripts][0], "stale: true\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: remote}, ConfigDir: ".", FilePaths: paths}
	pulled, err := store.Pull(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(source, paths[Scripts][0]))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != script {
		t.Fatalf("pulled script = %q, want %q", data, script)
	}
	if _, err := os.Stat(filepath.Join(source, paths[Scenes][0])); !os.IsNotExist(err) {
		t.Fatalf("legacy scenes were pulled: %v", err)
	}
	baseline, err := loadBaselineBundle(source, paths)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Hash != pulled.Hash || len(bundleEntries(baseline)) != 2 {
		t.Fatalf("baseline = %#v, pulled = %#v", baseline, pulled)
	}
}

func TestNativeYAMLPullFlattensPackageDraftPath(t *testing.T) {
	remote := t.TempDir()
	source := t.TempDir()
	packagePath := "packages/ha_lightcraft.yaml"
	data := "automation:\n  - id: holiday\ninput_select: {}\nscript: {}\n"
	writeNestedNative(t, remote, packagePath, data)
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: remote}, ConfigDir: ".", FilePaths: map[ConfigKind][]string{
		Scripts: {packagePath}, Automations: {packagePath}, Helpers: {packagePath},
	}}
	if _, err := store.Pull(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(source, "ha_lightcraft.yaml")); err != nil || string(got) != data {
		t.Fatalf("flat package = %q, err = %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(source, packagePath)); !os.IsNotExist(err) {
		t.Fatalf("nested package source exists: %v", err)
	}
}

func TestNativeYAMLPullReportsMissingRemotePackagePath(t *testing.T) {
	remote := t.TempDir()
	packagePath := "packages/ha_lightcraft.yaml"
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: remote}, ConfigDir: "ha-config", FilePaths: map[ConfigKind][]string{
		Scripts: {packagePath}, Automations: {packagePath}, Helpers: {packagePath},
	}}
	_, err := store.Pull(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no native HA YAML files found at ha-config/packages/ha_lightcraft.yaml") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportDraftCreatesColorsFromImportedSequences(t *testing.T) {
	remote := t.TempDir()
	proposed := t.TempDir()
	current := t.TempDir()
	colorsDir := t.TempDir()
	paths := map[ConfigKind][]string{
		Scripts:     {"packages/ha_lightcraft.yaml"},
		Automations: {"packages/ha_lightcraft.yaml"},
		Helpers:     {"packages/ha_lightcraft.yaml"},
	}
	writeNestedNative(t, remote, "packages/ha_lightcraft.yaml", `automation: []
input_select: {}
script:
  lighting_sequence_christmas:
    alias: Christmas
    variables:
      sequence_data:
        repeat: true
        steps:
          - name: Red
            xy: [0.64, 0.33]
            brightness: 255
            hold: 6
            transition: 0.5
          - name: Red
            xy: [0.64, 0.33]
            brightness: 200
            hold: 3
            transition: 0
`)
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: remote}, ConfigDir: ".", FilePaths: paths}
	bundle, _, _, err := importDraft(context.Background(), store, proposed, current, colorsDir, paths)
	if err != nil {
		t.Fatal(err)
	}
	colors := colorDefinitions(bundle)
	if len(colors) != 1 || colors["red"].Name != "Red" || colors["red"].X != .64 || colors["red"].Y != .33 {
		t.Fatalf("colors = %#v", colors)
	}
	if _, err := os.Stat(filepath.Join(colorsDir, "colors.yaml")); err != nil {
		t.Fatalf("colors.yaml was not created: %v", err)
	}
}

func TestMergeImportedColorsOverridesLocalCatalog(t *testing.T) {
	local := Bundle{Files: map[ConfigKind]Config{Colors: {Kind: Colors, Data: map[string]any{
		"red":  map[string]any{"name": "Red", "x": .1, "y": .1},
		"blue": map[string]any{"name": "Blue", "x": .15, "y": .06},
	}}}}
	imported, err := saveColorSequence(Bundle{}, ColorSequence{Name: "Christmas", ID: "christmas", Steps: []ColorStep{
		{Name: "Red", XY: []float64{.64, .33}, Brightness: 255, Hold: 6},
	}})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := mergeImportedColorCatalog(local, imported)
	if err != nil {
		t.Fatal(err)
	}
	colors := colorDefinitions(merged)
	if len(colors) != 2 || colors["red"].X != .64 || colors["red"].Y != .33 || colors["blue"].X != .15 {
		t.Fatalf("colors = %#v", colors)
	}
}

func TestNativeYAMLPublishPreservesUnrelatedEntries(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "automations.yaml", "- id: keep\n  name: Old\n  entities: {}\n- id: other\n  name: Preserve\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	ref := ConfigRef{Kind: Automations, ID: "keep"}
	imported, err := store.Import(context.Background(), []ConfigRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	draft := Bundle{Files: map[ConfigKind]Config{Automations: {Kind: Automations, Data: []any{map[string]any{"id": "keep", "name": "New", "entities": map[string]any{}}}}}}
	draft, _ = withHash(draft)
	if err := store.Publish(context.Background(), draft, imported, []ConfigRef{ref}, nil, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "automations.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: New") || !strings.Contains(string(data), "name: Preserve") {
		t.Fatalf("published YAML = %s", data)
	}
}

func TestNativeYAMLPublishRejectsUnrelatedLiveChange(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "automations.yaml", "- id: keep\n  name: Old\n  entities: {}\n- id: other\n  name: Preserve\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	ref := ConfigRef{Kind: Automations, ID: "keep"}
	imported, err := store.Import(context.Background(), []ConfigRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	writeNative(t, dir, "automations.yaml", "- id: keep\n  name: Old\n  entities: {}\n- id: other\n  name: Changed\n  entities: {}\n")
	draft := Bundle{Files: map[ConfigKind]Config{Automations: {Kind: Automations, Data: []any{map[string]any{"id": "keep", "name": "New", "entities": map[string]any{}}}}}}
	draft, _ = withHash(draft)
	if err := store.Publish(context.Background(), draft, imported, []ConfigRef{ref}, nil, ""); !errors.Is(err, ErrStaleConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestNativeYAMLPublishRollsBackRawFilesOnCheckFailure(t *testing.T) {
	dir := t.TempDir()
	original := "- id: keep\n  name: Old\n  entities: {}\n"
	writeNative(t, dir, "automations.yaml", original)
	checks := 0
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: ".", Check: func(context.Context) error {
		checks++
		if checks == 2 {
			return errors.New("invalid configuration")
		}
		return nil
	}}
	ref := ConfigRef{Kind: Automations, ID: "keep"}
	imported, err := store.Import(context.Background(), []ConfigRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	draft := Bundle{Files: map[ConfigKind]Config{Automations: {Kind: Automations, Data: []any{map[string]any{"id": "keep", "name": "New", "entities": map[string]any{}}}}}}
	draft, _ = withHash(draft)
	if err := store.Publish(context.Background(), draft, imported, []ConfigRef{ref}, nil, ""); err == nil || !strings.Contains(err.Error(), "rollback complete") {
		t.Fatalf("err = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "automations.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("rollback changed raw YAML: %q", data)
	}
}

func TestNativeYAMLPublishDeletesApprovedEntry(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "automations.yaml", "- id: keep\n  name: Keep\n  entities: {}\n- id: remove\n  name: Remove\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	refs := []ConfigRef{{Kind: Automations, ID: "keep"}, {Kind: Automations, ID: "remove"}}
	imported, err := store.Import(context.Background(), refs)
	if err != nil {
		t.Fatal(err)
	}
	draft := Bundle{Files: map[ConfigKind]Config{Automations: {Kind: Automations, Data: []any{map[string]any{"id": "keep", "name": "Keep", "entities": map[string]any{}}}}}}
	draft, _ = withHash(draft)
	if err := store.Publish(context.Background(), draft, imported, refs, []ConfigRef{{Kind: Automations, ID: "remove"}}, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "automations.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "id: remove") {
		t.Fatalf("deleted automation remains: %s", data)
	}
}

func writeNative(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeNestedNative(t *testing.T, dir, name, data string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeNative(t, dir, name, data)
}
