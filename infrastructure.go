package main

import (
	"fmt"
	"strings"
)

const holidayScriptID = "holiday_lights"

// coreHolidayLightsScript is the managed runtime installed in each HA
// instance. Holiday sequences are data under variables.holiday_colors.
func coreHolidayLightsScript(sequences map[string]any) map[string]any {
	if sequences == nil {
		sequences = map[string]any{"Other": []any{"exterior_lights"}}
	}
	return map[string]any{
		"alias": "Holiday lights",
		"icon":  "mdi:string-lights",
		"mode":  "single",
		"sequence": []any{
			map[string]any{
				"choose": []any{
					map[string]any{
						"conditions": []any{map[string]any{"condition": "template", "value_template": "{{ holiday_selected == 'Other' }}"}},
						"sequence": []any{map[string]any{
							"action": "scene.turn_on",
							"target": map[string]any{"entity_id": "scene.exterior_lights"},
						}},
					},
				},
				"default": []any{map[string]any{
					"alias": "Cycle holiday sequence",
					"repeat": map[string]any{
						"for_each": "{{ holiday_colors[holiday_selected] | list }}",
						"sequence": []any{
							map[string]any{
								"action": "scene.turn_on",
								"target": map[string]any{"entity_id": "scene.{{ repeat.item }}"},
								"data":   map[string]any{"transition": 0.5},
							},
							map[string]any{"delay": map[string]any{"seconds": 6}},
						},
					},
				}},
			},
		},
		"variables": map[string]any{
			"holiday_colors":   sequences,
			"holiday_selected": "{{ states('input_select.holiday') }}",
		},
	}
}

func holidaySequences(bundle Bundle) map[string][]string {
	result := map[string][]string{}
	config, ok := bundle.Files[Scripts]
	if !ok {
		return result
	}
	scripts, _ := config.Data.(map[string]any)
	script, _ := scripts[holidayScriptID].(map[string]any)
	variables, _ := script["variables"].(map[string]any)
	colors, _ := variables["holiday_colors"].(map[string]any)
	for holiday, raw := range colors {
		values, ok := raw.([]any)
		if !ok {
			continue
		}
		result[holiday] = []string{}
		for _, value := range values {
			if sceneID, ok := value.(string); ok && sceneID != "" {
				result[holiday] = append(result[holiday], strings.TrimPrefix(sceneID, "scene."))
			}
		}
	}
	return result
}

func ensureHolidayInfrastructure(bundle, source Bundle) Bundle {
	result, err := bootstrapHolidayInfrastructure(bundle, source)
	if err != nil {
		return bundle
	}
	return result
}

func legacyLightingDetected(bundle Bundle) bool {
	return len(holidaySequences(bundle)) > 0 && len(colorSequences(bundle)) == 0
}

// Upgrade keeps the complete old script under a new HA script ID before
// installing the managed core. No old scenes, selector options, or automation
// entries are removed.
func upgradeLegacyInfrastructure(bundle, source Bundle) (Bundle, error) {
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	scripts, _ := result.Files[Scripts].Data.(map[string]any)
	if original, ok := scripts[holidayScriptID]; ok {
		scripts[holidayScriptID+"_legacy_backup"] = original
	}
	result.Files[Scripts] = Config{Kind: Scripts, Data: scripts}
	for holiday := range holidaySequences(source) {
		result, err = convertLegacySequence(result, holiday)
		if err != nil {
			return Bundle{}, fmt.Errorf("upgrade %s: %w", holiday, err)
		}
	}
	return bootstrapHolidayInfrastructure(result, source)
}

func bootstrapHolidayInfrastructure(bundle, source Bundle) (Bundle, error) {
	sequences, err := holidaySequenceData(source)
	if err != nil {
		return Bundle{}, err
	}
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	if result.Files == nil {
		result.Files = map[ConfigKind]Config{}
	}
	scripts := map[string]any{}
	if config, ok := result.Files[Scripts]; ok {
		scripts, _ = config.Data.(map[string]any)
	}
	if scripts == nil {
		scripts = map[string]any{}
	}
	scripts[holidayScriptID] = coreHolidayLightsScript(sequences)
	if sourceScripts, ok := source.Files[Scripts].Data.(map[string]any); ok {
		if original, ok := sourceScripts[holidayScriptID].(map[string]any); ok && original["sequence"] != nil {
			copy, copyErr := cloneBundle(source)
			if copyErr != nil {
				return Bundle{}, copyErr
			}
			scripts[holidayScriptID] = copy.Files[Scripts].Data.(map[string]any)[holidayScriptID]
		}
	}
	result.Files[Scripts] = Config{Kind: Scripts, Data: scripts}
	if err := updateHolidaySelector(&result, sequences); err != nil {
		return Bundle{}, err
	}
	return withHash(result)
}

func holidaySequenceData(bundle Bundle) (map[string]any, error) {
	sequences := map[string]any{}
	config, ok := bundle.Files[Scripts]
	if !ok {
		sequences["Other"] = []any{"exterior_lights"}
		return sequences, nil
	}
	scripts, ok := config.Data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("scripts YAML must be a mapping")
	}
	rawScript, exists := scripts[holidayScriptID]
	if !exists {
		sequences["Other"] = []any{"exterior_lights"}
		return sequences, nil
	}
	script, ok := rawScript.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("holiday_lights script must be a mapping")
	}
	variables, ok := script["variables"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("holiday_lights variables are missing")
	}
	colors, ok := variables["holiday_colors"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("holiday_lights holiday_colors is missing")
	}
	for holiday, raw := range colors {
		values, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("holiday sequence %s must be a list", holiday)
		}
		copy := make([]any, len(values))
		for i, value := range values {
			id, ok := value.(string)
			if !ok || id == "" {
				return nil, fmt.Errorf("holiday sequence %s contains an invalid scene", holiday)
			}
			copy[i] = strings.TrimPrefix(id, "scene.")
		}
		sequences[holiday] = copy
	}
	return sequences, nil
}

func updateHolidaySelector(bundle *Bundle, sequences map[string]any) error {
	config := bundle.Files[Helpers]
	helpers, ok := config.Data.(map[string]any)
	if !ok {
		helpers = map[string]any{}
	}
	helper, ok := helpers["holiday"].(map[string]any)
	if !ok {
		helper = map[string]any{"icon": "mdi:gift", "name": "Holiday"}
	}
	options := []any{}
	if raw, exists := helper["options"]; exists {
		var ok bool
		options, ok = raw.([]any)
		if !ok {
			return fmt.Errorf("input_select holiday options must be a list")
		}
	}
	seen := map[string]bool{}
	for _, raw := range options {
		if option, ok := raw.(string); ok {
			seen[option] = true
		}
	}
	if !seen["Other"] {
		options = append([]any{"Other"}, options...)
	}
	for _, holiday := range sortedStrings(mapKeysAny(sequences)) {
		if !seen[holiday] {
			options = append(options, holiday)
			seen[holiday] = true
		}
	}
	helper["options"] = options
	helpers["holiday"] = helper
	config.Data = helpers
	bundle.Files[Helpers] = config
	return nil
}

func mapKeysAny(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func sequenceHolidays(bundle Bundle) []string {
	sequences := holidaySequences(bundle)
	holidays := make([]string, 0, len(sequences))
	for holiday := range sequences {
		holidays = append(holidays, holiday)
	}
	return sortedStrings(holidays)
}

func replaceHolidaySequence(bundle Bundle, holiday string, sceneIDs []string) (Bundle, error) {
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	config := result.Files[Scripts]
	scripts, ok := config.Data.(map[string]any)
	if !ok {
		return Bundle{}, fmt.Errorf("holiday_lights script data is invalid")
	}
	script, ok := scripts[holidayScriptID].(map[string]any)
	if !ok {
		return Bundle{}, fmt.Errorf("holiday_lights script is missing")
	}
	variables, ok := script["variables"].(map[string]any)
	if !ok {
		return Bundle{}, fmt.Errorf("holiday_lights variables are missing")
	}
	colors, ok := variables["holiday_colors"].(map[string]any)
	if !ok {
		return Bundle{}, fmt.Errorf("holiday_lights holiday_colors is missing")
	}
	values := make([]any, len(sceneIDs))
	for i, sceneID := range sceneIDs {
		values[i] = sceneID
	}
	colors[holiday] = values
	variables["holiday_colors"], script["variables"], scripts[holidayScriptID] = colors, variables, script
	config.Data, result.Files[Scripts] = scripts, config
	return withHash(result)
}
