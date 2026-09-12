package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const sequencePrefix = "lighting_sequence_"
const assignmentPrefix = "lighting_assignment_"
const lightingControllerID = "lighting_assignments"

// These values are native script variables, consumed by the HA player.
type ColorStep struct {
	Name       string    `json:"name"`
	XY         []float64 `json:"xy"`
	Brightness int       `json:"brightness"`
	Hold       float64   `json:"hold"`
	Transition float64   `json:"transition"`
}

type ColorSequence struct {
	ID     string      `json:"-"`
	Name   string      `json:"-"`
	Repeat bool        `json:"repeat"`
	Steps  []ColorStep `json:"steps"`
}

type WLEDProgram struct {
	Light  string `json:"light"`
	Select string `json:"select"`
	Option string `json:"option"`
}

type LightingAssignment struct {
	ID       string       `json:"-"`
	Name     string       `json:"-"`
	Sequence string       `json:"sequence,omitempty"`
	Targets  []string     `json:"targets,omitempty"`
	Start    string       `json:"start"`
	End      string       `json:"end"`
	On       string       `json:"on"`     // HH:MM or sunset
	Off      string       `json:"off"`    // HH:MM
	Finish   string       `json:"finish"` // off or leave
	Enabled  bool         `json:"enabled"`
	WLED     *WLEDProgram `json:"wled,omitempty"`
	AllDay   bool         `json:"all_day,omitempty"`
}

var entityName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var annualDatePattern = regexp.MustCompile(`^\d{1,2}-\d{1,2}$`)
var datedDatePattern = regexp.MustCompile(`^\d{4}-\d{1,2}-\d{1,2}$`)

func (s ColorSequence) Validate() error {
	if !entityName.MatchString(s.ID) || strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("sequence needs a name and a lowercase identifier")
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("add at least one color step")
	}
	for i, step := range s.Steps {
		if strings.TrimSpace(step.Name) == "" || len(step.XY) != 2 || math.IsNaN(step.XY[0]) || math.IsInf(step.XY[0], 0) || math.IsNaN(step.XY[1]) || math.IsInf(step.XY[1], 0) || step.XY[0] < 0 || step.XY[1] <= 0 || step.XY[0]+step.XY[1] > 1 {
			return fmt.Errorf("step %d needs a name and valid XY color", i+1)
		}
		if step.Brightness < 1 || step.Brightness > 255 || math.IsNaN(step.Hold) || math.IsInf(step.Hold, 0) || step.Hold < 1 || step.Hold > 86400 || math.IsNaN(step.Transition) || math.IsInf(step.Transition, 0) || step.Transition < 0 || step.Transition > step.Hold {
			return fmt.Errorf("step %d: brightness 1–255, hold 1–86400 seconds, transition 0–hold", i+1)
		}
	}
	return nil
}

func (a LightingAssignment) Validate() error {
	if !entityName.MatchString(a.ID) || strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("assignment needs a name")
	}
	if a.WLED != nil {
		if len(a.Targets) > 0 || a.Sequence != "" {
			return fmt.Errorf("WLED assignments cannot include a color sequence or Hue lights")
		}
		if err := validateEntityID(a.WLED.Light, "light"); err != nil {
			return fmt.Errorf("WLED light: %w", err)
		}
		if err := validateEntityID(a.WLED.Select, "select"); err != nil {
			return fmt.Errorf("WLED selector: %w", err)
		}
		if strings.TrimSpace(a.WLED.Option) == "" {
			return fmt.Errorf("WLED option is required")
		}
	} else {
		if !entityName.MatchString(a.Sequence) {
			return fmt.Errorf("assignment needs a name and sequence")
		}
		if len(a.Targets) == 0 {
			return fmt.Errorf("select at least one light")
		}
		seen := map[string]bool{}
		for _, id := range a.Targets {
			if err := validateEntityID(id, "light"); err != nil || seen[id] {
				return fmt.Errorf("invalid or duplicate light %q", id)
			}
			seen[id] = true
		}
	}
	start, annualStart, err := normalizeScheduleDate(a.Start)
	if err != nil {
		return fmt.Errorf("start date: use M-D annually or YYYY-M-D once")
	}
	end, annualEnd, err := normalizeScheduleDate(a.End)
	if err != nil || annualStart != annualEnd {
		return fmt.Errorf("end date must use the same format as start")
	}
	if !annualStart {
		startDate, _ := time.Parse("2006-01-02", start)
		endDate, _ := time.Parse("2006-01-02", end)
		if endDate.Before(startDate) {
			return fmt.Errorf("end date precedes start date")
		}
	}
	if !a.AllDay && a.On != "sunset" {
		if parsed, err := time.Parse("15:04", a.On); err != nil || parsed.Format("15:04") != a.On {
			return fmt.Errorf("start time: use HH:MM or sunset")
		}
	}
	if !a.AllDay {
		if parsed, err := time.Parse("15:04", a.Off); err != nil || parsed.Format("15:04") != a.Off {
			return fmt.Errorf("stop time: use HH:MM")
		}
		if a.On == a.Off {
			return fmt.Errorf("start and stop time must differ")
		}
	}
	if a.Finish != "off" && a.Finish != "leave" {
		return fmt.Errorf("end behavior must be off or leave")
	}
	return nil
}

func validateEntityID(value, domain string) error {
	prefix := domain + "."
	if !strings.HasPrefix(value, prefix) || !entityName.MatchString(strings.TrimPrefix(value, prefix)) {
		return fmt.Errorf("must be a %s entity", domain)
	}
	return nil
}

func normalizeScheduleDate(value string) (string, bool, error) {
	value = strings.TrimSpace(value)
	if annualDatePattern.MatchString(value) {
		parts := strings.Split(value, "-")
		month, _ := strconv.Atoi(parts[0])
		day, _ := strconv.Atoi(parts[1])
		parsed, err := time.Parse("2006-01-02", fmt.Sprintf("2000-%02d-%02d", month, day))
		if err != nil {
			return "", true, err
		}
		return parsed.Format("01-02"), true, nil
	}
	if datedDatePattern.MatchString(value) {
		parts := strings.Split(value, "-")
		year, _ := strconv.Atoi(parts[0])
		month, _ := strconv.Atoi(parts[1])
		day, _ := strconv.Atoi(parts[2])
		parsed, err := time.Parse("2006-01-02", fmt.Sprintf("%04d-%02d-%02d", year, month, day))
		if err != nil {
			return "", false, err
		}
		return parsed.Format("2006-01-02"), false, nil
	}
	return "", false, fmt.Errorf("invalid date")
}

func normalizeLightingAssignmentDates(a LightingAssignment) (LightingAssignment, error) {
	start, annualStart, err := normalizeScheduleDate(a.Start)
	if err != nil {
		return LightingAssignment{}, fmt.Errorf("start date: use M-D annually or YYYY-M-D once")
	}
	end, annualEnd, err := normalizeScheduleDate(a.End)
	if err != nil || annualStart != annualEnd {
		return LightingAssignment{}, fmt.Errorf("end date must use the same format as start")
	}
	a.Start, a.End = start, end
	return a, nil
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

// decodeColorSequence keeps old RGB-only sequence files readable while XY is
// the only representation written by the editor.
func decodeColorSequence(value any, colors map[string]ColorDefinition, target *ColorSequence) error {
	var wire struct {
		Repeat bool `json:"repeat"`
		Steps  []struct {
			Name       string    `json:"name"`
			XY         []float64 `json:"xy"`
			RGB        []int     `json:"rgb"`
			Brightness int       `json:"brightness"`
			Hold       float64   `json:"hold"`
			Transition float64   `json:"transition"`
		} `json:"steps"`
	}
	if err := decodeLighting(value, &wire); err != nil {
		return err
	}
	target.Repeat = wire.Repeat
	target.Steps = make([]ColorStep, 0, len(wire.Steps))
	for _, step := range wire.Steps {
		xy := step.XY
		if len(xy) != 2 && len(step.RGB) == 3 {
			for _, color := range colors {
				if strings.EqualFold(strings.TrimSpace(step.Name), strings.TrimSpace(color.Name)) {
					xy = []float64{color.X, color.Y}
					break
				}
			}
			if len(xy) != 2 {
				x, y, ok := rgbToXY([3]int{step.RGB[0], step.RGB[1], step.RGB[2]})
				if ok {
					xy = []float64{x, y}
				}
			}
		}
		xy = normalizeXYPair(xy)
		target.Steps = append(target.Steps, ColorStep{Name: step.Name, XY: xy, Brightness: step.Brightness, Hold: step.Hold, Transition: step.Transition})
	}
	return nil
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
		if decodeColorSequence(vars["sequence_data"], colorDefinitions(bundle), &s) == nil {
			s.ID = strings.TrimPrefix(id, sequencePrefix)
			s.Name, _ = entry["alias"].(string)
			result[s.ID] = s
		}
	}
	return result
}

func legacyRGBSequenceEntry(entry any) bool {
	value, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	vars, _ := value["variables"].(map[string]any)
	data, _ := vars["sequence_data"].(map[string]any)
	steps, _ := data["steps"].([]any)
	if len(steps) == 0 {
		return false
	}
	for _, raw := range steps {
		step, ok := raw.(map[string]any)
		if !ok {
			return false
		}
		if _, hasRGB := step["rgb"]; !hasRGB {
			return false
		}
		if _, hasXY := step["xy"]; hasXY {
			return false
		}
	}
	return true
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
			if normalized, err := normalizeLightingAssignmentDates(a); err == nil {
				a = normalized
			}
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
	if a.WLED != nil {
		return map[string]any{
			"alias": a.Name, "mode": "single", "max_exceeded": "silent",
			"variables": map[string]any{"assignment": jsonValue(a)},
			"sequence": []any{
				map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": a.WLED.Light}},
				map[string]any{"action": "select.select_option", "target": map[string]any{"entity_id": a.WLED.Select}, "data": map[string]any{"option": a.WLED.Option}},
				map[string]any{"wait_template": "{{ false }}"},
			},
		}
	}
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
							map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": "{{ assignment.targets }}"}, "data": "{{ dict(xy_color=repeat.item.xy, brightness=repeat.item.brightness, transition=repeat.item.transition) }}"},
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
{% set hours = true if assignment.all_day else ((is_state('sun.sun', 'below_horizon') and night) if assignment.on == 'sunset' else ((assignment.on <= t < assignment.off) if assignment.on < assignment.off else (t >= assignment.on or t < assignment.off))) %}
{{ assignment.enabled and dates and hours }}`
}

func assignmentTargetLights(a LightingAssignment) []string {
	if a.WLED != nil {
		return []string{a.WLED.Light}
	}
	return a.Targets
}

func assignmentResources(a LightingAssignment) []string {
	resources := append([]string(nil), assignmentTargetLights(a)...)
	if a.WLED != nil {
		resources = append(resources, a.WLED.Select)
	}
	return resources
}

func assignmentStopActions(a LightingAssignment) []any {
	player := "script." + assignmentPrefix + a.ID
	status := "input_select." + assignmentPrefix + a.ID
	stop := []any{map[string]any{"action": "script.turn_off", "target": map[string]any{"entity_id": player}}}
	if a.Finish == "off" {
		stop = append(stop, map[string]any{"action": "light.turn_off", "target": map[string]any{"entity_id": assignmentTargetLights(a)}})
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
	triggers := []any{map[string]any{"trigger": "homeassistant", "event": "start"}, map[string]any{"trigger": "time_pattern", "minutes": "/5"}, map[string]any{"trigger": "event", "event_type": "automation_reloaded"}}
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
	values, _ := config.Data.([]any)
	for i, value := range values {
		if entry, ok := value.(map[string]any); ok && entry["id"] == id {
			values[i] = automation
			config.Data = values
			result.Files[Automations] = config
			return withHash(result)
		}
	}
	config.Data = append(values, automation)
	result.Files[Automations] = config
	return withHash(result)
}

func saveColorSequence(bundle Bundle, s ColorSequence) (Bundle, error) {
	for i := range s.Steps {
		s.Steps[i].XY = normalizeXYPair(s.Steps[i].XY)
	}
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
	normalized, err := normalizeLightingAssignmentDates(a)
	if err != nil {
		return Bundle{}, err
	}
	a = normalized
	if a.WLED == nil {
		if _, ok := colorSequences(bundle)[a.Sequence]; !ok {
			return Bundle{}, fmt.Errorf("sequence %q is missing", a.Sequence)
		}
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

const scheduleAgendaDays = 14

type scheduleAgendaItem struct {
	ID      string
	Name    string
	Time    string
	Targets string
}

type scheduleAgendaDay struct {
	Date    string
	Weekday string
	Items   []scheduleAgendaItem
}

func normalizeAgendaDate(date time.Time) time.Time {
	if date.IsZero() {
		date = time.Now()
	}
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
}

func parseAgendaDate(value string) time.Time {
	date, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return normalizeAgendaDate(time.Time{})
	}
	return normalizeAgendaDate(date)
}

func scheduleAgenda(assignments map[string]LightingAssignment, start time.Time) []scheduleAgendaDay {
	start = normalizeAgendaDate(start)
	result := make([]scheduleAgendaDay, 0, scheduleAgendaDays)
	for offset := 0; offset < scheduleAgendaDays; offset++ {
		date := start.AddDate(0, 0, offset)
		day := scheduleAgendaDay{Date: date.Format("Jan 2, 2006"), Weekday: date.Format("Mon")}
		for _, assignment := range assignments {
			if assignment.Enabled && assignmentDateActive(assignment, date) {
				day.Items = append(day.Items, scheduleAgendaItem{
					ID:      assignment.ID,
					Name:    assignment.Name,
					Time:    assignmentTimeLabel(assignment),
					Targets: assignmentTargetLabel(assignment),
				})
			}
		}
		sort.SliceStable(day.Items, func(i, j int) bool {
			return strings.ToLower(day.Items[i].Name) < strings.ToLower(day.Items[j].Name)
		})
		result = append(result, day)
	}
	return result
}

func assignmentTimeLabel(a LightingAssignment) string {
	if a.AllDay {
		return "all day"
	}
	return a.On + "–" + a.Off
}

func assignmentTargetLabel(a LightingAssignment) string {
	if a.WLED != nil {
		return "WLED"
	}
	if len(a.Targets) == 0 {
		return ""
	}
	if len(a.Targets) == 1 {
		return "1 light"
	}
	return fmt.Sprintf("%d lights", len(a.Targets))
}

func assignmentMinutes(a LightingAssignment, minute int) bool {
	if a.AllDay {
		return true
	}
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
	for _, target := range assignmentResources(a) {
		if containsString(assignmentResources(b), target) {
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
		if stringMustJSON(entries[ConfigRef{Scripts, sequencePrefix + id}]) != stringMustJSON(sequenceScript(s)) && !legacyRGBSequenceEntry(entries[ConfigRef{Scripts, sequencePrefix + id}]) {
			return fmt.Errorf("sequence %s has unsupported script edits; preserve it outside the structured editor", id)
		}
	}
	for id, a := range assignments {
		if err := a.Validate(); err != nil {
			return fmt.Errorf("assignment %s: %w", id, err)
		}
		if a.WLED == nil {
			if _, ok := sequences[a.Sequence]; !ok {
				return fmt.Errorf("assignment %s references missing sequence %s", id, a.Sequence)
			}
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
