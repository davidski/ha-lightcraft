package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type ProjectConfig struct {
	HomeAssistant HomeAssistantConfig `yaml:"home_assistant"`
	Files         map[string][]string `yaml:"files"`
	Refs          []string            `yaml:"refs"`
}

type HomeAssistantConfig struct {
	URL       string `yaml:"url"`
	SSHHost   string `yaml:"ssh_host"`
	ConfigDir string `yaml:"config_dir"`
}

func loadProjectConfig(path string) (ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var config ProjectConfig
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return ProjectConfig{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return config, nil
}

func loadConfigRefs(path string) ([]ConfigRef, error) {
	config, err := loadProjectConfig(path)
	if err != nil {
		return nil, err
	}
	refs, err := configRefs(config)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return refs, nil
}

func configRefs(config ProjectConfig) ([]ConfigRef, error) {
	var refs []ConfigRef
	for _, value := range config.Refs {
		parsed, err := parseRefs(value)
		if err != nil {
			return nil, err
		}
		refs = append(refs, parsed...)
	}
	return refs, nil
}

func configFilePaths(config ProjectConfig) (map[ConfigKind][]string, error) {
	paths := make(map[ConfigKind][]string, len(config.Files))
	for value, configuredPaths := range config.Files {
		kind := ConfigKind(value)
		switch kind {
		case Scenes, Scripts, Automations, Helpers:
			if len(configuredPaths) == 0 {
				return nil, fmt.Errorf("config file path for %s is empty", kind)
			}
			for _, path := range configuredPaths {
				if strings.TrimSpace(path) == "" {
					return nil, fmt.Errorf("config file path for %s is empty", kind)
				}
			}
			paths[kind] = configuredPaths
		default:
			return nil, fmt.Errorf("unsupported config file kind %q", value)
		}
	}
	return paths, nil
}

func applyProjectConfig(config ProjectConfig, host, user, configDir, url *string) {
	if *host == "" {
		*host = config.HomeAssistant.SSHHost
	}
	if *configDir == "" {
		*configDir = config.HomeAssistant.ConfigDir
	}
	if *url == "" {
		*url = config.HomeAssistant.URL
	}
}

func formatConfigRefs(refs []ConfigRef) string {
	values := make([]string, 0, len(refs))
	for _, ref := range refs {
		values = append(values, string(ref.Kind)+":"+ref.ID)
	}
	return strings.Join(values, ",")
}
