package main

import "testing"

func TestLightCapabilityRejectsUnsupportedEffect(t *testing.T) {
	err := (LightCapability{EntityID: "light.test", State: "off", SupportedColorMode: []string{"rgb"}, Effects: []string{"Solid"}}).Validate(map[string]any{"effect": "Rainbow"})
	if err == nil {
		t.Fatal("expected unsupported effect")
	}
}

func TestLightCapabilityAcceptsRGB(t *testing.T) {
	err := (LightCapability{EntityID: "light.test", State: "off", SupportedColorMode: []string{"rgb"}}).Validate(map[string]any{"rgb_color": []any{255, 80, 0}, "brightness": 180})
	if err != nil {
		t.Fatal(err)
	}
}
