package main

import "testing"

func TestParseEntityRegistryRetainsWLEDMetadata(t *testing.T) {
	metadata, areas := parseEntityRegistry(map[string]any{"result": []any{
		map[string]any{
			"entity_id": "light.floating", "platform": "wled", "device_id": "device-1",
			"config_entry_id": "entry-1", "translation_key": "state", "area_id": "area-1",
		},
		map[string]any{
			"entity_id": "select.floating_playlist", "platform": "wled", "device_id": "device-1",
			"config_entry_id": "entry-1", "translation_key": "playlist",
		},
	}})
	if got := metadata["light.floating"]; got.Platform != "wled" || got.DeviceID != "device-1" || got.ConfigEntryID != "entry-1" || got.TranslationKey != "state" {
		t.Fatalf("light metadata was not retained: %#v", got)
	}
	if got := metadata["select.floating_playlist"]; got.TranslationKey != "playlist" || got.DeviceID != "device-1" {
		t.Fatalf("selector metadata was not retained: %#v", got)
	}
	if areas["light.floating"] != "area-1" {
		t.Fatalf("entity area = %q, want area-1", areas["light.floating"])
	}
}
