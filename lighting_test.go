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
		{Name: "Red", XY: []float64{.64, .33}, Brightness: 200, Hold: 6, Transition: .5},
		{Name: "Green", XY: []float64{.30, .60}, Brightness: 180, Hold: 3, Transition: 1},
	}}
}

func TestSequenceScriptPublishesXY(t *testing.T) {
	data := stringMustJSON(assignmentScript(testLightingAssignment()))
	if !strings.Contains(data, "xy_color") || strings.Contains(data, "rgb_color") {
		t.Fatalf("sequence script is not XY-based: %s", data)
	}
	sequence := stringMustJSON(sequenceScript(testColorSequence()))
	if !strings.Contains(sequence, `"xy":[0.64,0.33]`) {
		t.Fatalf("sequence data is not XY-based: %s", sequence)
	}
}

func TestColorSequencesConvertsLegacyRGB(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{Scripts: {Kind: Scripts, Data: map[string]any{
		sequencePrefix + "old": map[string]any{"alias": "Old", "variables": map[string]any{"sequence_data": map[string]any{
			"repeat": true, "steps": []any{map[string]any{"name": "Red", "rgb": []any{255, 0, 0}, "brightness": 100, "hold": 1, "transition": 0}},
		}}},
	}}}}
	step := colorSequences(bundle)["old"].Steps[0]
	if len(step.XY) != 2 || step.XY[0] <= 0.6 {
		t.Fatalf("legacy RGB was not converted to XY: %#v", step)
	}
}

func TestColorSequencesPreferCatalogXYForLegacyNamedSteps(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{
		Colors: {Kind: Colors, Data: map[string]any{"blue": map[string]any{"name": "American blue", "x": .1912978877, "y": .2086338447}}},
		Scripts: {Kind: Scripts, Data: map[string]any{sequencePrefix + "old": map[string]any{"alias": "Old", "variables": map[string]any{"sequence_data": map[string]any{
			"repeat": true, "steps": []any{map[string]any{"name": "American blue", "rgb": []any{0, 255, 255}, "brightness": 100, "hold": 1, "transition": 0}},
		}}}}},
	}}
	step := colorSequences(bundle)["old"].Steps[0]
	if len(step.XY) != 2 || step.XY[0] < .19 || step.XY[0] > .192 || step.XY[1] < .208 || step.XY[1] > .209 {
		t.Fatalf("legacy named step did not use catalog XY: %#v", step)
	}
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

func TestScheduleAgendaUsesEffectiveDatesAndCompactTargets(t *testing.T) {
	start := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.Local)
	agenda := scheduleAgenda(map[string]LightingAssignment{
		"annual": {Name: "Annual", Start: "12-01", End: "12-03", On: "sunset", Off: "00:00", Enabled: true, Targets: []string{"light.one", "light.two"}},
		"dated":  {Name: "Dated", Start: "2026-12-02", End: "2026-12-02", AllDay: true, Enabled: true, Targets: []string{"light.one"}},
		"off":    {Name: "Disabled", Start: "12-01", End: "12-03", Enabled: false},
	}, start)
	if len(agenda) != scheduleAgendaDays || len(agenda[0].Items) != 1 || agenda[0].Items[0].Name != "Annual" || agenda[0].Items[0].Targets != "2 lights" {
		t.Fatalf("day one agenda = %#v", agenda[0])
	}
	if len(agenda[1].Items) != 2 || agenda[1].Items[0].Name != "Annual" || agenda[1].Items[1].Time != "all day" || agenda[1].Items[1].Targets != "1 light" {
		t.Fatalf("day two agenda = %#v", agenda[1])
	}
}

func TestLightingNativeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	bundle := testLightingBundle(t)
	var err error
	bundle, err = saveLightingAssignment(bundle, testWLEDAssignment())
	if err != nil {
		t.Fatal(err)
	}
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
	if len(colorSequences(imported)) != 1 || len(lightingAssignments(imported)) != 3 || lightingAssignments(imported)["floating_string"].WLED == nil {
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

func TestLightingAssignmentAcceptsUnpaddedDates(t *testing.T) {
	a := testLightingAssignment()
	a.ID, a.Name = "unpadded", "Unpadded"
	a.Targets = []string{"light.other"}
	a.Start, a.End = "1-15", "2-3"
	if err := a.Validate(); err != nil {
		t.Fatalf("unpadded dates rejected: %v", err)
	}
	bundle := testLightingBundle(t)
	bundle, err := saveLightingAssignment(bundle, a)
	if err != nil {
		t.Fatalf("saving unpadded dates: %v", err)
	}
	saved := lightingAssignments(bundle)["unpadded"]
	if saved.Start != "01-15" || saved.End != "02-03" {
		t.Fatalf("saved dates = %q–%q, want 01-15–02-03", saved.Start, saved.End)
	}
}

func TestLightingGroupTargets(t *testing.T) {
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
}

func TestLightingWorkspaceAndControlConfirmation(t *testing.T) {
	bundle := testLightingBundle(t)
	api := &controlTestAPI{states: map[string]LightState{"input_select.lighting_assignment_exterior": {State: "running"}}}
	m := statusModel{bundle: bundle, baseline: &bundle, stateAPI: api, dashboardFocus: 1}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = updated.(statusModel)
	dashboard := func() string {
		original := selectedStyle
		selectedStyle = selectedStyle.Transform(func(value string) string { return "<selected>" + value + "</selected>" })
		defer func() { selectedStyle = original }()
		return m.renderDashboardWorkspace(80)
	}()
	if !strings.Contains(dashboard, "<selected>❯ Exterior") {
		t.Fatalf("focused dashboard schedule is not highlighted: %s", dashboard)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if !m.planner || m.lighting.pane != 1 {
		t.Fatal("Schedules is not independently reachable")
	}
	if m.lighting.form != "assignment" || m.lighting.assignment.ID != "exterior" {
		t.Fatal("dashboard schedule did not open directly for editing")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(statusModel)
	if m.planner || m.dashboardWorkspace != 4 {
		t.Fatal("cancelled dashboard schedule did not return to the dashboard")
	}
	m.openLighting(1)
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

func TestSequencePreviewCyclesStepsAndListsScheduledLights(t *testing.T) {
	bundle := testLightingBundle(t)
	m := statusModel{bundle: bundle, planner: true, lighting: lightingEditor{steps: true, sequence: testColorSequence()}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(statusModel)
	if !m.sequencePreview || cmd == nil {
		t.Fatal("sequence preview did not start")
	}
	view := m.View()
	if !strings.Contains(view, "light.accent_one") || !strings.Contains(view, previewRGBHex(testColorSequence().Steps[0])) || !strings.Contains(view, "brightness 78%") {
		t.Fatalf("preview = %s", view)
	}
	for i := 0; i < 6; i++ {
		updated, _ = m.Update(sequencePreviewTickMsg{})
		m = updated.(statusModel)
	}
	if !strings.Contains(m.View(), previewRGBHex(testColorSequence().Steps[1])) {
		t.Fatalf("preview did not advance: %s", m.View())
	}
}

func TestBrightnessEditingUsesPercentages(t *testing.T) {
	if got := brightnessPercentValue(200); got != 78 {
		t.Fatalf("brightness percent = %d, want 78", got)
	}
	if got := brightnessFromPercent(78); got != 199 {
		t.Fatalf("brightness model value = %d, want 199", got)
	}
}

func TestSequencePreviewUsesCurrentCatalogColor(t *testing.T) {
	bundle := testLightingBundle(t)
	bundle, _ = upsertColor(bundle, "red", ColorDefinition{Name: "Red", X: .2, Y: .2})
	m := statusModel{bundle: bundle, sequencePreview: true, previewSequence: testColorSequence()}
	if view := m.renderSequencePreview(); !strings.Contains(view, "XY (0.200, 0.200)") {
		t.Fatalf("TUI preview used the stored XY value: %s", view)
	}
}

func TestDashboardSequenceOpensDirectlyAndReturns(t *testing.T) {
	m := statusModel{bundle: testLightingBundle(t), dashboardFocus: 1, dashboardWorkspace: 1}
	dashboard := func() string {
		original := selectedStyle
		selectedStyle = selectedStyle.Transform(func(value string) string { return "<selected>" + value + "</selected>" })
		defer func() { selectedStyle = original }()
		return m.renderDashboardWorkspace(80)
	}()
	if !strings.Contains(dashboard, "<selected>❯ Christmas") {
		t.Fatalf("focused dashboard sequence is not highlighted: %s", dashboard)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if !m.planner || !m.lighting.steps || m.lighting.sequence.ID != "christmas" {
		t.Fatal("dashboard sequence did not open directly for editing")
	}
	view := m.renderLighting()
	if !strings.Contains(view, "\n❯ 1.") || !strings.Contains(view, "\n  2.") || strings.Contains(view, "Space toggle playback") || !strings.Contains(view, "[ and ] move") {
		t.Fatalf("sequence step markers are not aligned: %s", view)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(statusModel)
	if m.lighting.step != -1 || !strings.Contains(m.renderLighting(), "\n❯ Playback:") || !strings.Contains(m.renderLighting(), "←/h loop • →/l once") {
		t.Fatal("up from the first color step did not focus Playback")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(statusModel)
	if !m.lighting.sequence.Repeat || !colorSequences(m.bundle)["christmas"].Repeat {
		t.Fatal("left did not select loop for Playback")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(statusModel)
	if m.lighting.sequence.Repeat || colorSequences(m.bundle)["christmas"].Repeat {
		t.Fatal("right did not select once for Playback")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.lighting.form != "sequence" || m.lighting.focus != 1 {
		t.Fatal("focused Playback did not open for editing")
	}
	m.lighting.fields[0].SetValue("Changed name")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.lighting.form != "sequence" || m.lighting.focus != 0 || colorSequences(m.bundle)["christmas"].Name != "Christmas" {
		t.Fatal("final Enter unexpectedly saved the sequence")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(statusModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(statusModel)
	if m.lighting.step != -2 || !strings.Contains(m.renderLighting(), "\n❯ Name:") {
		t.Fatal("up from Playback did not focus Name")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(statusModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(statusModel)
	originalSelectedStyle := selectedStyle
	selectedStyle = selectedStyle.Transform(func(value string) string { return "<selected>" + value + "</selected>" })
	defer func() { selectedStyle = originalSelectedStyle }()
	highlighted := m.renderLighting()
	if !strings.Contains(highlighted, "<selected>❯ 1.") || strings.Contains(highlighted, "<selected>  2.") {
		t.Fatalf("focused sequence step is not highlighted exclusively: %s", highlighted)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(statusModel)
	if m.planner || m.dashboardWorkspace != 1 {
		t.Fatal("cancelled dashboard sequence did not return to the dashboard")
	}
}

func TestDashboardSequencePreviewStartsFromMainWorkspace(t *testing.T) {
	m := statusModel{bundle: testLightingBundle(t), dashboardFocus: 1, dashboardWorkspace: 1}
	if view := m.renderDashboard(); !strings.Contains(view, "p • preview selected sequence") {
		t.Fatalf("sequence navigation does not advertise preview: %s", view)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(statusModel)
	if !m.sequencePreview || cmd == nil || !strings.Contains(m.View(), "Sequence preview: Christmas") {
		t.Fatalf("main workspace preview did not start: preview=%v view=%s", m.sequencePreview, m.View())
	}
}

func TestDashboardWorkspaceOrderAndLabels(t *testing.T) {
	if workspaceAt(0) != 2 {
		t.Fatalf("default workspace = %d, want Colors (2)", workspaceAt(0))
	}
	view := (statusModel{bundle: testLightingBundle(t), dashboardWorkspace: 2}).renderDashboard()
	if strings.Index(view, "Colors") > strings.Index(view, "Sequences") || strings.Index(view, "Sequences") > strings.Index(view, "Schedules") || strings.Index(view, "Schedules") > strings.Index(view, "Publish") {
		t.Fatalf("workspace order is wrong: %s", view)
	}
	if strings.Contains(view, "1 Colors") || strings.Contains(view, "2 Sequences") || strings.Contains(view, "3 Schedules") || strings.Contains(view, "4 Publish") {
		t.Fatalf("workspace tabs still show number labels: %s", view)
	}
}

func TestDraftStatusDistinguishesLocalAndPublishedChanges(t *testing.T) {
	baseline := testLightingBundle(t)
	sequence := colorSequences(baseline)["christmas"]
	sequence.Steps[0].Hold++
	changed, err := saveColorSequence(baseline, sequence)
	if err != nil {
		t.Fatal(err)
	}
	m := statusModel{bundle: changed, baseline: &baseline, baselineImported: true, dirty: true}
	if got := m.draftStatus(); got != "unsaved in memory • unpublished" {
		t.Fatalf("draft status = %q", got)
	}
	m.dirty = false
	if got := m.draftStatus(); got != "unpublished" {
		t.Fatalf("saved draft status = %q", got)
	}
	m.bundle = baseline
	if got := m.draftStatus(); got != "" {
		t.Fatalf("published status = %q", got)
	}
}

func TestSequenceDeletionRequiresConfirmation(t *testing.T) {
	bundle, err := saveColorSequence(Bundle{}, testColorSequence())
	if err != nil {
		t.Fatal(err)
	}
	m := statusModel{bundle: bundle, dashboardWorkspace: 1, dashboardFocus: 1}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(statusModel)
	if m.prompt != "delete-sequence" || m.pending != "christmas" || m.confirmFocus != 1 {
		t.Fatalf("sequence delete did not ask for confirmation: prompt=%q pending=%q focus=%d", m.prompt, m.pending, m.confirmFocus)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(statusModel)
	if m.prompt != "" || len(colorSequences(m.bundle)) != 1 {
		t.Fatal("cancelling sequence deletion changed the draft")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(statusModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(statusModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.prompt != "" || len(colorSequences(m.bundle)) != 0 {
		t.Fatal("confirmed sequence deletion did not remove the sequence")
	}
}

func TestDashboardWorkspaceVimHorizontalNavigation(t *testing.T) {
	m := statusModel{bundle: testLightingBundle(t), dashboardWorkspace: 2}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(statusModel)
	if m.dashboardWorkspace != 1 {
		t.Fatalf("l moved to workspace %d, want Sequences (1)", m.dashboardWorkspace)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(statusModel)
	if m.dashboardWorkspace != 2 {
		t.Fatalf("h moved to workspace %d, want Colors (2)", m.dashboardWorkspace)
	}
}

func TestDashboardSequenceStepCountsAlign(t *testing.T) {
	bundle := testLightingBundle(t)
	sequence := testColorSequence()
	sequence.ID, sequence.Name = "winter", "Long winter sequence"
	bundle, _ = saveColorSequence(bundle, sequence)
	view := (statusModel{bundle: bundle, dashboardWorkspace: 1}).renderDashboardWorkspace(80)
	positions := []int{}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "2 steps") {
			positions = append(positions, strings.Index(line, "2 steps"))
		}
	}
	if len(positions) != 2 || positions[0] != positions[1] {
		t.Fatalf("sequence step counts are not aligned: %s", view)
	}
}

func TestScheduleAtStopOptions(t *testing.T) {
	bundle := testLightingBundle(t)
	e := lightingEditor{form: "assignment", focus: assignmentFinishField}
	if options := e.fieldOptions(bundle); strings.Join(options, ",") != "off,leave" {
		t.Fatalf("At stop options = %v", options)
	}
}

func testWLEDAssignment() LightingAssignment {
	return LightingAssignment{ID: "floating_string", Name: "Floating string", WLED: &WLEDProgram{Light: "light.floating_string", Select: "select.floating_string_preset", Option: "Christmas"}, Start: "11-15", End: "01-15", Finish: "off", Enabled: true, AllDay: true}
}

func TestWLEDProgramAssignmentAndAllDay(t *testing.T) {
	a := testWLEDAssignment()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if !assignmentMinutes(a, 0) || !assignmentMinutes(a, 1439) {
		t.Fatal("all-day assignment did not cover the full day")
	}
	script := stringMustJSON(assignmentScript(a))
	if !strings.Contains(script, "select.select_option") || !strings.Contains(script, "select.floating_string_preset") || !strings.Contains(script, "Christmas") || !strings.Contains(script, "wait_template") || strings.Contains(script, "lighting_sequence_") {
		t.Fatalf("WLED assignment script is incomplete: %s", script)
	}
	if strings.Contains(assignmentActiveTemplate(), "assignment.on <= t") == false || !strings.Contains(assignmentActiveTemplate(), "assignment.all_day") {
		t.Fatal("assignment activity template lost hourly or all-day behavior")
	}
	invalid := a
	invalid.Sequence = "christmas"
	if invalid.Validate() == nil {
		t.Fatal("accepted mixed WLED and color assignment")
	}
	invalid = a
	invalid.WLED = &WLEDProgram{Light: "switch.floating_string", Select: "select.floating_string_preset", Option: "Christmas"}
	if invalid.Validate() == nil {
		t.Fatal("accepted non-light WLED target")
	}
	hourly := testLightingAssignment()
	if !assignmentMinutes(hourly, 18*60) || assignmentMinutes(hourly, 8*60) {
		t.Fatal("existing hourly assignment behavior changed")
	}
}

func TestLightingControllerPollingInterval(t *testing.T) {
	controller := stringMustJSON(lightingController(testLightingBundle(t)))
	if !strings.Contains(controller, `"minutes":"/5"`) {
		t.Fatalf("lighting controller is not configured for five-minute polling: %s", controller)
	}
}

func TestWLEDAssignmentRoundTripAndResourceOverlap(t *testing.T) {
	bundle, err := saveLightingAssignment(Bundle{}, testWLEDAssignment())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBundle(bundle); err != nil {
		t.Fatal(err)
	}
	got := lightingAssignments(bundle)["floating_string"]
	if got.WLED == nil || got.WLED.Option != "Christmas" || !got.AllDay {
		t.Fatalf("WLED assignment did not round-trip: %#v", got)
	}
	other := testWLEDAssignment()
	other.ID, other.Name = "workshop", "Workshop"
	other.WLED = &WLEDProgram{Light: "light.workshop_pegboard", Select: "select.floating_string_preset", Option: "Christmas"}
	if !assignmentsOverlap(testWLEDAssignment(), other) {
		t.Fatal("shared WLED selector was not treated as a conflict resource")
	}
	other.WLED.Select = "select.workshop_pegboard_preset"
	if assignmentsOverlap(testWLEDAssignment(), other) {
		t.Fatal("independent WLED resources overlapped")
	}
}

func TestNativeValidationAllowsOfflineWLEDOptionReference(t *testing.T) {
	a := testWLEDAssignment()
	a.WLED.Option = "Removed from Home Assistant"
	bundle, err := saveLightingAssignment(Bundle{}, a)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBundle(bundle); err != nil {
		t.Fatalf("offline structural validation rejected a preserved option reference: %v", err)
	}
}

func assignmentSeparatorColumns(view string) []int {
	columns := []int{}
	for _, line := range strings.Split(view, "\n") {
		var prefix strings.Builder
		found := false
		for _, r := range line {
			if r == '│' {
				found = true
				break
			}
			prefix.WriteRune(r)
		}
		if found {
			columns = append(columns, lipgloss.Width(prefix.String()))
		}
	}
	return columns
}

func TestScheduleFormSeparatorsAlign(t *testing.T) {
	bundle := testLightingBundle(t)
	var err error
	bundle, err = saveLightingAssignment(bundle, testWLEDAssignment())
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		for _, test := range []struct {
			id   string
			rows int
		}{
			{id: "exterior", rows: 11},
			{id: "floating_string", rows: 12},
		} {
			m := statusModel{bundle: bundle, width: size[0], height: size[1]}
			m.editLightingAssignment(test.id)
			view := m.renderLighting()
			columns := assignmentSeparatorColumns(view)
			if len(columns) != test.rows {
				t.Fatalf("%dx%d %s form separators = %v, want %d rows\n%s", size[0], size[1], test.id, columns, test.rows, view)
			}
			for _, column := range columns[1:] {
				if column != columns[0] {
					t.Fatalf("%dx%d %s form separators are not aligned: %v\n%s", size[0], size[1], test.id, columns, view)
				}
			}
		}
	}
}

func TestScheduleEditSavesWithS(t *testing.T) {
	dir := t.TempDir()
	m := statusModel{bundle: testLightingBundle(t), draftDir: dir, width: 80, height: 24, planner: true}
	m.editLightingAssignment("exterior")
	m.lighting.focus = assignmentFinishField
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.lighting.form == "" || !strings.Contains(m.renderLighting(), "s save") || strings.Contains(m.renderLighting(), "Enter advances; s saves") {
		t.Fatal("final Enter unexpectedly saved the schedule")
	}
	m.lighting.fields[assignmentNameField].SetValue("Updated exterior")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(statusModel)
	if m.lighting.form != "" {
		t.Fatal("s did not save the schedule")
	}
	saved, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lightingAssignments(saved)["exterior"].Name != "Updated exterior" {
		t.Fatal("s did not persist the schedule")
	}
}

func TestColorEditSavesWithS(t *testing.T) {
	dir := t.TempDir()
	bundle, err := upsertColor(Bundle{}, "red", ColorDefinition{Name: "Red", X: .64, Y: .33})
	if err != nil {
		t.Fatal(err)
	}
	m := statusModel{bundle: bundle, draftDir: dir, prompt: "color", pending: "red", formValues: []string{"Red", "0.640", "0.330"}, formField: 0, input: "Crimson"}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(statusModel)
	if m.prompt != "" || colorDefinitions(m.bundle)["red"].Name != "Crimson" {
		t.Fatal("s did not apply the color form")
	}
	saved, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if colorDefinitions(saved)["red"].Name != "Crimson" {
		t.Fatal("s did not persist the color")
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
	m.lighting.fields[0].SetValue("missing")
	m.saveLightingForm()
	if m.lighting.error == "" || m.lighting.form != "step" {
		t.Fatal("bad input closed the editor")
	}
	m.lighting.fields[0].SetValue("white")
	m.saveLightingForm()
	if len(colorSequences(m.bundle)["winter"].Steps[0].XY) != 2 {
		t.Fatal("color edit lost")
	}
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(statusModel)
	if m.lighting.fields[assignmentSequenceField].Value() != "winter" {
		t.Fatal("assignment lost selected sequence")
	}
	m.lighting.fields[assignmentNameField].SetValue("Indoor winter")
	m.lighting.fields[assignmentLightsField].SetValue("light.indoor")
	m.saveLightingForm()
	if m.lighting.error != "" {
		t.Fatal(m.lighting.error)
	}
	if len(lightingAssignments(m.bundle)) != 3 {
		t.Fatal("assignment not saved")
	}
	m.editLightingAssignment("indoor_winter")
	view := m.renderLighting()
	if strings.Count(view, "│") != 11 || !strings.Contains(view, "Program type") || !strings.Contains(view, "Sequence") || !strings.Contains(view, "Lights") || !strings.Contains(view, "All day") || !strings.Contains(view, "│ Indoor winter") || !strings.Contains(view, "First date") || !strings.Contains(view, "[x] Turn lights off  [ ] Leave current light state") || !strings.Contains(view, "[x] enabled  [ ] disabled") {
		t.Fatalf("schedule form is not rendered with explicit assignment controls: %s", view)
	}
	m.lighting.focus = assignmentDailyStartField
	m.lighting.fields[assignmentDailyStartField].Focus()
	m.lighting.fields[assignmentDailyStartField].SetValue("")
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(statusModel)
	if m.lighting.error != "Daily start must be HH:MM or sunset." {
		t.Fatalf("invalid daily start was not rejected immediately: %q", m.lighting.error)
	}
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.lighting.focus != assignmentDailyStartField {
		t.Fatal("invalid daily start advanced to the next field")
	}
	m.lighting.fields[assignmentDailyStartField].SetValue("sunset")
	m.lighting.focus = assignmentEnabledField
	m.lighting.fields[assignmentEnabledField].Focus()
	beforeSchedule := m.lighting.fields[assignmentEnabledField].Value()
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(statusModel)
	if m.lighting.fields[assignmentEnabledField].Value() == beforeSchedule {
		t.Fatal("Space did not cycle the schedule checkbox")
	}
	cycledSchedule := m.lighting.fields[assignmentEnabledField].Value()
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(statusModel)
	if m.lighting.fields[assignmentEnabledField].Value() != cycledSchedule {
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

func TestTUIWLEDAssignmentForm(t *testing.T) {
	m := statusModel{bundle: testLightingBundle(t), width: 80, height: 24, inventoryStates: map[string]LightState{
		"light.floating_string":         {EntityID: "light.floating_string", State: "on", Attribute: map[string]any{"friendly_name": "Floating string", "supported_color_modes": []any{"rgb"}}},
		"select.floating_string_preset": {EntityID: "select.floating_string_preset", State: "Christmas", Attribute: map[string]any{"options": []any{"Christmas", "Winter ice"}}},
	}, inventoryMetadata: map[string]HAEntityMetadata{
		"light.floating_string":         {EntityID: "light.floating_string", Platform: "wled", DeviceID: "wled-1", ConfigEntryID: "entry-1"},
		"select.floating_string_preset": {EntityID: "select.floating_string_preset", Platform: "wled", DeviceID: "wled-1", ConfigEntryID: "entry-1", TranslationKey: "preset"},
	}}
	m.editLightingAssignment("")
	m.lighting.fields[assignmentNameField].SetValue("Floating string")
	m.lighting.fields[assignmentLightsField].SetValue("light.stale_sequence_target")
	m.lighting.fields[assignmentFirstDateField].SetValue("11-15")
	m.lighting.fields[assignmentLastDateField].SetValue("01-15")
	m.lighting.fields[assignmentDailyStartField].SetValue("17:00")
	m.lighting.fields[assignmentDailyStopField].SetValue("23:00")
	m.lighting.fields[assignmentFinishField].SetValue("leave")
	m.lighting.fields[assignmentEnabledField].SetValue("enabled")
	m.lighting.focus = assignmentTypeField
	updated, _ := m.updateLighting(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(statusModel)
	if !m.lighting.assignmentWLED() || len(m.lighting.fields) != 12 {
		t.Fatalf("keyboard kind switch did not open WLED form: %#v", m.lighting.labels)
	}
	if m.lighting.fields[wledLightField].Value() != "" || m.lighting.fields[wledFirstDateField].Value() != "11-15" || m.lighting.fields[wledLastDateField].Value() != "01-15" || m.lighting.fields[wledDailyStartField].Value() != "17:00" || m.lighting.fields[wledDailyStopField].Value() != "23:00" || m.lighting.fields[wledFinishField].Value() != "leave" || m.lighting.fields[wledEnabledField].Value() != "enabled" {
		t.Fatalf("kind switch lost common fields or stale sequence targets: %v", m.lighting.fields)
	}
	m.lighting.fields[wledSelectField].SetValue("select.stale")
	m.lighting.fields[wledOptionField].SetValue("stale option")
	for _, selection := range []struct {
		focus int
		want  string
	}{{wledLightField, "light.floating_string"}, {wledSelectField, "select.floating_string_preset"}, {wledOptionField, "Christmas"}} {
		focus, want := selection.focus, selection.want
		m.lighting.focus = focus
		updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(statusModel)
		if !m.lighting.picker {
			t.Fatalf("field %d did not open its picker", focus)
		}
		updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(statusModel)
		if m.lighting.fields[focus].Value() != want {
			t.Fatalf("field %d selected %q, want %q", focus, m.lighting.fields[focus].Value(), want)
		}
		if focus == wledLightField && (m.lighting.fields[wledSelectField].Value() != "" || m.lighting.fields[wledOptionField].Value() != "") {
			t.Fatalf("changing WLED light retained stale selector/option: %q %q", m.lighting.fields[wledSelectField].Value(), m.lighting.fields[wledOptionField].Value())
		}
		if focus == wledSelectField && m.lighting.fields[wledOptionField].Value() != "" {
			t.Fatalf("changing WLED selector retained stale option: %q", m.lighting.fields[wledOptionField].Value())
		}
	}
	m.saveLightingForm()
	a := lightingAssignments(m.bundle)["floating_string"]
	if a.WLED == nil || a.WLED.Light != "light.floating_string" || a.WLED.Select != "select.floating_string_preset" || a.WLED.Option != "Christmas" || a.Sequence != "" || len(a.Targets) != 0 || a.Start != "11-15" || a.End != "01-15" || a.On != "17:00" || a.Off != "23:00" || a.Finish != "leave" || !a.Enabled || a.AllDay {
		t.Fatalf("TUI WLED assignment not saved: %#v", a)
	}
	m.editLightingAssignment("floating_string")
	for i := range m.lighting.fields {
		m.lighting.focus = i
		if got := lipgloss.Height(strings.TrimSuffix(m.renderLighting(), "\n")); got > 24 {
			t.Fatalf("WLED form exceeds 80x24 at field %d: %d\n%s", i, got, m.renderLighting())
		}
	}
}

func TestTUIWLEDKeyboardPickerScopesByDevice(t *testing.T) {
	m := statusModel{bundle: testLightingBundle(t), width: 80, height: 24, inventoryStates: map[string]LightState{
		"light.floating_string":           {EntityID: "light.floating_string", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}}},
		"light.workshop":                  {EntityID: "light.workshop", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}}},
		"select.floating_string_playlist": {EntityID: "select.floating_string_playlist", Attribute: map[string]any{"options": []any{"Winter playlist"}}},
		"select.floating_string_preset":   {EntityID: "select.floating_string_preset", Attribute: map[string]any{"options": []any{"Christmas"}}},
		"select.floating_palette":         {EntityID: "select.floating_palette", Attribute: map[string]any{"options": []any{"Red"}}},
		"select.workshop_playlist":        {EntityID: "select.workshop_playlist", Attribute: map[string]any{"options": []any{"Workshop playlist"}}},
		"select.workshop_preset":          {EntityID: "select.workshop_preset", Attribute: map[string]any{"options": []any{"Workshop"}}},
	}, inventoryMetadata: map[string]HAEntityMetadata{
		"light.floating_string":           {EntityID: "light.floating_string", Platform: "wled", DeviceID: "wled-floating", ConfigEntryID: "entry-floating"},
		"light.workshop":                  {EntityID: "light.workshop", Platform: "wled", DeviceID: "wled-workshop", ConfigEntryID: "entry-workshop"},
		"select.floating_string_playlist": {EntityID: "select.floating_string_playlist", Platform: "wled", DeviceID: "wled-floating", ConfigEntryID: "entry-floating", TranslationKey: "playlist"},
		"select.floating_string_preset":   {EntityID: "select.floating_string_preset", Platform: "wled", DeviceID: "wled-floating", ConfigEntryID: "entry-floating", TranslationKey: "preset"},
		"select.floating_palette":         {EntityID: "select.floating_palette", Platform: "wled", DeviceID: "wled-floating", ConfigEntryID: "entry-floating", TranslationKey: "palette"},
		"select.workshop_playlist":        {EntityID: "select.workshop_playlist", Platform: "wled", DeviceID: "wled-workshop", ConfigEntryID: "entry-workshop", TranslationKey: "playlist"},
		"select.workshop_preset":          {EntityID: "select.workshop_preset", Platform: "wled", DeviceID: "wled-workshop", ConfigEntryID: "entry-workshop", TranslationKey: "preset"},
	}}
	m.editLightingAssignment("")
	m.lighting.focus = assignmentTypeField
	updated, _ := m.updateLighting(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(statusModel)
	if !m.lighting.assignmentWLED() {
		t.Fatal("keyboard path did not switch to WLED")
	}
	m.lighting.focus = wledLightField
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(statusModel)
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.lighting.fields[wledLightField].Value() != "light.workshop" || m.lighting.fields[wledSelectField].Value() != "" || m.lighting.fields[wledOptionField].Value() != "" {
		t.Fatalf("selecting workshop did not clear dependent fields: %q %q %q", m.lighting.fields[wledLightField].Value(), m.lighting.fields[wledSelectField].Value(), m.lighting.fields[wledOptionField].Value())
	}
	m.lighting.focus = wledSelectField
	if got := m.lightingPickerIDs(); stringMustJSON(got) != stringMustJSON([]string{"select.workshop_playlist", "select.workshop_preset"}) {
		t.Fatalf("workshop selector picker = %#v", got)
	}
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.lighting.fields[wledSelectField].Value() != "select.workshop_playlist" || m.lighting.fields[wledOptionField].Value() != "" {
		t.Fatalf("workshop selector selection was wrong: %q %q", m.lighting.fields[wledSelectField].Value(), m.lighting.fields[wledOptionField].Value())
	}
	m.lighting.focus = wledOptionField
	if got := m.lightingPickerIDs(); stringMustJSON(got) != stringMustJSON([]string{"Workshop playlist"}) {
		t.Fatalf("workshop option picker = %#v", got)
	}
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.lighting.fields[wledOptionField].Value() != "Workshop playlist" {
		t.Fatalf("workshop option selection was wrong: %q", m.lighting.fields[wledOptionField].Value())
	}
	m.lighting.focus = wledLightField
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.lighting.fields[wledLightField].Value() != "light.floating_string" || m.lighting.fields[wledSelectField].Value() != "" || m.lighting.fields[wledOptionField].Value() != "" {
		t.Fatalf("changing device retained invalid dependents: %q %q %q", m.lighting.fields[wledLightField].Value(), m.lighting.fields[wledSelectField].Value(), m.lighting.fields[wledOptionField].Value())
	}
	m.lighting.focus = wledSelectField
	if got := m.lightingPickerIDs(); stringMustJSON(got) != stringMustJSON([]string{"select.floating_string_playlist", "select.floating_string_preset"}) {
		t.Fatalf("floating selector picker = %#v", got)
	}
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(statusModel)
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	m.lighting.focus = wledOptionField
	if got := m.lightingPickerIDs(); stringMustJSON(got) != stringMustJSON([]string{"Christmas"}) {
		t.Fatalf("floating preset option picker = %#v", got)
	}
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	updated, _ = m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	m.saveLightingForm()
	a := lightingAssignments(m.bundle)["new_assignment"]
	if a.WLED == nil || a.WLED.Light != "light.floating_string" || a.WLED.Select != "select.floating_string_preset" || a.WLED.Option != "Christmas" {
		t.Fatalf("keyboard picker saved wrong WLED assignment: %#v", a)
	}
}

func TestTUIWLEDLightPickerFriendlyNameFallback(t *testing.T) {
	m := statusModel{
		bundle: testLightingBundle(t),
		width:  80,
		height: 24,
		inventoryStates: map[string]LightState{
			"light.floating_string": {EntityID: "light.floating_string", State: "on", Attribute: map[string]any{"friendly_name": "   ", "supported_color_modes": []any{"rgb"}}},
		},
		inventoryMetadata: map[string]HAEntityMetadata{
			"light.floating_string": {EntityID: "light.floating_string", Platform: "wled", DeviceID: "wled-floating", ConfigEntryID: "entry-floating"},
		},
	}
	m.editLightingAssignment("")
	m.switchToWLEDAssignment()
	m.lighting.focus = wledLightField
	updated, _ := m.updateLighting(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	view := m.renderLighting()
	if !strings.Contains(view, "light.floating_string") || strings.Contains(view, " · light.floating_string") {
		t.Fatalf("blank friendly name did not fall back cleanly: %s", view)
	}
	if lipgloss.Height(strings.TrimSuffix(view, "\n")) > 24 {
		t.Fatalf("fallback WLED picker exceeds 80x24: %s", view)
	}
	updated, _ = m.updateLightingPicker(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(statusModel)
	if m.lighting.fields[wledLightField].Value() != "light.floating_string" || m.lighting.picker {
		t.Fatalf("fallback picker did not persist the canonical entity ID: %q", m.lighting.fields[wledLightField].Value())
	}
}

func TestTUIWLEDInventoryReadiness(t *testing.T) {
	states := map[string]LightState{
		"light.imported":  {EntityID: "light.imported", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}}},
		"select.imported": {EntityID: "select.imported", State: "Saved", Attribute: map[string]any{"options": []any{"Saved"}}},
	}
	newForm := func(api StateAPI, status HAInventoryStatus, inventory map[string]LightState, metadata map[string]HAEntityMetadata) statusModel {
		m := statusModel{bundle: testLightingBundle(t), stateAPI: api, inventoryStates: inventory, inventoryMetadata: metadata, inventoryStatus: status}
		m.editLightingAssignment("")
		m.switchToWLEDAssignment()
		m.lighting.fields[assignmentNameField].SetValue("Readiness test")
		m.lighting.fields[wledLightField].SetValue("light.imported")
		m.lighting.fields[wledSelectField].SetValue("select.imported")
		m.lighting.fields[wledOptionField].SetValue("Saved")
		m.lighting.fields[wledFirstDateField].SetValue("11-15")
		m.lighting.fields[wledLastDateField].SetValue("01-15")
		m.lighting.fields[wledAllDayField].SetValue("disabled")
		m.lighting.fields[wledFinishField].SetValue("off")
		m.lighting.fields[wledEnabledField].SetValue("enabled")
		return m
	}
	load := func(t *testing.T, api *webTestStateAPI) inventoryResultMsg {
		t.Helper()
		message := loadInventoryCmd(api, 1)()
		result, ok := message.(inventoryResultMsg)
		if !ok {
			t.Fatalf("inventory command returned %T", message)
		}
		return result
	}

	t.Run("successful empty registry enforces connected validation", func(t *testing.T) {
		api := &webTestStateAPI{states: states, inventory: &HAInventory{Metadata: map[string]HAEntityMetadata{}}}
		result := load(t, api)
		if !result.status.Connected || !result.status.StatesReady || !result.status.EntityRegistryReady || result.status.Error != "" {
			t.Fatalf("empty registry status = %#v", result.status)
		}
		m := newForm(api, result.status, result.states, result.metadata)
		m.saveLightingForm()
		if !strings.Contains(m.lighting.error, "not a connected WLED color light") {
			t.Fatalf("empty registry did not reject WLED assignment: %q", m.lighting.error)
		}
	})

	t.Run("registry failure blocks connected save", func(t *testing.T) {
		api := &webTestStateAPI{states: states, inventoryErr: context.Canceled}
		result := load(t, api)
		if !result.status.Connected || !result.status.StatesReady || result.status.EntityRegistryReady || result.status.Error == "" {
			t.Fatalf("failed registry status = %#v", result.status)
		}
		stale := statusModel{planner: true, inventoryRequest: 1, inventoryMetadata: map[string]HAEntityMetadata{"light.stale": {EntityID: "light.stale", Platform: "wled", DeviceID: "stale"}}, inventoryStatus: HAInventoryStatus{Connected: true, StatesReady: true, EntityRegistryReady: true}}
		updated, _ := stale.Update(result)
		stale = updated.(statusModel)
		if len(stale.inventoryMetadata) != 0 || stale.inventoryStatus.EntityRegistryReady {
			t.Fatalf("registry failure left stale TUI metadata ready: %#v %#v", stale.inventoryMetadata, stale.inventoryStatus)
		}
		m := newForm(api, result.status, result.states, result.metadata)
		m.saveLightingForm()
		if !strings.Contains(m.lighting.error, "entity registry is unavailable") || !strings.Contains(m.lighting.error, "context canceled") {
			t.Fatalf("registry failure did not block WLED assignment: %q", m.lighting.error)
		}
	})

	t.Run("offline preserves imported reference", func(t *testing.T) {
		bundle := testLightingBundle(t)
		assignment := LightingAssignment{ID: "imported", Name: "Imported", Start: "11-15", End: "01-15", Finish: "off", Enabled: true, AllDay: true, WLED: &WLEDProgram{Light: "light.imported", Select: "select.imported", Option: "Saved"}}
		var err error
		bundle, err = saveLightingAssignment(bundle, assignment)
		if err != nil {
			t.Fatal(err)
		}
		m := statusModel{bundle: bundle}
		m.editLightingAssignment("imported")
		m.saveLightingForm()
		if m.lighting.error != "" || lightingAssignments(m.bundle)["imported"].WLED.Option != "Saved" {
			t.Fatalf("offline imported reference was not preserved: %q %#v", m.lighting.error, lightingAssignments(m.bundle)["imported"])
		}
	})
}

func TestTUIWLEDToSequenceAssignmentSwitch(t *testing.T) {
	m := statusModel{bundle: testLightingBundle(t), width: 80, height: 24, inventoryStates: map[string]LightState{
		"light.floating_string":         {EntityID: "light.floating_string", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}}},
		"select.floating_string_preset": {EntityID: "select.floating_string_preset", State: "Christmas", Attribute: map[string]any{"options": []any{"Christmas"}}},
		"light.target":                  {EntityID: "light.target", State: "off", Attribute: map[string]any{"supported_color_modes": []any{"xy"}}},
	}}
	m.editLightingAssignment("")
	m.switchToWLEDAssignment()
	m.lighting.fields[assignmentNameField].SetValue("Nightly bulbs")
	m.lighting.fields[wledLightField].SetValue("light.floating_string")
	m.lighting.fields[wledSelectField].SetValue("select.floating_string_preset")
	m.lighting.fields[wledOptionField].SetValue("Christmas")
	m.lighting.fields[wledFirstDateField].SetValue("11-15")
	m.lighting.fields[wledLastDateField].SetValue("01-15")
	m.lighting.fields[wledAllDayField].SetValue("enabled")
	m.lighting.fields[wledFinishField].SetValue("leave")
	m.lighting.fields[wledEnabledField].SetValue("enabled")
	m.lighting.focus = assignmentTypeField
	updated, _ := m.updateLighting(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(statusModel)
	if m.lighting.assignmentWLED() || len(m.lighting.fields) != 11 || m.lighting.fields[assignmentSequenceField].Value() != "christmas" {
		t.Fatalf("WLED-to-sequence switch did not rebuild the form: %#v", m.lighting.labels)
	}
	m.lighting.fields[assignmentLightsField].SetValue("light.target")
	m.saveLightingForm()
	a := lightingAssignments(m.bundle)["nightly_bulbs"]
	if a.WLED != nil || a.Sequence != "christmas" || len(a.Targets) != 1 || a.Targets[0] != "light.target" || a.Start != "11-15" || a.End != "01-15" || a.Finish != "leave" || !a.AllDay || a.On != "" || a.Off != "" {
		t.Fatalf("WLED-to-sequence switch saved the wrong assignment: %#v", a)
	}
}

func TestTUIColorEditRemainsAfterReopening(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{Colors: {Kind: Colors, Data: map[string]any{
		"red":  map[string]any{"name": "Red", "x": .64, "y": .33},
		"blue": map[string]any{"name": "Blue", "x": .15, "y": .06},
	}}}}
	m := statusModel{bundle: bundle, colorView: true, colorSelected: 1}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(statusModel)
	m.formValues[1], m.formValues[2] = "0.300", "0.600"
	m.finishColorForm()
	if got := colorDefinitions(m.bundle)["red"].X; got != .3 {
		t.Fatalf("TUI color edit X = %v, want .3", got)
	}
	m.colorView, m.colorSelected = true, 1
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(statusModel)
	if m.formValues[1] != "0.300" {
		t.Fatalf("reopened TUI color X = %q, want 0.300", m.formValues[1])
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
	bundle, err = saveLightingAssignment(bundle, testWLEDAssignment())
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
