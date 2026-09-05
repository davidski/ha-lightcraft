package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

const sequencePrefix = "lighting_sequence_"
const assignmentPrefix = "lighting_assignment_"
const lightingControllerID = "lighting_assignments"

// These values are native script variables, consumed by the HA player.
type ColorStep struct {
	Name       string         `json:"name"`
	RGB        []int          `json:"rgb"`
	Brightness int            `json:"brightness"`
	Hold       float64        `json:"hold"`
	Transition float64        `json:"transition"`
	Color      map[string]any `json:"color,omitempty"` // Preserve native XY/HS on legacy import.
}

type ColorSequence struct {
	ID     string      `json:"-"`
	Name   string      `json:"-"`
	Repeat bool        `json:"repeat"`
	Steps  []ColorStep `json:"steps"`
}

type LightingAssignment struct {
	ID       string   `json:"-"`
	Name     string   `json:"-"`
	Sequence string   `json:"sequence"`
	Targets  []string `json:"targets"`
	Start    string   `json:"start"`
	End      string   `json:"end"`
	On       string   `json:"on"`     // HH:MM or sunset
	Off      string   `json:"off"`    // HH:MM
	Finish   string   `json:"finish"` // off or leave
	Enabled  bool     `json:"enabled"`
}

var entityName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func (s ColorSequence) Validate() error {
	if !entityName.MatchString(s.ID) || strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("sequence needs a name and a lowercase identifier")
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("add at least one color step")
	}
	for i, step := range s.Steps {
		if strings.TrimSpace(step.Name) == "" || len(step.RGB) != 3 {
			return fmt.Errorf("step %d needs a name and RGB color", i+1)
		}
		for _, c := range step.RGB {
			if c < 0 || c > 255 {
				return fmt.Errorf("step %d RGB must be 0–255", i+1)
			}
		}
		if len(step.Color) > 0 {
			if len(step.Color) != 1 {
				return fmt.Errorf("step %d has ambiguous native color", i+1)
			}
			for mode, raw := range step.Color {
				var values []float64
				if decodeLighting(raw, &values) != nil || len(values) != 2 {
					return fmt.Errorf("step %d native color needs two numbers", i+1)
				}
				if mode == "xy_color" {
					if values[0] < 0 || values[1] <= 0 || values[0]+values[1] > 1 {
						return fmt.Errorf("step %d has invalid XY color", i+1)
					}
				} else if mode == "hs_color" {
					if values[0] < 0 || values[0] > 360 || values[1] < 0 || values[1] > 100 {
						return fmt.Errorf("step %d has invalid HS color", i+1)
					}
				} else {
					return fmt.Errorf("step %d has unsupported color mode", i+1)
				}
			}
		}
		if step.Brightness < 1 || step.Brightness > 255 || math.IsNaN(step.Hold) || math.IsInf(step.Hold, 0) || step.Hold < 1 || step.Hold > 86400 || math.IsNaN(step.Transition) || math.IsInf(step.Transition, 0) || step.Transition < 0 || step.Transition > step.Hold {
			return fmt.Errorf("step %d: brightness 1–255, hold 1–86400 seconds, transition 0–hold", i+1)
		}
	}
	return nil
}

func (a LightingAssignment) Validate() error {
	if !entityName.MatchString(a.ID) || strings.TrimSpace(a.Name) == "" || !entityName.MatchString(a.Sequence) {
		return fmt.Errorf("assignment needs a name and sequence")
	}
	if len(a.Targets) == 0 {
		return fmt.Errorf("select at least one light")
	}
	seen := map[string]bool{}
	for _, id := range a.Targets {
		if !strings.HasPrefix(id, "light.") || !entityName.MatchString(strings.TrimPrefix(id, "light.")) || seen[id] {
			return fmt.Errorf("invalid or duplicate light %q", id)
		}
		seen[id] = true
	}
	layout := "01-02"
	if len(a.Start) == 10 {
		layout = "2006-01-02"
	}
	start, err := time.Parse(layout, a.Start)
	if err != nil || start.Format(layout) != a.Start {
		return fmt.Errorf("start date: use MM-DD annually or YYYY-MM-DD once")
	}
	end, err := time.Parse(layout, a.End)
	if err != nil || end.Format(layout) != a.End {
		return fmt.Errorf("end date must use the same format as start")
	}
	if layout == "2006-01-02" && end.Before(start) {
		return fmt.Errorf("end date precedes start date")
	}
	if a.On != "sunset" {
		if parsed, err := time.Parse("15:04", a.On); err != nil || parsed.Format("15:04") != a.On {
			return fmt.Errorf("start time: use HH:MM or sunset")
		}
	}
	if parsed, err := time.Parse("15:04", a.Off); err != nil || parsed.Format("15:04") != a.Off {
		return fmt.Errorf("stop time: use HH:MM")
	}
	if a.On == a.Off {
		return fmt.Errorf("start and stop time must differ")
	}
	if a.Finish != "off" && a.Finish != "leave" && !(strings.HasPrefix(a.Finish, "scene.") && entityName.MatchString(strings.TrimPrefix(a.Finish, "scene."))) {
		return fmt.Errorf("end behavior must be off, leave, or a scene.entity_id")
	}
	return nil
}

func jsonValue(value any) any {
	data, _ := json.Marshal(value)
	var result any
	_ = json.Unmarshal(data, &result)
	return result
}

func decodeLighting(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func colorSequences(bundle Bundle) map[string]ColorSequence {
	result := map[string]ColorSequence{}
	scripts, _ := bundle.Files[Scripts].Data.(map[string]any)
	for id, raw := range scripts {
		if !strings.HasPrefix(id, sequencePrefix) {
			continue
		}
		entry, _ := raw.(map[string]any)
		vars, _ := entry["variables"].(map[string]any)
		var s ColorSequence
		if decodeLighting(vars["sequence_data"], &s) == nil {
			s.ID = strings.TrimPrefix(id, sequencePrefix)
			s.Name, _ = entry["alias"].(string)
			result[s.ID] = s
		}
	}
	return result
}

func lightingAssignments(bundle Bundle) map[string]LightingAssignment {
	result := map[string]LightingAssignment{}
	scripts, _ := bundle.Files[Scripts].Data.(map[string]any)
	for id, raw := range scripts {
		if !strings.HasPrefix(id, assignmentPrefix) {
			continue
		}
		entry, _ := raw.(map[string]any)
		vars, _ := entry["variables"].(map[string]any)
		var a LightingAssignment
		if decodeLighting(vars["assignment"], &a) == nil {
			a.ID = strings.TrimPrefix(id, assignmentPrefix)
			a.Name, _ = entry["alias"].(string)
			result[a.ID] = a
		}
	}
	return result
}

func sequenceScript(s ColorSequence) map[string]any {
	return map[string]any{
		"alias": s.Name, "mode": "parallel", "max": 100,
		"description": "Returns color steps to independently controlled lighting players.",
		"variables":   map[string]any{"sequence_data": jsonValue(s)},
		"sequence":    []any{map[string]any{"stop": "Sequence definition", "response_variable": "sequence_data"}},
	}
}

func assignmentScript(a LightingAssignment) map[string]any {
	return map[string]any{
		"alias": a.Name, "mode": "single", "max_exceeded": "silent",
		"variables": map[string]any{"assignment": jsonValue(a)},
		"sequence": []any{
			map[string]any{"action": "script." + sequencePrefix + a.Sequence, "response_variable": "palette"},
			map[string]any{"repeat": map[string]any{"for_each": "{{ assignment.targets }}", "sequence": []any{map[string]any{"if": "{{ 'Solid' in (state_attr(repeat.item, 'effect_list') or []) }}", "then": []any{map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": "{{ repeat.item }}"}, "data": map[string]any{"effect": "Solid"}}}}}}},
			map[string]any{"repeat": map[string]any{
				"while": "{{ true }}",
				"sequence": []any{
					map[string]any{"repeat": map[string]any{
						"for_each": "{{ palette.steps }}",
						"sequence": []any{
							map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": "{{ assignment.targets }}"}, "data": "{{ dict(repeat.item.color | default({'rgb_color': repeat.item.rgb}), brightness=repeat.item.brightness, transition=repeat.item.transition) }}"},
							map[string]any{"delay": map[string]any{"seconds": "{{ repeat.item.hold }}"}},
						},
					}},
					map[string]any{"if": "{{ not palette.repeat }}", "then": []any{map[string]any{"wait_template": "{{ false }}"}}},
				},
			}},
		},
	}
}

// Date eligibility is evaluated on the current local date, including overnight
// hours. The final date is inclusive; playback never spills into a later date.
func assignmentActiveTemplate() string {
	return `{% set d = now().strftime('%Y-%m-%d' if assignment.start|length == 10 else '%m-%d') %}
{% set t = now().strftime('%H:%M') %}
{% set dates = (assignment.start <= d <= assignment.end) if assignment.start <= assignment.end else (d >= assignment.start or d <= assignment.end) %}
{% set night = ('12:00' <= t < assignment.off) if assignment.off > '12:00' else (t >= '12:00' or t < assignment.off) %}
{% set hours = (is_state('sun.sun', 'below_horizon') and night) if assignment.on == 'sunset' else ((assignment.on <= t < assignment.off) if assignment.on < assignment.off else (t >= assignment.on or t < assignment.off)) %}
{{ assignment.enabled and dates and hours }}`
}

func assignmentStopActions(a LightingAssignment) []any {
	player := "script." + assignmentPrefix + a.ID
	status := "input_select." + assignmentPrefix + a.ID
	stop := []any{map[string]any{"action": "script.turn_off", "target": map[string]any{"entity_id": player}}}
	if a.Finish == "off" {
		stop = append(stop, map[string]any{"action": "light.turn_off", "target": map[string]any{"entity_id": a.Targets}})
	} else if strings.HasPrefix(a.Finish, "scene.") {
		stop = append(stop, map[string]any{"action": "scene.turn_on", "target": map[string]any{"entity_id": a.Finish}})
	}
	stop = append(stop, map[string]any{"if": "{{ not is_state('" + status + "', 'paused') }}", "then": []any{map[string]any{"action": "input_select.select_option", "target": map[string]any{"entity_id": status}, "data": map[string]any{"option": "idle"}}}})
	return []any{map[string]any{"if": "{{ is_state('" + player + "', 'on') or is_state('" + status + "', 'running') }}", "then": stop}}
}

// One short controller serializes handoffs: stop every expired player before
// starting any new one. Playback itself runs independently in per-target scripts.
func lightingController(bundle Bundle) map[string]any {
	assignments := lightingAssignments(bundle)
	var stopActions, startActions []any
	var statusIDs []string
	for _, id := range lightingIDs(bundle, 1) {
		a := assignments[id]
		status, player := "input_select."+assignmentPrefix+id, "script."+assignmentPrefix+id
		statusIDs = append(statusIDs, status)
		eligible := []any{map[string]any{"condition": "template", "value_template": assignmentActiveTemplate()}, map[string]any{"condition": "template", "value_template": "{{ not is_state('" + status + "', 'paused') }}"}}
		conditions := append(append([]any(nil), eligible...), map[string]any{"condition": "state", "entity_id": player, "state": "off"})
		branch := map[string]any{"conditions": conditions, "sequence": []any{map[string]any{"action": "input_select.select_option", "target": map[string]any{"entity_id": status}, "data": map[string]any{"option": "running"}}, map[string]any{"action": "script.turn_on", "target": map[string]any{"entity_id": player}}}}
		variables := map[string]any{"variables": map[string]any{"assignment": jsonValue(a)}}
		stopActions = append(stopActions, variables, map[string]any{"if": []any{map[string]any{"condition": "not", "conditions": []any{map[string]any{"condition": "and", "conditions": eligible}}}}, "then": assignmentStopActions(a)})
		startActions = append(startActions, variables, map[string]any{"choose": []any{branch}})
	}
	triggers := []any{map[string]any{"trigger": "homeassistant", "event": "start"}, map[string]any{"trigger": "time_pattern", "minutes": "/1"}, map[string]any{"trigger": "event", "event_type": "automation_reloaded"}}
	if len(statusIDs) > 0 {
		triggers = append(triggers, map[string]any{"trigger": "state", "entity_id": statusIDs, "to": "paused"}, map[string]any{"trigger": "state", "entity_id": statusIDs, "from": "paused"})
	}
	return map[string]any{"id": lightingControllerID, "alias": "Lighting assignment schedules", "mode": "single", "triggers": triggers, "actions": append(stopActions, startActions...)}
}

func putLightingScript(bundle *Bundle, id string, value map[string]any) {
	if bundle.Files == nil {
		bundle.Files = map[ConfigKind]Config{}
	}
	scripts, _ := bundle.Files[Scripts].Data.(map[string]any)
	if scripts == nil {
		scripts = map[string]any{}
	}
	scripts[id] = value
	bundle.Files[Scripts] = Config{Kind: Scripts, Data: scripts}
}

func saveColorSequence(bundle Bundle, s ColorSequence) (Bundle, error) {
	if err := s.Validate(); err != nil {
		return Bundle{}, err
	}
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	putLightingScript(&result, sequencePrefix+s.ID, sequenceScript(s))
	return withHash(result)
}

func deleteColorSequence(bundle Bundle, id string) (Bundle, error) {
	for _, a := range lightingAssignments(bundle) {
		if a.Sequence == id {
			return Bundle{}, fmt.Errorf("sequence %q is used by schedule %q", id, a.Name)
		}
	}
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	scripts, _ := result.Files[Scripts].Data.(map[string]any)
	delete(scripts, sequencePrefix+id)
	result.Files[Scripts] = Config{Kind: Scripts, Data: scripts}
	return withHash(result)
}

func saveLightingAssignment(bundle Bundle, a LightingAssignment) (Bundle, error) {
	if err := a.Validate(); err != nil {
		return Bundle{}, err
	}
	if _, ok := colorSequences(bundle)[a.Sequence]; !ok {
		return Bundle{}, fmt.Errorf("sequence %q is missing", a.Sequence)
	}
	result, err := cloneBundle(bundle)
	if err != nil {
		return Bundle{}, err
	}
	putLightingScript(&result, assignmentPrefix+a.ID, assignmentScript(a))
	helpers, _ := result.Files[Helpers].Data.(map[string]any)
	if helpers == nil {
		helpers = map[string]any{}
	}
	helpers[assignmentPrefix+a.ID] = assignmentHelper(a)
	result.Files[Helpers] = Config{Kind: Helpers, Data: helpers}
	result, err = UpsertAutomation(result, lightingController(result))
	if err != nil {
		return Bundle{}, err
	}
	if err := validateLighting(result); err != nil {
		return Bundle{}, err
	}
	return withHash(result)
}

func assignmentHelper(a LightingAssignment) map[string]any {
	return map[string]any{"name": a.Name + " playback", "options": []any{"idle", "running", "paused"}, "icon": "mdi:play-pause"}
}

func resolveLightingTargets(targets []string, states map[string]LightState) ([]string, error) {
	var result []string
	seen, visiting := map[string]bool{}, map[string]bool{}
	var add func(string) error
	add = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("light group %s contains a cycle", id)
		}
		if seen[id] {
			return nil
		}
		state, ok := states[id]
		if !ok {
			return fmt.Errorf("light %s was not found in Home Assistant", id)
		}
		visiting[id] = true
		if members := stringList(state.Attribute["entity_id"]); len(members) > 0 {
			for _, member := range members {
				if err := add(member); err != nil {
					return err
				}
			}
		} else {
			modes := stringList(state.Attribute["supported_color_modes"])
			if len(modes) > 0 && !supports(modes, "rgb", "rgbw", "rgbww", "xy", "hs") {
				return fmt.Errorf("%s does not support color", id)
			}
			result = append(result, id)
		}
		visiting[id], seen[id] = false, true
		return nil
	}
	for _, id := range targets {
		if err := add(id); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func assignmentDateActive(a LightingAssignment, date time.Time) bool {
	format := "01-02"
	if len(a.Start) == 10 {
		format = "2006-01-02"
	}
	d := date.Format(format)
	if a.Start > a.End {
		return d >= a.Start || d <= a.End
	}
	return a.Start <= d && d <= a.End
}

func assignmentMinutes(a LightingAssignment, minute int) bool {
	t := fmt.Sprintf("%02d:%02d", minute/60, minute%60)
	// Conservatively include all possible sunset hours for overlap checks.
	if a.On == "sunset" {
		if a.Off > "12:00" {
			return t >= "12:00" && t < a.Off
		}
		return t >= "12:00" || t < a.Off
	}
	if a.On > a.Off {
		return t >= a.On || t < a.Off
	}
	return a.On <= t && t < a.Off
}

func assignmentsOverlap(a, b LightingAssignment) bool {
	if !a.Enabled || !b.Enabled {
		return false
	}
	shared := false
	for _, target := range a.Targets {
		if containsString(b.Targets, target) {
			shared = true
		}
	}
	if !shared {
		return false
	}
	hours := false
	for minute := 0; minute < 1440; minute++ {
		if assignmentMinutes(a, minute) && assignmentMinutes(b, minute) {
			hours = true
			break
		}
	}
	if !hours {
		return false
	}
	start, end := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2000, 12, 31, 0, 0, 0, 0, time.UTC)
	oneOff := false
	for _, candidate := range []LightingAssignment{a, b} {
		if len(candidate.Start) == 10 {
			lo, _ := time.Parse("2006-01-02", candidate.Start)
			hi, _ := time.Parse("2006-01-02", candidate.End)
			if !oneOff || lo.After(start) {
				start = lo
			}
			if !oneOff || hi.Before(end) {
				end = hi
			}
			oneOff = true
		}
	}
	// Eight years includes a leap day even across a non-leap century.
	for d, count := start, 0; !d.After(end) && count < 2922; d, count = d.AddDate(0, 0, 1), count+1 {
		if assignmentDateActive(a, d) && assignmentDateActive(b, d) {
			return true
		}
	}
	return false
}

func validateLighting(bundle Bundle) error {
	sequences, assignments := colorSequences(bundle), lightingAssignments(bundle)
	entries := bundleEntries(bundle)
	for ref := range entries {
		if ref.Kind == Scripts && strings.HasPrefix(ref.ID, sequencePrefix) {
			if _, ok := sequences[strings.TrimPrefix(ref.ID, sequencePrefix)]; !ok {
				return fmt.Errorf("cannot decode sequence %s", ref.ID)
			}
		}
		if strings.HasPrefix(ref.ID, assignmentPrefix) {
			if _, ok := assignments[strings.TrimPrefix(ref.ID, assignmentPrefix)]; !ok {
				return fmt.Errorf("cannot decode assignment %s; import its player, schedule and helper", ref.ID)
			}
		}
	}
	for id, s := range sequences {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("sequence %s: %w", id, err)
		}
		if stringMustJSON(entries[ConfigRef{Scripts, sequencePrefix + id}]) != stringMustJSON(sequenceScript(s)) {
			return fmt.Errorf("sequence %s has unsupported script edits; preserve it outside the structured editor", id)
		}
	}
	for id, a := range assignments {
		if err := a.Validate(); err != nil {
			return fmt.Errorf("assignment %s: %w", id, err)
		}
		if _, ok := sequences[a.Sequence]; !ok {
			return fmt.Errorf("assignment %s references missing sequence %s", id, a.Sequence)
		}
		if stringMustJSON(entries[ConfigRef{Scripts, assignmentPrefix + id}]) != stringMustJSON(assignmentScript(a)) || stringMustJSON(entries[ConfigRef{Helpers, assignmentPrefix + id}]) != stringMustJSON(assignmentHelper(a)) {
			return fmt.Errorf("assignment %s needs its matching player and playback helper; import both", id)
		}
		for otherID, other := range assignments {
			if id < otherID && assignmentsOverlap(a, other) {
				return fmt.Errorf("%s and %s overlap on the same light; choose different dates, hours, or lights", a.Name, other.Name)
			}
		}
	}
	if len(assignments) > 0 && stringMustJSON(entries[ConfigRef{Automations, lightingControllerID}]) != stringMustJSON(lightingController(bundle)) {
		return fmt.Errorf("lighting assignments need their matching schedule controller; import lighting_assignments")
	}
	return nil
}

func convertLegacySequence(bundle Bundle, name string) (Bundle, error) {
	s := ColorSequence{ID: colorID(name), Name: name, Repeat: true}
	if _, exists := colorSequences(bundle)[s.ID]; exists {
		return Bundle{}, fmt.Errorf("sequence %s already exists", name)
	}
	for _, sceneID := range holidaySequences(bundle)[name] {
		values := SceneValues(bundle, sceneID)
		var step ColorStep
		var previous string
		for _, id := range mapKeys(values) {
			state := values[id]
			if state["state"] != "on" || state["effect"] != nil {
				return Bundle{}, fmt.Errorf("scene %s uses off states or effects; keep it as a legacy scene", sceneID)
			}
			rgb, _, ok := sceneDisplayRGB(state)
			if !ok {
				return Bundle{}, fmt.Errorf("scene %s has no supported color", sceneID)
			}
			brightness := 255
			if state["brightness"] != nil {
				brightness = int(number(state["brightness"]))
			}
			step = ColorStep{Name: sequenceSceneName(bundle, sceneID), RGB: []int{rgb[0], rgb[1], rgb[2]}, Brightness: brightness, Hold: 6, Transition: .5}
			for _, mode := range []string{"xy_color", "hs_color"} {
				if raw, ok := state[mode]; ok {
					step.Color = map[string]any{mode: raw}
					break
				}
			}
			current := stringMustJSON(step)
			if previous != "" && current != previous {
				return Bundle{}, fmt.Errorf("scene %s has different colors per light; keep it as a legacy scene", sceneID)
			}
			previous = current
		}
		if len(values) == 0 {
			return Bundle{}, fmt.Errorf("import scene %s before converting this sequence", sceneID)
		}
		s.Steps = append(s.Steps, step)
	}
	return saveColorSequence(bundle, s)
}
