package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

type ConfigRef struct {
	Kind ConfigKind
	ID   string
}

type ConfigPublisher interface {
	Publish(context.Context, Bundle, Bundle, []ConfigRef, []ConfigRef, string) error
}

var ErrStaleConfig = fmt.Errorf("home assistant configuration changed since import")

// HAConfigStore provides validation and reload for native YAML publishing.
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
		domain := map[ConfigKind]string{Scripts: "script", Automations: "automation", Helpers: "input_select"}[kind]
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
	return slices.Contains(refs, wanted)
}

func bundleEntries(bundle Bundle) map[ConfigRef]map[string]any {
	result := map[ConfigRef]map[string]any{}
	for kind, config := range bundle.Files {
		switch kind {
		case Scripts, Helpers:
			if values, ok := config.Data.(map[string]any); ok {
				for id, value := range values {
					if entry, ok := value.(map[string]any); ok {
						result[ConfigRef{Kind: kind, ID: id}] = entry
					}
				}
			}
		case Automations:
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
	defer func() { _ = response.Body.Close() }()
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
