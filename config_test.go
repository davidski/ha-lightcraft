package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigRefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("home_assistant:\n  url: https://ha.example\n  ssh_host: svc-01\n  config_dir: /config\nrefs:\n  - scenes:halloween\n  - scripts:holiday\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	refs, err := loadConfigRefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := formatConfigRefs(refs); got != "scenes:halloween,scripts:holiday" {
		t.Fatalf("refs = %q", got)
	}
}

func TestLoadConfigRefsAllowsAllEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("files:\n  scenes:\n    - scenes/holiday_lighting_designer.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	refs, err := loadConfigRefs(path)
	if err != nil || len(refs) != 0 {
		t.Fatalf("refs = %#v, err = %v", refs, err)
	}
}

func TestApplyProjectConfig(t *testing.T) {
	config := ProjectConfig{HomeAssistant: HomeAssistantConfig{URL: "https://ha.example", SSHHost: "svc-01", ConfigDir: "/config"}}
	var host, user, dir, url string
	applyProjectConfig(config, &host, &user, &dir, &url)
	if host != "svc-01" || dir != "/config" || url != "https://ha.example" || user != "" {
		t.Fatalf("applied config = host %q user %q dir %q url %q", host, user, dir, url)
	}
}

func TestConfigFilePaths(t *testing.T) {
	paths, err := configFilePaths(ProjectConfig{Files: map[string][]string{"scenes": {"scenes/exterior_halloween.yaml"}}})
	if err != nil {
		t.Fatal(err)
	}
	if paths[Scenes][0] != "scenes/exterior_halloween.yaml" {
		t.Fatalf("scene path = %q", paths[Scenes][0])
	}
}

func TestLoadConfigRefsRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("entries: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfigRefs(path); err == nil {
		t.Fatal("expected unknown field error")
	}
}
