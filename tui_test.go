package main

import (
	"context"
	"errors"
	"image"
	"image/color"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type inventoryStatesStub struct {
	StateAPI
	err error
}

func (a inventoryStatesStub) States(context.Context, []string) (map[string]LightState, error) {
	return map[string]LightState{"light.test": {EntityID: "light.test"}}, a.err
}

type inventoryLocationsStub struct {
	inventoryStatesStub
	locationErr error
}

func (a inventoryLocationsStub) LightLocations(context.Context) (map[string]LightLocation, error) {
	return map[string]LightLocation{"light.test": {Area: "Kitchen"}}, a.locationErr
}

func TestLoadInventoryPreservesFallbackErrors(t *testing.T) {
	failure := errors.New("inventory unavailable")
	for _, tc := range []struct {
		name             string
		api              StateAPI
		statesReady      bool
		err, locationErr error
		message          string
	}{
		{"states failure", inventoryStatesStub{err: failure}, false, failure, nil, failure.Error()},
		{"no registry", inventoryStatesStub{}, true, nil, nil, "entity registry metadata is unavailable"},
		{"locations only", inventoryLocationsStub{}, true, nil, nil, "entity registry metadata is unavailable"},
		{"locations failure", inventoryLocationsStub{locationErr: failure}, true, nil, failure, failure.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := loadInventoryCmd(tc.api, 42)().(inventoryResultMsg)
			wantStatus := HAInventoryStatus{Connected: true, StatesReady: tc.statesReady, Error: tc.message}
			if got.request != 42 || got.status != wantStatus || !errors.Is(got.err, tc.err) || !errors.Is(got.locationErr, tc.locationErr) {
				t.Fatalf("inventory result = %#v", got)
			}
			if tc.statesReady != (got.states != nil) {
				t.Fatalf("states = %#v", got.states)
			}
			if _, ok := tc.api.(inventoryLocationsStub); ok && got.locations["light.test"].Area != "Kitchen" {
				t.Fatalf("partial locations lost: %#v", got.locations)
			}
		})
	}
}

func TestSortedStringsCaseInsensitive(t *testing.T) {
	values := []string{"z", "Beta", "alpha", "ALPHA", "Ä", "ä"}
	got := sortedStrings(values)
	if !slices.IsSortedFunc(got, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	}) || !reflect.DeepEqual(got, values) {
		t.Fatalf("sort = %q, input = %q", got, values)
	}
	if sortedStrings(nil) != nil {
		t.Fatal("nil input became non-nil")
	}
}

func TestPublishPromptIncludesDiff(t *testing.T) {
	paths := map[ConfigKind][]string{
		Scripts:     {"packages/ha_lightcraft.yaml"},
		Automations: {"packages/ha_lightcraft.yaml"},
		Helpers:     {"packages/ha_lightcraft.yaml"},
	}
	old := Bundle{Files: map[ConfigKind]Config{
		Scripts:     {Kind: Scripts, Data: []any{}},
		Automations: {Kind: Automations, Data: []any{}},
		Helpers:     {Kind: Helpers, Data: map[string]any{}},
	}}
	old, _ = withHash(old)
	next := Bundle{Files: map[ConfigKind]Config{
		Scripts:     {Kind: Scripts, Data: []any{map[string]any{"id": "halloween", "name": "Halloween", "entities": map[string]any{}}}},
		Automations: {Kind: Automations, Data: []any{}},
		Helpers:     {Kind: Helpers, Data: map[string]any{}},
	}}
	next, _ = withHash(next)
	view := RenderPrompt(statusModel{prompt: "publish", baseline: &old, bundle: next, filePaths: paths})
	if !strings.Contains(view, "File to publish: packages/ha_lightcraft.yaml") || !strings.Contains(view, "Changes to publish") || !strings.Contains(view, "--- packages/ha_lightcraft.yaml") || !strings.Contains(view, "+++ packages/ha_lightcraft.yaml") || strings.Contains(view, "--- scripts") || strings.Count(view, "--- packages/ha_lightcraft.yaml") != 1 || !strings.Contains(view, "Type PUBLISH") {
		t.Fatalf("prompt = %q", view)
	}
}

func TestDashboardDoesNotOfferLegacyUpgrade(t *testing.T) {
	modern, _ := legacyBundleForTest()
	m := statusModel{bundle: modern}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = updated.(statusModel)
	if m.prompt != "" || strings.Contains(m.renderDashboard(), "b upgrade") {
		t.Fatalf("dashboard still offers legacy upgrade: prompt=%q view=%s", m.prompt, m.renderDashboard())
	}
}

func TestDeletePromptKeepsButtonsHorizontal(t *testing.T) {
	view := RenderPrompt(statusModel{prompt: "delete-sequence", pending: "halloween", confirmFocus: 1})
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Delete") && strings.Contains(line, "Cancel") {
			return
		}
	}
	t.Fatalf("delete buttons are not on one line: %q", view)
}

func TestQuitPromptUsesChoiceButtons(t *testing.T) {
	view := RenderPrompt(statusModel{prompt: "quit-confirm", confirmFocus: 1})
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Quit") && strings.Contains(line, "Keep editing") {
			return
		}
	}
	t.Fatalf("quit buttons are not on one line: %q", view)
}

func TestStatusViewShowsMessage(t *testing.T) {
	view := (statusModel{bundle: Bundle{Hash: "abc"}, message: "Published, verified."}).View()
	if !strings.HasPrefix(view, "Published, verified.") {
		t.Fatalf("view = %q", view)
	}
}

func TestDashboardShowsWorkflowAndSeparateInventory(t *testing.T) {
	view := (statusModel{width: 100, height: 24}).renderDashboard()
	for _, want := range []string{"WORKFLOW", "Colors → Sequences → Schedules → Agenda → Publish", "Inventory", "EDIT WORKSPACES", "i inventory"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dashboard omitted %q: %s", want, view)
		}
	}
}

func TestProjectLogoUsesKittyTerminals(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm-ghostty")
	if got := projectLogo(); !strings.Contains(got, "\x1b_G") || len(got) <= 30 || !strings.Contains(got, "m=1") {
		t.Fatalf("ghostty logo = %q", got)
	}
	t.Setenv("TERM", "dumb")
	if got := projectLogo(); got != "" {
		t.Fatalf("fallback logo = %q", got)
	}
}

func TestTransparentLogoOnlyRemovesEdgeBackground(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 5, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			source.Set(x, y, color.Black)
		}
	}
	for y := 1; y <= 3; y++ {
		for x := 1; x <= 3; x++ {
			source.Set(x, y, color.White)
		}
	}
	source.Set(2, 2, color.Black)
	got := transparentLogo(source).(*image.RGBA)
	if got.RGBAAt(0, 0).A != 0 || got.RGBAAt(2, 2).A != 255 {
		t.Fatalf("transparent logo alpha = edge %d, interior %d", got.RGBAAt(0, 0).A, got.RGBAAt(2, 2).A)
	}
}

func TestProjectLogoAppearsOnlyOnTallHelp(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm-ghostty")
	if view := (statusModel{width: 120, height: 40}).renderDashboard(); strings.Contains(view, "\x1b_G") {
		t.Fatalf("dashboard unexpectedly contains logo")
	}
	if view := renderHelp(120, 40); !strings.Contains(view, "\x1b_G") {
		t.Fatalf("help omitted logo")
	}
	if view := renderHelp(120, 24); strings.Contains(view, "\x1b_G") {
		t.Fatalf("small help unexpectedly contains logo")
	}
}

func TestProjectLogoIsClearedWhenHelpCloses(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm-ghostty")
	_ = projectLogo()
	m := statusModel{helpView: true, width: 120, height: 40}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(statusModel)
	if !m.clearProjectLogo {
		t.Fatal("help close did not request logo cleanup")
	}
	if view := m.renderDashboard(); !strings.Contains(view, "a=d") {
		t.Fatalf("dashboard omitted logo cleanup: %q", view)
	}
}

func TestTUIAgendaNavigationAndRendering(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		m := statusModel{bundle: testLightingBundle(t), width: size[0], height: size[1], agendaView: true, agendaDate: time.Date(2026, 12, 1, 0, 0, 0, 0, time.Local)}
		view := m.View()
		if !strings.Contains(view, "SCHEDULE AGENDA") || !strings.Contains(view, "Exterior") || !strings.Contains(view, "4 lights") {
			t.Fatalf("%dx%d agenda view = %q", size[0], size[1], view)
		}
		if lipgloss.Height(strings.TrimSuffix(view, "\n")) > size[1] {
			t.Fatalf("%dx%d agenda exceeds terminal height: %d", size[0], size[1], lipgloss.Height(strings.TrimSuffix(view, "\n")))
		}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
		m = updated.(statusModel)
		if !m.agendaDate.Equal(time.Date(2026, 12, 2, 0, 0, 0, 0, time.Local)) {
			t.Fatalf("%dx%d agenda date = %v", size[0], size[1], m.agendaDate)
		}
	}
}

func TestDraftContentViewShowsWorkingCopy(t *testing.T) {
	m := statusModel{contentView: true, bundle: Bundle{Files: map[ConfigKind]Config{
		Scripts: {Kind: Scripts, Data: []any{map[string]any{"id": "halloween", "name": "Halloween"}}},
	}}}
	view := m.View()
	if !strings.Contains(view, "Home Assistant file: scripts.yaml") || !strings.Contains(view, "halloween") {
		t.Fatalf("content view = %q", view)
	}
}

func TestPublishWorkspaceUsesHomeAssistantPackage(t *testing.T) {
	paths := map[ConfigKind][]string{
		Scripts:     {"packages/ha_lightcraft.yaml"},
		Automations: {"packages/ha_lightcraft.yaml"},
		Helpers:     {"packages/ha_lightcraft.yaml"},
	}
	m := statusModel{bundle: Bundle{Files: map[ConfigKind]Config{
		Scripts:     {Kind: Scripts, Data: map[string]any{"holiday": map[string]any{"alias": "Holiday"}}},
		Automations: {Kind: Automations, Data: []any{map[string]any{"id": "holiday"}}},
		Helpers:     {Kind: Helpers, Data: map[string]any{"holiday": map[string]any{"name": "Holiday"}}},
	}}, filePaths: paths, dashboardWorkspace: 3, dashboardFocus: 1, width: 100, height: 24}
	view := m.renderDashboardWorkspace(100)
	if !strings.Contains(view, "PUBLISH") || !strings.Contains(view, "packages/ha_lightcraft.yaml") || strings.Contains(view, "scripts/") || strings.Contains(view, "automations/") || strings.Contains(view, "input_select/") {
		t.Fatalf("publish workspace = %s", view)
	}
}

func TestRenderInventoryListsOnlyLights(t *testing.T) {
	view := RenderInventory(map[string]LightState{
		"sensor.test": {EntityID: "sensor.test", State: "on"},
		"light.wled":  {EntityID: "light.wled", State: "off", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}, "effect_list": []any{"Solid"}, "entity_id": []any{"light.wled_left", "light.wled_right"}}},
	})
	if !strings.Contains(view, "light.wled") || strings.Contains(view, "sensor.test") || !strings.Contains(view, "light.wled · group") || !strings.Contains(view, "Color modes (1): rgb") {
		t.Fatalf("inventory = %q", view)
	}
}

func TestRenderLightColorShowsXYSwatch(t *testing.T) {
	rendered := renderLightColor(map[string]any{"xy_color": []any{0.385, 0.155}})
	if !strings.Contains(rendered, "#") || !strings.Contains(rendered, "XY (0.385, 0.155)") {
		t.Fatalf("color = %q", rendered)
	}
}

func TestRenderLightColorPreservesRGBRange(t *testing.T) {
	rendered := renderLightColor(map[string]any{"rgb_color": []any{255, 80, 0}})
	if !strings.Contains(rendered, "#FF5000") {
		t.Fatalf("color = %q", rendered)
	}
}

func TestInventoryIDsSortByFloorAndArea(t *testing.T) {
	states := map[string]LightState{
		"light.z": {}, "light.a": {}, "light.m": {},
	}
	locations := map[string]LightLocation{
		"light.z": {Floor: "Second floor", Area: "Kitchen"},
		"light.a": {Floor: "Basement", Area: "Workshop"},
		"light.m": {Floor: "Ground floor", Area: "Living Room"},
	}
	ids := inventoryIDs(states, locations)
	want := []string{"light.a", "light.m", "light.z"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
}

func TestColorLightIDsFilterNonColorLights(t *testing.T) {
	states := map[string]LightState{
		"light.color":  {Attribute: map[string]any{"supported_color_modes": []any{"xy"}}},
		"light.switch": {Attribute: map[string]any{"supported_color_modes": []any{"onoff"}}},
	}
	ids := colorLightIDs(states, nil)
	if strings.Join(ids, ",") != "light.color" {
		t.Fatalf("color light IDs = %v, want [light.color]", ids)
	}
}

func TestTUIDashboardScheduleWLEDFlow(t *testing.T) {
	states := map[string]LightState{
		"light.floating_string":    {EntityID: "light.floating_string", State: "on", Attribute: map[string]any{"friendly_name": "Living room fairy lights", "supported_color_modes": []any{"rgb"}}},
		"light.workshop":           {EntityID: "light.workshop", State: "off", Attribute: map[string]any{"friendly_name": "Workshop fairy lights", "supported_color_modes": []any{"rgb"}}},
		"light.hue":                {EntityID: "light.hue", State: "off", Attribute: map[string]any{"supported_color_modes": []any{"xy"}}},
		"select.floating_preset":   {EntityID: "select.floating_preset", State: "Christmas", Attribute: map[string]any{"options": []any{"Christmas", "Winter"}}},
		"select.floating_playlist": {EntityID: "select.floating_playlist", State: "Holiday", Attribute: map[string]any{"options": []any{"Holiday"}}},
		"select.floating_palette":  {EntityID: "select.floating_palette", State: "Christmas", Attribute: map[string]any{"options": []any{"Christmas"}}},
		"select.workshop_preset":   {EntityID: "select.workshop_preset", State: "Workshop", Attribute: map[string]any{"options": []any{"Workshop"}}},
	}
	metadata := map[string]HAEntityMetadata{
		"light.floating_string":    {EntityID: "light.floating_string", Platform: "wled", DeviceID: "wled-floating", ConfigEntryID: "entry-floating"},
		"light.workshop":           {EntityID: "light.workshop", Platform: "wled", DeviceID: "wled-workshop", ConfigEntryID: "entry-workshop"},
		"light.hue":                {EntityID: "light.hue", Platform: "hue", DeviceID: "hue-1"},
		"select.floating_preset":   {EntityID: "select.floating_preset", Platform: "wled", DeviceID: "wled-floating", ConfigEntryID: "entry-floating", TranslationKey: "preset"},
		"select.floating_playlist": {EntityID: "select.floating_playlist", Platform: "wled", DeviceID: "wled-floating", ConfigEntryID: "entry-floating", TranslationKey: "playlist"},
		"select.floating_palette":  {EntityID: "select.floating_palette", Platform: "wled", DeviceID: "wled-floating", ConfigEntryID: "entry-floating", TranslationKey: "palette"},
		"select.workshop_preset":   {EntityID: "select.workshop_preset", Platform: "wled", DeviceID: "wled-workshop", ConfigEntryID: "entry-workshop", TranslationKey: "preset"},
	}

	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		bundle, err := saveLightingAssignment(Bundle{Files: map[ConfigKind]Config{}}, testWLEDAssignment())
		if err != nil {
			t.Fatal(err)
		}
		m := statusModel{
			bundle:             bundle,
			dashboardWorkspace: 2,
			dashboardFocus:     1,
			width:              size[0],
			height:             size[1],
			inventoryStates:    states,
			inventoryMetadata:  metadata,
			inventoryStatus:    HAInventoryStatus{StatesReady: true, EntityRegistryReady: true},
		}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
		m = updated.(statusModel)
		if m.dashboardWorkspace != 4 {
			t.Fatalf("%dx%d dashboard did not route 3 to schedules", size[0], size[1])
		}
		if view := m.renderDashboard(); !strings.Contains(view, "Create a color-sequence or WLED schedule") {
			t.Fatalf("%dx%d schedule dashboard omitted WLED path: %s", size[0], size[1], view)
		}
		existing := m
		updated, _ = existing.Update(tea.KeyMsg{Type: tea.KeyEnter})
		existing = updated.(statusModel)
		if !existing.planner || !existing.lighting.assignmentWLED() {
			t.Fatalf("%dx%d existing WLED schedule did not open from dashboard", size[0], size[1])
		}
		if view := existing.renderLighting(); !strings.Contains(view, "Program type") || strings.Contains(view, "Assignment type") || !strings.Contains(view, "WLED light") || !strings.Contains(view, "Preset/playlist") || !strings.Contains(view, "Program option") || !strings.Contains(view, "All day") {
			t.Fatalf("%dx%d existing WLED form omitted WLED controls: %s", size[0], size[1], view)
		}
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		m = updated.(statusModel)
		if !m.planner || !m.lighting.assignmentWLED() {
			t.Fatalf("%dx%d dashboard new schedule did not open WLED form: %#v", size[0], size[1], m.lighting)
		}
		view := m.renderLighting()
		if strings.Contains(view, "Assignment type") {
			t.Fatalf("%dx%d WLED form retained the old type label: %s", size[0], size[1], view)
		}
		for _, label := range []string{"Program type", "WLED preset/playlist", "WLED light", "Preset/playlist", "Program option", "All day"} {
			if !strings.Contains(view, label) {
				t.Fatalf("%dx%d WLED form omitted %q: %s", size[0], size[1], label, view)
			}
		}

		m.lighting.focus = wledLightField
		m.lighting.fields[wledLightField].Focus()
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(statusModel)
		if !m.lighting.picker {
			t.Fatal("WLED light picker did not open from schedule form")
		}
		view = m.renderLighting()
		for _, value := range []string{"Living room fairy lights · light.floating_string", "Workshop fairy lights · light.workshop"} {
			if !strings.Contains(view, value) {
				t.Fatalf("WLED light picker omitted %s: %s", value, view)
			}
		}
		if strings.Contains(view, "light.hue") || strings.Contains(view, "select.") {
			t.Fatalf("WLED light picker leaked non-WLED choices: %s", view)
		}
		if !strings.Contains(view, "Enter choose") || strings.Contains(view, "Space toggle") {
			t.Fatalf("WLED light picker has misleading multi-select help: %s", view)
		}
		m.lighting.pick = 0
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(statusModel)
		if m.lighting.fields[wledLightField].Value() != "light.floating_string" {
			t.Fatalf("WLED picker stored display text instead of entity ID: %q", m.lighting.fields[wledLightField].Value())
		}
		if lipgloss.Height(strings.TrimSuffix(view, "\n")) > size[1] {
			t.Fatalf("%dx%d WLED light picker exceeds terminal height", size[0], size[1])
		}
		m.lighting.focus = wledSelectField
		m.lighting.fields[wledSelectField].Focus()
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(statusModel)
		view = m.renderLighting()
		for _, value := range []string{"select.floating_playlist", "select.floating_preset"} {
			if !strings.Contains(view, value) {
				t.Fatalf("same-device selector picker omitted %s: %s", value, view)
			}
		}
		if strings.Contains(view, "select.workshop_preset") || strings.Contains(view, "select.floating_palette") {
			t.Fatalf("selector picker leaked another device or palette: %s", view)
		}
		if !strings.Contains(view, "Enter choose") || strings.Contains(view, "Space toggle") {
			t.Fatalf("WLED selector picker has misleading multi-select help: %s", view)
		}
		m.lighting.pick = 1
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(statusModel)
		m.lighting.focus = wledOptionField
		m.lighting.fields[wledOptionField].Focus()
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(statusModel)
		view = m.renderLighting()
		if !strings.Contains(view, "Christmas") || !strings.Contains(view, "Winter") {
			t.Fatalf("preset option picker omitted options: %s", view)
		}
		if !strings.Contains(view, "Enter choose") || strings.Contains(view, "Space toggle") {
			t.Fatalf("WLED option picker has misleading multi-select help: %s", view)
		}
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(statusModel)
		m.lighting.focus = wledAllDayField
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = updated.(statusModel)
		m.lighting.fields[wledFirstDateField].SetValue("02-01")
		m.lighting.fields[wledLastDateField].SetValue("02-28")
		if !m.lighting.assignmentAllDay() || !strings.Contains(m.renderLighting(), "ignored while all day is enabled") {
			t.Fatalf("%dx%d WLED all-day toggle was not visible: %s", size[0], size[1], m.renderLighting())
		}
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		m = updated.(statusModel)
		if m.lighting.form != "" || lightingAssignments(m.bundle)["new_assignment"].WLED == nil || !lightingAssignments(m.bundle)["new_assignment"].AllDay {
			t.Fatalf("%dx%d WLED assignment did not save from dashboard path: %#v", size[0], size[1], lightingAssignments(m.bundle)["new_assignment"])
		}
	}
}

func TestTUIDashboardScheduleColorAllDayFlow(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		m := statusModel{
			bundle:             testLightingBundle(t),
			dashboardWorkspace: 2,
			dashboardFocus:     1,
			width:              size[0],
			height:             size[1],
		}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
		m = updated.(statusModel)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		m = updated.(statusModel)
		view := m.renderLighting()
		if strings.Contains(view, "Assignment type") {
			t.Fatalf("%dx%d color form retained the old type label: %s", size[0], size[1], view)
		}
		for _, label := range []string{"Program type", "Color sequence", "Sequence", "Lights", "All day", "enabled", "disabled"} {
			if !strings.Contains(view, label) {
				t.Fatalf("%dx%d color form omitted %q: %s", size[0], size[1], label, view)
			}
		}
		m.lighting.fields[assignmentLightsField].SetValue("light.hue")
		m.lighting.focus = assignmentAllDayField
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = updated.(statusModel)
		view = m.renderLighting()
		if !strings.Contains(view, "[x] enabled") || !strings.Contains(view, "ignored while all day is enabled") {
			t.Fatalf("%dx%d all-day state was not visible: %s", size[0], size[1], view)
		}
		m.lighting.focus = assignmentTypeField
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
		m = updated.(statusModel)
		if !m.lighting.assignmentWLED() || !strings.Contains(m.renderLighting(), "WLED light") {
			t.Fatalf("%dx%d color-to-WLED switch was not visible: %s", size[0], size[1], m.renderLighting())
		}
		m.lighting.focus = assignmentTypeField
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
		m = updated.(statusModel)
		if m.lighting.assignmentWLED() || !strings.Contains(m.renderLighting(), "Sequence") {
			t.Fatalf("%dx%d WLED-to-color switch was not visible: %s", size[0], size[1], m.renderLighting())
		}
		m.lighting.focus = assignmentEnabledField
		m.lighting.fields[assignmentLightsField].SetValue("light.hue")
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		m = updated.(statusModel)
		a := lightingAssignments(m.bundle)["new_assignment"]
		if a.WLED != nil || a.Sequence == "" || len(a.Targets) != 1 || !a.AllDay || a.On != "" || a.Off != "" {
			t.Fatalf("%dx%d all-day color assignment did not save: %#v", size[0], size[1], a)
		}
	}
}

func TestTUIDashboardScheduleAllDayKeyPath(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		m := statusModel{
			bundle:             testLightingBundle(t),
			dashboardWorkspace: 2,
			dashboardFocus:     1,
			width:              size[0],
			height:             size[1],
		}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
		m = updated.(statusModel)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		m = updated.(statusModel)
		m.lighting.fields[assignmentLightsField].SetValue("light.hue")

		m.lighting.focus = assignmentAllDayField
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = updated.(statusModel)
		if !m.lighting.assignmentAllDay() || m.lighting.fields[assignmentDailyStartField].Value() != "sunset" || m.lighting.fields[assignmentDailyStopField].Value() != "00:00" {
			t.Fatalf("%dx%d all-day did not retain valid defaults: %v", size[0], size[1], m.lighting.fields)
		}

		for _, field := range []int{assignmentDailyStartField, assignmentDailyStopField} {
			m.lighting.focus = field
			m.lighting.fields[field].Focus()
			before := m.lighting.fields[field].Value()
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
			m = updated.(statusModel)
			if m.lighting.fields[field].Value() != before || !strings.Contains(m.renderLighting(), "ignored while all day is enabled") {
				t.Fatalf("%dx%d all-day allowed editing field %d: %q -> %q\n%s", size[0], size[1], field, before, m.lighting.fields[field].Value(), m.renderLighting())
			}
		}

		m.lighting.focus = assignmentAllDayField
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
		m = updated.(statusModel)
		if m.lighting.assignmentAllDay() || assignmentDailyStartError(m.lighting.fields[assignmentDailyStartField].Value()) != "" || m.lighting.fields[assignmentDailyStopField].Value() != "00:00" {
			t.Fatalf("%dx%d disabling all-day did not restore editable defaults: %v", size[0], size[1], m.lighting.fields)
		}

		m.lighting.focus = assignmentDailyStartField
		m.lighting.fields[assignmentDailyStartField].Focus()
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
		m = updated.(statusModel)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("17:00")})
		m = updated.(statusModel)
		m.lighting.focus = assignmentDailyStopField
		m.lighting.fields[assignmentDailyStopField].Focus()
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
		m = updated.(statusModel)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("23:00")})
		m = updated.(statusModel)
		if m.lighting.fields[assignmentDailyStartField].Value() != "17:00" || m.lighting.fields[assignmentDailyStopField].Value() != "23:00" {
			t.Fatalf("%dx%d disabled all-day fields were not editable: %v", size[0], size[1], m.lighting.fields)
		}

		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		m = updated.(statusModel)
		a := lightingAssignments(m.bundle)["new_assignment"]
		if m.lighting.form != "" || a.AllDay || a.On != "17:00" || a.Off != "23:00" {
			t.Fatalf("%dx%d disabled all-day assignment did not save: form=%q assignment=%#v", size[0], size[1], m.lighting.form, a)
		}
	}
}

func TestTUIWLEDPickerReportsMissingRegistry(t *testing.T) {
	m := statusModel{bundle: Bundle{Files: map[ConfigKind]Config{}}, width: 80, height: 24, inventoryStatus: HAInventoryStatus{Connected: true, StatesReady: true, Error: "registry unavailable"}}
	m.editLightingAssignment("")
	m.lighting.focus = wledLightField
	m.lighting.picker = true
	view := m.renderLighting()
	if !strings.Contains(view, "WLED choices unavailable") || !strings.Contains(view, "reload HA inventory") {
		t.Fatalf("missing registry message not visible: %s", view)
	}
}
