package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveBackupWritesRestrictedYAML(t *testing.T) {
	dir := t.TempDir()
	path, err := SaveBackup(dir, Bundle{Files: map[ConfigKind]Config{
		Scripts: {Kind: Scripts, Data: map[string]any{"script": true}},
	}}, nil, time.Date(2026, 9, 4, 13, 14, 15, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "scripts.yaml")
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestSaveBackupWritesPackageFile(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{Files: map[ConfigKind]Config{
		Automations: {Kind: Automations, Data: []any{}},
		Helpers:     {Kind: Helpers, Data: map[string]any{}},
		Scripts:     {Kind: Scripts, Data: map[string]any{}},
	}}
	paths := map[ConfigKind][]string{
		Scripts: {"packages/ha_lightcraft.yaml"}, Automations: {"packages/ha_lightcraft.yaml"}, Helpers: {"packages/ha_lightcraft.yaml"},
	}
	path, err := SaveBackup(dir, bundle, paths, time.Date(2026, 9, 4, 13, 14, 15, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(path, "ha_lightcraft.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := marshalPackageYAML(bundle)
	if err != nil || string(data) != string(want) {
		t.Fatalf("package backup = %q, want %q; err = %v", data, want, err)
	}
	for _, filename := range []string{"automations.yaml", "input_select.yaml", "scripts.yaml"} {
		if _, err := os.Stat(filepath.Join(path, filename)); !os.IsNotExist(err) {
			t.Fatalf("standalone backup %s exists: %v", filename, err)
		}
	}
}
