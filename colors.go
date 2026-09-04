package main

import (
	"fmt"
	"math"
	"strings"
)

type ColorDefinition struct {
	Name string
	X    float64
	Y    float64
}

func colorDefinitions(bundle Bundle) map[string]ColorDefinition {
	result := map[string]ColorDefinition{}
	config, ok := bundle.Files[Colors]
	if !ok {
		return result
	}
	values, ok := config.Data.(map[string]any)
	if !ok {
		return result
	}
	for id, raw := range values {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		result[id] = ColorDefinition{Name: name, X: number(entry["x"]), Y: number(entry["y"])}
	}
	return result
}

func colorIDs(bundle Bundle) []string {
	values := colorDefinitions(bundle)
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	return sortedStrings(ids)
}

func sortedStrings(values []string) []string {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if strings.ToLower(values[j]) < strings.ToLower(values[i]) {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
	return values
}

func upsertColor(bundle Bundle, id string, color ColorDefinition) (Bundle, error) {
	if id == "" || color.Name == "" || color.X < 0 || color.Y <= 0 || color.X+color.Y > 1 {
		return Bundle{}, fmt.Errorf("invalid color definition")
	}
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	if result.Files == nil {
		result.Files = map[ConfigKind]Config{}
	}
	values := map[string]any{}
	if existing, ok := result.Files[Colors].Data.(map[string]any); ok {
		values = existing
	}
	values[id] = map[string]any{"name": color.Name, "x": color.X, "y": color.Y}
	result.Files[Colors] = Config{Kind: Colors, Data: values}
	return withHash(result)
}

func colorReferenced(bundle Bundle, id string) bool {
	for _, sceneID := range SceneIDs(bundle) {
		if colorRefForScene(bundle, sceneID) == id {
			return true
		}
	}
	return false
}

func deleteColor(bundle Bundle, id string) (Bundle, error) {
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	values, ok := result.Files[Colors].Data.(map[string]any)
	if !ok {
		return Bundle{}, fmt.Errorf("color catalog is malformed")
	}
	if _, ok := values[id]; !ok {
		return Bundle{}, fmt.Errorf("color %q not found", id)
	}
	delete(values, id)
	result.Files[Colors] = Config{Kind: Colors, Data: values}
	return withHash(result)
}

func colorRefForScene(bundle Bundle, sceneID string) string {
	config, ok := bundle.Files[Scenes]
	if !ok {
		return ""
	}
	values, _ := config.Data.([]any)
	for _, raw := range values {
		entry, _ := raw.(map[string]any)
		if entry["id"] == sceneID {
			if ref, ok := entry["color_ref"].(string); ok {
				return ref
			}
		}
	}
	return ""
}

func materializeNativeBundle(bundle Bundle) Bundle {
	result, err := cloneBundle(bundle)
	if err != nil {
		return bundle
	}
	config, ok := result.Files[Scenes]
	if ok {
		if values, ok := config.Data.([]any); ok {
			for _, raw := range values {
				if entry, ok := raw.(map[string]any); ok {
					delete(entry, "color_ref")
				}
			}
			config.Data = values
			result.Files[Scenes] = config
		}
	}
	delete(result.Files, Colors)
	return result
}

func mergeImportedColors(bundle Bundle) Bundle {
	result, err := cloneBundle(bundle)
	if err != nil {
		return bundle
	}
	colors := colorDefinitions(result)
	values := map[string]any{}
	if config, ok := result.Files[Colors]; ok {
		if existing, ok := config.Data.(map[string]any); ok {
			values = existing
		}
	}
	config, ok := result.Files[Scenes]
	if !ok {
		return result
	}
	scenes, ok := config.Data.([]any)
	if !ok {
		return result
	}
	for _, raw := range scenes {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := entry["id"].(string)
		name, _ := entry["name"].(string)
		xy, ok := sceneXY(entry)
		if !ok {
			continue
		}
		colorID := id
		for existingID, existing := range colors {
			if math.Abs(existing.X-xy[0]) < 0.003 && math.Abs(existing.Y-xy[1]) < 0.003 {
				colorID = existingID
				break
			}
		}
		if _, exists := colors[colorID]; !exists {
			values[colorID] = map[string]any{"name": name, "x": xy[0], "y": xy[1]}
			colors[colorID] = ColorDefinition{Name: name, X: xy[0], Y: xy[1]}
		}
		entry["color_ref"] = colorID
	}
	result.Files[Colors] = Config{Kind: Colors, Data: values}
	result, _ = withHash(result)
	return result
}

func sceneXY(scene map[string]any) ([2]float64, bool) {
	entities, _ := scene["entities"].(map[string]any)
	for _, raw := range entities {
		value, _ := raw.(map[string]any)
		if xy, ok := value["xy_color"].([]any); ok && len(xy) >= 2 {
			return [2]float64{number(xy[0]), number(xy[1])}, true
		}
		if hs, ok := value["hs_color"].([]any); ok && len(hs) >= 2 {
			return hsToXY(number(hs[0]), number(hs[1])), true
		}
	}
	return [2]float64{}, false
}

func hsToXY(hue, saturation float64) [2]float64 {
	h := math.Mod(hue, 360) / 60
	c := saturation / 100
	x := c * (1 - math.Abs(math.Mod(h, 2)-1))
	m := 1 - c
	r, g, b := 0.0, 0.0, 0.0
	switch int(h) {
	case 0:
		r, g = c, x
	case 1:
		r, g = x, c
	case 2:
		g, b = c, x
	case 3:
		g, b = x, c
	case 4:
		r, b = x, c
	default:
		r, b = c, x
	}
	r, g, b = r+m, g+m, b+m
	X := 0.4124*r + 0.3576*g + 0.1805*b
	Y := 0.2126*r + 0.7152*g + 0.0722*b
	Z := 0.0193*r + 0.1192*g + 0.9505*b
	sum := X + Y + Z
	if sum == 0 {
		return [2]float64{0.3127, 0.3290}
	}
	return [2]float64{X / sum, Y / sum}
}
