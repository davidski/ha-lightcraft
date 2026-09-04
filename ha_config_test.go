package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHAConfigStoreUsesFrontendEndpoints(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotMethod, gotPath, gotAuth = request.Method, request.URL.Path, request.Header.Get("Authorization")
		if request.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "halloween_orange"})
		}
	}))
	defer server.Close()
	store := HAConfigStore{BaseURL: server.URL, Token: "secret", HTTP: server.Client()}
	config, err := store.Get(context.Background(), Scenes, "halloween_orange")
	if err != nil || config["id"] != "halloween_orange" {
		t.Fatalf("get config = %#v, err = %v", config, err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/config/scene/config/halloween_orange" || gotAuth != "Bearer secret" {
		t.Fatalf("request = %s %s %q", gotMethod, gotPath, gotAuth)
	}
}

func TestHAConfigStoreRejectsHelperWrites(t *testing.T) {
	err := (HAConfigStore{}).Put(context.Background(), Helpers, "holiday", nil)
	if err == nil {
		t.Fatal("expected unsupported helper error")
	}
}

func TestHAConfigStoreChecksAndReloadsNativeDomains(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		if request.URL.Path == "/api/config/core/check_config" {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "valid", "errors": nil})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	store := HAConfigStore{BaseURL: server.URL, Token: "secret", HTTP: server.Client()}
	if err := store.CheckConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(context.Background(), []ConfigKind{Scenes, Scripts, Automations, Helpers}); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, path := range paths {
		got[path] = true
	}
	for _, path := range []string{"/api/config/core/check_config", "/api/services/scene/reload", "/api/services/script/reload", "/api/services/automation/reload", "/api/services/input_select/reload"} {
		if !got[path] {
			t.Fatalf("missing request path %s in %#v", path, paths)
		}
	}
}

func TestHAConfigStoreRejectsInvalidConfigCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "invalid", "errors": "bad YAML"})
	}))
	defer server.Close()
	store := HAConfigStore{BaseURL: server.URL, Token: "secret", HTTP: server.Client()}
	if err := store.CheckConfig(context.Background()); err == nil {
		t.Fatal("expected invalid config error")
	}
}
