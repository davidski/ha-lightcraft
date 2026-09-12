package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	homeAssistantPackageEnv = "HOMEASSISTANT_PACKAGE"
	legacyScenesFileEnv     = "HOMEASSISTANT_LEGACY_SCENES_FILE"
)

func configuredFilePaths() (map[ConfigKind][]string, error) {
	packageFile := strings.TrimSpace(os.Getenv(homeAssistantPackageEnv))
	if packageFile == "" {
		packageFile = defaultPackagePath
	}
	if err := validateRelativePath(packageFile); err != nil {
		return nil, fmt.Errorf("%s: %w", homeAssistantPackageEnv, err)
	}
	paths := map[ConfigKind][]string{
		Scripts:     {packageFile},
		Automations: {packageFile},
		Helpers:     {packageFile},
	}
	scenesFile := strings.TrimSpace(os.Getenv(legacyScenesFileEnv))
	if scenesFile != "" {
		if err := validateRelativePath(scenesFile); err != nil {
			return nil, fmt.Errorf("%s: %w", legacyScenesFileEnv, err)
		}
		paths[Scenes] = []string{scenesFile}
	}
	return paths, nil
}

const defaultPackagePath = "packages/ha_lightcraft.yaml"
