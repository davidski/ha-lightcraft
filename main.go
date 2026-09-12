package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "web" {
		if err := runWeb(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	command := "tui"
	if len(args) > 0 {
		switch args[0] {
		case "import", "publish", "tui":
			command = args[0]
			args = args[1:]
		}
	}
	draft := flag.String("data", "data", "root directory containing Lightcraft data")
	source := flag.String("source", "", "directory for pulled Home Assistant YAML files during import")
	baseline := flag.String("baseline", "", "imported baseline draft used for stale detection")
	deleteValue := flag.String("delete", "", "comma-separated refs explicitly approved for HA deletion")
	backupRoot := flag.String("backup-dir", "backups", "local HA config backup directory")
	yes := flag.Bool("yes", false, "skip publish confirmation")
	haURL := flag.String("ha-url", os.Getenv("HOMEASSISTANT_URL"), "Home Assistant URL")
	tokenEnv := flag.String("token-env", "HOMEASSISTANT_TOKEN", "environment variable containing HA token")
	sshHost := flag.String("ssh-host", os.Getenv("HOMEASSISTANT_SSH_HOST"), "SSH host for native HA YAML")
	sshUser := flag.String("ssh-user", os.Getenv("HOMEASSISTANT_SSH_USER"), "SSH user for native HA YAML")
	haConfigDir := flag.String("ha-config-dir", os.Getenv("HOMEASSISTANT_CONFIG_DIR"), "remote HA configuration directory, e.g. /config")
	if err := flag.CommandLine.Parse(args); err != nil {
		os.Exit(2)
	}
	sourceSet, baselineSet := false, false
	flag.Visit(func(value *flag.Flag) {
		switch value.Name {
		case "source":
			sourceSet = true
		case "baseline":
			baselineSet = true
		}
	})
	draftRoot := *draft
	proposedDir, currentDir := draftDirectories(draftRoot)
	*draft = proposedDir
	if !sourceSet {
		*source = currentDir
	}
	if !baselineSet {
		*baseline = currentDir
	}
	filePaths, err := configuredFilePaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	colorsDir := draftRoot
	if command == "import" {
		if *sshHost == "" || *haConfigDir == "" {
			fmt.Fprintln(os.Stderr, "import requires --ssh-host and --ha-config-dir for native HA YAML")
			os.Exit(1)
		}
		if *source == "" {
			fmt.Fprintln(os.Stderr, "import requires --source for the pulled Home Assistant files")
			os.Exit(1)
		}
		if filepath.Clean(*source) == filepath.Clean(*draft) {
			fmt.Fprintln(os.Stderr, "--source and --data must be separate directories")
			os.Exit(1)
		}
		var bundle Bundle
		store, storeErr := nativeStore(*sshHost, *sshUser, *haConfigDir, *haURL, os.Getenv(*tokenEnv), filePaths)
		if storeErr != nil {
			fmt.Fprintln(os.Stderr, storeErr)
			os.Exit(1)
		}
		full, err := store.Pull(context.Background(), *source)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		legacyDetected := legacyLightingDetected(full)
		bundle, err = importedBundleWithLegacyUpgrade(context.Background(), store, full, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if existing, err := LoadBundleAtWithReferences(*draft, colorsDir, filePaths); err == nil {
			if colors, ok := existing.Files[Colors]; ok {
				bundle.Files[Colors] = colors
			}
		}
		if err := SaveBundleAtWithReferences(*draft, colorsDir, bundle, filePaths); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if legacyDetected {
			fmt.Printf("Pulled Home Assistant files into %s, automatically upgraded legacy lighting, and initialized proposed draft %s, fingerprint %s\n", *source, *draft, bundle.Hash)
		} else {
			fmt.Printf("Pulled Home Assistant files into %s and initialized proposed draft %s, fingerprint %s\n", *source, *draft, bundle.Hash)
		}
		return
	}
	if command == "publish" {
		if *baseline == "" {
			fmt.Fprintln(os.Stderr, "publish requires --baseline")
			os.Exit(1)
		}
		draftBundle, err := LoadBundleAtWithReferences(*draft, colorsDir, filePaths)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		baseBundle, err := LoadBundleAt(*baseline, filePaths)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		refs := bundleRefs(baseBundle)
		deletes, err := parseRefs(*deleteValue)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		changes, err := PublishDiff(baseBundle, draftBundle)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(FormatChanges(changes))
		if len(changes) == 0 {
			return
		}
		if !*yes && !confirm() {
			fmt.Println("Publish cancelled.")
			return
		}
		publisher, err := publisherFor(*sshHost, *sshUser, *haConfigDir, *haURL, os.Getenv(*tokenEnv), filePaths)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := publisher.Publish(context.Background(), draftBundle, baseBundle, refs, deletes, *backupRoot); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Published and verified.")
		return
	}
	bundle, err := LoadBundleAtWithReferences(*draft, colorsDir, filePaths)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var baselineBundle *Bundle
	baselineImported := true
	if err := validateLighting(bundle); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	value, err := loadBaselineBundle(currentDir, filePaths)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	baselineBundle = &value
	refs := bundleRefs(*baselineBundle)
	deletes, err := parseRefs(*deleteValue)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	model := statusModel{bundle: bundle, draftDir: *draft, filePaths: filePaths, colorsDir: colorsDir, baseline: baselineBundle, baselineImported: baselineImported, refs: refs, deletes: deletes, backup: *backupRoot, dashboardWorkspace: 2, dashboardFocus: 1}
	model.loadingSpinner = spinner.New()
	if (*sshHost != "" || *haConfigDir != "") && *haURL != "" && os.Getenv(*tokenEnv) != "" {
		store, storeErr := nativeStore(*sshHost, *sshUser, *haConfigDir, *haURL, os.Getenv(*tokenEnv), filePaths)
		if storeErr != nil {
			fmt.Fprintln(os.Stderr, storeErr)
			os.Exit(1)
		}
		model.store = store
	}
	if *haURL != "" && os.Getenv(*tokenEnv) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		client, connectErr := ConnectHA(ctx, *haURL, os.Getenv(*tokenEnv))
		cancel()
		if connectErr != nil {
			model.message = "Home Assistant unavailable; editing draft offline: " + connectErr.Error()
		} else {
			defer func() { _ = client.Close() }()
			model.stateAPI = client
		}
	}
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func draftDirectories(root string) (proposed, current string) {
	return filepath.Join(root, "proposed"), filepath.Join(root, "current")
}

func nativeStore(host, user, configDir, haURL, token string, filePaths map[ConfigKind][]string) (NativeYAMLStore, error) {
	if host == "" || configDir == "" {
		return NativeYAMLStore{}, fmt.Errorf("native YAML requires --ssh-host and --ha-config-dir")
	}
	store := NativeYAMLStore{Transport: SSHFileTransport{Host: host, User: user, Args: sshArgs()}, ConfigDir: configDir, FilePaths: filePaths}
	if haURL != "" && token != "" {
		api := HAConfigStore{BaseURL: haURL, Token: token}
		store.Check = api.CheckConfig
		store.Reload = api.Reload
	}
	return store, nil
}

func sshArgs() []string {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "GlobalKnownHostsFile=/dev/null"}
	if key := strings.TrimSpace(os.Getenv("HOMEASSISTANT_SSH_KEY")); key != "" {
		args = append(args, "-o", "IdentitiesOnly=yes", "-i", key)
	}
	return args
}

func publisherFor(host, user, configDir, haURL, token string, filePaths map[ConfigKind][]string) (ConfigPublisher, error) {
	if host == "" || configDir == "" {
		return nil, fmt.Errorf("native YAML publish requires --ssh-host and --ha-config-dir")
	}
	if haURL == "" || token == "" {
		return nil, fmt.Errorf("native YAML publish requires --ha-url and token for config validation")
	}
	return nativeStore(host, user, configDir, haURL, token, filePaths)
}

func confirm() bool {
	fmt.Print("Publish these changes to Home Assistant? [y/N] ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return err == nil && strings.EqualFold(strings.TrimSpace(line), "y")
}

func parseRefs(value string) ([]ConfigRef, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var refs []ConfigRef
	for _, item := range strings.Split(value, ",") {
		parts := strings.SplitN(item, ":", 2)
		if len(parts) != 2 || parts[1] == "" {
			return nil, fmt.Errorf("invalid ref %q; expected kind:id", item)
		}
		kind := ConfigKind(parts[0])
		switch kind {
		case Scripts, Automations, Helpers:
		default:
			return nil, fmt.Errorf("unsupported ref kind %q", kind)
		}
		refs = append(refs, ConfigRef{Kind: kind, ID: parts[1]})
	}
	return refs, nil
}
