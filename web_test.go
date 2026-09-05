package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWebDraftEditAndSave(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{Files: map[ConfigKind]Config{
		Scenes: {Kind: Scenes, Data: []any{}},
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
	request := httptest.NewRequest(http.MethodGet, "/?view=colors&edit=red", nil)
	request.Host = "127.0.0.1:8080"
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	wantHex := cieRGBHex(CIEColor{X: .64, Y: .33})
	if response.Code != http.StatusOK || !strings.Contains(body, "YAML / Diff") || !strings.Contains(body, "Red") || !strings.Contains(body, `type="color" value="`+wantHex+`"`) || !strings.Contains(body, "setColorXY") {
		t.Fatalf("unexpected color page: %d %s", response.Code, response.Body.String())
	}

	postForm(t, handler, "/color", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "name": {"Green"}, "x": {"0.3"}, "y": {"0.6"},
	}, http.StatusSeeOther)
	if colorDefinitions(app.bundle)["green"].Name != "Green" || app.bundle.Hash == app.persistedHash {
		t.Fatal("color edit was not held as an unsaved draft change")
	}

	postForm(t, handler, "/save", url.Values{"token": {app.token}, "hash": {app.bundle.Hash}}, http.StatusSeeOther)
	saved, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if colorDefinitions(saved)["green"].Name != "Green" || app.bundle.Hash != app.persistedHash {
		t.Fatal("explicit save did not persist the web edit")
	}
	postForm(t, handler, "/delete", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "kind": {"colors"}, "id": {"green"},
	}, http.StatusSeeOther)
	if _, exists := colorDefinitions(app.bundle)["green"]; exists {
		t.Fatal("draft delete did not remove the color")
	}
	postForm(t, handler, "/save", url.Values{"token": {app.token}, "hash": {app.bundle.Hash}}, http.StatusSeeOther)

	postForm(t, handler, "/color", url.Values{
		"token": {app.token}, "hash": {"stale"}, "name": {"Blue"}, "x": {"0.2"}, "y": {"0.2"},
	}, http.StatusConflict)

	postForm(t, handler, "/color", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "name": {"Blue"}, "x": {"0.2"}, "y": {"0.2"},
	}, http.StatusSeeOther)
	external, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	external, err = upsertColor(external, "yellow", ColorDefinition{Name: "Yellow", X: .4, Y: .5})
	if err != nil || SaveBundle(dir, external) != nil {
		t.Fatal("could not prepare external draft edit")
	}
	postForm(t, handler, "/save", url.Values{"token": {app.token}, "hash": {app.bundle.Hash}}, http.StatusConflict)
	onDisk, err := LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, overwritten := colorDefinitions(onDisk)["blue"]; overwritten || colorDefinitions(onDisk)["yellow"].Name != "Yellow" {
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
