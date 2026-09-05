package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderStatusListsKinds(t *testing.T) {
	got := RenderStatus(Bundle{Hash: "abc", Files: map[ConfigKind]Config{Scenes: {Kind: Scenes}}})
	if got == "" || !strings.Contains(got, "scenes") || !strings.Contains(got, "abc") {
		t.Fatalf("unexpected status: %q", got)
	}
}

func TestDeleteApprovalDoesNotMutateHAImmediately(t *testing.T) {
	m := statusModel{prompt: "delete-ha", input: "DELETE HA", pending: "halloween_orange", store: &HAConfigStore{}}
	m.finishPrompt()
	if len(m.deletes) != 1 || m.deletes[0] != (ConfigRef{Kind: Scenes, ID: "halloween_orange"}) {
		t.Fatalf("deletes = %#v", m.deletes)
	}
	if m.message != "HA scene deletion approved for the next publish." {
		t.Fatalf("message = %q", m.message)
	}
}

func TestSceneRGBAllowsNamedDefaultsAndOverrides(t *testing.T) {
	rgb, err := sceneRGB("Halloween", "orange", []string{"", "", ""})
	if err != nil || rgb != [3]int{255, 80, 0} {
		t.Fatalf("rgb=%v err=%v", rgb, err)
	}
	rgb, err = sceneRGB("Halloween", "orange", []string{"1", "2", "3"})
	if err != nil || rgb != [3]int{1, 2, 3} {
		t.Fatalf("rgb=%v err=%v", rgb, err)
	}
}

func TestPublishPromptIncludesDiff(t *testing.T) {
	old := Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{}}}}
	old, _ = withHash(old)
	next := Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{map[string]any{"id": "halloween", "name": "Halloween", "entities": map[string]any{}}}}}}
	next, _ = withHash(next)
	view := RenderPrompt(statusModel{prompt: "publish", baseline: &old, bundle: next})
	if !strings.Contains(view, "--- scenes") || !strings.Contains(view, "+++ scenes") || !strings.Contains(view, "Type PUBLISH") {
		t.Fatalf("prompt = %q", view)
	}
}

func TestUpgradeModalFitsTerminal(t *testing.T) {
	m := statusModel{width: 80, height: 24, prompt: "upgrade-legacy"}
	view := m.renderUpgradeModal()
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("modal line is %d columns in an %d-column terminal: %q", lipgloss.Width(line), m.width, line)
		}
	}
	if !strings.Contains(view, "Upgrade legacy lighting setup") {
		t.Fatalf("modal title was clipped: %q", view)
	}
}

func TestStatusViewShowsMessage(t *testing.T) {
	view := (statusModel{bundle: Bundle{Hash: "abc"}, message: "Published, verified."}).View()
	if !strings.HasPrefix(view, "Published, verified.") {
		t.Fatalf("view = %q", view)
	}
}

func TestDashboardShowsLegacyUpgradeHint(t *testing.T) {
	bundle := Bundle{Files: map[ConfigKind]Config{
		Scripts: {Kind: Scripts, Data: map[string]any{
			"holiday_lights": map[string]any{"variables": map[string]any{"holiday_colors": map[string]any{"Halloween": []any{"halloween_orange"}}}},
		}},
	}}
	m := statusModel{bundle: bundle, height: 24}
	view := m.View()
	lines := strings.Split(view, "\n")
	visible := strings.Join(lines[max(0, len(lines)-m.height):], "\n")
	for _, want := range []string{
		"Legacy setup detected — press b to upgrade",
		"Tab focus • ←/→ workspace • 1–5 jump • j/k navigate • Enter open",
		"n new • i lights • s save • d diff • u publish • ? help • q quit",
	} {
		if !strings.Contains(visible, want) {
			t.Fatalf("view = %q", view)
		}
	}
}

func TestDraftContentViewShowsWorkingCopy(t *testing.T) {
	m := statusModel{contentView: true, bundle: Bundle{Files: map[ConfigKind]Config{
		Scenes: {Kind: Scenes, Data: []any{map[string]any{"id": "halloween", "name": "Halloween"}}},
	}}}
	view := m.View()
	if !strings.Contains(view, "Draft content: scenes.yaml") || !strings.Contains(view, "halloween") {
		t.Fatalf("content view = %q", view)
	}
}

func TestRenderInventoryListsOnlyLights(t *testing.T) {
	view := RenderInventory(map[string]LightState{
		"sensor.test": {EntityID: "sensor.test", State: "on"},
		"light.wled":  {EntityID: "light.wled", State: "off", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}, "effect_list": []any{"Solid"}}},
	})
	if !strings.Contains(view, "light.wled") || strings.Contains(view, "sensor.test") || !strings.Contains(view, "Color modes (1): rgb") {
		t.Fatalf("inventory = %q", view)
	}
}

func TestRenderSceneColorShowsXYSwatch(t *testing.T) {
	rendered := renderSceneColor(map[string]any{"xy_color": []any{0.385, 0.155}})
	if !strings.Contains(rendered, "#") || !strings.Contains(rendered, "XY (0.385, 0.155)") {
		t.Fatalf("color = %q", rendered)
	}
}

func TestRenderSceneColorPreservesRGBRange(t *testing.T) {
	rendered := renderSceneColor(map[string]any{"rgb_color": []any{255, 80, 0}})
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

type failingPreviewAPI struct{ calls int }

func (f *failingPreviewAPI) States(context.Context, []string) (map[string]LightState, error) {
	return map[string]LightState{
		"light.one": {EntityID: "light.one", State: "on", Attribute: map[string]any{"brightness": 100}},
		"light.two": {EntityID: "light.two", State: "on", Attribute: map[string]any{"brightness": 100}},
	}, nil
}
func (f *failingPreviewAPI) CallService(context.Context, string, string, string, map[string]any) error {
	f.calls++
	if f.calls == 2 {
		return errors.New("device failed")
	}
	return nil
}

func TestLiveSceneFormsDoNotUseDraftTargetsBeforeDiscovery(t *testing.T) {
	api := &failingPreviewAPI{}
	m := statusModel{
		stateAPI: api,
		bundle: Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{map[string]any{
			"id": "existing", "entities": map[string]any{"light.draft_only": map[string]any{}},
		}}}}},
	}
	if m.refreshLightIDs() {
		t.Fatal("refreshLightIDs used draft targets without live discovery")
	}
	if api.calls != 0 {
		t.Fatalf("refreshLightIDs made a synchronous HA call: %d", api.calls)
	}
}

func TestPreviewFailureAutomaticallyRestores(t *testing.T) {
	api := &failingPreviewAPI{}
	m := statusModel{
		bundle: Bundle{Files: map[ConfigKind]Config{Scenes: {Kind: Scenes, Data: []any{map[string]any{"id": "test", "name": "Test", "entities": map[string]any{
			"light.one": map[string]any{"state": "on", "brightness": 200}, "light.two": map[string]any{"state": "on", "brightness": 200},
		}}}}}},
		prompt: "preview", input: "PREVIEW", stateAPI: api, preview: Preview{API: api},
	}
	m.finishPrompt()
	if !strings.Contains(m.message, "Preview failed") || api.calls != 4 {
		t.Fatalf("message=%q calls=%d", m.message, api.calls)
	}
}
