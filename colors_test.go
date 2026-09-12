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

func TestColorCatalogIsDraftOnly(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{Colors: {Kind: Colors, Data: map[string]any{
		"orange": map[string]any{"name": "Orange", "x": .6, "y": .3},
	}}}}
	if _, ok := materializeNativeBundle(bundle).Files[Colors]; ok {
		t.Fatal("color catalog leaked into native bundle")
	}
}
