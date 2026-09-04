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
		Scenes: {Kind: Scenes, Data: map[string]any{"scene": true}},
	}}, time.Date(2026, 9, 4, 13, 14, 15, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "scenes.yaml")
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %v, want 0600", info.Mode().Perm())
	}
}
