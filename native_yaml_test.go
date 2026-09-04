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
	writeNative(t, dir, "scenes.yaml", "- id: keep\n  name: Keep\n  entities: {}\n- id: other\n  name: Other\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	bundle, err := store.Import(context.Background(), []ConfigRef{{Kind: Scenes, ID: "keep"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Files[Scenes].Data.([]any)) != 1 || SceneIDs(bundle)[0] != "keep" {
		t.Fatalf("bundle = %#v", bundle)
	}
}

func TestNativeYAMLPublishPreservesUnrelatedEntries(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "scenes.yaml", "- id: keep\n  name: Old\n  entities: {}\n- id: other\n  name: Preserve\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	ref := ConfigRef{Kind: Scenes, ID: "keep"}
	imported, err := store.Import(context.Background(), []ConfigRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	draft := Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{map[string]any{"id": "keep", "name": "New", "entities": map[string]any{}}}}}}
	draft, _ = withHash(draft)
	if err := store.Publish(context.Background(), draft, imported, []ConfigRef{ref}, nil, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "scenes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: New") || !strings.Contains(string(data), "name: Preserve") {
		t.Fatalf("published YAML = %s", data)
	}
}

func TestNativeYAMLPublishRejectsUnrelatedLiveChange(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "scenes.yaml", "- id: keep\n  name: Old\n  entities: {}\n- id: other\n  name: Preserve\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	ref := ConfigRef{Kind: Scenes, ID: "keep"}
	imported, err := store.Import(context.Background(), []ConfigRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	writeNative(t, dir, "scenes.yaml", "- id: keep\n  name: Old\n  entities: {}\n- id: other\n  name: Changed\n  entities: {}\n")
	draft := Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{map[string]any{"id": "keep", "name": "New", "entities": map[string]any{}}}}}}
	draft, _ = withHash(draft)
	if err := store.Publish(context.Background(), draft, imported, []ConfigRef{ref}, nil, ""); !errors.Is(err, ErrStaleConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestNativeYAMLPublishRollsBackRawFilesOnCheckFailure(t *testing.T) {
	dir := t.TempDir()
	original := "- id: keep\n  name: Old\n  entities: {}\n"
	writeNative(t, dir, "scenes.yaml", original)
	checks := 0
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: ".", Check: func(context.Context) error {
		checks++
		if checks == 2 {
			return errors.New("invalid configuration")
		}
		return nil
	}}
	ref := ConfigRef{Kind: Scenes, ID: "keep"}
	imported, err := store.Import(context.Background(), []ConfigRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	draft := Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{map[string]any{"id": "keep", "name": "New", "entities": map[string]any{}}}}}}
	draft, _ = withHash(draft)
	if err := store.Publish(context.Background(), draft, imported, []ConfigRef{ref}, nil, ""); err == nil || !strings.Contains(err.Error(), "rollback complete") {
		t.Fatalf("err = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "scenes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("rollback changed raw YAML: %q", data)
	}
}

func TestNativeYAMLPublishDeletesApprovedEntry(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "scenes.yaml", "- id: keep\n  name: Keep\n  entities: {}\n- id: remove\n  name: Remove\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	refs := []ConfigRef{{Kind: Scenes, ID: "keep"}, {Kind: Scenes, ID: "remove"}}
	imported, err := store.Import(context.Background(), refs)
	if err != nil {
		t.Fatal(err)
	}
	draft := Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{map[string]any{"id": "keep", "name": "Keep", "entities": map[string]any{}}}}}}
	draft, _ = withHash(draft)
	if err := store.Publish(context.Background(), draft, imported, refs, []ConfigRef{{Kind: Scenes, ID: "remove"}}, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "scenes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "id: remove") {
		t.Fatalf("deleted scene remains: %s", data)
	}
}

func writeNative(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
