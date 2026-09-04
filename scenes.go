package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func SceneIDs(bundle Bundle) []string {
	config, ok := bundle.Files[Scenes]
	if !ok {
		return nil
	}
	values, ok := config.Data.([]any)
	if !ok {
		return nil
	}
	result := []string{}
	for _, value := range values {
		if entry, ok := value.(map[string]any); ok {
			if id, ok := entry["id"].(string); ok {
				result = append(result, id)
			}
		}
	}
	sort.Strings(result)
	return result
}

func SceneValues(bundle Bundle, sceneID string) map[string]map[string]any {
	result := map[string]map[string]any{}
	config, ok := bundle.Files[Scenes]
	if !ok {
		return result
	}
	values, ok := config.Data.([]any)
	if !ok {
		return result
	}
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if !ok || entry["id"] != sceneID {
			continue
		}
		entities, ok := entry["entities"].(map[string]any)
		if !ok {
			return result
		}
		for entityID, raw := range entities {
			if state, ok := raw.(map[string]any); ok {
				result[entityID] = state
			}
		}
	}
	return result
}

func SceneInput(bundle Bundle, sceneID string) string {
	values := SceneValues(bundle, sceneID)
	entities := mapKeys(values)
	if len(entities) == 0 {
		return sceneID + ",,,,,,,"
	}
	value := values[entities[0]]
	rgb := []any{0, 0, 0}
	if candidate, ok := value["rgb_color"].([]any); ok && len(candidate) >= 3 {
		rgb = candidate[:3]
	}
	brightness := 180
	if candidate, ok := value["brightness"].(int); ok {
		brightness = candidate
	}
	h, color := sceneLabels(bundle, sceneID)
	return fmt.Sprintf("%s,%s,%s,%s,%v,%v,%v,%d", sceneID, h, color, strings.Join(entities, ";"), rgb[0], rgb[1], rgb[2], brightness)
}

func sceneLabels(bundle Bundle, sceneID string) (string, string) {
	config, ok := bundle.Files[Scenes]
	if ok {
		if values, ok := config.Data.([]any); ok {
			for _, raw := range values {
				entry, ok := raw.(map[string]any)
				if !ok || entry["id"] != sceneID {
					continue
				}
				if name, ok := entry["name"].(string); ok {
					parts := strings.Fields(name)
					if len(parts) > 1 {
						return parts[0], strings.Join(parts[1:], " ")
					}
				}
			}
		}
	}
	return "Custom", "color"
}

func NewScene(id, name string, entities []string, rgb [3]int, brightness int) map[string]any {
	targets := map[string]any{}
	for _, entity := range entities {
		targets[entity] = map[string]any{
			"state":      "on",
			"rgb_color":  []any{rgb[0], rgb[1], rgb[2]},
			"brightness": brightness,
		}
	}
	return map[string]any{"id": id, "name": name, "entities": targets}
}

func NewXYScene(id, name string, entities []string, x, y float64, brightness int) map[string]any {
	targets := map[string]any{}
	for _, entity := range entities {
		targets[entity] = map[string]any{
			"state":      "on",
			"xy_color":   []any{x, y},
			"brightness": brightness,
		}
	}
	return map[string]any{"id": id, "name": name, "entities": targets}
}

func DeleteScene(bundle Bundle, sceneID string) (Bundle, []ConfigRef, error) {
	refs := AffectedReferences(bundle, sceneID)
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, refs, err
	}
	config, ok := result.Files[Scenes]
	if !ok {
		return result, refs, nil
	}
	values, ok := config.Data.([]any)
	if !ok {
		return Bundle{}, refs, fmt.Errorf("scenes YAML must be a list")
	}
	filtered := make([]any, 0, len(values))
	for _, value := range values {
		entry, isEntry := value.(map[string]any)
		if !isEntry || entry["id"] != sceneID {
			filtered = append(filtered, value)
		}
	}
	config.Data = filtered
	result.Files[Scenes] = config
	hashed, err := withHash(result)
	return hashed, refs, err
}

func UpsertAutomation(bundle Bundle, automation map[string]any) (Bundle, error) {
	id, ok := automation["id"].(string)
	if !ok || id == "" {
		return Bundle{}, fmt.Errorf("automation requires id")
	}
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	config := result.Files[Automations]
	values, ok := config.Data.([]any)
	if !ok {
		values = []any{}
	}
	replaced := false
	for index, value := range values {
		entry, ok := value.(map[string]any)
		if ok && entry["id"] == id {
			values[index] = automation
			replaced = true
		}
	}
	if !replaced {
		values = append(values, automation)
	}
	config.Data = values
	result.Files[Automations] = config
	return withHash(result)
}

func UpsertScene(bundle Bundle, scene map[string]any) (Bundle, error) {
	id, ok := scene["id"].(string)
	if !ok || id == "" {
		return Bundle{}, fmt.Errorf("scene requires id")
	}
	if _, ok := scene["name"].(string); !ok {
		return Bundle{}, fmt.Errorf("scene %s requires name", id)
	}
	if _, ok := scene["entities"]; !ok {
		return Bundle{}, fmt.Errorf("scene %s requires entities", id)
	}
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	config := result.Files[Scenes]
	values, ok := config.Data.([]any)
	if !ok {
		values = []any{}
	}
	replaced := false
	for index, value := range values {
		entry, ok := value.(map[string]any)
		if ok && entry["id"] == id {
			values[index] = scene
			replaced = true
		}
	}
	if !replaced {
		values = append(values, scene)
	}
	config.Data = values
	result.Files[Scenes] = config
	return withHash(result)
}

func UpsertHolidayColor(bundle Bundle, scriptID, holiday, sceneID string) (Bundle, error) {
	if holiday == "" || sceneID == "" {
		return bundle, nil
	}
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	config, ok := result.Files[Scripts]
	if !ok {
		return bundle, nil
	}
	scripts, ok := config.Data.(map[string]any)
	if !ok {
		return Bundle{}, fmt.Errorf("scripts YAML must be a mapping")
	}
	script, ok := scripts[scriptID].(map[string]any)
	if !ok {
		return bundle, nil
	}
	variables, ok := script["variables"].(map[string]any)
	if !ok {
		return bundle, nil
	}
	colors, ok := variables["holiday_colors"].(map[string]any)
	if !ok {
		return bundle, nil
	}
	values, ok := colors[holiday].([]any)
	if !ok {
		values = []any{}
	}
	for _, value := range values {
		if value == sceneID {
			return bundle, nil
		}
	}
	colors[holiday] = append(values, sceneID)
	variables["holiday_colors"] = colors
	script["variables"] = variables
	scripts[scriptID] = script
	config.Data = scripts
	result.Files[Scripts] = config
	return withHash(result)
}

func RemoveHolidayColor(bundle Bundle, scriptID, sceneID string) (Bundle, error) {
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	config, ok := result.Files[Scripts]
	if !ok {
		return bundle, nil
	}
	scripts, ok := config.Data.(map[string]any)
	if !ok {
		return Bundle{}, fmt.Errorf("scripts YAML must be a mapping")
	}
	script, ok := scripts[scriptID].(map[string]any)
	if !ok {
		return bundle, nil
	}
	variables, ok := script["variables"].(map[string]any)
	if !ok {
		return bundle, nil
	}
	colors, ok := variables["holiday_colors"].(map[string]any)
	if !ok {
		return bundle, nil
	}
	changed := false
	for holiday, raw := range colors {
		values, ok := raw.([]any)
		if !ok {
			continue
		}
		filtered := values[:0]
		for _, value := range values {
			if value == sceneID {
				changed = true
			} else {
				filtered = append(filtered, value)
			}
		}
		colors[holiday] = filtered
	}
	if !changed {
		return bundle, nil
	}
	variables["holiday_colors"] = colors
	script["variables"] = variables
	scripts[scriptID] = script
	config.Data = scripts
	result.Files[Scripts] = config
	return withHash(result)
}

func AffectedReferences(bundle Bundle, sceneID string) []ConfigRef {
	wanted := "scene." + sceneID
	var result []ConfigRef
	for kind, config := range bundle.Files {
		if kind == Scenes || kind == Helpers {
			continue
		}
		entries := bundleEntries(Bundle{Files: map[ConfigKind]Config{kind: config}})
		for ref, entry := range entries {
			if containsValue(entry, wanted) || containsValue(entry, sceneID) {
				result = append(result, ref)
			}
		}
	}
	return result
}

func containsValue(value any, wanted string) bool {
	switch value := value.(type) {
	case string:
		return value == wanted
	case map[string]any:
		for _, item := range value {
			if containsValue(item, wanted) {
				return true
			}
		}
	case []any:
		for _, item := range value {
			if containsValue(item, wanted) {
				return true
			}
		}
	}
	return false
}

func cloneBundle(bundle Bundle) (Bundle, error) {
	result := Bundle{Files: map[ConfigKind]Config{}, SourceHash: bundle.SourceHash}
	for kind, config := range bundle.Files {
		data, err := canonicalJSON(config.Data)
		if err != nil {
			return Bundle{}, err
		}
		var copy any
		if err := json.Unmarshal(data, &copy); err != nil {
			return Bundle{}, err
		}
		result.Files[kind] = Config{Kind: config.Kind, Data: copy}
	}
	return result, nil
}
