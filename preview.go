package main

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type LightState struct {
	EntityID  string         `json:"entity_id"`
	State     string         `json:"state"`
	Attribute map[string]any `json:"attributes"`
}

type StateAPI interface {
	States(context.Context, []string) (map[string]LightState, error)
	CallService(context.Context, string, string, string, map[string]any) error
}

type Preview struct {
	API          StateAPI
	Before       map[string]LightState
	Target       []string
	Capabilities map[string]LightCapability
}

func (p *Preview) Apply(ctx context.Context, values map[string]map[string]any) error {
	for entityID, data := range values {
		if capability, ok := p.Capabilities[entityID]; ok {
			if err := capability.Validate(data); err != nil {
				return fmt.Errorf("preview validation: %w", err)
			}
		}
		if state, ok := data["state"].(string); ok && state == "off" {
			if err := p.API.CallService(ctx, "light", "turn_off", entityID, serviceData(data)); err != nil {
				return fmt.Errorf("preview %s: %w", entityID, err)
			}
			continue
		}
		if err := p.API.CallService(ctx, "light", "turn_on", entityID, serviceData(data)); err != nil {
			return fmt.Errorf("preview %s: %w", entityID, err)
		}
	}
	return nil
}

func (p *Preview) Capture(ctx context.Context, entities []string) error {
	states, err := p.API.States(ctx, entities)
	if err != nil {
		return err
	}
	for _, entityID := range entities {
		state, ok := states[entityID]
		if !ok {
			return fmt.Errorf("preview state missing for %s", entityID)
		}
		if state.State == "unavailable" || state.State == "unknown" {
			return fmt.Errorf("preview state unavailable for %s", entityID)
		}
	}
	p.Before = states
	p.Target = append([]string(nil), entities...)
	p.Capabilities = capabilitiesFromStates(states)
	return nil
}

func capabilitiesFromStates(states map[string]LightState) map[string]LightCapability {
	result := map[string]LightCapability{}
	for entityID, state := range states {
		capability := LightCapability{EntityID: entityID, State: state.State}
		if values, ok := state.Attribute["supported_color_modes"].([]any); ok {
			for _, value := range values {
				if mode, ok := value.(string); ok {
					capability.SupportedColorMode = append(capability.SupportedColorMode, mode)
				}
			}
		}
		if values, ok := state.Attribute["effect_list"].([]any); ok {
			for _, value := range values {
				if effect, ok := value.(string); ok {
					capability.Effects = append(capability.Effects, effect)
				}
			}
		}
		result[entityID] = capability
	}
	return result
}

func (p *Preview) Restore(ctx context.Context) error {
	if len(p.Before) == 0 {
		return fmt.Errorf("no preview state captured")
	}
	entities := make([]string, 0, len(p.Before))
	for entityID := range p.Before {
		entities = append(entities, entityID)
	}
	sort.Strings(entities)
	var skipped []string
	for _, entityID := range entities {
		state := p.Before[entityID]
		if state.State == "unavailable" || state.State == "unknown" {
			skipped = append(skipped, entityID)
			continue
		}
		data := map[string]any{}
		for key, value := range state.Attribute {
			switch key {
			case "brightness", "rgb_color", "rgbw_color", "hs_color", "xy_color", "color_temp_kelvin", "color_temp", "effect":
				data[key] = value
			}
		}
		service := "turn_on"
		if state.State == "off" {
			service = "turn_off"
			data = map[string]any{}
		}
		if err := p.API.CallService(ctx, "light", service, entityID, data); err != nil {
			return fmt.Errorf("restore %s: %w", entityID, err)
		}
	}
	if len(skipped) > 0 {
		return fmt.Errorf("could not restore unavailable lights: %s", strings.Join(skipped, ", "))
	}
	current, err := p.API.States(ctx, entities)
	if err != nil {
		return fmt.Errorf("verify restore: %w", err)
	}
	for _, entityID := range entities {
		before, ok := p.Before[entityID]
		after, exists := current[entityID]
		if !ok || !exists || before.State != after.State {
			return fmt.Errorf("verify restore %s: state mismatch", entityID)
		}
		for _, key := range []string{"brightness", "rgb_color", "rgbw_color", "hs_color", "xy_color", "color_temp_kelvin", "color_temp", "effect"} {
			if value, present := before.Attribute[key]; present && !reflect.DeepEqual(value, after.Attribute[key]) {
				return fmt.Errorf("verify restore %s: %s mismatch", entityID, key)
			}
		}
	}
	return nil
}

func serviceData(value map[string]any) map[string]any {
	result := map[string]any{}
	for key, item := range value {
		switch key {
		case "brightness", "rgb_color", "rgbw_color", "hs_color", "xy_color", "color_temp_kelvin", "color_temp", "effect", "transition", "flash":
			result[key] = item
		}
	}
	return result
}
