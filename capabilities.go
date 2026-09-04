package main

import "fmt"

type LightCapability struct {
	EntityID           string
	State              string
	SupportedColorMode []string
	Effects            []string
}

func (c LightCapability) Validate(value map[string]any) error {
	if c.State == "unavailable" || c.State == "unknown" {
		return fmt.Errorf("%s is %s", c.EntityID, c.State)
	}
	if brightness, ok := value["brightness"].(int); ok && (brightness < 0 || brightness > 255) {
		return fmt.Errorf("%s brightness must be 0-255", c.EntityID)
	}
	if _, ok := value["rgb_color"]; ok && len(c.SupportedColorMode) > 0 && !supports(c.SupportedColorMode, "rgb", "rgbw", "hs") {
		return fmt.Errorf("%s does not support RGB color", c.EntityID)
	}
	if _, ok := value["color_temp_kelvin"]; ok && len(c.SupportedColorMode) > 0 && !supports(c.SupportedColorMode, "color_temp") {
		return fmt.Errorf("%s does not support color temperature", c.EntityID)
	}
	if effect, ok := value["effect"].(string); ok && len(c.Effects) > 0 && !containsString(c.Effects, effect) {
		return fmt.Errorf("%s does not support effect %q", c.EntityID, effect)
	}
	return nil
}

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

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
