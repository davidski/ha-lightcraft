package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "web" {
		if err := runWeb(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	draft := flag.String("draft", ".", "directory containing native HA YAML drafts")
	against := flag.String("against", "", "baseline draft directory for TUI diff")
	importRefs := flag.String("import", "", "comma-separated refs, e.g. scenes:halloween_orange,scripts:holiday_lights")
	importConfigPath := flag.String("import-config", "", "YAML file containing Home Assistant import settings")
	configPath := flag.String("config", "", "YAML file containing Home Assistant settings")
	publish := flag.Bool("publish", false, "publish draft after diff confirmation")
	baseline := flag.String("baseline", "", "imported baseline draft used for stale detection")
	refsValue := flag.String("refs", "", "comma-separated HA refs included in the baseline")
	deleteValue := flag.String("delete", "", "comma-separated refs explicitly approved for HA deletion")
	backupRoot := flag.String("backup-dir", "backups", "local HA config backup directory")
	yes := flag.Bool("yes", false, "skip publish confirmation")
	haURL := flag.String("ha-url", os.Getenv("HOMEASSISTANT_URL"), "Home Assistant URL")
	tokenEnv := flag.String("token-env", "HOMEASSISTANT_TOKEN", "environment variable containing HA token")
	sshHost := flag.String("ssh-host", os.Getenv("HOMEASSISTANT_SSH_HOST"), "SSH host for native HA YAML")
	sshUser := flag.String("ssh-user", os.Getenv("HOMEASSISTANT_SSH_USER"), "SSH user for native HA YAML")
	haConfigDir := flag.String("ha-config-dir", os.Getenv("HOMEASSISTANT_CONFIG_DIR"), "remote HA configuration directory, e.g. /config")
	flag.Parse()
	loadConfig := func(path string) ProjectConfig {
		config, err := loadProjectConfig(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		applyProjectConfig(config, sshHost, sshUser, haConfigDir, haURL)
		return config
	}
	if *importRefs != "" || *importConfigPath != "" {
		var config ProjectConfig
		filePaths := map[ConfigKind][]string(nil)
		if *importConfigPath != "" {
			config = loadConfig(*importConfigPath)
			var err error
			filePaths, err = configFilePaths(config)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		if *sshHost == "" || *haConfigDir == "" {
			fmt.Fprintln(os.Stderr, "--import requires --ssh-host and --ha-config-dir for native HA YAML")
			os.Exit(1)
		}
		var refs []ConfigRef
		var err error
		if *importRefs != "" {
			refs, err = parseRefs(*importRefs)
		} else {
			refs, err = configRefs(config)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var bundle Bundle
		store, storeErr := nativeStore(*sshHost, *sshUser, *haConfigDir, *haURL, os.Getenv(*tokenEnv), filePaths)
		if storeErr != nil {
			fmt.Fprintln(os.Stderr, storeErr)
			os.Exit(1)
		}
		bundle, err = store.Import(context.Background(), refs)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if existing, err := LoadBundle(*draft); err == nil {
			if colors, ok := existing.Files[Colors]; ok {
				bundle.Files[Colors] = colors
			}
		}
		bundle = mergeImportedColors(bundle)
		if err := SaveBundle(*draft, bundle); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("Imported %d config files, fingerprint %s\n", len(bundle.Files), bundle.Hash)
		return
	}
	if *publish {
		filePaths := map[ConfigKind][]string(nil)
		if *configPath != "" {
			config := loadConfig(*configPath)
			var err error
			filePaths, err = configFilePaths(config)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		if *baseline == "" {
			fmt.Fprintln(os.Stderr, "--publish requires --baseline")
			os.Exit(1)
		}
		draftBundle, err := LoadBundle(*draft)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		baseBundle, err := LoadBundle(*baseline)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var refs []ConfigRef
		if *refsValue != "" {
			refs, err = parseRefs(*refsValue)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		} else {
			refs = bundleRefs(baseBundle)
		}
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
	filePaths := map[ConfigKind][]string(nil)
	if *configPath != "" {
		config := loadConfig(*configPath)
		var configErr error
		filePaths, configErr = configFilePaths(config)
		if configErr != nil {
			fmt.Fprintln(os.Stderr, configErr)
			os.Exit(1)
		}
	}

	bundle, err := LoadBundle(*draft)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var baselineBundle *Bundle
	if err := validateLighting(bundle); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *against != "" {
		value, err := LoadBundle(*against)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		baselineBundle = &value
	}
	var refs []ConfigRef
	if *refsValue != "" {
		refs, err = parseRefs(*refsValue)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	} else if baselineBundle != nil {
		refs = bundleRefs(*baselineBundle)
	}
	deletes, err := parseRefs(*deleteValue)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	model := statusModel{bundle: bundle, draftDir: *draft, baseline: baselineBundle, refs: refs, deletes: deletes, backup: *backupRoot, dashboardWorkspace: 0, dashboardFocus: 1}
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
			defer client.Close()
			model.stateAPI = client
			model.preview = Preview{API: client}
		}
	}
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func nativeStore(host, user, configDir, haURL, token string, filePaths map[ConfigKind][]string) (NativeYAMLStore, error) {
	if host == "" || configDir == "" {
		return NativeYAMLStore{}, fmt.Errorf("native YAML requires --ssh-host and --ha-config-dir")
	}
	store := NativeYAMLStore{Transport: SSHFileTransport{Host: host, User: user}, ConfigDir: configDir, FilePaths: filePaths}
	if haURL != "" && token != "" {
		api := HAConfigStore{BaseURL: haURL, Token: token}
		store.Check = api.CheckConfig
		store.Reload = api.Reload
	}
	return store, nil
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
		case Scenes, Scripts, Automations, Helpers:
		default:
			return nil, fmt.Errorf("unsupported ref kind %q", kind)
		}
		refs = append(refs, ConfigRef{Kind: kind, ID: parts[1]})
	}
	return refs, nil
}
