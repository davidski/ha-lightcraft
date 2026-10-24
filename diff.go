package main

import (
	"encoding/json"
	"sort"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

type Change struct {
	Kind ConfigKind
	Old  string
	New  string
}

// removedRefs lists baseline entries absent from the draft. Publishing
// approves these deletions; the diff shown before PUBLISH includes them.
func removedRefs(baseline, draft Bundle) []ConfigRef {
	kept := bundleEntries(materializeNativeBundle(draft))
	var removed []ConfigRef
	for ref := range bundleEntries(materializeNativeBundle(baseline)) {
		if _, ok := kept[ref]; !ok {
			removed = append(removed, ref)
		}
	}
	sort.Slice(removed, func(i, j int) bool {
		if removed[i].Kind != removed[j].Kind {
			return removed[i].Kind < removed[j].Kind
		}
		return removed[i].ID < removed[j].ID
	})
	return removed
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
	return FormatChangesForPaths(changes, nil)
}

func FormatChangesForPaths(changes []Change, filePaths map[ConfigKind][]string) string {
	if len(changes) == 0 {
		return "No changes."
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Kind < changes[j].Kind })
	result := ""
	for _, change := range changes {
		oldYAML, newYAML := indentYAMLForKind(change.Kind, change.Old), indentYAMLForKind(change.Kind, change.New)
		label := string(change.Kind)
		if paths := filePaths[change.Kind]; len(paths) > 0 {
			label = strings.Join(paths, ", ")
		}
		result += udiff.Unified(label, label, oldYAML, newYAML)
	}
	return result
}

func formatPackageDiff(old, next Bundle, path string) (string, error) {
	oldYAML, err := marshalPackageYAML(materializeNativeBundle(old))
	if err != nil {
		return "", err
	}
	nextYAML, err := marshalPackageYAML(materializeNativeBundle(next))
	if err != nil {
		return "", err
	}
	return udiff.Unified(path, path, string(oldYAML), string(nextYAML)), nil
}

func indentYAMLForKind(kind ConfigKind, value string) string {
	var output any
	if json.Unmarshal([]byte(value), &output) != nil {
		return value + "\n"
	}
	data, _ := marshalConfigYAML(kind, output)
	return string(data)
}
