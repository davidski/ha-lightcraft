package main

import (
	"fmt"
	"strings"
)

const holidayScriptID = "holiday_lights"

// legacyLightingDetected identifies the old holiday_lights selector format.
// It is intentionally a detector only; legacy data is not part of the modern
// structured editor.
func legacyLightingDetected(bundle Bundle) bool {
	return len(holidaySequences(bundle)) > 0 && len(colorSequences(bundle)) == 0
}

func holidaySequences(bundle Bundle) map[string][]string {
	result := map[string][]string{}
	scripts, _ := bundle.Files[Scripts].Data.(map[string]any)
	script, _ := scripts[holidayScriptID].(map[string]any)
	variables, _ := script["variables"].(map[string]any)
	colors, _ := variables["holiday_colors"].(map[string]any)
	for holiday, raw := range colors {
		values, ok := raw.([]any)
		if !ok {
			continue
		}
		for _, value := range values {
			if id, ok := value.(string); ok && id != "" {
				result[holiday] = append(result[holiday], strings.TrimPrefix(id, "scene."))
			}
		}
	}
	return result
}

func legacySceneValues(bundle Bundle, id string) map[string]map[string]any {
	result := map[string]map[string]any{}
	values, _ := bundle.Files[Scenes].Data.([]any)
	for _, raw := range values {
		entry, ok := raw.(map[string]any)
		if !ok || entry["id"] != id {
			continue
		}
		entities, _ := entry["entities"].(map[string]any)
		for entity, state := range entities {
			if value, ok := state.(map[string]any); ok {
				result[entity] = value
			}
		}
	}
	return result
}

func legacySceneName(bundle Bundle, id string) string {
	values, _ := bundle.Files[Scenes].Data.([]any)
	for _, raw := range values {
		entry, ok := raw.(map[string]any)
		if !ok || entry["id"] != id {
			continue
		}
		if name, ok := entry["name"].(string); ok && name != "" {
			return name
		}
	}
	return id
}

// upgradeLegacyInfrastructure converts each old holiday selector sequence to
// a modern color sequence. The original selector script remains in the draft
// for compatibility with existing Home Assistant automations.
func upgradeLegacyInfrastructure(bundle, source Bundle) (Bundle, error) {
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}

	conversionSource, err := cloneBundle(source)
	if err != nil {
		return Bundle{}, err
	}
	for holiday := range holidaySequences(source) {
		conversionSource.Files[Scripts] = result.Files[Scripts]
		result, err = convertLegacySequence(conversionSource, holiday)
		if err != nil {
			return Bundle{}, fmt.Errorf("upgrade %s: %w", holiday, err)
		}
		delete(result.Files, Scenes)
		conversionSource, err = cloneBundle(result)
		if err != nil {
			return Bundle{}, err
		}
		conversionSource.Files[Scenes] = source.Files[Scenes]
	}
	delete(result.Files, Scenes)
	return withHash(result)
}

func convertLegacySequence(bundle Bundle, name string) (Bundle, error) {
	sequence := ColorSequence{ID: colorID(name), Name: name, Repeat: true}
	if _, exists := colorSequences(bundle)[sequence.ID]; exists {
		return Bundle{}, fmt.Errorf("sequence %s already exists", name)
	}
	for _, sceneID := range holidaySequences(bundle)[name] {
		values := legacySceneValues(bundle, sceneID)
		if len(values) == 0 {
			return Bundle{}, fmt.Errorf("import scene %s before converting this sequence", sceneID)
		}
		var step ColorStep
		var previous string
		for _, entity := range sortedLegacyEntityIDs(values) {
			state := values[entity]
			if state["state"] != "on" || state["effect"] != nil {
				return Bundle{}, fmt.Errorf("scene %s uses off states or effects; keep it as a legacy scene", sceneID)
			}
			color, ok := legacyColorDefinition(state, legacySceneName(bundle, sceneID))
			if !ok {
				return Bundle{}, fmt.Errorf("scene %s has no supported color", sceneID)
			}
			brightness := 255
			if state["brightness"] != nil {
				brightness = int(number(state["brightness"]))
			}
			step = ColorStep{Name: legacySceneName(bundle, sceneID), XY: []float64{color.X, color.Y}, Brightness: brightness, Hold: 6, Transition: .5}
			current := stringMustJSON(step)
			if previous != "" && current != previous {
				return Bundle{}, fmt.Errorf("scene %s has different colors per light; keep it as a legacy scene", sceneID)
			}
			previous = current
		}
		next, err := addLegacyColor(bundle, sceneID, legacySceneName(bundle, sceneID), values)
		if err != nil {
			return Bundle{}, err
		}
		bundle = next
		sequence.Steps = append(sequence.Steps, step)
	}
	return saveColorSequence(bundle, sequence)
}

func addLegacyColor(bundle Bundle, sceneID, name string, values map[string]map[string]any) (Bundle, error) {
	if len(values) == 0 {
		return bundle, nil
	}
	ids := sortedLegacyEntityIDs(values)
	color, ok := legacyColorDefinition(values[ids[0]], name)
	if !ok {
		return bundle, fmt.Errorf("scene %s has no catalogable color", sceneID)
	}
	color.X, color.Y = normalizeXY(color.X), normalizeXY(color.Y)
	colors := colorDefinitions(bundle)
	id := colorID(name)
	if id == "" {
		id = colorID(sceneID)
	}
	for n := 2; ; n++ {
		existing, exists := colors[id]
		if !exists || (existing.Name == color.Name && existing.X == color.X && existing.Y == color.Y) {
			break
		}
		id = fmt.Sprintf("%s_%d", colorID(name), n)
	}
	return upsertColor(bundle, id, color)
}

func legacyColorDefinition(state map[string]any, name string) (ColorDefinition, bool) {
	if raw, ok := state["xy_color"].([]any); ok && len(raw) >= 2 {
		return ColorDefinition{Name: name, X: number(raw[0]), Y: number(raw[1])}, true
	}
	if raw, ok := state["hs_color"].([]any); ok && len(raw) >= 2 {
		xy := hsToXY(number(raw[0]), number(raw[1]))
		return ColorDefinition{Name: name, X: xy[0], Y: xy[1]}, true
	}
	if raw, ok := state["rgb_color"].([]any); ok && len(raw) >= 3 {
		rgb := [3]int{clampByte(number(raw[0])), clampByte(number(raw[1])), clampByte(number(raw[2]))}
		x, y, ok := rgbToXY(rgb)
		return ColorDefinition{Name: name, X: x, Y: y}, ok
	}
	return ColorDefinition{}, false
}

func sortedLegacyEntityIDs(values map[string]map[string]any) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	return sortedStrings(ids)
}
