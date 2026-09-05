package main

import (
	"encoding/json"
	"sort"

	udiff "github.com/aymanbagabas/go-udiff"
	"gopkg.in/yaml.v3"
)

type Change struct {
	Kind ConfigKind
	Old  string
	New  string
}

func PublishDiff(old, draft Bundle) ([]Change, error) {
	return Diff(materializeNativeBundle(old), materializeNativeBundle(draft))
}

func Diff(old, next Bundle) ([]Change, error) {
	seen := map[ConfigKind]bool{}
	for kind := range old.Files {
		seen[kind] = true
	}
	for kind := range next.Files {
		seen[kind] = true
	}
	var changes []Change
	kinds := make([]ConfigKind, 0, len(seen))
	for kind := range seen {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	for _, kind := range kinds {
		var oldData, newData any
		if config, ok := old.Files[kind]; ok {
			oldData = config.Data
		}
		if config, ok := next.Files[kind]; ok {
			newData = config.Data
		}
		oldJSON, err := canonicalJSON(oldData)
		if err != nil {
			return nil, err
		}
		newJSON, err := canonicalJSON(newData)
		if err != nil {
			return nil, err
		}
		if string(oldJSON) != string(newJSON) {
			changes = append(changes, Change{Kind: kind, Old: string(oldJSON), New: string(newJSON)})
		}
	}
	return changes, nil
}

func FormatChanges(changes []Change) string {
	if len(changes) == 0 {
		return "No changes."
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Kind < changes[j].Kind })
	result := ""
	for _, change := range changes {
		oldYAML, newYAML := indentYAML(change.Old), indentYAML(change.New)
		result += udiff.Unified(string(change.Kind), string(change.Kind), oldYAML, newYAML)
	}
	return result
}

func indentYAML(value string) string {
	var output any
	if json.Unmarshal([]byte(value), &output) != nil {
		return value + "\n"
	}
	data, _ := yaml.Marshal(output)
	return string(data)
}

func prettyJSON(value string) string {
	var output any
	if json.Unmarshal([]byte(value), &output) != nil {
		return value
	}
	data, _ := json.MarshalIndent(output, "", "  ")
	return string(data)
}
