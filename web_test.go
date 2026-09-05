package main

import (
	"context"
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

func TestWebPublishUsesGuardedPublisher(t *testing.T) {
	dir := t.TempDir()
	bundle := Bundle{Files: map[ConfigKind]Config{
		Scenes: {Kind: Scenes, Data: []any{}},
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
	postForm(t, handler, "/scene", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "name": {"Test"}, "color": {"red"}, "entities": {"light.one"}, "brightness": {"100"},
	}, http.StatusSeeOther)
	beforePublish, err := PublishDiff(app.baseline, app.bundle)
	if err != nil || len(beforePublish) == 0 {
		t.Fatalf("web edit did not differ from publish baseline: changes=%v err=%v", beforePublish, err)
	}
	postForm(t, handler, "/save", url.Values{"token": {app.token}, "hash": {app.bundle.Hash}}, http.StatusSeeOther)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?view=yaml", nil)
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
	postForm(t, handler, "/delete", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "kind": {"scenes"}, "id": {"test_red"},
	}, http.StatusSeeOther)
	postForm(t, handler, "/save", url.Values{"token": {app.token}, "hash": {app.bundle.Hash}}, http.StatusSeeOther)
	postForm(t, handler, "/publish", url.Values{
		"token": {app.token}, "hash": {app.bundle.Hash}, "confirmation": {"PUBLISH"},
	}, http.StatusSeeOther)
	if publisher.calls != 1 {
		t.Fatal("web publish attempted an unapproved Home Assistant deletion")
	}
}

type webTestPublisher struct {
	calls int
	last  Bundle
}

func (p *webTestPublisher) Publish(_ context.Context, draft, _ Bundle, _ []ConfigRef, _ []ConfigRef, _ string) error {
	p.calls++
	p.last = draft
	return nil
}

func (p *webTestPublisher) Import(_ context.Context, _ []ConfigRef) (Bundle, error) {
	return cloneBundle(p.last)
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
