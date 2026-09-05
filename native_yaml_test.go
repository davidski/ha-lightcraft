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

func TestNativeYAMLImportWithoutRefsSelectsAllEntries(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "scenes.yaml", "- id: first\n  name: First\n  entities: {}\n- id: second\n  name: Second\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	bundle, err := store.Import(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	scenes := bundle.Files[Scenes].Data.([]any)
	if len(scenes) != 2 || scenes[0].(map[string]any)["id"] != "first" || bundle.SourceHash != bundle.Hash {
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

func TestNativeYAMLPublishMatchesReviewedProjection(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "scenes.yaml", "- id: keep\n  name: Old\n  entities: {}\n")
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	imported, err := store.Import(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	draft := Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{map[string]any{
		"id": "keep", "name": "New", "entities": map[string]any{}, "color_ref": "orange",
	}}}}}
	draft, _ = withHash(draft)
	designerBaseline := imported
	designerBaseline.Files[Colors] = Config{Kind: Colors, Data: map[string]any{"orange": map[string]any{"name": "Orange", "x": .6, "y": .3}}}
	designerBaseline.Files[Scenes].Data.([]any)[0].(map[string]any)["color_ref"] = "orange"
	designerBaseline, _ = withHash(designerBaseline)
	changes, err := PublishDiff(designerBaseline, draft)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes=%#v err=%v", changes, err)
	}
	if strings.Contains(FormatChanges(changes), "color_ref") || changes[0].Kind != Scenes {
		t.Fatalf("publish diff contains designer metadata: %s", FormatChanges(changes))
	}
	if err := store.Publish(context.Background(), draft, designerBaseline, bundleRefs(imported), nil, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "scenes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "color_ref") {
		t.Fatalf("published YAML contains designer metadata: %s", data)
	}
	published, err := store.Import(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	publishedJSON, err := canonicalJSON(published.Files[Scenes].Data)
	if err != nil {
		t.Fatal(err)
	}
	if string(publishedJSON) != changes[0].New {
		t.Fatalf("published scenes differ from reviewed projection\npublished: %s\nreviewed: %s", publishedJSON, changes[0].New)
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
