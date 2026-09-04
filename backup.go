package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

func SaveBackup(root string, bundle Bundle, now time.Time) (string, error) {
	dir := filepath.Join(root, now.UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup: %w", err)
	}
	for _, kind := range bundle.Kinds() {
		filename := filenameForKind(kind)
		data, err := yaml.Marshal(bundle.Files[kind].Data)
		if err != nil {
			return "", fmt.Errorf("serialize %s backup: %w", kind, err)
		}
		if err := os.WriteFile(filepath.Join(dir, filename), data, 0o600); err != nil {
			return "", fmt.Errorf("write %s backup: %w", kind, err)
		}
	}
	return dir, nil
}
