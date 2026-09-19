package main

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

type ColorDefinition struct {
	Name string
	X    float64
	Y    float64
}

const xyScale = 1_000.0

func normalizeXY(value float64) float64 {
	return math.Round(value*xyScale) / xyScale
}

func normalizeXYPair(values []float64) []float64 {
	if len(values) != 2 {
		return values
	}
	return []float64{normalizeXY(values[0]), normalizeXY(values[1])}
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
		result[id] = ColorDefinition{Name: name, X: normalizeXY(number(entry["x"])), Y: normalizeXY(number(entry["y"]))}
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
	slices.SortFunc(values, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return values
}

func upsertColor(bundle Bundle, id string, color ColorDefinition) (Bundle, error) {
	color.X, color.Y = normalizeXY(color.X), normalizeXY(color.Y)
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

func materializeNativeBundle(bundle Bundle) Bundle {
	result, err := cloneBundle(bundle)
	if err != nil {
		return bundle
	}
	delete(result.Files, Colors)
	return result
}

func mergeImportedColorCatalog(bundle, imported Bundle) (Bundle, error) {
	importedValues := sequenceColorCatalog(imported)
	if len(importedValues) == 0 {
		return bundle, nil
	}
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	values := map[string]any{}
	if existing, ok := result.Files[Colors].Data.(map[string]any); ok {
		values = existing
	}
	for id, value := range importedValues {
		values[id] = value
	}
	result.Files[Colors] = Config{Kind: Colors, Data: values}
	return withHash(result)
}

func sequenceColorCatalog(bundle Bundle) map[string]any {
	sequences := colorSequences(bundle)
	sequenceIDs := make([]string, 0, len(sequences))
	for id := range sequences {
		sequenceIDs = append(sequenceIDs, id)
	}
	sortedStrings(sequenceIDs)

	values := map[string]any{}
	for _, sequenceID := range sequenceIDs {
		for _, step := range sequences[sequenceID].Steps {
			if len(step.XY) != 2 || step.Name == "" || step.XY[0] < 0 || step.XY[1] <= 0 || step.XY[0]+step.XY[1] > 1 {
				continue
			}
			id := colorID(step.Name)
			if id == "" {
				continue
			}
			if existing, ok := values[id].(map[string]any); ok && existing["name"] == step.Name && number(existing["x"]) == normalizeXY(step.XY[0]) && number(existing["y"]) == normalizeXY(step.XY[1]) {
				continue
			}
			if _, exists := values[id]; exists {
				for n := 2; ; n++ {
					candidate := fmt.Sprintf("%s_%d", id, n)
					if _, exists := values[candidate]; !exists {
						id = candidate
						break
					}
				}
			}
			values[id] = map[string]any{"name": step.Name, "x": normalizeXY(step.XY[0]), "y": normalizeXY(step.XY[1])}
		}
	}
	return values
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

func rgbToXY(rgb [3]int) (float64, float64, bool) {
	linear := func(value int) float64 {
		v := float64(value) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	r, g, b := linear(rgb[0]), linear(rgb[1]), linear(rgb[2])
	x := 0.4124*r + 0.3576*g + 0.1805*b
	y := 0.2126*r + 0.7152*g + 0.0722*b
	z := 0.0193*r + 0.1192*g + 0.9505*b
	sum := x + y + z
	if sum == 0 {
		return 0, 0, false
	}
	return x / sum, y / sum, true
}
