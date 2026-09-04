package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestXyYToSRGBUsesChromaticityWithoutRoundTrip(t *testing.T) {
	xyz, ok := xyYToXYZ(CIEColor{X: 0.3127, Y: 0.3290}, 1)
	if !ok || xyz[1] != 1 || xyz[0] < 0.94 || xyz[0] > 0.96 {
		t.Fatalf("xyz = %v, ok=%v", xyz, ok)
	}
	white, ok := xyYToSRGB(CIEColor{X: 0.3127, Y: 0.3290}, 1)
	if !ok || white[0] < 240 || white[1] < 240 || white[2] < 240 {
		t.Fatalf("white = %v, ok=%v", white, ok)
	}
	if _, ok := xyYToSRGB(CIEColor{X: 0.8, Y: 0.4}, 1); ok {
		t.Fatal("accepted invalid xy")
	}
}

func TestCIEPickerKeyboardMovementCommitsXY(t *testing.T) {
	m := statusModel{prompt: "color", formValues: []string{"Red", "0.3000", "0.3000"}, formField: 1, input: "0.3000"}
	model, _ := m.updateColorForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = model.(statusModel)
	if !m.ciePicker {
		t.Fatal("picker did not open")
	}
	model, _ = m.updateCIEPicker(tea.KeyMsg{Type: tea.KeyRight})
	m = model.(statusModel)
	if m.ciePickerX <= 0.3 || m.formValues[1] != "0.3000" {
		t.Fatalf("picker movement = %v, form x = %q", m.ciePickerX, m.formValues[1])
	}
	model, _ = m.updateCIEPicker(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(statusModel)
	if m.ciePicker || m.formValues[1] == "0.3000" {
		t.Fatalf("picker did not commit: picker=%v form=%v", m.ciePicker, m.formValues)
	}
}

func TestCIEPickerRendersPlaneCursorAndCoordinates(t *testing.T) {
	view := renderCIEPicker(CIEColor{X: 0.3127, Y: 0.3290})
	for _, want := range []string{"CIE xy Color", "0.3127", "0.3290", "▀", "●", "Preview"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker missing %q in %q", want, view)
		}
	}
}
