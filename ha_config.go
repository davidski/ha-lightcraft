package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ConfigRef struct {
	Kind ConfigKind
	ID   string
}

type ConfigPublisher interface {
	Publish(context.Context, Bundle, Bundle, []ConfigRef, []ConfigRef, string) error
}

type ConfigReferenceFinder interface {
	AffectedReferences(context.Context, string) ([]ConfigRef, error)
}

var ErrStaleConfig = fmt.Errorf("Home Assistant configuration changed since import")

// HAConfigStore is limited to HA validation/reload and legacy storage-backed
// scene/script/automation compatibility. Native YAML publishing uses
// NativeYAMLStore.
type HAConfigStore struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func (s HAConfigStore) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return http.DefaultClient
}

func (s HAConfigStore) path(kind ConfigKind, id string) (string, error) {
	var domain string
	switch kind {
	case Scenes:
		domain = "scene"
	case Scripts:
		domain = "script"
	case Automations:
		domain = "automation"
	default:
		return "", fmt.Errorf("HA storage API is not used for native YAML kind %s", kind)
	}
	return "/api/config/" + domain + "/config/" + url.PathEscape(id), nil
}

func (s HAConfigStore) Get(ctx context.Context, kind ConfigKind, id string) (map[string]any, error) {
	path, err := s.path(kind, id)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := s.request(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s HAConfigStore) Put(ctx context.Context, kind ConfigKind, id string, config map[string]any) error {
	path, err := s.path(kind, id)
	if err != nil {
		return err
	}
	return s.request(ctx, http.MethodPost, path, config, nil)
}

func (s HAConfigStore) Delete(ctx context.Context, kind ConfigKind, id string) error {
	path, err := s.path(kind, id)
	if err != nil {
		return err
	}
	return s.request(ctx, http.MethodDelete, path, nil, nil)
}

func (s HAConfigStore) CheckConfig(ctx context.Context) error {
	var result struct {
		Result string `json:"result"`
		Errors any    `json:"errors"`
	}
	if err := s.request(ctx, http.MethodPost, "/api/config/core/check_config", nil, &result); err != nil {
		return fmt.Errorf("check HA configuration: %w", err)
	}
	if result.Result != "valid" {
		return fmt.Errorf("HA configuration invalid: %v", result.Errors)
	}
	return nil
}

func (s HAConfigStore) Reload(ctx context.Context, kinds []ConfigKind) error {
	seen := map[string]bool{}
	for _, kind := range kinds {
		domain := map[ConfigKind]string{Scenes: "scene", Scripts: "script", Automations: "automation", Helpers: "input_select"}[kind]
		if domain == "" {
			return fmt.Errorf("unsupported reload kind %s", kind)
		}
		if seen[domain] {
			continue
		}
		seen[domain] = true
		if err := s.request(ctx, http.MethodPost, "/api/services/"+domain+"/reload", map[string]any{}, nil); err != nil {
			return fmt.Errorf("reload %s: %w", domain, err)
		}
	}
	return nil
}

func (s HAConfigStore) Import(ctx context.Context, refs []ConfigRef) (Bundle, error) {
	data := map[ConfigKind]any{
		Scenes:      []any{},
		Scripts:     map[string]any{},
		Automations: []any{},
		Helpers:     map[string]any{},
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		key := string(ref.Kind) + ":" + ref.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		config, err := s.Get(ctx, ref.Kind, ref.ID)
		if err != nil {
			return Bundle{}, fmt.Errorf("import %s %s: %w", ref.Kind, ref.ID, err)
		}
		if _, ok := config["id"]; !ok {
			config["id"] = ref.ID
		}
		switch ref.Kind {
		case Scenes, Automations:
			data[ref.Kind] = append(data[ref.Kind].([]any), config)
		case Scripts, Helpers:
			if ref.Kind == Helpers {
				data[Helpers].(map[string]any)[ref.ID] = config
				continue
			}
			data[Scripts].(map[string]any)[ref.ID] = config
		default:
			return Bundle{}, fmt.Errorf("import does not support %s", ref.Kind)
		}
	}
	return withHash(Bundle{Files: map[ConfigKind]Config{
		Scenes:      {Kind: Scenes, Data: data[Scenes]},
		Scripts:     {Kind: Scripts, Data: data[Scripts]},
		Automations: {Kind: Automations, Data: data[Automations]},
		Helpers:     {Kind: Helpers, Data: data[Helpers]},
	}})
}

func (s HAConfigStore) Publish(ctx context.Context, draft, imported Bundle, refs, approvedDeletes []ConfigRef, backupRoot string) error {
	if err := ValidateBundle(draft); err != nil {
		return err
	}
	for _, ref := range approvedDeletes {
		if !containsRef(refs, ref) {
			return fmt.Errorf("approved deletion was not included in imported refs: %s:%s", ref.Kind, ref.ID)
		}
	}
	if err := s.CheckConfig(ctx); err != nil {
		return err
	}
	allRefs := unionRefs(refs, bundleRefs(draft))
	live, err := s.Import(ctx, refs)
	if err != nil {
		return err
	}
	if live.Hash != imported.Hash {
		return ErrStaleConfig
	}
	if backupRoot != "" {
		if _, err := SaveBackup(backupRoot, live, time.Now()); err != nil {
			return err
		}
	}
	oldEntries := bundleEntries(imported)
	newEntries := bundleEntries(draft)
	changed := []ConfigRef{}
	for _, kind := range []ConfigKind{Scenes, Scripts, Automations, Helpers} {
		for _, ref := range refsForKind(allRefs, kind) {
			oldValue, oldOK := oldEntries[ref]
			newValue, newOK := newEntries[ref]
			if !newOK {
				if !containsRef(approvedDeletes, ref) {
					continue
				}
				if err := s.Delete(ctx, kind, ref.ID); err != nil {
					rollbackErr := s.rollback(ctx, oldEntries, changed)
					if rollbackErr != nil {
						return fmt.Errorf("delete %s %s: %w; rollback: %v", kind, ref.ID, err, rollbackErr)
					}
					return fmt.Errorf("delete %s %s: %w; rollback complete", kind, ref.ID, err)
				}
				changed = append(changed, ref)
				continue
			}
			if !oldOK || stringMustJSON(oldValue) == stringMustJSON(newValue) {
				continue
			}
			if err := s.Put(ctx, kind, ref.ID, newValue); err != nil {
				rollbackErr := s.rollback(ctx, oldEntries, changed)
				if rollbackErr != nil {
					return fmt.Errorf("publish %s %s: %w; rollback: %v", kind, ref.ID, err, rollbackErr)
				}
				return fmt.Errorf("publish %s %s: %w; rollback complete", kind, ref.ID, err)
			}
			changed = append(changed, ref)
		}
	}
	if err := s.CheckConfig(ctx); err != nil {
		rollbackErr := s.rollback(ctx, oldEntries, changed)
		if rollbackErr != nil {
			return fmt.Errorf("HA configuration check: %w; rollback: %v", err, rollbackErr)
		}
		return fmt.Errorf("HA configuration check: %w; rollback complete", err)
	}
	verifyRefs := []ConfigRef{}
	for _, ref := range allRefs {
		if !containsRef(approvedDeletes, ref) {
			verifyRefs = append(verifyRefs, ref)
		}
	}
	verified, err := s.Import(ctx, verifyRefs)
	if err != nil {
		rollbackErr := s.rollback(ctx, oldEntries, changed)
		if rollbackErr != nil {
			return fmt.Errorf("verify publish: %w; rollback: %v", err, rollbackErr)
		}
		return fmt.Errorf("verify publish: %w; rollback complete", err)
	}
	verifiedEntries := bundleEntries(verified)
	for _, ref := range verifyRefs {
		if containsRef(approvedDeletes, ref) {
			if _, exists := verifiedEntries[ref]; exists {
				rollbackErr := s.rollback(ctx, oldEntries, changed)
				if rollbackErr != nil {
					return fmt.Errorf("verify delete %s %s: deletion did not persist; rollback: %v", ref.Kind, ref.ID, rollbackErr)
				}
				return fmt.Errorf("verify delete %s %s: deletion did not persist; rollback complete", ref.Kind, ref.ID)
			}
			continue
		}
		if expected, ok := newEntries[ref]; ok && stringMustJSON(expected) != stringMustJSON(verifiedEntries[ref]) {
			rollbackErr := s.rollback(ctx, oldEntries, changed)
			if rollbackErr != nil {
				return fmt.Errorf("verify %s %s: published config differs; rollback: %v", ref.Kind, ref.ID, rollbackErr)
			}
			return fmt.Errorf("verify %s %s: published config differs; rollback complete", ref.Kind, ref.ID)
		}
	}
	return nil
}

func unionRefs(left, right []ConfigRef) []ConfigRef {
	result := append([]ConfigRef(nil), left...)
	seen := map[ConfigRef]bool{}
	for _, ref := range result {
		seen[ref] = true
	}
	for _, ref := range right {
		if !seen[ref] {
			result = append(result, ref)
			seen[ref] = true
		}
	}
	return result
}

func bundleRefs(bundle Bundle) []ConfigRef {
	entries := bundleEntries(bundle)
	result := make([]ConfigRef, 0, len(entries))
	for ref := range entries {
		result = append(result, ref)
	}
	return result
}

func containsRef(refs []ConfigRef, wanted ConfigRef) bool {
	for _, ref := range refs {
		if ref == wanted {
			return true
		}
	}
	return false
}

func (s HAConfigStore) rollback(ctx context.Context, entries map[ConfigRef]map[string]any, refs []ConfigRef) error {
	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]
		var err error
		if entry, ok := entries[ref]; ok {
			err = s.Put(ctx, ref.Kind, ref.ID, entry)
		} else {
			err = s.Delete(ctx, ref.Kind, ref.ID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func bundleEntries(bundle Bundle) map[ConfigRef]map[string]any {
	result := map[ConfigRef]map[string]any{}
	for kind, config := range bundle.Files {
		switch kind {
		case Scripts:
			if values, ok := config.Data.(map[string]any); ok {
				for id, value := range values {
					if entry, ok := value.(map[string]any); ok {
						result[ConfigRef{Kind: kind, ID: id}] = entry
					}
				}
			}
		case Helpers:
			if values, ok := config.Data.(map[string]any); ok {
				for id, value := range values {
					if entry, ok := value.(map[string]any); ok {
						result[ConfigRef{Kind: kind, ID: id}] = entry
					}
				}
			}
		case Scenes, Automations:
			if values, ok := config.Data.([]any); ok {
				for _, value := range values {
					if entry, ok := value.(map[string]any); ok {
						if id, ok := entry["id"].(string); ok {
							result[ConfigRef{Kind: kind, ID: id}] = entry
						}
					}
				}
			}
		}
	}
	return result
}

func refsForKind(refs []ConfigRef, kind ConfigKind) []ConfigRef {
	result := []ConfigRef{}
	for _, ref := range refs {
		if ref.Kind == kind {
			result = append(result, ref)
		}
	}
	return result
}

func stringMustJSON(value any) string {
	data, _ := canonicalJSON(value)
	return string(data)
}

func (s HAConfigStore) request(ctx context.Context, method, path string, body any, output any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode HA config: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.BaseURL, "/")+path, reader)
	if err != nil {
		return fmt.Errorf("create HA request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+s.Token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := s.client().Do(request)
	if err != nil {
		return fmt.Errorf("HA config request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HA config request: HTTP %s", response.Status)
	}
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("decode HA config response: %w", err)
	}
	return nil
}
