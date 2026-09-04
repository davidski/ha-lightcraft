package main

import "testing"

func TestSuggestSceneID(t *testing.T) {
	if got := SuggestSceneID("Halloween", "Orange"); got != "halloween_orange" {
		t.Fatalf("got %q", got)
	}
	if got := SuggestSceneID("", ""); got != "holiday_scene" {
		t.Fatalf("got %q", got)
	}
}

func TestSuggestedRGB(t *testing.T) {
	rgb, ok := SuggestedRGB("Halloween", "orange")
	if !ok || rgb != [3]int{255, 80, 0} {
		t.Fatalf("rgb=%v ok=%v", rgb, ok)
	}
}
