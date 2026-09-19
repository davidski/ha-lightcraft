package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWebHealthz(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	(&webApp{}).handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Body.String() != "ok\n" {
		t.Fatalf("healthz = %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestWebAllowedHosts(t *testing.T) {
	dir := t.TempDir()
	if err := SaveBundle(dir, testLightingBundle(t)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEB_ALLOWED_HOSTS", "cherry.woohouse.world, editor.example.test")
	app, err := newWebApp(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		host string
		want int
	}{
		{host: "127.0.0.1:8080", want: http.StatusOK},
		{host: "CHERRY.WOOHOUSE.WORLD", want: http.StatusOK},
		{host: "editor.example.test:443", want: http.StatusOK},
		{host: "untrusted.example.test", want: http.StatusForbidden},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Host = test.host
		app.handler().ServeHTTP(response, request)
		if response.Code != test.want {
			t.Errorf("Host %q = %d, want %d", test.host, response.Code, test.want)
		}
	}
}

func TestWebTemplateContextualEscaping(t *testing.T) {
	malicious := `</script><script>alert("x")</script>&'"`
	page := webPage{
		View:        "schedules",
		Assignment:  webAssignment{ID: malicious, Name: malicious, Kind: "wled", WLEDLight: malicious, WLEDSelect: malicious, WLEDOption: malicious},
		WLEDLights:  []webLight{{ID: malicious, Name: malicious}},
		WLEDSelects: []webSelect{{ID: malicious, Name: malicious, Options: []string{malicious}}},
		WLEDChoices: []webWLEDChoice{{Light: webLight{ID: malicious, Name: malicious}, Selects: []webSelect{{ID: malicious, Name: malicious, Options: []string{malicious}}}}},
		Sequence: webSequence{ID: malicious, Name: malicious, Repeat: true,
			PreviewSteps:  []webPreviewStep{{Name: malicious, Hex: "#FF0000", XY: "XY (0.640, 0.330)", Brightness: 128, Hold: 2}},
			PreviewLights: []webPreviewLight{{ID: malicious, Name: malicious}},
		},
		Sequences: []webSequence{{ID: malicious, Name: malicious}},
	}
	for _, view := range []string{"schedules", "sequences"} {
		t.Run(view, func(t *testing.T) {
			page.View = view
			var output bytes.Buffer
			if err := webTemplate.Execute(&output, page); err != nil {
				t.Fatal(err)
			}
			body := output.String()
			if strings.Contains(body, malicious) || strings.Contains(body, "ZgotmplZ") || !strings.Contains(body, html.EscapeString(malicious)) {
				t.Fatalf("unsafe or lost template value: %s", body)
			}
			if view == "schedules" {
				_, data, ok := strings.Cut(body, "const wledChoices=")
				if !ok {
					t.Fatal("missing WLED choices")
				}
				data, _, _ = strings.Cut(data, ";function wledChoice")
				var choices []webWLEDChoice
				if err := json.Unmarshal([]byte(data), &choices); err != nil {
					t.Fatal(err)
				}
				if len(choices) != 1 || choices[0].Light.ID != malicious || choices[0].Light.Name != malicious || choices[0].Selects[0].ID != malicious || choices[0].Selects[0].Name != malicious || choices[0].Selects[0].Options[0] != malicious {
					t.Fatalf("WLED JSON did not round trip: %#v", choices)
				}
				if strings.Contains(data, "</script>") {
					t.Fatal("WLED JSON can terminate script")
				}
			} else {
				_, value, ok := strings.Cut(body, `data-name="`)
				if !ok {
					t.Fatal("missing preview data")
				}
				value, _, _ = strings.Cut(value, `"`)
				if html.UnescapeString(value) != malicious {
					t.Fatalf("preview name did not round trip: %q", value)
				}
				for _, want := range []string{`background-color:#FF0000;opacity:0.502`, `data-brightness="128" data-hold="2"`, "repeat= true", "name.textContent=step.dataset.name", "setTimeout(()=>", "show()})()", "querySelector('.preview-bar')"} {
					if !strings.Contains(body, want) {
						t.Errorf("preview missing %q", want)
					}
				}
				_, href, ok := strings.Cut(body, `href="/?view=sequences&edit=`)
				if !ok {
					t.Fatal("missing sequence link")
				}
				href, _, _ = strings.Cut(href, `"`)
				id, err := url.QueryUnescape(html.UnescapeString(href))
				if err != nil || id != malicious {
					t.Fatalf("sequence ID did not round trip: %q, %v", id, err)
				}
			}
		})
	}
}

func TestWebLegacyViewAliases(t *testing.T) {
	dir := t.TempDir()
	if err := SaveBundle(dir, testLightingBundle(t)); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"publish", "yaml"}, {"inventory", "lights"}} {
		var canonical string
		for _, view := range pair {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/?view="+view, nil)
			request.Host = "127.0.0.1:8080"
			app.handler().ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", view, response.Code, response.Body.String())
			}
			body := response.Body.String()
			if strings.Contains(body, "?view=yaml") || strings.Contains(body, "?view=lights") {
				t.Fatalf("legacy navigation in %s", view)
			}
			if canonical == "" {
				canonical = body
			} else if body != canonical {
				t.Fatalf("%s differs from %s", view, pair[0])
			}
		}
	}
}

func TestWebStartsEmptyAndImportsHomeAssistantYAML(t *testing.T) {
	draftDir := t.TempDir()
	baselineDir := t.TempDir()
	colorsDir := t.TempDir()
	remoteDir := t.TempDir()
	paths := map[ConfigKind][]string{
		Scripts:     {"packages/ha_lightcraft.yaml"},
		Automations: {"packages/ha_lightcraft.yaml"},
		Helpers:     {"packages/ha_lightcraft.yaml"},
	}
	writeNestedNative(t, remoteDir, "packages/ha_lightcraft.yaml", "automation: []\ninput_select: {}\nscript: {}\n")

	app, err := newWebAppWithReferences(draftDir, baselineDir, colorsDir, paths)
	if err != nil {
		t.Fatal(err)
	}
	if app.baselineReady || len(app.bundle.Files) != 0 {
		t.Fatalf("empty web app was marked ready: baseline=%v bundle=%#v", app.baselineReady, app.bundle)
	}
	store := NativeYAMLStore{Transport: LocalFileTransport{Root: remoteDir}, ConfigDir: ".", FilePaths: paths}
	app.importer = &store

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?view=publish", nil)
	request.Host = "127.0.0.1:8080"
	app.handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "Type IMPORT to confirm") {
		t.Fatalf("empty web import form = %d %s", response.Code, response.Body.String())
	}

	postForm(t, app.handler(), "/import", url.Values{
		"token": {app.token},
		"hash":  {app.bundle.Hash},
	}, http.StatusSeeOther)
	if !app.baselineReady || len(app.bundle.Files) != 3 {
		t.Fatalf("import did not initialize web state: baseline=%v bundle=%#v", app.baselineReady, app.bundle)
	}
	if _, err := LoadBundleAt(baselineDir, paths); err != nil {
		t.Fatalf("imported baseline missing: %v", err)
	}
	if _, err := LoadBundleAt(draftDir, paths); err != nil {
		t.Fatalf("imported draft missing: %v", err)
	}
	postForm(t, app.handler(), "/import", url.Values{
		"token": {app.token},
		"hash":  {app.bundle.Hash},
	}, http.StatusSeeOther)
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/?view=publish", nil)
	request.Host = "127.0.0.1:8080"
	app.handler().ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), "type IMPORT to confirm") {
		t.Fatalf("non-empty web import did not require confirmation: %s", response.Body.String())
	}
}

func TestWebStatusShowsPublishedChanges(t *testing.T) {
	dir := t.TempDir()
	baselineDir := t.TempDir()
	bundle, err := saveColorSequence(Bundle{}, testColorSequence())
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	if err := SaveBundle(baselineDir, bundle); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, baselineDir)
	if err != nil {
		t.Fatal(err)
	}
	handler := app.handler()
	get := func() string {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/?view=publish", nil)
		request.Host = "127.0.0.1:8080"
		handler.ServeHTTP(response, request)
		return response.Body.String()
	}
	if body := get(); strings.Contains(body, "Unsaved changes in memory") || strings.Contains(body, "Unpublished changes") {
		t.Fatalf("clean draft has status markers: %s", body)
	}
	sequence := colorSequences(app.bundle)["christmas"]
	sequence.Steps[0].Hold++
	app.bundle, err = saveColorSequence(app.bundle, sequence)
	if err != nil {
		t.Fatal(err)
	}
	if body := get(); strings.Contains(body, "Unsaved changes in memory") || !strings.Contains(body, "Unpublished changes") {
		t.Fatalf("draft status incorrectly reports an in-memory-only change: %s", body)
	}
	app.baseline = app.bundle
	if body := get(); strings.Contains(body, "Unpublished changes") {
		t.Fatalf("published draft still shows unpublished status: %s", body)
	}
}

func TestWebAgendaShowsDenseScheduleRows(t *testing.T) {
	dir := t.TempDir()
	if err := SaveBundle(dir, testLightingBundle(t)); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?view=agenda&date=2026-12-01", nil)
	request.Host = "127.0.0.1:8080"
	app.handler().ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `class="active">Agenda</a>`) || !strings.Contains(body, "Schedule agenda") || strings.Count(body, `class="agenda-week"`) != 2 || strings.Count(body, `class="agenda-table"`) != 2 || !strings.Contains(body, `view=schedules&amp;edit=exterior`) || !strings.Contains(body, "Exterior") || !strings.Contains(body, "4 lights") || !strings.Contains(body, "Nov 29") || !strings.Contains(body, "Dec 6") || !strings.Contains(body, "2026") {
		t.Fatalf("agenda page = %d %s", response.Code, body)
	}
}

func TestWebSequenceUpdateKeepsSelection(t *testing.T) {
	dir := t.TempDir()
	bundle, err := saveColorSequence(Bundle{Files: map[ConfigKind]Config{
		Colors: {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}},
	}}, testColorSequence())
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/sequence", strings.NewReader(url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"christmas"}, "display_name": {"Christmas lights"}, "repeat": {"on"},
		"step_color": {"red", "red"}, "step_brightness": {"80", "70"}, "step_hold": {"6", "3"}, "step_transition": {"0.5", "1"},
	}.Encode()))
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	app.handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/?view=sequences&edit=christmas" {
		t.Fatalf("sequence update redirect = %d %q", response.Code, response.Header().Get("Location"))
	}

	request = httptest.NewRequest(http.MethodGet, response.Header().Get("Location"), nil)
	request.Host = "127.0.0.1:8080"
	response = httptest.NewRecorder()
	app.handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Edit sequence") || !strings.Contains(response.Body.String(), "Christmas lights") {
		t.Fatalf("updated sequence was not selected: %d %s", response.Code, response.Body.String())
	}
}

func TestWebYAMLUsesOnePackageEntryAndDiff(t *testing.T) {
	dir := t.TempDir()
	colorsDir := t.TempDir()
	paths := map[ConfigKind][]string{
		Scripts:     {"packages/ha_lightcraft.yaml"},
		Automations: {"packages/ha_lightcraft.yaml"},
		Helpers:     {"packages/ha_lightcraft.yaml"},
	}
	bundle := Bundle{Files: map[ConfigKind]Config{
		Scripts:     {Kind: Scripts, Data: map[string]any{"holiday": map[string]any{"alias": "Holiday", "sequence": []any{}}}},
		Automations: {Kind: Automations, Data: []any{map[string]any{"id": "holiday"}}},
		Helpers:     {Kind: Helpers, Data: map[string]any{"holiday": map[string]any{"name": "Holiday"}}},
	}}
	if err := SaveBundleAtWithReferences(dir, colorsDir, bundle, paths); err != nil {
		t.Fatal(err)
	}
	app, err := newWebAppWithReferences(dir, "", colorsDir, paths)
	if err != nil {
		t.Fatal(err)
	}
	app.bundle.Files[Scripts] = Config{Kind: Scripts, Data: map[string]any{"holiday": map[string]any{"alias": "Changed", "sequence": []any{}}}}
	app.bundle, err = withHash(app.bundle)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?view=publish", nil)
	request.Host = "127.0.0.1:8080"
	app.handler().ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || strings.Contains(body, "Home Assistant files") || strings.Contains(body, `<div class="grid"><section class="panel"><h2>Unified diff:`) || strings.Count(body, `class="panel yaml-file"`) != 1 || strings.Count(body, `class="panel yaml-diff"`) != 1 || !strings.Contains(body, `class="panel yaml-file"><h2>File to publish</h2>`) || !strings.Contains(body, `class="panel yaml-diff"><h2>Changes to publish</h2>`) || !strings.Contains(body, `.yaml-panels{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:20px;margin-top:20px}`) || !strings.Contains(body, "alias: Changed") || strings.Contains(body, "Save draft") || strings.Contains(body, "Save draft files") || !strings.Contains(body, ">Home Assistant</a>") {
		t.Fatalf("web package YAML = %d %s", response.Code, body)
	}
}

func TestWebDraftEditPersists(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{Files: map[ConfigKind]Config{
		Colors: {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}},
	}}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.handler()

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	if body := response.Body.String(); !strings.Contains(body, `<a href="/?view=colors" class="active">Colors</a>`) || !strings.Contains(body, `<link rel="preload" href="/favicon.png" as="image" type="image/png" fetchpriority="high">`) || !strings.Contains(body, `<link rel="icon" href="/favicon.png" type="image/png">`) || !strings.Contains(body, `<img class="brand-icon" src="/favicon.png" alt="">`) {
		t.Fatalf("default web view is not Colors: %s", body)
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/favicon.png", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" || response.Body.Len() == 0 {
		t.Fatalf("favicon is not served: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/?view=colors&edit=red", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	wantHex := cieRGBHex(CIEColor{X: .64, Y: .33})
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(body, "scrollbar-gutter:stable") || !strings.Contains(body, ">Home Assistant</a>") || !strings.Contains(body, ">Inventory</a>") || !strings.Contains(body, `.nav-divider{width:1px;align-self:stretch;`) || !strings.Contains(body, `class="nav-divider"`) || strings.Contains(body, ">YAML / Diff</a>") || !strings.Contains(body, "Red") || !strings.Contains(body, `name="display_name" autocomplete="off"`) || strings.Contains(body, `name="name"`) || !strings.Contains(body, `type="color" value="`+wantHex+`"`) || !strings.Contains(body, "setColorXY") || !strings.Contains(body, `style="display:grid;grid-template-columns:1fr 1fr;gap:8px"`) || strings.Index(body, `view=colors`) > strings.Index(body, `view=sequences`) || strings.Index(body, `view=sequences`) > strings.Index(body, `view=schedules`) || strings.Index(body, `view=schedules`) > strings.Index(body, `view=inventory`) {
		t.Fatalf("unexpected color page: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(body, `.grid > .panel:first-child > .button{order:1`) || !strings.Contains(body, `.grid > .panel:first-child > .button{width:max-content;max-width:100%}`) || !strings.Contains(body, `.grid > .panel:first-child > .items{order:2}`) {
		t.Fatalf("New color action is not positioned above the sidebar list: %s", body)
	}
	if !strings.Contains(body, `input[type=color]{height:52px;padding:4px;cursor:pointer;border-radius:5px}`) {
		t.Fatalf("color picker does not match preview bar rounding: %s", body)
	}
	xyStart := strings.Index(body, `style="display:grid;grid-template-columns:1fr 1fr;gap:8px"`)
	if xyStart < 0 || !strings.Contains(body[xyStart:], `<div><label for="color-x">`) || !strings.Contains(body[xyStart:], `</div><div><label for="color-y">`) || !strings.Contains(body[xyStart:], `</div></div><button>Update draft`) {
		t.Fatalf("X/Y controls are not in one row container: %s", body)
	}

	postForm(t, handler, "/color", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"red"}, "display_name": {"Blue"}, "x": {"0.3"}, "y": {"0.6"},
	}, http.StatusSeeOther)
	if colorDefinitions(app.bundle)["red"].X != .3 {
		t.Fatal("color edit was not applied to the draft")
	}
	saved, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if colorDefinitions(saved)["red"].X != .3 {
		t.Fatal("color edit was not persisted immediately")
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/?view=colors&edit=red", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	if body := response.Body.String(); !strings.Contains(body, `name="x" type="number" step="any" required value="0.3"`) {
		t.Fatalf("edited color was not retained in the web form: %s", body)
	}
	for _, id := range []string{"blue", "red"} {
		response = httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodGet, "/?view=colors&edit="+id, nil)
		request.Host = "127.0.0.1:8080"
		handler.ServeHTTP(response, request)
	}
	if body := response.Body.String(); !strings.Contains(body, `name="x" type="number" step="any" required value="0.3"`) {
		t.Fatalf("edited color was lost after navigation: %s", body)
	}

	postForm(t, handler, "/delete", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "kind": {"colors"}, "id": {"red"},
	}, http.StatusSeeOther)
	if _, exists := colorDefinitions(app.bundle)["red"]; exists {
		t.Fatal("draft delete did not remove the color")
	}
	saved, err = LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := colorDefinitions(saved)["red"]; exists {
		t.Fatal("draft delete was not persisted immediately")
	}

	postForm(t, handler, "/color", url.Values{
		"token": {app.token}, "hash": {"stale"}, "display_name": {"Blue"}, "x": {"0.2"}, "y": {"0.2"},
	}, http.StatusConflict)

	postForm(t, handler, "/color", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "display_name": {"Blue"}, "x": {"0.2"}, "y": {"0.2"},
	}, http.StatusSeeOther)
	external, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	external, err = upsertColor(external, "yellow", ColorDefinition{Name: "Yellow", X: .4, Y: .5})
	if err != nil || SaveBundle(dir, external) != nil {
		t.Fatal("could not prepare external draft edit")
	}
	postForm(t, handler, "/color", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"blue"}, "display_name": {"Blue"}, "x": {"0.25"}, "y": {"0.25"},
	}, http.StatusConflict)
	onDisk, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	blue, blueOK := colorDefinitions(onDisk)["blue"]
	yellow, yellowOK := colorDefinitions(onDisk)["yellow"]
	if !blueOK || blue.X != .2 || !yellowOK || yellow.Name != "Yellow" {
		t.Fatal("save overwrote an external draft edit")
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "attacker.example"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-local Host status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestWebForwardedPrefix(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{Files: map[ConfigKind]Config{
		Colors: {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}},
	}}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.handler()
	request := httptest.NewRequest(http.MethodGet, "/designer/?view=colors", nil)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("X-Forwarded-Prefix", "/designer")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `href="/designer/?view=colors"`) || !strings.Contains(body, `href="/designer/favicon.png"`) || !strings.Contains(body, `action="/designer/color"`) {
		t.Fatalf("forwarded-prefix page paths are wrong: %d %s", response.Code, body)
	}

	form := url.Values{"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"red"}, "display_name": {"Blue"}, "x": {"0.3"}, "y": {"0.6"}}
	request = httptest.NewRequest(http.MethodPost, "/designer/color", strings.NewReader(form.Encode()))
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Forwarded-Prefix", "/designer")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/designer/?view=colors" {
		t.Fatalf("forwarded-prefix redirect is wrong: %d %q", response.Code, response.Header().Get("Location"))
	}

	request = httptest.NewRequest(http.MethodGet, "/designer/favicon.png", nil)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("X-Forwarded-Prefix", "/designer")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.Len() == 0 {
		t.Fatalf("forwarded-prefix favicon failed: %d", response.Code)
	}
}

func TestWebLightInventoryUsesPlainColorText(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{Files: map[ConfigKind]Config{
		Colors: {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}},
	}}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	app.inventoryStates = map[string]LightState{
		"light.bath": {EntityID: "light.bath", State: "off", Attribute: map[string]any{"entity_id": []any{"light.bath_left", "light.bath_right"}}},
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?view=inventory", nil)
	request.Host = "127.0.0.1:8080"
	app.handler().ServeHTTP(response, request)
	body := response.Body.String()
	if strings.Contains(body, "\x1b[") || !strings.Contains(body, `class="light-kind group">Group</span>`) || !strings.Contains(body, "Color: not specified") {
		t.Fatalf("web inventory contains terminal styling: %q", body)
	}
}

func TestWebPublishUsesGuardedPublisher(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{Files: map[ConfigKind]Config{
		Colors: {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}},
	}}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &webTestPublisher{}
	app.publisher = publisher
	handler := app.handler()
	postForm(t, handler, "/sequence", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "display_name": {"Red loop"}, "repeat": {"on"},
		"step_color": {"red"}, "step_brightness": {"100"}, "step_hold": {"1"}, "step_transition": {"0"},
	}, http.StatusSeeOther)
	if got := colorSequences(app.bundle)["red_loop"].Steps[0].Brightness; got != 255 {
		t.Fatalf("web percentage brightness = %d, want 255", got)
	}
	beforePublish, err := PublishDiff(app.baseline, app.bundle)
	if err != nil || len(beforePublish) == 0 {
		t.Fatalf("web edit did not differ from publish baseline: changes=%v err=%v", beforePublish, err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?view=publish", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Type PUBLISH to confirm") {
		t.Fatalf("publish form unavailable: %d %s", response.Code, response.Body.String())
	}

	postForm(t, handler, "/publish", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "confirmation": {"publish"},
	}, http.StatusSeeOther)
	if publisher.calls != 0 {
		t.Fatal("publish accepted the wrong confirmation")
	}
	postForm(t, handler, "/publish", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "confirmation": {"PUBLISH"},
	}, http.StatusSeeOther)
	if publisher.calls != 1 {
		t.Fatalf("publisher calls = %d, want 1", publisher.calls)
	}
	changes, err := PublishDiff(app.baseline, app.bundle)
	if err != nil || len(changes) != 0 {
		t.Fatalf("publish baseline was not refreshed: changes=%v err=%v", changes, err)
	}
}

func TestWebStepColorMatchesConvertedSequenceStep(t *testing.T) {
	x, y, ok := rgbToXY([3]int{255, 0, 0})
	if !ok {
		t.Fatal("red RGB did not convert to XY")
	}
	bundle := Bundle{Files: map[ConfigKind]Config{Colors: {Kind: Colors, Data: map[string]any{
		"red": map[string]any{"name": "Red", "x": .64, "y": .33},
	}}}}
	if got := webStepColor(bundle, ColorStep{Name: "Red", XY: []float64{x, y}}); got != "red" {
		t.Fatalf("web step color = %q, want red", got)
	}
}

func TestWebScheduleLightPickerAndPlayback(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{Files: map[ConfigKind]Config{
		Colors: {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}},
	}}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	api := &webTestStateAPI{states: map[string]LightState{
		"light.one":       {EntityID: "light.one", State: "off", Attribute: map[string]any{"friendly_name": "One", "supported_color_modes": []any{"xy"}}},
		"light.two":       {EntityID: "light.two", State: "on", Attribute: map[string]any{"friendly_name": "Two", "brightness": 80, "supported_color_modes": []any{"rgb"}, "entity_id": []any{"light.two_left", "light.two_right"}}},
		"light.two_left":  {EntityID: "light.two_left", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}}},
		"light.two_right": {EntityID: "light.two_right", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}}},
	}}
	app.stateAPI = api
	app.refreshInventory(context.Background())
	handler := app.handler()

	sequence := ColorSequence{ID: "red_loop", Name: "Red loop", Repeat: true, Steps: []ColorStep{{Name: "Red", XY: []float64{.64, .33}, Brightness: 100, Hold: 1}}}
	app.bundle, err = saveColorSequence(app.bundle, sequence)
	if err != nil {
		t.Fatal(err)
	}
	assignment := LightingAssignment{ID: "porch", Name: "Porch", Sequence: "red_loop", Targets: []string{"light.one"}, Start: "12-01", End: "12-31", On: "sunset", Off: "00:00", Finish: "off", Enabled: true}
	app.bundle, err = saveLightingAssignment(app.bundle, assignment)
	if err != nil {
		t.Fatal(err)
	}
	app.bundle, err = upsertColor(app.bundle, "red", ColorDefinition{Name: "Red", X: .2, Y: .2})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveBundle(dir, app.bundle); err != nil {
		t.Fatal(err)
	}
	app.baseline, _ = cloneBundle(app.bundle)
	emptyResponse := httptest.NewRecorder()
	emptyRequest := httptest.NewRequest(http.MethodGet, "/?view=sequences", nil)
	emptyRequest.Host = "127.0.0.1:8080"
	handler.ServeHTTP(emptyResponse, emptyRequest)
	emptyBody := emptyResponse.Body.String()
	emptySlot := strings.Index(emptyBody, `class="sequence-preview-slot"`)
	emptyGrid := strings.Index(emptyBody, `<div class="grid"><section class="panel"><h2>Sequences`)
	if emptySlot < 0 || emptyGrid < 0 || emptySlot > emptyGrid || !strings.Contains(emptyBody, `class="sequence-preview-slot" style="height:300px;margin:0 0 20px`) || !strings.Contains(emptyBody, "Select a sequence to preview it.") {
		t.Fatalf("sequence preview slot missing before selection: %s", emptyBody)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?view=schedules&edit=porch", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	groupIndex := strings.Index(body, `class="group-target"`)
	singleIndex := strings.Index(body, `value="light.one"`)
	if !strings.Contains(body, `type="checkbox" value="light.one"`) || strings.Contains(body, `class="light-kind single">Single</span>`) || !strings.Contains(body, `class="light-kind group">Group</span>`) || groupIndex < 0 || singleIndex < 0 || groupIndex > singleIndex || !strings.Contains(body, `id="schedule-title" name="schedule_title" autocomplete="off"`) || strings.Contains(body, `id="schedule-name"`) || !strings.Contains(body, `Start date (M-D or YYYY-M-D)`) || !strings.Contains(body, `End date (M-D or YYYY-M-D)`) || !strings.Contains(body, `Start time (HH:MM or sunset)`) || !strings.Contains(body, `Stop time (HH:MM)`) || !strings.Contains(body, `<label>When schedule stops<select name="finish" required>`) || !strings.Contains(body, `<option value="off" selected>Turn lights off</option>`) || !strings.Contains(body, `<option value="leave">Leave current light state</option>`) || strings.Contains(body, `<input name="finish"`) || !strings.Contains(body, `class="step-grid schedule-times"`) || !strings.Contains(body, `grid-template-areas:"start-date start-time" "end-date stop-time" "finish finish"`) {
		t.Fatalf("schedule light picker missing: %s", body)
	}
	if !strings.Contains(body, `</ul><a class="button" href="/?view=schedules">New schedule</a>`) {
		t.Fatalf("new schedule action is still inside the list: %s", body)
	}
	scheduleStart := strings.Index(body, `<form method="post" action="/schedule">`)
	scheduleEnd := -1
	if scheduleStart >= 0 {
		scheduleEnd = strings.Index(body[scheduleStart:], `</form>`)
	}
	if scheduleStart < 0 || scheduleEnd < 0 || !strings.Contains(body[scheduleStart:scheduleStart+scheduleEnd], `<div class="form-actions"><button>Update draft</button></div>`) {
		t.Fatalf("schedule update action is not inside the editor form: %s", body)
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/?view=sequences&edit=red_loop", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	body = response.Body.String()
	previewIndex := strings.Index(body, `<section class="panel sequence-preview" id="sequence-preview">`)
	gridIndex := strings.Index(body, `<div class="grid"><section class="panel"><h2>Sequences`)
	if previewIndex < 0 || gridIndex < 0 || previewIndex > gridIndex || !strings.Contains(body, `class="sequence-preview-slot" style="height:300px;margin:0 0 20px`) || !strings.Contains(body, `name="display_name" autocomplete="off"`) || !strings.Contains(body, `name="step_brightness" type="number" min="0" max="100" step="1"`) || !strings.Contains(body, "light.one") || !strings.Contains(body, `class="preview-color-bar"`) || !strings.Contains(body, `background-color:`+previewRGBHex(currentCatalogStep(app.bundle, sequence.Steps[0]))) || !strings.Contains(body, "XY (0.200, 0.200)") || !strings.Contains(body, "RGB") || !strings.Contains(body, `data-brightness="100"`) || !strings.Contains(body, `<label style="display:none">Name<input name="step_name"`) {
		t.Fatalf("sequence preview missing: %s", body)
	}
	api.states["light.back_door_light"] = LightState{EntityID: "light.back_door_light", State: "off", Attribute: map[string]any{"supported_color_modes": []any{"onoff"}}}
	app.refreshInventory(context.Background())
	if ids := webTargetLightIDs(app.inventoryStates, app.inventoryLocations); strings.Contains(strings.Join(ids, ","), "light.back_door_light") {
		t.Fatalf("web target picker included non-color light: %v", ids)
	}
	postForm(t, handler, "/schedule", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"back_door"}, "schedule_title": {"Back door"}, "sequence": {"red_loop"},
		"targets": {"light.back_door_light"}, "start": {"02-14"}, "end": {"02-18"}, "on": {"18:30"}, "off": {"23:15"}, "finish": {"leave"}, "enabled": {"on"},
	}, http.StatusSeeOther)
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/?view=schedules", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	body = response.Body.String()
	if !strings.Contains(body, "Could not update draft: light.back_door_light does not support color") || !strings.Contains(body, `value="Back door"`) || !strings.Contains(body, `value="02-14"`) || !strings.Contains(body, `value="02-18"`) || !strings.Contains(body, `value="18:30"`) || !strings.Contains(body, `value="23:15"`) || !strings.Contains(body, `value="light.back_door_light" checked`) || !strings.Contains(body, `<option value="leave" selected>Leave current light state</option>`) {
		t.Fatalf("schedule form data was lost after target validation error: %s", body)
	}
	if _, exists := lightingAssignments(app.bundle)["back_door"]; exists {
		t.Fatal("invalid schedule was added to the draft")
	}
	postForm(t, handler, "/schedule", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"unpadded_web"}, "schedule_title": {"Unpadded web"}, "sequence": {"red_loop"},
		"targets": {"light.one"}, "start": {"1-15"}, "end": {"2-3"}, "on": {"18:30"}, "off": {"23:15"}, "finish": {"off"}, "enabled": {"on"},
	}, http.StatusSeeOther)
	if saved := lightingAssignments(app.bundle)["unpadded_web"]; saved.Start != "01-15" || saved.End != "02-03" {
		t.Fatalf("web saved dates = %q–%q, want 01-15–02-03", saved.Start, saved.End)
	}
	api.states["input_select.lighting_assignment_porch"] = LightState{EntityID: "input_select.lighting_assignment_porch", State: "idle", Attribute: map[string]any{}}
	postForm(t, handler, "/playback", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"porch"}, "action": {"pause"}, "confirmation": {"PAUSE"},
	}, http.StatusSeeOther)
	if !api.called("input_select", "select_option", "input_select.lighting_assignment_porch") {
		t.Fatalf("playback calls = %#v", api.calls)
	}
}

func TestWebWLEDScheduleAndAllDayForm(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{Files: map[ConfigKind]Config{Colors: {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}}}}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	app.stateAPI = &webTestStateAPI{states: map[string]LightState{
		"light.floating_string":         {EntityID: "light.floating_string", State: "on", Attribute: map[string]any{"friendly_name": "Floating string", "supported_color_modes": []any{"rgb"}}},
		"select.floating_string_preset": {EntityID: "select.floating_string_preset", State: "Christmas", Attribute: map[string]any{"friendly_name": "Floating string presets", "options": []any{"Christmas", "Winter ice"}}},
	}, inventory: &HAInventory{Metadata: map[string]HAEntityMetadata{
		"light.floating_string":         {EntityID: "light.floating_string", Platform: "wled", DeviceID: "wled-1", ConfigEntryID: "entry-1"},
		"select.floating_string_preset": {EntityID: "select.floating_string_preset", Platform: "wled", DeviceID: "wled-1", ConfigEntryID: "entry-1", TranslationKey: "preset"},
	}}}
	app.refreshInventory(context.Background())
	handler := app.handler()
	postForm(t, handler, "/schedule", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"floating_string"}, "schedule_title": {"Floating string"}, "kind": {"wled"},
		"wled_light": {"light.floating_string"}, "wled_select": {"select.floating_string_preset"}, "wled_option": {"Christmas"},
		"start": {"11-15"}, "end": {"01-15"}, "all_day": {"on"}, "finish": {"off"}, "enabled": {"on"},
	}, http.StatusSeeOther)
	a := lightingAssignments(app.bundle)["floating_string"]
	if a.WLED == nil || !a.AllDay || a.WLED.Option != "Christmas" {
		t.Fatalf("web WLED assignment not saved: %#v", a)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?view=schedules&edit=floating_string", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	for _, want := range []string{"Program type", "WLED preset or playlist", "light.floating_string", "select.floating_string_preset", `<select id="wled-option" name="wled_option">`, `<option value="Christmas" selected>Christmas</option>`, `<option value="Winter ice">Winter ice</option>`, "name=\"all_day\"", "All day (runs continuously"} {
		if !strings.Contains(body, want) {
			t.Fatalf("WLED form missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `<input id="wled-option"`) || strings.Contains(body, `<datalist id="wled-options">`) {
		t.Fatalf("WLED option control is not a dropdown: %s", body)
	}
	if strings.Contains(body, "Assignment type") || strings.Contains(body, "Assignment kind") {
		t.Fatalf("WLED form retained the old program type label: %s", body)
	}
	postForm(t, handler, "/schedule", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"invalid_wled"}, "schedule_title": {"Invalid WLED"}, "kind": {"wled"},
		"wled_light": {"light.floating_string"}, "wled_select": {"select.floating_string_preset"}, "wled_option": {"Missing option"},
		"start": {"11-15"}, "end": {"01-15"}, "all_day": {"on"}, "finish": {"off"}, "enabled": {"on"},
	}, http.StatusSeeOther)
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/?view=schedules", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	body = response.Body.String()
	if !strings.Contains(body, "Could not update draft: WLED option") || !strings.Contains(body, `value="Missing option"`) || !strings.Contains(body, `option value="wled" selected`) {
		t.Fatalf("WLED validation did not preserve form values: %s", body)
	}
	if _, exists := lightingAssignments(app.bundle)["invalid_wled"]; exists {
		t.Fatal("invalid WLED assignment was added to draft")
	}
}

func TestWebWLEDPickerScopesSelectorsAndOptionsToController(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{Files: map[ConfigKind]Config{Colors: {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}}}}
	if err := SaveBundle(dir, bundle); err != nil {
		t.Fatal(err)
	}
	app, err := newWebApp(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]LightState{
		"light.one":            {EntityID: "light.one", State: "on", Attribute: map[string]any{"friendly_name": "WLED one", "supported_color_modes": []any{"rgb"}}},
		"light.two":            {EntityID: "light.two", State: "on", Attribute: map[string]any{"friendly_name": "WLED two", "supported_color_modes": []any{"rgb"}}},
		"light.hue_named_wled": {EntityID: "light.hue_named_wled", State: "on", Attribute: map[string]any{"friendly_name": "WLED Rainbow", "supported_color_modes": []any{"rgb"}, "effect": "WLED"}},
		"select.one_preset":    {EntityID: "select.one_preset", Attribute: map[string]any{"options": []any{"One preset"}}},
		"select.one_playlist":  {EntityID: "select.one_playlist", Attribute: map[string]any{"options": []any{"One playlist"}}},
		"select.one_palette":   {EntityID: "select.one_palette", Attribute: map[string]any{"options": []any{"Palette"}}},
		"select.two_preset":    {EntityID: "select.two_preset", Attribute: map[string]any{"options": []any{"Two preset"}}},
		"select.hue_preset":    {EntityID: "select.hue_preset", Attribute: map[string]any{"options": []any{"Hue"}}},
	}
	metadata := map[string]HAEntityMetadata{
		"light.one":            {EntityID: "light.one", Platform: "wled", DeviceID: "wled-1", ConfigEntryID: "entry-1"},
		"light.two":            {EntityID: "light.two", Platform: "wled", DeviceID: "wled-2", ConfigEntryID: "entry-2"},
		"light.hue_named_wled": {EntityID: "light.hue_named_wled", Platform: "hue", DeviceID: "hue-1", ConfigEntryID: "hue-entry"},
		"select.one_preset":    {EntityID: "select.one_preset", Platform: "wled", DeviceID: "wled-1", ConfigEntryID: "entry-1", TranslationKey: "preset"},
		"select.one_playlist":  {EntityID: "select.one_playlist", Platform: "wled", DeviceID: "wled-1", ConfigEntryID: "entry-1", TranslationKey: "playlist"},
		"select.one_palette":   {EntityID: "select.one_palette", Platform: "wled", DeviceID: "wled-1", ConfigEntryID: "entry-1", TranslationKey: "palette"},
		"select.two_preset":    {EntityID: "select.two_preset", Platform: "wled", DeviceID: "wled-2", ConfigEntryID: "entry-2", TranslationKey: "preset"},
		"select.hue_preset":    {EntityID: "select.hue_preset", Platform: "hue", DeviceID: "wled-1", ConfigEntryID: "entry-1", TranslationKey: "preset"},
	}
	app.stateAPI = &webTestStateAPI{states: states, inventory: &HAInventory{Metadata: metadata}}
	app.refreshInventory(context.Background())
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?view=schedules", nil)
	request.Host = "127.0.0.1:8080"
	app.handler().ServeHTTP(response, request)
	body := response.Body.String()
	lightStart := strings.Index(body, `id="wled-light"`)
	lightEnd := strings.Index(body[lightStart:], `</select>`)
	if lightStart < 0 || lightEnd < 0 {
		t.Fatalf("WLED light picker missing: %s", body)
	}
	lightPicker := body[lightStart : lightStart+lightEnd]
	if !strings.Contains(lightPicker, `value="light.one"`) || !strings.Contains(lightPicker, `value="light.two"`) || strings.Contains(lightPicker, "hue_named_wled") {
		t.Fatalf("WLED light picker was not registry-scoped: %s", lightPicker)
	}
	if strings.Contains(lightPicker, "multiple") || !strings.Contains(body, `<select id="wled-light"`) {
		t.Fatalf("WLED light control is not a single-select: %s", lightPicker)
	}
	selectStart := strings.Index(body, `id="wled-select"`)
	selectEnd := strings.Index(body[selectStart:], `</select>`)
	if selectStart < 0 || selectEnd < 0 {
		t.Fatalf("WLED selector picker missing: %s", body)
	}
	selectPicker := body[selectStart : selectStart+selectEnd]
	if strings.Contains(selectPicker, "select.one_preset") || strings.Contains(selectPicker, "select.one_playlist") || strings.Contains(selectPicker, "select.one_palette") || strings.Contains(selectPicker, "select.two_preset") {
		t.Fatalf("new WLED selector picker should wait for a light: %s", selectPicker)
	}
	if strings.Contains(selectPicker, "multiple") || !strings.Contains(body, `<select id="wled-select"`) {
		t.Fatalf("WLED selector control is not a single-select: %s", selectPicker)
	}
	optionStart := strings.Index(body, `id="wled-option"`)
	optionEnd := strings.Index(body[optionStart:], ">")
	if optionStart < 0 || optionEnd < 0 || strings.Contains(body[optionStart:optionStart+optionEnd], "multiple") {
		t.Fatalf("WLED program option control is not single-value: %s", body)
	}
	if !strings.Contains(body, "select.one_preset") || !strings.Contains(body, "select.one_playlist") || !strings.Contains(body, "select.two_preset") || strings.Contains(body, "select.one_palette") {
		t.Fatalf("WLED dependent picker data is not scoped: %s", body)
	}
	if strings.Contains(body, "select.hue_preset") {
		t.Fatalf("non-WLED selector was exposed: %s", body)
	}
	for _, want := range []string{`onchange="updateWLEDLight()"`, `onchange="updateWLEDSelector()"`, "selector.value='';", "document.querySelector('#wled-option').value=''", "updateWLEDOptions()"} {
		if !strings.Contains(body, want) {
			t.Fatalf("WLED dependent browser contract is missing %q: %s", want, body)
		}
	}
	postForm(t, app.handler(), "/schedule", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"wrong_device"}, "schedule_title": {"Wrong device"}, "kind": {"wled"},
		"wled_light": {"light.one"}, "wled_select": {"select.two_preset"}, "wled_option": {"Two preset"},
		"start": {"11-15"}, "end": {"01-15"}, "all_day": {"on"}, "finish": {"off"}, "enabled": {"on"},
	}, http.StatusSeeOther)
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/?view=schedules", nil)
	request.Host = "127.0.0.1:8080"
	app.handler().ServeHTTP(response, request)
	body = response.Body.String()
	if !strings.Contains(body, "WLED selector select.two_preset is not a preset or playlist for light.one") || !strings.Contains(body, `value="Two preset"`) || !strings.Contains(body, `value="select.two_preset" selected`) {
		t.Fatalf("connected WLED validation did not preserve invalid form values: %s", body)
	}
}

func TestWebWLEDInventoryReadiness(t *testing.T) {
	newApp := func(t *testing.T) (*webApp, *webTestStateAPI) {
		t.Helper()
		dir := t.TempDir()
		bundle := Bundle{Files: map[ConfigKind]Config{Colors: {Kind: Colors, Data: map[string]any{"red": map[string]any{"name": "Red", "x": .64, "y": .33}}}}}
		if err := SaveBundle(dir, bundle); err != nil {
			t.Fatal(err)
		}
		app, err := newWebApp(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		api := &webTestStateAPI{states: map[string]LightState{
			"light.imported":  {EntityID: "light.imported", State: "on", Attribute: map[string]any{"supported_color_modes": []any{"rgb"}}},
			"select.imported": {EntityID: "select.imported", State: "Saved", Attribute: map[string]any{"options": []any{"Saved"}}},
		}}
		app.stateAPI = api
		return app, api
	}
	post := func(t *testing.T, app *webApp) string {
		t.Helper()
		postForm(t, app.handler(), "/schedule", url.Values{
			"token": {app.token}, "hash": {app.bundle.Hash}, "id": {"connected_wled"}, "schedule_title": {"Connected WLED"}, "kind": {"wled"},
			"wled_light": {"light.imported"}, "wled_select": {"select.imported"}, "wled_option": {"Saved"},
			"start": {"11-15"}, "end": {"01-15"}, "all_day": {"on"}, "finish": {"off"}, "enabled": {"on"},
		}, http.StatusSeeOther)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/?view=schedules", nil)
		request.Host = "127.0.0.1:8080"
		app.handler().ServeHTTP(response, request)
		return response.Body.String()
	}

	t.Run("successful empty registry rejects WLED references", func(t *testing.T) {
		app, api := newApp(t)
		api.inventory = &HAInventory{Metadata: map[string]HAEntityMetadata{}}
		app.refreshInventory(context.Background())
		if !app.inventoryStatus.Connected || !app.inventoryStatus.StatesReady || !app.inventoryStatus.EntityRegistryReady || app.inventoryStatus.Error != "" {
			t.Fatalf("empty successful registry status = %#v", app.inventoryStatus)
		}
		body := post(t, app)
		if !strings.Contains(body, "WLED light light.imported is not a connected WLED color light") || !strings.Contains(body, `value="light.imported"`) || !strings.Contains(body, `value="Saved"`) {
			t.Fatalf("empty registry rejection did not preserve form values: %s", body)
		}
	})

	t.Run("registry failure blocks WLED save", func(t *testing.T) {
		app, api := newApp(t)
		app.inventoryMetadata = map[string]HAEntityMetadata{"light.stale": {EntityID: "light.stale", Platform: "wled", DeviceID: "stale"}}
		app.inventoryStatus = HAInventoryStatus{Connected: true, StatesReady: true, EntityRegistryReady: true}
		api.inventoryErr = errors.New("registry timeout")
		app.refreshInventory(context.Background())
		if !app.inventoryStatus.Connected || !app.inventoryStatus.StatesReady || app.inventoryStatus.EntityRegistryReady || app.inventoryStatus.Error != "registry timeout" || len(app.inventoryMetadata) != 0 {
			t.Fatalf("failed registry status = %#v", app.inventoryStatus)
		}
		body := post(t, app)
		if !strings.Contains(body, "home assistant entity registry is unavailable") || !strings.Contains(body, "registry timeout") || !strings.Contains(body, `value="light.imported"`) || !strings.Contains(body, `value="Saved"`) {
			t.Fatalf("registry failure did not block and preserve form values: %s", body)
		}
	})

	t.Run("offline preserves imported reference", func(t *testing.T) {
		app, _ := newApp(t)
		app.stateAPI = nil
		assignment := LightingAssignment{ID: "imported", Name: "Imported", Start: "11-15", End: "01-15", Finish: "off", Enabled: true, AllDay: true, WLED: &WLEDProgram{Light: "light.imported", Select: "select.imported", Option: "Saved"}}
		var err error
		app.bundle, err = saveLightingAssignment(app.bundle, assignment)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/?view=schedules&edit=imported", nil)
		request.Host = "127.0.0.1:8080"
		app.handler().ServeHTTP(response, request)
		body := response.Body.String()
		if app.inventoryStatus.Connected || !strings.Contains(body, `value="light.imported"`) || !strings.Contains(body, `value="select.imported"`) || !strings.Contains(body, `value="Saved"`) || !strings.Contains(body, "offline draft reference") {
			t.Fatalf("offline imported WLED reference was not preserved: %#v %s", app.inventoryStatus, body)
		}
	})
}

type webTestPublisher struct {
	calls       int
	last        Bundle
	lastDeletes []ConfigRef
}

func (p *webTestPublisher) Publish(_ context.Context, draft, _ Bundle, _ []ConfigRef, deletes []ConfigRef, _ string) error {
	p.calls++
	p.last = draft
	p.lastDeletes = append([]ConfigRef(nil), deletes...)
	return nil
}

func (p *webTestPublisher) Import(_ context.Context, _ []ConfigRef) (Bundle, error) {
	return cloneBundle(p.last)
}

type webTestCall struct{ domain, service, entity string }

type webTestStateAPI struct {
	states       map[string]LightState
	calls        []webTestCall
	inventory    *HAInventory
	inventoryErr error
}

func (a *webTestStateAPI) Inventory(_ context.Context) (HAInventory, error) {
	if a.inventoryErr != nil {
		return HAInventory{}, a.inventoryErr
	}
	if a.inventory != nil {
		return *a.inventory, nil
	}
	return HAInventory{}, nil
}

func (a *webTestStateAPI) States(_ context.Context, entities []string) (map[string]LightState, error) {
	if len(entities) == 0 {
		return a.states, nil
	}
	result := map[string]LightState{}
	for _, id := range entities {
		if state, ok := a.states[id]; ok {
			result[id] = state
		}
	}
	return result, nil
}

func (a *webTestStateAPI) CallService(_ context.Context, domain, service, entity string, _ map[string]any) error {
	a.calls = append(a.calls, webTestCall{domain, service, entity})
	if domain == "light" {
		state := a.states[entity]
		state.State = strings.TrimPrefix(service, "turn_")
		a.states[entity] = state
	}
	return nil
}

func (a *webTestStateAPI) called(domain, service, entity string) bool {
	for _, call := range a.calls {
		if call == (webTestCall{domain, service, entity}) {
			return true
		}
	}
	return false
}

func postForm(t *testing.T, handler http.Handler, path string, values url.Values, want int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("POST %s status = %d, want %d: %s", path, response.Code, want, response.Body.String())
	}
}
