package main

import (
	"strings"
	"testing"
)

func sequenceBundle() Bundle {
	return Bundle{Files: map[ConfigKind]Config{
		Scenes: {Kind: Scenes, Data: []any{
			map[string]any{"id": "red", "name": "Red", "entities": map[string]any{}},
			map[string]any{"id": "white", "name": "White", "entities": map[string]any{}},
		}},
		Scripts: {Kind: Scripts, Data: map[string]any{
			holidayScriptID: map[string]any{
				"variables": map[string]any{"holiday_colors": map[string]any{
					"Christmas": []any{"red", "white", "red"},
				}},
			},
		}},
	}}
}

func TestEnsureHolidayInfrastructurePreservesOrderedSequences(t *testing.T) {
	got := ensureHolidayInfrastructure(sequenceBundle(), sequenceBundle())
	sequences := holidaySequences(got)["Christmas"]
	if strings.Join(sequences, ",") != "red,white,red" {
		t.Fatalf("sequence = %v", sequences)
	}
	script := got.Files[Scripts].Data.(map[string]any)[holidayScriptID].(map[string]any)
	if script["sequence"] == nil {
		t.Fatal("managed script has no runtime sequence")
	}
}

func TestReplaceHolidaySequencePreservesOrderAndDuplicates(t *testing.T) {
	got, err := replaceHolidaySequence(sequenceBundle(), "Christmas", []string{"white", "red", "white"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(holidaySequences(got)["Christmas"], ",") != "white,red,white" {
		t.Fatalf("sequence = %v", holidaySequences(got)["Christmas"])
	}
}

func TestValidateHolidaySequencesRejectsMissingScene(t *testing.T) {
	bundle := ensureHolidayInfrastructure(sequenceBundle(), sequenceBundle())
	sequences := holidaySequences(bundle)
	sequences["Christmas"] = []string{"missing"}
	updated, err := replaceHolidaySequence(bundle, "Christmas", sequences["Christmas"])
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBundle(updated); err == nil {
		t.Fatal("missing sequence scene was accepted")
	}
}
