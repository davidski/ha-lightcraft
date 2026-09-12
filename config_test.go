package main

import (
	"strings"
	"testing"
)

func TestConfiguredFilePaths(t *testing.T) {
	t.Setenv(homeAssistantPackageEnv, "packages/custom.yaml")
	t.Setenv(legacyScenesFileEnv, "scenes/legacy.yaml")

	paths, err := configuredFilePaths()
	if err != nil {
		t.Fatal(err)
	}
	if paths[Scripts][0] != "packages/custom.yaml" || paths[Automations][0] != paths[Scripts][0] || paths[Helpers][0] != paths[Scripts][0] || paths[Scenes][0] != "scenes/legacy.yaml" {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestConfiguredFilePathsDefaultsPackage(t *testing.T) {
	t.Setenv(homeAssistantPackageEnv, "")
	t.Setenv(legacyScenesFileEnv, "")

	paths, err := configuredFilePaths()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := packagePath(paths); !ok || got != defaultPackagePath {
		t.Fatalf("package path = %q, ok = %v", got, ok)
	}
	if _, ok := paths[Scenes]; ok {
		t.Fatalf("default paths unexpectedly include legacy scenes: %#v", paths)
	}
}

func TestConfiguredFilePathsRejectsInvalidPackage(t *testing.T) {
	t.Setenv(homeAssistantPackageEnv, "../ha_lightcraft.yaml")
	if _, err := configuredFilePaths(); err == nil || !strings.Contains(err.Error(), homeAssistantPackageEnv) {
		t.Fatalf("err = %v", err)
	}
}

func TestConfiguredFilePathsRejectsInvalidLegacyScenesFile(t *testing.T) {
	t.Setenv(homeAssistantPackageEnv, defaultPackagePath)
	t.Setenv(legacyScenesFileEnv, "../scenes.yaml")
	if _, err := configuredFilePaths(); err == nil || !strings.Contains(err.Error(), legacyScenesFileEnv) {
		t.Fatalf("err = %v", err)
	}
}
