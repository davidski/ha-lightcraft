package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func SaveBackup(root string, bundle Bundle, filePaths map[ConfigKind][]string, now time.Time) (string, error) {
	dir := filepath.Join(root, now.UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup: %w", err)
	}
	if packageFile, ok := packagePath(filePaths); ok {
		data, err := marshalPackageYAML(bundle)
		if err != nil {
			return "", fmt.Errorf("serialize package backup: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(filepath.Clean(packageFile))), data, 0o600); err != nil {
			return "", fmt.Errorf("write package backup: %w", err)
		}
		return dir, nil
	}
	for _, kind := range bundle.Kinds() {
		filename := filenameForKind(kind)
		data, err := marshalConfigYAML(kind, bundle.Files[kind].Data)
		if err != nil {
			return "", fmt.Errorf("serialize %s backup: %w", kind, err)
		}
		if err := os.WriteFile(filepath.Join(dir, filename), data, 0o600); err != nil {
			return "", fmt.Errorf("write %s backup: %w", kind, err)
		}
	}
	return dir, nil
}
