package main

import "testing"

import tea "github.com/charmbracelet/bubbletea"

func TestColorEditLeavesCatalogView(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{Colors: {Kind: Colors, Data: map[string]any{
		"orange": map[string]any{"name": "Orange", "x": .6, "y": .3},
	}}}}
	model := statusModel{bundle: bundle, colorView: true}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	result := updated.(statusModel)
	if result.colorView || result.prompt != "color" || result.pending != "orange" {
		t.Fatalf("edit did not enter color form: view=%v prompt=%q pending=%q", result.colorView, result.prompt, result.pending)
	}
}

func TestSceneColorSelectorKeepsFormControls(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{Colors: {Kind: Colors, Data: map[string]any{
		"orange": map[string]any{"name": "Orange", "x": .6, "y": .3},
	}}}}
	model := statusModel{bundle: bundle, prompt: "new", formValues: []string{"Halloween", "orange", "light.front", "255"}, formField: 1}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEscape})
	result := updated.(statusModel)
	if result.prompt != "" {
		t.Fatalf("escape was swallowed by color selector: prompt=%q", result.prompt)
	}
}

func TestImportedColorsAreDraftOnly(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{
		Scenes: {Kind: Scenes, Data: []any{map[string]any{
			"id": "halloween_orange", "name": "Halloween orange",
			"entities": map[string]any{"light.front": map[string]any{"xy_color": []any{.612, .374}}},
		}}},
	}}
	bundle = mergeImportedColors(bundle)
	if colorRefForScene(bundle, "halloween_orange") != "halloween_orange" {
		t.Fatal("import did not attach a color reference")
	}
	if _, ok := bundle.Files[Colors]; !ok {
		t.Fatal("import did not create color catalog")
	}
	native := materializeNativeBundle(bundle)
	if _, ok := native.Files[Colors]; ok {
		t.Fatal("color catalog leaked into native bundle")
	}
	if _, ok := native.Files[Scenes].Data.([]any)[0].(map[string]any)["color_ref"]; ok {
		t.Fatal("draft-only color reference leaked into native scene")
	}
}
