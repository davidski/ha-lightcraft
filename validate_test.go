package main

import "testing"

func TestValidateBundleAcceptsNativeShapes(t *testing.T) {
	err := ValidateBundle(Bundle{Files: map[ConfigKind]Config{
		Scripts:     {Kind: Scripts, Data: map[string]any{"x": map[string]any{"sequence": []any{}}}},
		Automations: {Kind: Automations, Data: []any{map[string]any{"id": "x"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
}
