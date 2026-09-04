package main

import (
	"strings"
	"testing"
)

func TestScheduleCompilesRelativeWindow(t *testing.T) {
	automation, err := (Schedule{ID: "halloween_schedule", Name: "Halloween", Selector: "Halloween", AnchorMonth: 10, AnchorDay: 31, StartDays: -16, EndDays: 2}).Automation()
	if err != nil {
		t.Fatal(err)
	}
	trigger := automation["triggers"].([]any)[0].(map[string]any)["value_template"].(string)
	if !strings.Contains(trigger, "timedelta(days=-16)") || !strings.Contains(trigger, "timedelta(days=2)") {
		t.Fatalf("unexpected template: %s", trigger)
	}
}

func TestScheduleRejectsInvalidAnchor(t *testing.T) {
	_, err := (Schedule{ID: "bad", Name: "Bad", Selector: "Other", AnchorMonth: 13, AnchorDay: 1}).Automation()
	if err == nil {
		t.Fatal("expected invalid anchor")
	}
}

func TestScheduleCompilesFixedWindow(t *testing.T) {
	automation, err := (Schedule{ID: "fixed", Name: "Fixed", Selector: "Halloween", Mode: "fixed", AnchorMonth: 10, AnchorDay: 31, StartDate: "10-15", EndDate: "11-02"}).Automation()
	if err != nil {
		t.Fatal(err)
	}
	trigger := automation["triggers"].([]any)[0].(map[string]any)["value_template"].(string)
	if !strings.Contains(trigger, "month=10") || !strings.Contains(trigger, "day=15") || !strings.Contains(trigger, "day=2") {
		t.Fatalf("unexpected fixed template: %s", trigger)
	}
}

func TestScheduleCompilesCrossYearFixedWindow(t *testing.T) {
	automation, err := (Schedule{ID: "winter", Name: "Winter", Selector: "Christmas", Mode: "fixed", AnchorMonth: 12, AnchorDay: 25, StartDate: "11-15", EndDate: "01-15"}).Automation()
	if err != nil {
		t.Fatal(err)
	}
	trigger := automation["triggers"].([]any)[0].(map[string]any)["value_template"].(string)
	if !strings.Contains(trigger, " or ") {
		t.Fatalf("expected cross-year OR template: %s", trigger)
	}
}
