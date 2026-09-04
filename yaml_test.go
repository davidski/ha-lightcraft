package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBundleCanonicalizesMapOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scenes.yaml"), []byte("b: 2\na: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes.yaml"), []byte("a: 1\nb: 2\n"), 0o600); err != nil {
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

func TestDiffDetectsChangedConfig(t *testing.T) {
	old := Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: map[string]any{"x": 1}}}}
	next := Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: map[string]any{"x": 2}}}}
	changes, err := Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != Scenes {
		t.Fatalf("unexpected changes: %#v", changes)
	}
}

func TestDraftPreservesSourceFingerprint(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{SourceHash: "full-ha-hash", Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{}}}}
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
