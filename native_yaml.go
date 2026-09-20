package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	defer func() { _ = os.Remove(tmpName) }()
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
	var stderr bytes.Buffer
	command.Stderr = io.MultiWriter(&stderr, os.Stderr)
	_, _ = fmt.Fprintf(os.Stdout, "ha-lightcraft: ssh %s\n", t.target())
	output, err := command.Output()
	if err != nil {
		commandErr := &sshCommandError{err: err, stderr: stderr.String()}
		fmt.Fprintf(os.Stderr, "ha-lightcraft: ssh %s failed: %v\n", t.target(), commandErr)
		return nil, commandErr
	}
	return output, nil
}

type sshCommandError struct {
	err    error
	stderr string
}

func (e *sshCommandError) Error() string {
	message := strings.TrimSpace(e.stderr)
	if message == "" {
		return fmt.Sprintf("ssh: %v", e.err)
	}
	return fmt.Sprintf("ssh: %v: %s", e.err, message)
}

func (e *sshCommandError) Unwrap() error { return e.err }

func (e *sshCommandError) exitCode() int {
	var exitErr *exec.ExitError
	if errors.As(e.err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func (t SSHFileTransport) ReadFile(ctx context.Context, path string) ([]byte, error) {
	remote := "if [ -e " + shellQuote(path) + " ]; then cat -- " + shellQuote(path) + "; else exit 44; fi"
	data, err := t.run(ctx, remote)
	if err != nil {
		var commandErr *sshCommandError
		if errors.As(err, &commandErr) && commandErr.exitCode() == 44 {
			return nil, os.ErrNotExist
		}
		return nil, err
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

var errNoNativeYAML = errors.New("no native HA YAML files")

func (s NativeYAMLStore) packageFile() (string, bool) {
	relative, ok := packagePath(s.FilePaths)
	if !ok {
		return "", false
	}
	return filepath.Join(s.ConfigDir, relative), true
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
	if path, ok := s.packageFile(); ok {
		data, err := s.Transport.ReadFile(ctx, path)
		if errors.Is(err, os.ErrNotExist) {
			return Bundle{}, nil, fmt.Errorf("%w found in %s", errNoNativeYAML, s.ConfigDir)
		}
		if err != nil {
			return Bundle{}, nil, fmt.Errorf("read %s: %w", path, err)
		}
		values, err := parsePackageYAML(data)
		if err != nil {
			return Bundle{}, nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for kind, value := range values {
			bundle.Files[kind] = Config{Kind: kind, Data: value}
			raw[kind] = append([]byte(nil), data...)
		}
		bundle, err = withHash(bundle)
		return bundle, raw, err
	}
	for _, kind := range []ConfigKind{Scripts, Automations, Helpers} {
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
			} else if kind == Automations {
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
		return Bundle{}, nil, fmt.Errorf("%w found in %s", errNoNativeYAML, s.ConfigDir)
	}
	bundle, err := withHash(bundle)
	return bundle, raw, err
}

func parsePackageYAML(data []byte) (map[ConfigKind]any, error) {
	var value any
	if err := yaml.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	root, ok := normalizeYAML(value).(map[string]any)
	if !ok {
		return nil, errors.New("package must be a mapping")
	}
	keys := map[string]ConfigKind{
		"automation":   Automations,
		"input_select": Helpers,
		"script":       Scripts,
	}
	if len(root) != len(keys) {
		return nil, errors.New("package must contain only automation, input_select, and script")
	}
	result := make(map[ConfigKind]any, len(keys))
	for key, kind := range keys {
		item, exists := root[key]
		if !exists {
			return nil, fmt.Errorf("package is missing top-level %s", key)
		}
		switch kind {
		case Automations:
			if _, ok := item.([]any); !ok {
				return nil, errors.New("package automation must be a list")
			}
		case Helpers, Scripts:
			if _, ok := item.(map[string]any); !ok {
				return nil, fmt.Errorf("package %s must be a mapping", key)
			}
		}
		result[kind] = item
	}
	return result, nil
}

func (s NativeYAMLStore) Import(ctx context.Context, refs []ConfigRef) (Bundle, error) {
	full, _, err := s.readFull(ctx)
	if err != nil {
		return Bundle{}, err
	}
	return importedBundle(full, refs)
}

func (s NativeYAMLStore) ReadAll(ctx context.Context) (Bundle, error) {
	full, _, err := s.readFull(ctx)
	if err == nil {
		full.SourceHash = full.Hash
	}
	return full, err
}

func importedBundle(full Bundle, refs []ConfigRef) (Bundle, error) {
	sourceHash := full.SourceHash
	if sourceHash == "" {
		sourceHash = full.Hash
	}
	if len(refs) == 0 {
		full.SourceHash = sourceHash
		return full, nil
	}
	selected, err := selectRefs(full, refs)
	if err != nil {
		return Bundle{}, err
	}
	selected.SourceHash = sourceHash
	return selected, nil
}

func importedBundleWithLegacyUpgrade(ctx context.Context, store NativeYAMLStore, full Bundle, refs []ConfigRef) (Bundle, error) {
	bundle, err := importedBundle(full, refs)
	if err != nil {
		return Bundle{}, err
	}
	if legacyLightingDetected(full) {
		legacy, err := store.ReadLegacy(ctx)
		if err != nil {
			return Bundle{}, fmt.Errorf("read legacy HA files for automatic upgrade: %w", err)
		}
		bundle, err = upgradeLegacyInfrastructure(bundle, legacy)
		if err != nil {
			return Bundle{}, fmt.Errorf("automatically upgrade legacy lighting: %w", err)
		}
	}
	return bundle, nil
}

func importDraft(ctx context.Context, store NativeYAMLStore, proposedDir, currentDir, colorsDir string, filePaths map[ConfigKind][]string) (Bundle, Bundle, bool, error) {
	full, err := store.Pull(ctx, currentDir)
	if err != nil {
		return Bundle{}, Bundle{}, false, err
	}
	legacyDetected := legacyLightingDetected(full)
	bundle, err := importedBundleWithLegacyUpgrade(ctx, store, full, nil)
	if err != nil {
		return Bundle{}, Bundle{}, false, err
	}
	imported := bundle
	if colors, ok, err := loadColors(colorsDir); err != nil {
		return Bundle{}, Bundle{}, false, err
	} else if ok {
		bundle.Files[Colors] = colors
	}
	bundle, err = mergeImportedColorCatalog(bundle, imported)
	if err != nil {
		return Bundle{}, Bundle{}, false, fmt.Errorf("merge imported color catalog: %w", err)
	}
	if err := SaveBundleAtWithReferences(proposedDir, colorsDir, bundle, filePaths); err != nil {
		return Bundle{}, Bundle{}, false, err
	}
	return bundle, full, legacyDetected, nil
}

// Pull copies the configured native files to a local source snapshot and
// returns the same parsed bundle that those files represent.
func (s NativeYAMLStore) Pull(ctx context.Context, dir string) (Bundle, error) {
	if s.Transport == nil || s.ConfigDir == "" {
		return Bundle{}, fmt.Errorf("native YAML transport and config directory are required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Bundle{}, fmt.Errorf("create source directory: %w", err)
	}
	if packageFile, ok := packagePath(s.FilePaths); ok {
		if err := validateRelativePath(packageFile); err != nil {
			return Bundle{}, err
		}
		remotePath, _ := s.packageFile()
		localPackage := filepath.Base(filepath.Clean(packageFile))
		data, err := s.Transport.ReadFile(ctx, remotePath)
		if errors.Is(err, os.ErrNotExist) {
			if removeErr := os.Remove(filepath.Join(dir, localPackage)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return Bundle{}, fmt.Errorf("remove missing source file %s: %w", localPackage, removeErr)
			}
			return Bundle{}, fmt.Errorf("%w found at %s", errNoNativeYAML, remotePath)
		} else {
			if err != nil {
				return Bundle{}, fmt.Errorf("read %s: %w", remotePath, err)
			}
			if err := writePulledFile(dir, localPackage, data); err != nil {
				return Bundle{}, err
			}
		}
		return NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: ".", FilePaths: localFilePaths(s.FilePaths)}.ReadAll(ctx)
	}
	for _, relative := range s.relativeNames(Scenes) {
		if err := validateRelativePath(relative); err != nil {
			return Bundle{}, err
		}
		if err := os.Remove(filepath.Join(dir, relative)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Bundle{}, fmt.Errorf("remove legacy source file %s: %w", relative, err)
		}
	}
	found := false
	seen := map[string]bool{}
	for _, kind := range []ConfigKind{Scripts, Automations, Helpers} {
		paths := s.relativeNames(kind)
		for i, remotePath := range s.names(kind) {
			relative := paths[i]
			if err := validateRelativePath(relative); err != nil {
				return Bundle{}, err
			}
			if seen[relative] {
				return Bundle{}, fmt.Errorf("native YAML path configured more than once: %s", relative)
			}
			seen[relative] = true
			data, err := s.Transport.ReadFile(ctx, remotePath)
			if errors.Is(err, os.ErrNotExist) {
				if removeErr := os.Remove(filepath.Join(dir, relative)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					return Bundle{}, fmt.Errorf("remove missing source file %s: %w", relative, removeErr)
				}
				continue
			}
			if err != nil {
				return Bundle{}, fmt.Errorf("read %s: %w", remotePath, err)
			}
			if err := writePulledFile(dir, relative, data); err != nil {
				return Bundle{}, err
			}
			found = true
		}
	}
	if !found {
		return Bundle{}, fmt.Errorf("no native HA YAML files found in %s", s.ConfigDir)
	}
	return NativeYAMLStore{Transport: LocalFileTransport{Root: dir}, ConfigDir: ".", FilePaths: s.FilePaths}.ReadAll(ctx)
}

func (s NativeYAMLStore) relativeNames(kind ConfigKind) []string {
	if paths := s.FilePaths[kind]; len(paths) > 0 {
		return paths
	}
	return []string{filenameForKind(kind)}
}

func validateRelativePath(path string) error {
	clean := filepath.Clean(path)
	if filepath.IsAbs(path) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("native YAML path must stay inside the source directory: %q", path)
	}
	return nil
}

func writePulledFile(root, relative string, data []byte) error {
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create source parent for %s: %w", relative, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".holiday-designer-pull-*")
	if err != nil {
		return fmt.Errorf("create source file %s: %w", relative, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write source file %s: %w", relative, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace source file %s: %w", relative, err)
	}
	return nil
}

// ReadLegacy reads the old scene file only for the one-time migration path.
// Modern imports and drafts never include Scenes.
func (s NativeYAMLStore) ReadLegacy(ctx context.Context) (Bundle, error) {
	result, err := s.Import(ctx, nil)
	if err != nil {
		return Bundle{}, err
	}
	var merged any
	found := false
	for _, path := range s.names(Scenes) {
		data, readErr := s.Transport.ReadFile(ctx, path)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return Bundle{}, fmt.Errorf("read legacy scenes %s: %w", path, readErr)
		}
		var value any
		if parseErr := yaml.Unmarshal(data, &value); parseErr != nil {
			return Bundle{}, fmt.Errorf("parse legacy scenes %s: %w", path, parseErr)
		}
		if !found {
			merged, found = value, true
		} else {
			merged = append(merged.([]any), value.([]any)...)
		}
	}
	if !found {
		return Bundle{}, fmt.Errorf("no legacy scenes YAML found in %s", s.ConfigDir)
	}
	result.Files[Scenes] = Config{Kind: Scenes, Data: normalizeYAML(merged)}
	return withHash(result)
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
		if _, err := SaveBackup(backupRoot, full, s.FilePaths, time.Now()); err != nil {
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
	if packageFile, ok := s.packageFile(); ok {
		if len(changed) > 0 {
			data, err := marshalPackageYAML(working)
			if err != nil {
				return s.failAndRollback(ctx, oldRaw, changed, fmt.Errorf("serialize package: %w", err))
			}
			if err := s.Transport.WriteFileAtomic(ctx, packageFile, data); err != nil {
				return s.failAndRollback(ctx, oldRaw, changed, fmt.Errorf("write package: %w", err))
			}
		}
	} else {
		for _, kind := range changed {
			if len(s.names(kind)) > 1 {
				return fmt.Errorf("cannot safely publish multi-file %s configuration yet", kind)
			}
			data, err := marshalConfigYAML(kind, working.Files[kind].Data)
			if err != nil {
				return s.failAndRollback(ctx, oldRaw, changed, fmt.Errorf("serialize %s: %w", kind, err))
			}
			if err := s.Transport.WriteFileAtomic(ctx, s.name(kind), data); err != nil {
				return s.failAndRollback(ctx, oldRaw, changed, fmt.Errorf("write %s: %w", s.name(kind), err))
			}
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
	seen := map[string]bool{}
	for _, kind := range changed {
		path := s.name(kind)
		if seen[path] {
			continue
		}
		seen[path] = true
		data, ok := raw[kind]
		var err error
		if ok {
			err = s.Transport.WriteFileAtomic(ctx, path, data)
		} else {
			err = s.Transport.DeleteFile(ctx, path)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func selectRefs(bundle Bundle, refs []ConfigRef) (Bundle, error) {
	result := Bundle{Files: map[ConfigKind]Config{
		Scripts:     {Kind: Scripts, Data: map[string]any{}},
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
		case Automations:
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
	case Automations:
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
