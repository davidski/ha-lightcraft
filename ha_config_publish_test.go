package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
)

func TestHAConfigStorePublishesOnlySelectedSceneAndVerifies(t *testing.T) {
	var mu sync.Mutex
	configs := map[string]map[string]any{
		"halloween_orange": {"id": "halloween_orange", "name": "Old", "entities": map[string]any{}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/config/core/check_config" {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "valid", "errors": nil})
			return
		}
		id := filepath.Base(request.URL.Path)
		mu.Lock()
		defer mu.Unlock()
		switch request.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(configs[id])
		case http.MethodPost:
			var config map[string]any
			_ = json.NewDecoder(request.Body).Decode(&config)
			configs[id] = config
		case http.MethodDelete:
			delete(configs, id)
		}
	}))
	defer server.Close()
	store := HAConfigStore{BaseURL: server.URL, HTTP: server.Client()}
	refs := []ConfigRef{{Kind: Scenes, ID: "halloween_orange"}}
	imported, err := store.Import(context.Background(), refs)
	if err != nil {
		t.Fatal(err)
	}
	draft := Bundle{Files: map[ConfigKind]Config{
		Scenes: {Kind: Scenes, Data: []any{map[string]any{"id": "halloween_orange", "name": "New", "entities": map[string]any{}}}},
	}}
	draft, err = withHash(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(context.Background(), draft, imported, refs, nil, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if configs["halloween_orange"]["name"] != "New" {
		t.Fatalf("scene was not published: %#v", configs)
	}
}
