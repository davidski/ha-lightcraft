package main

import (
	"fmt"
)

func ValidateBundle(bundle Bundle) error {
	for kind, config := range bundle.Files {
		switch kind {
		case Automations:
			if err := validateAutomations(config.Data); err != nil {
				return err
			}
		case Scripts:
			values, ok := config.Data.(map[string]any)
			if !ok {
				return fmt.Errorf("%s YAML must be a mapping", kind)
			}
			for id, value := range values {
				if id == "" {
					return fmt.Errorf("%s contains an empty id", kind)
				}
				entry, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("%s %s must be a mapping", kind, id)
				}
				if _, ok := entry["sequence"]; !ok {
					return fmt.Errorf("%s %s missing sequence", kind, id)
				}
			}
		case Helpers:
			_, ok := config.Data.(map[string]any)
			if !ok {
				return fmt.Errorf("%s YAML must be a mapping", kind)
			}
		case Colors:
			if err := validateColors(config.Data); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported config kind %s", kind)
		}
	}
	return validateLighting(bundle)
}

func validateColors(value any) error {
	values, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s YAML must be a mapping", Colors)
	}
	for id, raw := range values {
		entry, ok := raw.(map[string]any)
		if !ok || id == "" {
			return fmt.Errorf("%s %s must be a mapping", Colors, id)
		}
		if name, ok := entry["name"].(string); !ok || name == "" {
			return fmt.Errorf("%s %s missing name", Colors, id)
		}
		x, y := number(entry["x"]), number(entry["y"])
		if _, xOK := entry["x"]; !xOK || x < 0 || y <= 0 || x+y > 1 {
			return fmt.Errorf("%s %s has invalid XY coordinates", Colors, id)
		}
	}
	return nil
}

func validateAutomations(value any) error {
	values, ok := value.([]any)
	if !ok {
		return fmt.Errorf("%s YAML must be a list", Automations)
	}
	for index, raw := range values {
		entry, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s entry %d must be a mapping", Automations, index)
		}
		id, ok := entry["id"].(string)
		if !ok || id == "" {
			return fmt.Errorf("%s entry %d missing id", Automations, index)
		}
	}
	return nil
}
