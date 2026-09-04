package main

import "testing"

func TestParseRefs(t *testing.T) {
	refs, err := parseRefs("scenes:halloween_orange,scripts:holiday_lights")
	if err != nil || len(refs) != 2 || refs[0].Kind != Scenes || refs[1].ID != "holiday_lights" {
		t.Fatalf("refs = %#v, err = %v", refs, err)
	}
}

func TestParseEmptyRefs(t *testing.T) {
	refs, err := parseRefs("")
	if err != nil || len(refs) != 0 {
		t.Fatalf("refs = %#v, err = %v", refs, err)
	}
}
