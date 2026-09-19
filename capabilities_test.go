package main

import "testing"

func TestLightIsGroupUsesHAAndZigbee2MQTTAttributes(t *testing.T) {
	if !lightIsGroup(LightState{Attribute: map[string]any{"group_entities": []any{"light.one"}}}) {
		t.Fatal("Zigbee2MQTT group not detected")
	}
	if !lightIsGroup(LightState{Attribute: map[string]any{"entity_id": []any{"light.one"}}}) {
		t.Fatal("Home Assistant group not detected")
	}
	if lightIsGroup(LightState{Attribute: map[string]any{"group_entities": []any{}}}) {
		t.Fatal("empty group reported as group")
	}
}

func TestColorLightIDsFilterGroupsWithNonColorMembers(t *testing.T) {
	states := map[string]LightState{
		"light.good":   {Attribute: map[string]any{"supported_color_modes": []any{"xy"}}},
		"light.bad":    {Attribute: map[string]any{"supported_color_modes": []any{"onoff"}}},
		"light.group":  {Attribute: map[string]any{"supported_color_modes": []any{"rgb"}, "entity_id": []any{"light.good", "light.bad"}}},
		"light.nested": {Attribute: map[string]any{"group_entities": []any{"light.good"}}},
	}
	ids := colorLightIDs(states, nil)
	if stringMustJSON(ids) != stringMustJSON([]string{"light.good", "light.nested"}) {
		t.Fatalf("color light IDs = %v", ids)
	}
}

func TestWLEDInventoryFiltersByRegistryRelationship(t *testing.T) {
	states := map[string]LightState{
		"light.floating":              {EntityID: "light.floating", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}}},
		"light.workshop":              {EntityID: "light.workshop", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}}},
		"light.hue_wled":              {EntityID: "light.hue_wled", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}, "effect": "WLED Rainbow"}},
		"select.floating_preset":      {EntityID: "select.floating_preset", Attribute: map[string]any{"options": []any{"Christmas"}}},
		"select.floating_playlist":    {EntityID: "select.floating_playlist", Attribute: map[string]any{"options": []any{"Winter"}}},
		"select.floating_palette":     {EntityID: "select.floating_palette", Attribute: map[string]any{"options": []any{"Red"}}},
		"select.floating_live":        {EntityID: "select.floating_live", Attribute: map[string]any{"options": []any{"Live"}}},
		"select.workshop_preset":      {EntityID: "select.workshop_preset", Attribute: map[string]any{"options": []any{"Workshop"}}},
		"select.hue_preset":           {EntityID: "select.hue_preset", Attribute: map[string]any{"options": []any{"Hue"}}},
		"select.floating_wrong_entry": {EntityID: "select.floating_wrong_entry", Attribute: map[string]any{"options": []any{"Wrong"}}},
	}
	metadata := map[string]HAEntityMetadata{
		"light.floating":              {EntityID: "light.floating", Platform: "wled", DeviceID: "device-1", ConfigEntryID: "entry-1"},
		"light.workshop":              {EntityID: "light.workshop", Platform: "wled", DeviceID: "device-2", ConfigEntryID: "entry-2"},
		"light.hue_wled":              {EntityID: "light.hue_wled", Platform: "hue", DeviceID: "hue-1", ConfigEntryID: "hue-entry"},
		"select.floating_preset":      {EntityID: "select.floating_preset", Platform: "wled", DeviceID: "device-1", ConfigEntryID: "entry-1", TranslationKey: "preset"},
		"select.floating_playlist":    {EntityID: "select.floating_playlist", Platform: "wled", DeviceID: "device-1", ConfigEntryID: "entry-1", TranslationKey: "playlist"},
		"select.floating_palette":     {EntityID: "select.floating_palette", Platform: "wled", DeviceID: "device-1", ConfigEntryID: "entry-1", TranslationKey: "palette"},
		"select.floating_live":        {EntityID: "select.floating_live", Platform: "wled", DeviceID: "device-1", ConfigEntryID: "entry-1", TranslationKey: "live_override"},
		"select.workshop_preset":      {EntityID: "select.workshop_preset", Platform: "wled", DeviceID: "device-2", ConfigEntryID: "entry-2", TranslationKey: "preset"},
		"select.hue_preset":           {EntityID: "select.hue_preset", Platform: "hue", DeviceID: "device-1", ConfigEntryID: "entry-1", TranslationKey: "preset"},
		"select.floating_wrong_entry": {EntityID: "select.floating_wrong_entry", Platform: "wled", DeviceID: "device-1", ConfigEntryID: "entry-2", TranslationKey: "preset"},
	}
	if got := wledLightIDs(states, metadata); stringMustJSON(got) != stringMustJSON([]string{"light.floating", "light.workshop"}) {
		t.Fatalf("WLED lights = %#v", got)
	}
	if got := wledSelectorIDs("light.floating", states, metadata); stringMustJSON(got) != stringMustJSON([]string{"select.floating_playlist", "select.floating_preset"}) {
		t.Fatalf("floating selectors = %#v", got)
	}
	if got := wledSelectorIDs("light.workshop", states, metadata); stringMustJSON(got) != stringMustJSON([]string{"select.workshop_preset"}) {
		t.Fatalf("workshop selectors = %#v", got)
	}
	for _, program := range []WLEDProgram{
		{Light: "light.floating", Select: "select.floating_preset", Option: "Christmas"},
		{Light: "light.floating", Select: "select.floating_playlist", Option: "Winter"},
	} {
		if err := validateWLEDProgram(program, states, metadata); err != nil {
			t.Fatalf("valid WLED program rejected: %v", err)
		}
	}
	for _, program := range []WLEDProgram{
		{Light: "light.hue_wled", Select: "select.floating_preset", Option: "Christmas"},
		{Light: "light.floating", Select: "select.workshop_preset", Option: "Workshop"},
		{Light: "light.floating", Select: "select.floating_palette", Option: "Red"},
		{Light: "light.floating", Select: "select.floating_live", Option: "Live"},
		{Light: "light.floating", Select: "select.floating_wrong_entry", Option: "Wrong"},
		{Light: "light.floating", Select: "select.floating_preset", Option: "Missing"},
	} {
		if err := validateWLEDProgram(program, states, metadata); err == nil {
			t.Fatalf("invalid WLED program accepted: %#v", program)
		}
	}
}

func TestWLEDPickerRequiresMetadata(t *testing.T) {
	states := map[string]LightState{
		"light.imported":  {EntityID: "light.imported", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}}},
		"select.imported": {EntityID: "select.imported", Attribute: map[string]any{"options": []any{"Saved"}}},
	}
	if got := wledLightIDs(states, nil); len(got) != 0 {
		t.Fatalf("offline WLED light picker broadened to %#v", got)
	}
	if got := wledSelectorIDs("light.imported", states, nil); len(got) != 0 {
		t.Fatalf("offline WLED selector picker broadened to %#v", got)
	}
	if err := (LightingAssignment{ID: "imported", Name: "Imported", Start: "11-01", End: "11-30", On: "sunset", Off: "00:00", Finish: "off", WLED: &WLEDProgram{Light: "light.imported", Select: "select.imported", Option: "Saved"}}).Validate(); err != nil {
		t.Fatalf("offline imported WLED reference failed structural validation: %v", err)
	}
}
