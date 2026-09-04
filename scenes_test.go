package main

import "testing"

func TestUpsertSceneAndFindReferences(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{
		Scenes:  {Kind: Scenes, Data: []any{}},
		Scripts: {Kind: Scripts, Data: map[string]any{"holiday_lights": map[string]any{"sequence": []any{"scene.halloween_orange"}}}},
	}}
	updated, err := UpsertScene(bundle, map[string]any{"id": "halloween_orange", "name": "Halloween orange", "entities": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Files[Scenes].Data.([]any)) != 1 {
		t.Fatal("scene was not added")
	}
	refs := AffectedReferences(updated, "halloween_orange")
	if len(refs) != 1 || refs[0].ID != "holiday_lights" {
		t.Fatalf("references = %#v", refs)
	}
}

func TestUpsertHolidayColorPreservesOrderAndAvoidsDuplicates(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{Scripts: {Kind: Scripts, Data: map[string]any{
		"holiday_lights": map[string]any{"variables": map[string]any{"holiday_colors": map[string]any{"Halloween": []any{"halloween_orange"}}}},
	}}}}
	bundle, _ = withHash(bundle)
	updated, err := UpsertHolidayColor(bundle, "holiday_lights", "Halloween", "halloween_purple")
	if err != nil {
		t.Fatal(err)
	}
	colors := updated.Files[Scripts].Data.(map[string]any)["holiday_lights"].(map[string]any)["variables"].(map[string]any)["holiday_colors"].(map[string]any)["Halloween"].([]any)
	if len(colors) != 2 || colors[1] != "halloween_purple" {
		t.Fatalf("colors = %#v", colors)
	}
	unchanged, err := UpsertHolidayColor(updated, "holiday_lights", "Halloween", "halloween_purple")
	if err != nil || unchanged.Hash != updated.Hash {
		t.Fatalf("duplicate changed bundle: err=%v hash=%s/%s", err, unchanged.Hash, updated.Hash)
	}
}

func TestSceneInputPreservesEditableFields(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{map[string]any{
		"id": "halloween_orange", "name": "Halloween orange", "entities": map[string]any{
			"light.one": map[string]any{"rgb_color": []any{255, 80, 0}, "brightness": 180},
			"light.two": map[string]any{"rgb_color": []any{255, 80, 0}, "brightness": 180},
		},
	}}}}}
	input := SceneInput(bundle, "halloween_orange")
	if input != "halloween_orange,Halloween,orange,light.one;light.two,255,80,0,180" {
		t.Fatalf("input = %q", input)
	}
}
