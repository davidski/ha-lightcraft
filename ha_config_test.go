package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHAConfigStoreChecksAndReloadsNativeDomains(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("request = %s, auth = %q", request.Method, request.Header.Get("Authorization"))
		}
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
	if err := store.Reload(context.Background(), []ConfigKind{Scripts, Automations, Helpers}); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, path := range paths {
		got[path] = true
	}
	for _, path := range []string{"/api/config/core/check_config", "/api/services/script/reload", "/api/services/automation/reload", "/api/services/input_select/reload"} {
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
