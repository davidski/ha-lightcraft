package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func testColorSequence() ColorSequence {
	return ColorSequence{ID: "christmas", Name: "Christmas", Repeat: true, Steps: []ColorStep{
		{Name: "Red", RGB: []int{255, 0, 0}, Brightness: 200, Hold: 6, Transition: .5},
		{Name: "Green", RGB: []int{0, 255, 0}, Brightness: 180, Hold: 3, Transition: 1},
	}}
}

func testLightingAssignment() LightingAssignment {
	return LightingAssignment{ID: "exterior", Name: "Exterior", Sequence: "christmas", Targets: []string{"light.accent_one", "light.accent_two", "light.accent_three", "light.front_door"}, Start: "12-01", End: "12-31", On: "sunset", Off: "00:00", Finish: "off", Enabled: true}
}

func testLightingBundle(t *testing.T) Bundle {
	t.Helper()
	b, err := saveColorSequence(Bundle{}, testColorSequence())
	if err != nil {
		t.Fatal(err)
	}
	b, err = saveLightingAssignment(b, testLightingAssignment())
	if err != nil {
		t.Fatal(err)
	}
	a := testLightingAssignment()
	a.ID, a.Name, a.Targets, a.On = "living_room", "Living room", []string{"light.living_room_wled"}, "17:00"
	b, err = saveLightingAssignment(b, a)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLightingNativeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	bundle := testLightingBundle(t)
	if err := SaveBundle(dir, materializeNativeBundle(bundle)); err != nil {
		t.Fatal(err)
	}
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: "."}
	imported, err := store.Import(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBundle(imported); err != nil {
		t.Fatal(err)
	}
	if len(colorSequences(imported)) != 1 || len(lightingAssignments(imported)) != 2 {
		t.Fatal("fresh HA import lost the project")
	}
	s := colorSequences(imported)["christmas"]
	s.Steps[0].Hold = 12
	draft, err := saveColorSequence(imported, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(context.Background(), draft, imported, bundleRefs(imported), nil, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.Import(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if colorSequences(fresh)["christmas"].Steps[0].Hold != 12 {
		t.Fatal("published sequence did not survive fresh import")
	}
	if strings.Contains(stringMustJSON(fresh.Files), "colors.yaml") {
		t.Fatal("unexpected local palette dependency")
	}
}

func TestLightingValidationAndOverlap(t *testing.T) {
	a := testLightingAssignment()
	b := a
	b.ID, b.Name = "other", "Other"
	if !assignmentsOverlap(a, b) {
		t.Fatal("missed duplicate lighting assignment")
	}
	b.Start, b.End = "01-01", "01-31"
	if assignmentsOverlap(a, b) {
		t.Fatal("disjoint dates overlap")
	}
	b.Start, b.End = "12-15", "01-15"
	if !assignmentsOverlap(a, b) {
		t.Fatal("missed cross-year overlap")
	}
	b.Targets = []string{"light.wled"}
	if assignmentsOverlap(a, b) {
		t.Fatal("independent lights overlap")
	}
	b = a
	b.On, b.Off = "06:00", "08:00"
	if assignmentsOverlap(a, b) {
		t.Fatal("disjoint hours overlap")
	}
	a.On, a.Off = "22:00", "02:00"
	b.On, b.Off = "01:00", "03:00"
	if !assignmentsOverlap(a, b) {
		t.Fatal("missed overnight overlap")
	}
	a.Start, a.End = "2026-01-01", "2040-01-01"
	b.Start, b.End = "2038-12-01", "2038-12-31"
	if !assignmentsOverlap(a, b) {
		t.Fatal("missed distant one-off overlap")
	}
	for _, mutate := range []func(*LightingAssignment){func(a *LightingAssignment) { a.Start = "02-30" }, func(a *LightingAssignment) { a.End = "2026-12-31" }, func(a *LightingAssignment) { a.On = "1:00" }, func(a *LightingAssignment) { a.Targets = []string{"switch.x"} }} {
		a = testLightingAssignment()
		mutate(&a)
		if a.Validate() == nil {
			t.Fatalf("accepted invalid assignment %#v", a)
		}
	}
	a = testLightingAssignment()
	a.Start, a.End = "02-29", "02-29"
	if a.Validate() != nil {
		t.Fatal("annual leap day rejected")
	}
	date, _ := time.Parse("2006-01-02 15:04", "2028-02-29 23:59")
	if !assignmentDateActive(a, date) {
		t.Fatal("final date not inclusive")
	}
	bundle := testLightingBundle(t)
	a = testLightingAssignment()
	a.ID = "conflict"
	if _, err := saveLightingAssignment(bundle, a); err == nil {
		t.Fatal("saved an overlapping assignment")
	}
	broken, _ := cloneBundle(bundle)
	scripts := broken.Files[Scripts].Data.(map[string]any)
	scripts[sequencePrefix+"christmas"].(map[string]any)["variables"] = map[string]any{"sequence_data": "invalid"}
	if ValidateBundle(broken) == nil {
		t.Fatal("malformed managed script accepted")
	}
}

func TestLightingGroupTargetsAndLegacyImport(t *testing.T) {
	states := map[string]LightState{
		"light.group": {Attribute: map[string]any{"entity_id": []any{"light.one", "light.two"}}},
		"light.one":   {Attribute: map[string]any{"supported_color_modes": []any{"xy"}}},
		"light.two":   {Attribute: map[string]any{"supported_color_modes": []any{"rgbww"}}},
	}
	targets, err := resolveLightingTargets([]string{"light.group", "light.one"}, states)
	if err != nil || strings.Join(targets, ",") != "light.one,light.two" {
		t.Fatalf("group resolution: %v %v", targets, err)
	}
	states["light.two"] = LightState{Attribute: map[string]any{"supported_color_modes": []any{"onoff"}}}
	if _, err := resolveLightingTargets([]string{"light.group"}, states); err == nil {
		t.Fatal("non-color target accepted")
	}
	bundle := sequenceBundle()
	red := NewXYScene("red", "Red", []string{"light.one"}, .6, .3, 200)
	white := NewXYScene("white", "White", []string{"light.one"}, .3, .3, 200)
	bundle, _ = UpsertScene(bundle, red)
	bundle, _ = UpsertScene(bundle, white)
	before := stringMustJSON(bundle.Files)
	converted, err := convertLegacySequence(bundle, "Christmas")
	if err != nil {
		t.Fatal(err)
	}
	s := colorSequences(converted)["christmas"]
	if len(s.Steps) != 3 || s.Steps[0].Color["xy_color"] == nil {
		t.Fatal("lost ordered duplicate colors or native XY")
	}
	if stringMustJSON(bundle.Files) != before {
		t.Fatal("conversion changed original scenes/script")
	}
}

func TestLightingWorkspaceAndControlConfirmation(t *testing.T) {
	bundle := testLightingBundle(t)
	api := &controlTestAPI{states: map[string]LightState{"input_select.lighting_assignment_exterior": {State: "running"}}}
	m := statusModel{bundle: bundle, baseline: &bundle, stateAPI: api, dashboardFocus: 1}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("3")}, {Type: tea.KeyEnter}} {
		updated, _ := m.Update(key)
		m = updated.(statusModel)
	}
	if !m.planner || m.lighting.pane != 1 {
		t.Fatal("Schedules is not independently reachable")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(statusModel)
	if cmd != nil || len(api.calls) != 0 || m.lighting.form != "pause" {
		t.Fatal("pause bypassed confirmation")
	}
	m.lighting.fields[0].SetValue("PAUSE")
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if cmd == nil {
		t.Fatal("confirmed pause did not run")
	}
	result := cmd().(lightingControlMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if len(api.data) != 1 || api.data[0]["option"] != "paused" {
		t.Fatalf("wrong control call: %#v", api.calls)
	}
}

type controlTestAPI struct {
	states map[string]LightState
	calls  []string
	data   []map[string]any
}

func (a *controlTestAPI) States(_ context.Context, entities []string) (map[string]LightState, error) {
	result := map[string]LightState{}
	for _, id := range entities {
		if state, ok := a.states[id]; ok {
			result[id] = state
		}
	}
	return result, nil
}
func (a *controlTestAPI) CallService(_ context.Context, domain, service, entity string, data map[string]any) error {
	a.calls = append(a.calls, domain+"."+service+" "+entity)
	a.data = append(a.data, data)
	return nil
}

func TestLightingEditorFlow(t *testing.T) {
	m := statusModel{bundle: testLightingBundle(t), draftDir: t.TempDir(), width: 80, height: 24}
	m.openLighting(0)
	m.editLightingSequence("")
	m.dirty = true
	m.lighting.fields[0].SetValue("Se")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(statusModel)
	if m.prompt != "" || m.lighting.fields[0].Value() != "Seq" {
		t.Fatal("typing q opened quit confirmation")
	}
	m.lighting.fields[0].SetValue("Winter")
	m.saveLightingForm()
	if !m.lighting.steps || m.lighting.sequence.ID != "winter" {
		t.Fatal("new sequence did not open its steps")
	}
	m.bundle, _ = upsertColor(m.bundle, "white", ColorDefinition{Name: "White", X: .3127, Y: .329})
	m.editLightingStep(0)
	m.lighting.fields[1].SetValue("missing")
	m.saveLightingForm()
	if m.lighting.error == "" || m.lighting.form != "step" {
		t.Fatal("bad input closed the editor")
	}
	m.lighting.fields[1].SetValue("white")
	m.saveLightingForm()
	if colorSequences(m.bundle)["winter"].Steps[0].RGB[0] != 255 {
		t.Fatal("color edit lost")
	}
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(statusModel)
	if m.lighting.fields[1].Value() != "winter" {
		t.Fatal("assignment lost selected sequence")
	}
	m.lighting.fields[0].SetValue("Indoor winter")
	m.lighting.fields[2].SetValue("light.indoor")
	m.saveLightingForm()
	if m.lighting.error != "" {
		t.Fatal(m.lighting.error)
	}
	if len(lightingAssignments(m.bundle)) != 3 {
		t.Fatal("assignment not saved")
	}
	m.editLightingAssignment("indoor_winter")
	view := m.renderLighting()
	if strings.Count(view, "│") != 9 || !strings.Contains(view, "│ Indoor winter") || !strings.Contains(view, "First date") || !strings.Contains(view, "[x] enabled  [ ] disabled") {
		t.Fatalf("schedule form is not rendered as nine label/value rows: %s", view)
	}
	m.lighting.focus = 5
	m.lighting.fields[5].Focus()
	m.lighting.fields[5].SetValue("")
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(statusModel)
	if m.lighting.error != "Daily start must be HH:MM or sunset." {
		t.Fatalf("invalid daily start was not rejected immediately: %q", m.lighting.error)
	}
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.lighting.focus != 5 {
		t.Fatal("invalid daily start advanced to the next field")
	}
	m.lighting.focus = 8
	m.lighting.fields[8].Focus()
	beforeSchedule := m.lighting.fields[8].Value()
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(statusModel)
	if m.lighting.fields[8].Value() != beforeSchedule {
		t.Fatal("schedule checkbox accepted free text")
	}
	for _, size := range [][2]int{{80, 24}, {60, 18}, {100, 30}} {
		m.width, m.height = size[0], size[1]
		for i := range m.lighting.fields {
			m.lighting.focus = i
			view := m.renderLighting()
			if lipgloss.Height(strings.TrimSuffix(view, "\n")) > m.height {
				t.Fatalf("form exceeds %dx%d: %s", m.width, m.height, view)
			}
			if !strings.Contains(view, "ESC cancel") {
				t.Fatal("cancel hidden")
			}
		}
	}
	before := stringMustJSON(m.bundle.Files)
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(statusModel)
	if before != stringMustJSON(m.bundle.Files) {
		t.Fatal("cancel changed draft")
	}
}

// Optional installed-HA validation. It runs an isolated Python process and never
// calls the live HA service bus or loads the production configuration.
func TestLightingHASchema(t *testing.T) {
	host := os.Getenv("LIGHTING_HA_CHECK_HOST")
	if host == "" {
		t.Skip("set LIGHTING_HA_CHECK_HOST to validate against an installed HA container")
	}
	code, err := os.ReadFile("testdata/check_lighting_ha.py")
	if err != nil {
		t.Fatal(err)
	}
	bundle := testLightingBundle(t)
	a := testLightingAssignment()
	a.ID, a.Name, a.Start, a.End, a.On = "january", "January", "01-01", "01-31", "00:00"
	a.Off = "23:59"
	bundle, err = saveLightingAssignment(bundle, a)
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"scripts": bundle.Files[Scripts].Data, "automations": bundle.Files[Automations].Data, "helpers": bundle.Files[Helpers].Data, "active": assignmentActiveTemplate()}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", host, "docker exec -i homeassistant python -B -c "+shellQuote(string(code)))
	command.Stdin = strings.NewReader(string(data))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("HA validation: %v\n%s", err, output)
	}
	t.Log(string(output))
	if dir := os.Getenv("LIGHTING_REVIEW_DIR"); dir != "" {
		if err := SaveBundle(filepath.Clean(dir), bundle); err != nil {
			t.Fatal(err)
		}
	}
}
