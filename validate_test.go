package main

import "testing"

func TestValidateBundleRejectsMalformedScene(t *testing.T) {
	err := ValidateBundle(Bundle{Files: map[ConfigKind]Config{
		Scenes: {Kind: Scenes, Data: []any{map[string]any{"name": "x"}}},
	}})
	if err == nil {
		t.Fatal("expected missing entities error")
	}
}

func TestValidateBundleAcceptsNativeShapes(t *testing.T) {
	err := ValidateBundle(Bundle{Files: map[ConfigKind]Config{
		Scenes:      {Kind: Scenes, Data: []any{map[string]any{"id": "x", "name": "X", "entities": map[string]any{}}}},
		Scripts:     {Kind: Scripts, Data: map[string]any{"x": map[string]any{"sequence": []any{}}}},
		Automations: {Kind: Automations, Data: []any{map[string]any{"id": "x"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateBundleRejectsMalformedSceneEntities(t *testing.T) {
	err := ValidateBundle(Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{map[string]any{
		"id": "x", "name": "X", "entities": []any{"light.one"},
	}}}}})
	if err == nil {
		t.Fatal("expected malformed entities error")
	}
}
