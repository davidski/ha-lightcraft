package main

import (
	"fmt"
	"slices"
	"strings"
)

func supports(values []string, wanted ...string) bool {
	for _, value := range values {
		for _, candidate := range wanted {
			if value == candidate {
				return true
			}
		}
	}
	return false
}

func lightSupportsColor(state LightState) bool {
	modes := stringList(state.Attribute["supported_color_modes"])
	return len(modes) > 0 && supports(modes, "rgb", "rgbw", "rgbww", "hs", "xy")
}

func lightIsGroup(state LightState) bool {
	return len(lightGroupMembers(state)) > 0
}

func lightGroupMembers(state LightState) []string {
	for _, attribute := range []string{"group_entities", "entity_id"} {
		if members := stringList(state.Attribute[attribute]); len(members) > 0 {
			return members
		}
	}
	return nil
}

func lightTargetSupportsColor(id string, states map[string]LightState) bool {
	visiting := map[string]bool{}
	var supportsTarget func(string) bool
	supportsTarget = func(id string) bool {
		if visiting[id] {
			return false
		}
		state, ok := states[id]
		if !ok {
			return false
		}
		members := lightGroupMembers(state)
		if len(members) == 0 {
			return lightSupportsColor(state)
		}
		visiting[id] = true
		defer delete(visiting, id)
		for _, member := range members {
			if !supportsTarget(member) {
				return false
			}
		}
		return true
	}
	return supportsTarget(id)
}

func lightTypeLabel(state LightState) string {
	if lightIsGroup(state) {
		return "group"
	}
	return "single"
}

func colorLightIDs(states map[string]LightState, locations map[string]LightLocation) []string {
	ids := inventoryIDs(states, locations)
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if lightTargetSupportsColor(id, states) {
			result = append(result, id)
		}
	}
	return result
}

func wledLightIDs(states map[string]LightState, metadata map[string]HAEntityMetadata) []string {
	if len(metadata) == 0 {
		return nil
	}
	ids := []string{}
	for id, entity := range metadata {
		state, ok := states[id]
		if !ok || !strings.HasPrefix(id, "light.") || entity.Platform != "wled" || lightIsGroup(state) || state.State == "unknown" || state.State == "unavailable" || !lightSupportsColor(state) {
			continue
		}
		ids = append(ids, id)
	}
	return sortedStrings(ids)
}

func wledSelectorIDs(lightID string, states map[string]LightState, metadata map[string]HAEntityMetadata) []string {
	light, ok := metadata[lightID]
	if !ok || light.Platform != "wled" || light.DeviceID == "" {
		return nil
	}
	ids := []string{}
	for id, entity := range metadata {
		if !strings.HasPrefix(id, "select.") || entity.Platform != "wled" || entity.DeviceID == "" || entity.DeviceID != light.DeviceID || (light.ConfigEntryID != "" && entity.ConfigEntryID != "" && entity.ConfigEntryID != light.ConfigEntryID) || (entity.TranslationKey != "preset" && entity.TranslationKey != "playlist") {
			continue
		}
		if _, ok := states[id]; !ok {
			continue
		}
		ids = append(ids, id)
	}
	return sortedStrings(ids)
}

func containsString(values []string, wanted string) bool {
	return slices.Contains(values, wanted)
}

func validateWLEDProgram(program WLEDProgram, states map[string]LightState, metadata map[string]HAEntityMetadata) error {
	if err := validateEntityID(program.Light, "light"); err != nil {
		return fmt.Errorf("WLED light: %w", err)
	}
	if err := validateEntityID(program.Select, "select"); err != nil {
		return fmt.Errorf("WLED selector: %w", err)
	}
	if !containsString(wledLightIDs(states, metadata), program.Light) {
		return fmt.Errorf("WLED light %s is not a connected WLED color light", program.Light)
	}
	if !containsString(wledSelectorIDs(program.Light, states, metadata), program.Select) {
		return fmt.Errorf("WLED selector %s is not a preset or playlist for %s", program.Select, program.Light)
	}
	selector := states[program.Select]
	if !containsString(stringList(selector.Attribute["options"]), program.Option) {
		return fmt.Errorf("WLED option %q is not available in %s", program.Option, program.Select)
	}
	return nil
}

func validateConnectedWLEDProgram(status HAInventoryStatus, program WLEDProgram, states map[string]LightState, metadata map[string]HAEntityMetadata) error {
	if !status.Connected {
		return nil
	}
	if !status.StatesReady {
		if status.Error != "" {
			return fmt.Errorf("home assistant state inventory is unavailable; refresh inventory before saving WLED assignments: %s", status.Error)
		}
		return fmt.Errorf("home assistant state inventory is not loaded; refresh inventory before saving WLED assignments")
	}
	if !status.EntityRegistryReady {
		if status.Error != "" {
			return fmt.Errorf("home assistant entity registry is unavailable; refresh inventory before saving WLED assignments: %s", status.Error)
		}
		return fmt.Errorf("home assistant entity registry is not loaded; refresh inventory before saving WLED assignments")
	}
	return validateWLEDProgram(program, states, metadata)
}
