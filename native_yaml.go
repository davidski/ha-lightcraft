package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type FileTransport interface {
	ReadFile(context.Context, string) ([]byte, error)
	WriteFileAtomic(context.Context, string, []byte) error
	DeleteFile(context.Context, string) error
}

type LocalFileTransport struct{ Root string }

func (t LocalFileTransport) path(name string) string { return filepath.Join(t.Root, name) }
func (t LocalFileTransport) ReadFile(_ context.Context, name string) ([]byte, error) {
	return os.ReadFile(t.path(name))
}
func (t LocalFileTransport) WriteFileAtomic(_ context.Context, name string, data []byte) error {
	path := t.path(name)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".holiday-designer-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
func (t LocalFileTransport) DeleteFile(_ context.Context, name string) error {
	err := os.Remove(t.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type SSHFileTransport struct {
	Host string
	User string
	Args []string
}

func (t SSHFileTransport) target() string {
	if t.User == "" {
		return t.Host
	}
	return t.User + "@" + t.Host
}
func (t SSHFileTransport) run(ctx context.Context, remote string) ([]byte, error) {
	args := append([]string(nil), t.Args...)
	args = append(args, t.target(), remote)
	command := exec.CommandContext(ctx, "ssh", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ssh: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
func (t SSHFileTransport) ReadFile(ctx context.Context, path string) ([]byte, error) {
	data, err := t.run(ctx, "cat -- "+shellQuote(path))
	if err != nil && strings.Contains(err.Error(), "No such file or directory") {
		return nil, os.ErrNotExist
	}
	return data, err
}
func (t SSHFileTransport) WriteFileAtomic(ctx context.Context, path string, data []byte) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	template := path + ".holiday-designer.XXXXXX"
	remote := "set -eu; tmp=$(mktemp " + shellQuote(template) + "); trap 'rm -f -- \"$tmp\"' EXIT; printf %s " + shellQuote(encoded) + " | base64 -d > \"$tmp\"; chmod 600 \"$tmp\"; mv -f \"$tmp\" " + shellQuote(path)
	_, err := t.run(ctx, remote)
	return err
}
func (t SSHFileTransport) DeleteFile(ctx context.Context, path string) error {
	_, err := t.run(ctx, "rm -f -- "+shellQuote(path))
	return err
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

type NativeYAMLStore struct {
	Transport FileTransport
	ConfigDir string
	FilePaths map[ConfigKind][]string
	Check     func(context.Context) error
	Reload    func(context.Context, []ConfigKind) error
}

func (s NativeYAMLStore) name(kind ConfigKind) string {
	if paths := s.FilePaths[kind]; len(paths) > 0 {
		return filepath.Join(s.ConfigDir, paths[0])
	}
	return filepath.Join(s.ConfigDir, filenameForKind(kind))
}

func (s NativeYAMLStore) names(kind ConfigKind) []string {
	if paths := s.FilePaths[kind]; len(paths) > 0 {
		result := make([]string, len(paths))
		for i, path := range paths {
			result[i] = filepath.Join(s.ConfigDir, path)
		}
		return result
	}
	return []string{s.name(kind)}
}

func (s NativeYAMLStore) readFull(ctx context.Context) (Bundle, map[ConfigKind][]byte, error) {
	if s.Transport == nil || s.ConfigDir == "" {
		return Bundle{}, nil, fmt.Errorf("native YAML transport and config directory are required")
	}
	bundle := Bundle{Files: map[ConfigKind]Config{}}
	raw := map[ConfigKind][]byte{}
	for _, kind := range []ConfigKind{Scenes, Scripts, Automations, Helpers} {
		var merged any
		found := false
		var onlyData []byte
		for _, path := range s.names(kind) {
			data, err := s.Transport.ReadFile(ctx, path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return Bundle{}, nil, fmt.Errorf("read %s: %w", path, err)
			}
			var value any
			if err := yaml.Unmarshal(data, &value); err != nil {
				return Bundle{}, nil, fmt.Errorf("parse %s: %w", path, err)
			}
			onlyData = data
			if !found {
				merged = value
				found = true
			} else if kind == Scenes || kind == Automations {
				merged = append(merged.([]any), value.([]any)...)
			} else {
				for key, item := range value.(map[string]any) {
					merged.(map[string]any)[key] = item
				}
			}
		}
		if found {
			bundle.Files[kind] = Config{Kind: kind, Data: normalizeYAML(merged)}
			if len(s.names(kind)) == 1 {
				raw[kind] = append([]byte(nil), onlyData...)
			}
		}
	}
	if len(bundle.Files) == 0 {
		return Bundle{}, nil, fmt.Errorf("no native HA YAML files found in %s", s.ConfigDir)
	}
	bundle, err := withHash(bundle)
	return bundle, raw, err
}

func (s NativeYAMLStore) Import(ctx context.Context, refs []ConfigRef) (Bundle, error) {
	full, _, err := s.readFull(ctx)
	if err != nil {
		return Bundle{}, err
	}
	if len(refs) == 0 {
		full.SourceHash = full.Hash
		return full, nil
	}
	selected, err := selectRefs(full, refs)
	if err != nil {
		return Bundle{}, err
	}
	selected.SourceHash = full.Hash
	return selected, nil
}

func (s NativeYAMLStore) ReadAll(ctx context.Context) (Bundle, error) {
	full, _, err := s.readFull(ctx)
	return full, err
}

func (s NativeYAMLStore) AffectedReferences(ctx context.Context, sceneID string) ([]ConfigRef, error) {
	full, _, err := s.readFull(ctx)
	if err != nil {
		return nil, err
	}
	return AffectedReferences(full, sceneID), nil
}

func (s NativeYAMLStore) Publish(ctx context.Context, draft, imported Bundle, refs, approvedDeletes []ConfigRef, backupRoot string) error {
	if err := ValidateBundle(draft); err != nil {
		return err
	}
	for _, ref := range approvedDeletes {
		if !containsRef(refs, ref) {
			return fmt.Errorf("approved deletion was not included in imported refs: %s:%s", ref.Kind, ref.ID)
		}
	}
	if s.Check != nil {
		if err := s.Check(ctx); err != nil {
			return err
		}
	}
	full, oldRaw, err := s.readFull(ctx)
	if err != nil {
		return err
	}
	_, err = selectRefs(full, refs)
	if err != nil {
		return err
	}
	expectedHash := imported.SourceHash
	if expectedHash == "" {
		expectedHash = imported.Hash
	}
	if full.Hash != expectedHash {
		return ErrStaleConfig
	}
	if backupRoot != "" {
		if _, err := SaveBackup(backupRoot, full, time.Now()); err != nil {
			return err
		}
	}
	working, err := cloneBundle(full)
	if err != nil {
		return err
	}
	oldEntries := bundleEntries(full)
	nativeDraft := materializeNativeBundle(draft)
	newEntries := bundleEntries(nativeDraft)
	allRefs := unionRefs(refs, bundleRefs(nativeDraft))
	changedKinds := map[ConfigKind]bool{}
	for _, ref := range allRefs {
		oldValue, oldOK := oldEntries[ref]
		newValue, newOK := newEntries[ref]
		if !newOK {
			if !containsRef(approvedDeletes, ref) {
				continue
			}
			if err := replaceEntry(&working, ref, nil); err != nil {
				return err
			}
			if oldOK {
				changedKinds[ref.Kind] = true
			}
			continue
		}
		if oldOK && stringMustJSON(oldValue) == stringMustJSON(newValue) {
			continue
		}
		if err := replaceEntry(&working, ref, newValue); err != nil {
			return err
		}
		changedKinds[ref.Kind] = true
	}
	changed := sortedKinds(changedKinds)
	for _, kind := range changed {
		if len(s.names(kind)) > 1 {
			return fmt.Errorf("cannot safely publish multi-file %s configuration yet", kind)
		}
		data, err := yaml.Marshal(working.Files[kind].Data)
		if err != nil {
			return s.failAndRollback(ctx, oldRaw, changed, fmt.Errorf("serialize %s: %w", kind, err))
		}
		if err := s.Transport.WriteFileAtomic(ctx, s.name(kind), data); err != nil {
			return s.failAndRollback(ctx, oldRaw, changed, fmt.Errorf("write %s: %w", s.name(kind), err))
		}
	}
	if s.Check != nil {
		if err := s.Check(ctx); err != nil {
			return s.failAndRollback(ctx, oldRaw, changed, err)
		}
	}
	if s.Reload != nil {
		if err := s.Reload(ctx, changed); err != nil {
			return s.failAndRollback(ctx, oldRaw, changed, err)
		}
	}
	verified, _, err := s.readFull(ctx)
	if err != nil {
		return s.failAndRollback(ctx, oldRaw, changed, fmt.Errorf("verify YAML: %w", err))
	}
	verifiedEntries := bundleEntries(verified)
	for _, ref := range allRefs {
		if containsRef(approvedDeletes, ref) {
			if _, exists := verifiedEntries[ref]; exists {
				return s.failAndRollback(ctx, oldRaw, changed, fmt.Errorf("verify %s %s: deletion did not persist", ref.Kind, ref.ID))
			}
			continue
		}
		expected, ok := newEntries[ref]
		if ok && stringMustJSON(expected) != stringMustJSON(verifiedEntries[ref]) {
			return s.failAndRollback(ctx, oldRaw, changed, fmt.Errorf("verify %s %s: published config differs", ref.Kind, ref.ID))
		}
	}
	return nil
}

func (s NativeYAMLStore) failAndRollback(ctx context.Context, oldRaw map[ConfigKind][]byte, changed []ConfigKind, cause error) error {
	if err := s.restoreRaw(ctx, oldRaw, changed); err != nil {
		return fmt.Errorf("%w; rollback: %v", cause, err)
	}
	if s.Reload != nil {
		if err := s.Reload(ctx, changed); err != nil {
			return fmt.Errorf("%w; rollback complete; reload rollback: %v", cause, err)
		}
	}
	return fmt.Errorf("%w; rollback complete", cause)
}
func (s NativeYAMLStore) restoreRaw(ctx context.Context, raw map[ConfigKind][]byte, changed []ConfigKind) error {
	for _, kind := range changed {
		data, ok := raw[kind]
		var err error
		if ok {
			err = s.Transport.WriteFileAtomic(ctx, s.name(kind), data)
		} else {
			err = s.Transport.DeleteFile(ctx, s.name(kind))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func selectRefs(bundle Bundle, refs []ConfigRef) (Bundle, error) {
	result := Bundle{Files: map[ConfigKind]Config{
		Scenes: {Kind: Scenes, Data: []any{}}, Scripts: {Kind: Scripts, Data: map[string]any{}},
		Automations: {Kind: Automations, Data: []any{}}, Helpers: {Kind: Helpers, Data: map[string]any{}},
	}}
	entries := bundleEntries(bundle)
	for _, ref := range refs {
		entry, ok := entries[ref]
		if !ok {
			return Bundle{}, fmt.Errorf("native HA entry not found: %s:%s", ref.Kind, ref.ID)
		}
		copy, err := cloneMap(entry)
		if err != nil {
			return Bundle{}, err
		}
		switch ref.Kind {
		case Scenes, Automations:
			config := result.Files[ref.Kind]
			config.Data = append(config.Data.([]any), copy)
			result.Files[ref.Kind] = config
		case Scripts, Helpers:
			config := result.Files[ref.Kind]
			config.Data.(map[string]any)[ref.ID] = copy
			result.Files[ref.Kind] = config
		default:
			return Bundle{}, fmt.Errorf("unsupported native HA kind %s", ref.Kind)
		}
	}
	return withHash(result)
}

func replaceEntry(bundle *Bundle, ref ConfigRef, value map[string]any) error {
	config := bundle.Files[ref.Kind]
	switch ref.Kind {
	case Scripts, Helpers:
		values, ok := config.Data.(map[string]any)
		if !ok {
			values = map[string]any{}
		}
		if value == nil {
			delete(values, ref.ID)
		} else {
			values[ref.ID] = value
		}
		config.Data = values
	case Scenes, Automations:
		values, ok := config.Data.([]any)
		if !ok {
			values = []any{}
		}
		filtered := make([]any, 0, len(values)+1)
		replaced := false
		for _, raw := range values {
			entry, ok := raw.(map[string]any)
			if ok && entry["id"] == ref.ID {
				replaced = true
				if value != nil {
					filtered = append(filtered, value)
				}
				continue
			}
			filtered = append(filtered, raw)
		}
		if value != nil && !replaced {
			filtered = append(filtered, value)
		}
		config.Data = filtered
	default:
		return fmt.Errorf("unsupported native HA kind %s", ref.Kind)
	}
	bundle.Files[ref.Kind] = config
	return nil
}

func cloneMap(value map[string]any) (map[string]any, error) {
	data, err := canonicalJSON(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	return result, json.Unmarshal(data, &result)
}

func sortedKinds(values map[ConfigKind]bool) []ConfigKind {
	result := make([]ConfigKind, 0, len(values))
	for kind := range values {
		result = append(result, kind)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
