package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type webApp struct {
	mu                 sync.Mutex
	draftDir           string
	baselineDir        string
	allowedWebHosts    []string
	filePaths          map[ConfigKind][]string
	colorsDir          string
	bundle             Bundle
	baseline           Bundle
	token              string
	message            string
	publisher          ConfigPublisher
	refs               []ConfigRef
	backupDir          string
	publishError       string
	baselineReady      bool
	stateAPI           StateAPI
	store              ConfigPublisher
	importer           *NativeYAMLStore
	inventoryStates    map[string]LightState
	inventoryLocations map[string]LightLocation
	inventoryMetadata  map[string]HAEntityMetadata
	inventoryStatus    HAInventoryStatus
	inventoryError     string
	pendingSchedule    *webAssignment
}

var errWebDraftChanged = errors.New("draft files changed outside the web editor; restart to load them")

func forwardedWebPrefix(r *http.Request) string {
	prefix := strings.TrimSpace(r.Header.Get("X-Forwarded-Prefix"))
	if prefix == "" || !strings.HasPrefix(prefix, "/") || strings.HasPrefix(prefix, "//") || strings.ContainsAny(prefix, "?#\x00\r\n") {
		return ""
	}
	decoded, err := url.PathUnescape(prefix)
	if err != nil {
		return ""
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return ""
		}
	}
	return strings.TrimRight(prefix, "/")
}

func webPath(prefix, target string) string {
	if prefix == "" {
		return target
	}
	if target == "" {
		return prefix
	}
	return prefix + "/" + strings.TrimLeft(target, "/")
}

func stripForwardedWebPrefix(r *http.Request, prefix string) *http.Request {
	if prefix == "" || (r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/")) {
		return r
	}
	clone := r.Clone(r.Context())
	clone.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
	if clone.URL.Path == "" {
		clone.URL.Path = "/"
	}
	clone.URL.RawPath = ""
	return clone
}

func prefixWebDocument(document, prefix string) string {
	if prefix == "" {
		return document
	}
	escaped := template.HTMLEscapeString(prefix)
	for _, attribute := range []string{"href", "action", "formaction", "src"} {
		document = strings.ReplaceAll(document, attribute+`="/`, attribute+`="`+escaped+`/`)
	}
	return document
}

type webColor struct {
	ID   string
	Name string
	X    float64
	Y    float64
	Hex  string
}

type webStep struct {
	Name       string
	Color      string
	Brightness int
	Hold       float64
	Transition float64
}

type webPreviewStep struct {
	Name       string
	Hex        string
	XY         string
	Brightness int
	Hold       float64
}

type webPreviewLight struct {
	ID   string
	Name string
}

type webSequence struct {
	ID            string
	Name          string
	Repeat        bool
	Steps         []webStep
	PreviewSteps  []webPreviewStep
	PreviewLights []webPreviewLight
}

type webAssignment struct {
	ID         string
	Name       string
	Kind       string
	Sequence   string
	Targets    string
	WLEDLight  string
	WLEDSelect string
	WLEDOption string
	Start      string
	End        string
	On         string
	Off        string
	Finish     string
	Enabled    bool
	AllDay     bool
}

type webSelect struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Options []string `json:"options"`
}

type webWLEDChoice struct {
	Light   webLight    `json:"light"`
	Selects []webSelect `json:"selects"`
}

type webLight struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Location   string `json:"location"`
	State      string `json:"state"`
	Brightness string `json:"brightness"`
	Color      string `json:"color"`
	Modes      string `json:"modes"`
	Effects    string `json:"effects"`
	Group      bool   `json:"group"`
	Selected   bool   `json:"selected"`
}

type webPage struct {
	View                    string
	Message                 string
	Token                   string
	Hash                    string
	Unpublished             bool
	Colors                  []webColor
	Sequences               []webSequence
	Assignments             []webAssignment
	Color                   webColor
	Sequence                webSequence
	Assignment              webAssignment
	YAMLFile                string
	YAML                    string
	Diff                    string
	ImportReady             bool
	ImportReason            string
	ImportNeedsConfirmation bool
	PublishReady            bool
	PublishReason           string
	Lights                  []webLight
	ScheduleLights          []webLight
	WLEDLights              []webLight
	WLEDSelects             []webSelect
	WLEDChoices             []webWLEDChoice
	LiveReady               bool
	AgendaDate              string
	AgendaPrevious          string
	AgendaNext              string
	Agenda                  []scheduleAgendaDay
}

func runWeb(args []string) error {
	flags := flag.NewFlagSet("web", flag.ContinueOnError)
	draftRootFlag := flags.String("data", "data", "root directory containing Lightcraft data")
	backupDir := flags.String("backup-dir", "backups", "local HA config backup directory")
	haURL := flags.String("ha-url", os.Getenv("HOMEASSISTANT_URL"), "Home Assistant URL")
	tokenEnv := flags.String("token-env", "HOMEASSISTANT_TOKEN", "environment variable containing HA token")
	sshHost := flags.String("ssh-host", os.Getenv("HOMEASSISTANT_SSH_HOST"), "SSH host for native HA YAML")
	sshUser := flags.String("ssh-user", os.Getenv("HOMEASSISTANT_SSH_USER"), "SSH user for native HA YAML")
	haConfigDir := flags.String("ha-config-dir", os.Getenv("HOMEASSISTANT_CONFIG_DIR"), "remote Home Assistant configuration directory")
	port := flags.Int("port", 8080, "local web server port")
	host := flags.String("host", "127.0.0.1", "web listen address")
	open := flags.Bool("open", false, "open the web interface in a browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	draftRoot := *draftRootFlag
	draftDir, currentDir := draftDirectories(draftRoot)
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("web port must be 1-65535")
	}
	filePaths, err := configuredFilePaths()
	if err != nil {
		return err
	}
	colorsDir := draftRoot
	app, err := newWebAppWithReferences(draftDir, currentDir, colorsDir, filePaths)
	if err != nil {
		return err
	}
	app.backupDir = *backupDir
	var client *HAClient
	if store, storeErr := nativeStore(*sshHost, *sshUser, *haConfigDir, *haURL, os.Getenv(*tokenEnv), filePaths); storeErr == nil {
		app.store = &store
		app.importer = &store
	}
	if *haURL != "" && os.Getenv(*tokenEnv) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		client, err = ConnectHA(ctx, *haURL, os.Getenv(*tokenEnv))
		cancel()
		if err == nil {
			app.stateAPI = client
			app.refreshInventory(context.Background())
		} else {
			app.inventoryError = err.Error()
		}
	}
	if len(app.refs) == 0 {
		app.refs = bundleRefs(app.baseline)
	}
	if app.store == nil {
		app.publishError = "Native YAML publish requires SSH and Home Assistant configuration."
	} else if app.stateAPI == nil {
		app.publishError = "Publishing requires a working Home Assistant connection."
	} else {
		app.publisher = app.store
	}
	if client != nil {
		defer func() { _ = client.Close() }()
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(*host, strconv.Itoa(*port)))
	if err != nil {
		return fmt.Errorf("start web server: %w", err)
	}
	address := "http://" + listener.Addr().String()
	fmt.Printf("HA Lightcraft: %s\n", address)
	if *open {
		go func() { _ = openWebBrowser(address) }()
	}
	return http.Serve(listener, app.handler())
}

func newWebApp(draftDir, baselineDir string) (*webApp, error) {
	return newWebAppWithReferences(draftDir, baselineDir, filepath.Dir(draftDir), nil)
}

func newWebAppWithReferences(draftDir, baselineDir, referencesDir string, filePaths map[ConfigKind][]string) (*webApp, error) {
	bundle, err := LoadBundleAtWithReferences(draftDir, referencesDir, filePaths)
	if err != nil {
		return nil, err
	}
	if err := ValidateBundle(bundle); err != nil {
		return nil, err
	}
	baseline, err := cloneBundle(bundle)
	if err != nil {
		return nil, err
	}
	if baselineDir != "" {
		baseline, err = loadBaselineBundle(baselineDir, filePaths)
		if err != nil {
			return nil, err
		}
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("create web session: %w", err)
	}
	return &webApp{draftDir: draftDir, baselineDir: baselineDir, allowedWebHosts: configuredWebHosts(), filePaths: filePaths, colorsDir: referencesDir, bundle: bundle, baseline: baseline, token: hex.EncodeToString(tokenBytes), baselineReady: baselineDir != "" && len(baseline.Files) > 0}, nil
}

func (a *webApp) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.index)
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /favicon.png", serveFavicon)
	mux.HandleFunc("POST /sequence", a.saveSequence)
	mux.HandleFunc("POST /schedule", a.saveSchedule)
	mux.HandleFunc("POST /color", a.saveColor)
	mux.HandleFunc("POST /delete", a.deleteDraftItem)
	mux.HandleFunc("POST /import", a.importDraft)
	mux.HandleFunc("POST /publish", a.publishDraft)
	mux.HandleFunc("POST /inventory", a.refreshInventoryPost)
	mux.HandleFunc("POST /playback", a.controlPlayback)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, stripForwardedWebPrefix(r, forwardedWebPrefix(r)))
	})
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

//go:embed favicon.png
var faviconPNG []byte

func serveFavicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(faviconPNG)
}

func (a *webApp) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !a.validWebHost(r.Host) {
		http.Error(w, "invalid host", http.StatusForbidden)
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "colors"
	}
	switch view {
	case "publish":
		view = "yaml"
	case "inventory":
		view = "lights"
	}
	prefix := forwardedWebPrefix(r)
	a.mu.Lock()
	defer a.mu.Unlock()
	page, err := a.page(view, r.URL.Query().Get("edit"), r.URL.Query().Get("date"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	var body bytes.Buffer
	if err := webTemplate.Execute(&body, page); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write([]byte(prefixWebDocument(body.String(), prefix)))
}

func (a *webApp) page(view, edit, selectedDate string) (webPage, error) {
	page := webPage{WLEDChoices: []webWLEDChoice{}, View: view, Message: a.message, Token: a.token, Hash: a.bundle.Hash, LiveReady: a.stateAPI != nil}
	a.message = ""
	if view == "agenda" {
		date := parseAgendaDate(selectedDate)
		date = date.AddDate(0, 0, -int(date.Weekday()))
		page.AgendaDate = date.Format("2006-01-02")
		page.AgendaPrevious = date.AddDate(0, 0, -scheduleAgendaDays).Format("2006-01-02")
		page.AgendaNext = date.AddDate(0, 0, scheduleAgendaDays).Format("2006-01-02")
		page.Agenda = scheduleAgenda(lightingAssignments(a.bundle), date)
	}
	for _, id := range colorIDs(a.bundle) {
		color := colorDefinitions(a.bundle)[id]
		page.Colors = append(page.Colors, webColor{ID: id, Name: color.Name, X: color.X, Y: color.Y, Hex: cieRGBHex(CIEColor{X: color.X, Y: color.Y})})
	}
	for _, id := range lightingIDs(a.bundle, 0) {
		sequence := colorSequences(a.bundle)[id]
		item := webSequence{ID: id, Name: sequence.Name, Repeat: sequence.Repeat}
		for _, step := range sequence.Steps {
			item.Steps = append(item.Steps, webStep{Name: step.Name, Color: webStepColor(a.bundle, step), Brightness: brightnessPercentValue(step.Brightness), Hold: step.Hold, Transition: step.Transition})
		}
		if edit == id {
			item.PreviewSteps = make([]webPreviewStep, 0, len(sequence.Steps))
			for _, step := range sequence.Steps {
				step = currentCatalogStep(a.bundle, step)
				item.PreviewSteps = append(item.PreviewSteps, webPreviewStep{Name: step.Name, Hex: previewRGBHex(step), XY: fmt.Sprintf("XY (%.3f, %.3f)", step.XY[0], step.XY[1]), Brightness: step.Brightness, Hold: step.Hold})
			}
			for _, target := range sequencePreviewTargetIDs(a.bundle, id) {
				name := target
				if state, ok := a.inventoryStates[target]; ok {
					name = lightName(state, target)
				}
				item.PreviewLights = append(item.PreviewLights, webPreviewLight{ID: target, Name: name})
			}
		}
		page.Sequences = append(page.Sequences, item)
	}
	for _, id := range lightingIDs(a.bundle, 1) {
		assignment := lightingAssignments(a.bundle)[id]
		kind := "sequence"
		if assignment.WLED != nil {
			kind = "wled"
		}
		item := webAssignment{ID: id, Name: assignment.Name, Kind: kind, Sequence: assignment.Sequence, Targets: strings.Join(assignment.Targets, ";"), Start: assignment.Start, End: assignment.End, On: assignment.On, Off: assignment.Off, Finish: assignment.Finish, Enabled: assignment.Enabled, AllDay: assignment.AllDay}
		if assignment.WLED != nil {
			item.WLEDLight, item.WLEDSelect, item.WLEDOption = assignment.WLED.Light, assignment.WLED.Select, assignment.WLED.Option
		}
		page.Assignments = append(page.Assignments, item)
	}
	page.Color = webColor{X: .3127, Y: .3290, Hex: "#FFFFFF"}
	page.Sequence = webSequence{Repeat: true, Steps: []webStep{{Brightness: 100, Hold: 6, Transition: .5}}}
	page.Assignment = webAssignment{Kind: "sequence", On: "sunset", Off: "00:00", Finish: "off", Enabled: true}
	if edit != "" {
		for _, item := range page.Colors {
			if item.ID == edit {
				page.Color = item
			}
		}
		for _, item := range page.Sequences {
			if item.ID == edit {
				page.Sequence = item
			}
		}
		for _, item := range page.Assignments {
			if item.ID == edit {
				page.Assignment = item
			}
		}
	}
	pendingSchedule := a.pendingSchedule
	if pendingSchedule != nil && view == "schedules" {
		page.Assignment = *a.pendingSchedule
		a.pendingSchedule = nil
	}
	targetLights := map[string]bool{}
	wledLights := map[string]webLight{}
	for _, id := range webTargetLightIDs(a.inventoryStates, a.inventoryLocations) {
		targetLights[id] = true
	}
	if pendingSchedule != nil && view == "schedules" {
		for _, id := range splitWebValues(pendingSchedule.Targets) {
			if _, ok := a.inventoryStates[id]; ok {
				targetLights[id] = true
			}
		}
	}
	for _, id := range webLightIDs(a.bundle, a.inventoryStates, a.inventoryLocations) {
		state, location := a.inventoryStates[id], a.inventoryLocations[id]
		light := webLight{ID: id, Name: lightName(state, id), Location: locationLabel(location), State: state.State, Brightness: formatBrightness(state.Attribute["brightness"]), Color: renderWebLightColor(state.Attribute), Modes: strings.Join(stringList(state.Attribute["supported_color_modes"]), ", "), Effects: strings.Join(stringList(state.Attribute["effect_list"]), ", "), Group: lightIsGroup(state)}
		page.Lights = append(page.Lights, light)
		if targetLights[id] {
			scheduleLight := light
			scheduleLight.Selected = webContains(page.Assignment.Targets, id)
			page.ScheduleLights = append(page.ScheduleLights, scheduleLight)
		}
		if containsString(wledLightIDs(a.inventoryStates, a.inventoryMetadata), id) {
			page.WLEDLights = append(page.WLEDLights, light)
			wledLights[id] = light
		}
	}
	for _, lightID := range wledLightIDs(a.inventoryStates, a.inventoryMetadata) {
		choice := webWLEDChoice{Light: wledLights[lightID]}
		for _, id := range wledSelectorIDs(lightID, a.inventoryStates, a.inventoryMetadata) {
			state := a.inventoryStates[id]
			choice.Selects = append(choice.Selects, webSelect{ID: id, Name: lightName(state, id), Options: stringList(state.Attribute["options"])})
		}
		page.WLEDChoices = append(page.WLEDChoices, choice)
		if lightID == page.Assignment.WLEDLight {
			page.WLEDSelects = append(page.WLEDSelects, choice.Selects...)
		}
	}
	sort.SliceStable(page.ScheduleLights, func(i, j int) bool {
		if page.ScheduleLights[i].Group != page.ScheduleLights[j].Group {
			return page.ScheduleLights[i].Group
		}
		return strings.ToLower(page.ScheduleLights[i].Name) < strings.ToLower(page.ScheduleLights[j].Name)
	})
	if a.inventoryError != "" && page.Message == "" {
		page.Message = "Home Assistant inventory unavailable: " + a.inventoryError
	}
	changes, err := PublishDiff(a.baseline, a.bundle)
	if err != nil {
		return webPage{}, err
	}
	if view == "yaml" {
		packageName, ok := packagePath(a.filePaths)
		if !ok && len(a.filePaths) == 0 {
			packageName, ok = defaultPackagePath, true
		}
		if !ok {
			return webPage{}, fmt.Errorf("web editor requires the Lightcraft package configuration")
		}
		page.YAMLFile = packageName
		data, err := marshalPackageYAML(materializeNativeBundle(a.bundle))
		if err != nil {
			return webPage{}, err
		}
		page.YAML = string(data)
		if len(changes) > 0 {
			diff, diffErr := formatPackageDiff(a.baseline, a.bundle, packageName)
			if diffErr != nil {
				return webPage{}, diffErr
			}
			page.Diff = diff
		} else {
			page.Diff = "No changes."
		}
	}
	if err != nil {
		return webPage{}, err
	}
	page.Unpublished = a.baselineReady && len(changes) > 0
	page.PublishReason = a.publishError
	page.ImportReady = a.importer != nil
	page.ImportNeedsConfirmation = len(nativeDraftKinds(a.bundle)) > 0
	if !page.ImportReady {
		page.ImportReason = "Import requires SSH and Home Assistant configuration."
	}
	if page.PublishReason == "" && len(changes) == 0 {
		page.PublishReason = "No changes to publish."
	}
	page.PublishReady = a.baselineReady && a.publisher != nil && page.PublishReason == ""
	return page, nil
}

func renderWebLightColor(value map[string]any) string {
	rgb, source, ok := displayRGB(value)
	if !ok {
		return "Color: not specified"
	}
	return fmt.Sprintf("Color: #%02X%02X%02X · %s", rgb[0], rgb[1], rgb[2], source)
}

func (a *webApp) post(w http.ResponseWriter, r *http.Request, view, success string, persist bool, mutate func(Bundle, url.Values) (Bundle, error)) {
	if !a.validWebHost(r.Host) {
		http.Error(w, "invalid host", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if view == "" {
		view = r.Form.Get("kind")
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != r.Host {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if subtle.ConstantTimeCompare([]byte(r.Form.Get("token")), []byte(a.token)) != 1 {
		http.Error(w, "invalid session token", http.StatusForbidden)
		return
	}
	if r.Form.Get("hash") != a.bundle.Hash {
		http.Error(w, "draft changed; reload the page", http.StatusConflict)
		return
	}
	a.message = ""
	next, err := mutate(a.bundle, r.Form)
	if err == nil && persist {
		err = a.persistDraft(next)
	}
	if errors.Is(err, errWebDraftChanged) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		if view == "schedules" {
			pending := webAssignmentFromForm(r.Form)
			a.pendingSchedule = &pending
		}
		a.message = "Could not update draft: " + err.Error()
	} else {
		a.bundle = next
		if a.message == "" {
			a.message = success
		}
	}
	location := "/?view=" + url.QueryEscape(view)
	if err == nil && view == "sequences" {
		id := r.Form.Get("id")
		if id == "" {
			id = colorID(strings.TrimSpace(r.Form.Get("display_name")))
		}
		if id != "" {
			location += "&edit=" + url.QueryEscape(id)
		}
	}
	http.Redirect(w, r, webPath(forwardedWebPrefix(r), location), http.StatusSeeOther)
}

func (a *webApp) persistDraft(bundle Bundle) error {
	if err := ValidateBundle(bundle); err != nil {
		return err
	}
	disk, err := LoadBundleAtWithReferences(a.draftDir, a.colorsDir, a.filePaths)
	if err != nil {
		return err
	}
	if disk.Hash != a.bundle.Hash {
		return errWebDraftChanged
	}
	return SaveBundleAtWithReferences(a.draftDir, a.colorsDir, bundle, a.filePaths)
}

func (a *webApp) saveSequence(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "sequences", "Draft updated.", true, func(bundle Bundle, form url.Values) (Bundle, error) {
		sequence := ColorSequence{ID: form.Get("id"), Name: strings.TrimSpace(form.Get("display_name")), Repeat: form.Get("repeat") == "on"}
		if sequence.ID == "" {
			sequence.ID = colorID(sequence.Name)
			if _, exists := colorSequences(bundle)[sequence.ID]; exists {
				return Bundle{}, fmt.Errorf("a sequence with that name already exists")
			}
		}
		colors := form["step_color"]
		brightnesses, holds, transitions := form["step_brightness"], form["step_hold"], form["step_transition"]
		if len(colors) == 0 || len(colors) != len(brightnesses) || len(colors) != len(holds) || len(colors) != len(transitions) {
			return Bundle{}, fmt.Errorf("each color step must be complete")
		}
		for i := range colors {
			if colors[i] == "" && strings.TrimSpace(brightnesses[i]) == "" {
				continue
			}
			color, ok := colorDefinitions(bundle)[colors[i]]
			brightnessPercent, bErr := strconv.ParseFloat(brightnesses[i], 64)
			hold, hErr := strconv.ParseFloat(holds[i], 64)
			transition, tErr := strconv.ParseFloat(transitions[i], 64)
			if !ok || bErr != nil || brightnessPercent < 0 || brightnessPercent > 100 || hErr != nil || tErr != nil {
				return Bundle{}, fmt.Errorf("step %d needs a catalog color, 0-100%% brightness, and numeric timing values", i+1)
			}
			step := ColorStep{Name: color.Name, XY: []float64{color.X, color.Y}, Brightness: brightnessFromPercent(brightnessPercent), Hold: hold, Transition: transition}
			sequence.Steps = append(sequence.Steps, step)
		}
		return saveColorSequence(bundle, sequence)
	})
}

func (a *webApp) saveSchedule(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "schedules", "Draft updated.", true, func(bundle Bundle, form url.Values) (Bundle, error) {
		pending := webAssignmentFromForm(form)
		assignment := LightingAssignment{ID: pending.ID, Name: pending.Name, Sequence: pending.Sequence, Targets: form["targets"], Start: pending.Start, End: pending.End, On: pending.On, Off: pending.Off, Finish: pending.Finish, Enabled: pending.Enabled, AllDay: pending.AllDay}
		if pending.Kind == "wled" {
			assignment.Sequence, assignment.Targets = "", nil
			assignment.WLED = &WLEDProgram{Light: pending.WLEDLight, Select: pending.WLEDSelect, Option: pending.WLEDOption}
			status := a.inventoryStatus
			status.Connected = status.Connected || a.stateAPI != nil
			if err := validateConnectedWLEDProgram(status, *assignment.WLED, a.inventoryStates, a.inventoryMetadata); err != nil {
				return Bundle{}, err
			}
		} else if len(a.inventoryStates) > 0 {
			var err error
			assignment.Targets, err = resolveLightingTargets(assignment.Targets, a.inventoryStates)
			if err != nil {
				return Bundle{}, err
			}
		}
		allowed := webLightSet(bundle, a.inventoryStates, a.inventoryLocations)
		for _, target := range assignment.Targets {
			if !allowed[target] {
				return Bundle{}, fmt.Errorf("invalid light %q", target)
			}
		}
		if assignment.ID == "" {
			assignment.ID = colorID(assignment.Name)
			if _, exists := lightingAssignments(bundle)[assignment.ID]; exists {
				return Bundle{}, fmt.Errorf("a schedule with that name already exists")
			}
		}
		return saveLightingAssignment(bundle, assignment)
	})
}

func webAssignmentFromForm(form url.Values) webAssignment {
	kind := form.Get("kind")
	if kind != "wled" {
		kind = "sequence"
	}
	return webAssignment{
		ID: form.Get("id"), Name: strings.TrimSpace(form.Get("schedule_title")), Kind: kind, Sequence: form.Get("sequence"),
		Targets: strings.Join(form["targets"], ";"), Start: strings.TrimSpace(form.Get("start")), End: strings.TrimSpace(form.Get("end")),
		WLEDLight: form.Get("wled_light"), WLEDSelect: form.Get("wled_select"), WLEDOption: form.Get("wled_option"),
		On: strings.TrimSpace(form.Get("on")), Off: strings.TrimSpace(form.Get("off")), Finish: strings.TrimSpace(form.Get("finish")), Enabled: form.Get("enabled") == "on", AllDay: form.Get("all_day") == "on",
	}
}

func (a *webApp) saveColor(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "colors", "Draft updated.", true, func(bundle Bundle, form url.Values) (Bundle, error) {
		name := strings.TrimSpace(form.Get("display_name"))
		x, xErr := strconv.ParseFloat(form.Get("x"), 64)
		y, yErr := strconv.ParseFloat(form.Get("y"), 64)
		if xErr != nil || yErr != nil {
			return Bundle{}, fmt.Errorf("XY coordinates must be numbers")
		}
		id := form.Get("id")
		if id == "" {
			id = colorID(name)
			if _, exists := colorDefinitions(bundle)[id]; exists {
				return Bundle{}, fmt.Errorf("a color with that name already exists")
			}
		}
		return upsertColor(bundle, id, ColorDefinition{Name: name, X: x, Y: y})
	})
}

func (a *webApp) deleteDraftItem(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "", "Draft updated.", true, func(bundle Bundle, form url.Values) (Bundle, error) {
		id := form.Get("id")
		switch form.Get("kind") {
		case "sequences":
			return deleteColorSequence(bundle, id)
		case "colors":
			return deleteColor(bundle, id)
		default:
			return Bundle{}, fmt.Errorf("unsupported draft item")
		}
	})
}

func (a *webApp) importDraft(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "yaml", "Imported Home Assistant YAML.", false, func(bundle Bundle, form url.Values) (Bundle, error) {
		if len(nativeDraftKinds(bundle)) > 0 && form.Get("confirmation") != "IMPORT" {
			return Bundle{}, fmt.Errorf("type IMPORT to confirm")
		}
		if a.importer == nil || a.baselineDir == "" {
			return Bundle{}, fmt.Errorf("import is unavailable: %s", a.pageImportReason())
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		bundle, baseline, _, err := importDraft(ctx, *a.importer, a.draftDir, a.baselineDir, a.colorsDir, a.filePaths)
		if err != nil {
			return Bundle{}, fmt.Errorf("import failed: %w", err)
		}
		a.baseline, a.baselineReady, a.refs = baseline, true, bundleRefs(baseline)
		return bundle, nil
	})
}

func (a *webApp) pageImportReason() string {
	if a.importer == nil {
		return "SSH and Home Assistant configuration are required"
	}
	return "the baseline directory is unavailable"
}

func (a *webApp) publishDraft(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "yaml", "Published, verified, and backed up.", false, func(bundle Bundle, form url.Values) (Bundle, error) {
		if form.Get("confirmation") != "PUBLISH" {
			return Bundle{}, fmt.Errorf("type PUBLISH to confirm")
		}
		if !a.baselineReady || a.publisher == nil {
			return Bundle{}, fmt.Errorf("publishing is unavailable: %s", a.publishError)
		}
		disk, err := LoadBundleAtWithReferences(a.draftDir, a.colorsDir, a.filePaths)
		if err != nil {
			return Bundle{}, err
		}
		if disk.Hash != bundle.Hash {
			return Bundle{}, errWebDraftChanged
		}
		changes, err := PublishDiff(a.baseline, bundle)
		if err != nil {
			return Bundle{}, err
		}
		if len(changes) == 0 {
			return Bundle{}, fmt.Errorf("no changes to publish")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := a.publisher.Publish(ctx, bundle, a.baseline, a.refs, nil, a.backupDir); err != nil {
			return Bundle{}, fmt.Errorf("publish failed: %w", err)
		}
		if importer, ok := a.publisher.(interface {
			ReadAll(context.Context) (Bundle, error)
		}); ok {
			refreshed, err := importer.ReadAll(ctx)
			if err != nil {
				a.message = "Published, verified, and backed up. Could not refresh the baseline: " + err.Error()
				a.publisher = nil
				a.publishError = "Restart the web editor before publishing again."
			} else {
				a.baseline = refreshed
			}
		} else if importer, ok := a.publisher.(interface {
			Import(context.Context, []ConfigRef) (Bundle, error)
		}); ok {
			refreshed, err := importer.Import(ctx, a.refs)
			if err != nil {
				a.message = "Published, verified, and backed up. Could not refresh the baseline: " + err.Error()
				a.publisher = nil
				a.publishError = "Restart the web editor before publishing again."
			} else {
				a.baseline = refreshed
			}
		} else {
			a.publisher = nil
			a.publishError = "Restart the web editor before publishing again."
		}
		return bundle, nil
	})
}

func (a *webApp) refreshInventory(ctx context.Context) {
	a.inventoryStates, a.inventoryLocations, a.inventoryMetadata = nil, nil, nil
	a.inventoryStatus = HAInventoryStatus{}
	a.inventoryError = ""
	if a.stateAPI == nil {
		a.inventoryError = "set Home Assistant URL and token in the project config"
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	states, inventory, status, _ := fetchInventory(ctx, a.stateAPI)
	a.inventoryStates, a.inventoryLocations, a.inventoryMetadata = states, inventory.Locations, inventory.Metadata
	a.inventoryStatus = status
	a.inventoryError = status.Error
}

func (a *webApp) refreshInventoryPost(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "lights", "Home Assistant light inventory refreshed.", false, func(bundle Bundle, _ url.Values) (Bundle, error) {
		a.refreshInventory(r.Context())
		if a.inventoryError != "" {
			return Bundle{}, errors.New(a.inventoryError)
		}
		return bundle, nil
	})
}

func (a *webApp) controlPlayback(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "schedules", "Live schedule playback updated.", false, func(bundle Bundle, form url.Values) (Bundle, error) {
		id, action := form.Get("id"), form.Get("action")
		assignment, ok := lightingAssignments(bundle)[id]
		if !a.baselineReady || !ok || stringMustJSON(lightingAssignments(a.baseline)[id]) != stringMustJSON(assignment) {
			return Bundle{}, fmt.Errorf("publish this schedule before controlling live playback")
		}
		option := "paused"
		if action == "resume" {
			option = "idle"
		} else if action != "pause" {
			return Bundle{}, fmt.Errorf("unsupported playback action")
		}
		if form.Get("confirmation") != strings.ToUpper(action) {
			return Bundle{}, fmt.Errorf("type %s to confirm", strings.ToUpper(action))
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err := controlLighting(ctx, a.stateAPI, id, option); err != nil {
			return Bundle{}, err
		}
		return bundle, nil
	})
}

func webContains(values, id string) bool {
	for _, value := range splitWebValues(values) {
		if value == id {
			return true
		}
	}
	return false
}

func webLightIDs(bundle Bundle, states map[string]LightState, locations map[string]LightLocation) []string {
	seen := map[string]bool{}
	for _, id := range inventoryIDs(states, locations) {
		seen[id] = true
	}
	for _, assignment := range lightingAssignments(bundle) {
		for _, id := range assignment.Targets {
			seen[id] = true
		}
		if assignment.WLED != nil {
			seen[assignment.WLED.Light] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return sortedStrings(ids)
}

func webTargetLightIDs(states map[string]LightState, locations map[string]LightLocation) []string {
	return colorLightIDs(states, locations)
}

func webLightSet(bundle Bundle, states map[string]LightState, locations map[string]LightLocation) map[string]bool {
	result := map[string]bool{}
	ids := webTargetLightIDs(states, locations)
	if len(states) == 0 {
		ids = webLightIDs(bundle, states, locations)
	}
	for _, id := range ids {
		result[id] = true
	}
	return result
}

func splitWebValues(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' })
}

func configuredWebHosts() []string {
	values := splitWebValues(os.Getenv("WEB_ALLOWED_HOSTS"))
	hosts := make([]string, 0, len(values))
	for _, value := range values {
		if host := webHost(value); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func webHost(hostport string) string {
	host := hostport
	if parsed, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsed
	}
	return strings.ToLower(strings.Trim(host, "[]"))
}

func (a *webApp) validWebHost(hostport string) bool {
	host := webHost(hostport)
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return true
	}
	for _, allowed := range a.allowedWebHosts {
		if host == allowed {
			return true
		}
	}
	return false
}

func webStepColor(bundle Bundle, step ColorStep) string {
	for id, color := range colorDefinitions(bundle) {
		if strings.EqualFold(strings.TrimSpace(step.Name), strings.TrimSpace(color.Name)) {
			return id
		}
	}
	for id, color := range colorDefinitions(bundle) {
		if nearXY(step.XY, []float64{color.X, color.Y}) {
			return id
		}
	}
	return ""
}

type webAgendaWeek struct {
	Label string
	Days  []scheduleAgendaDay
}

func (p webPage) AgendaWeeks() []webAgendaWeek {
	var weeks []webAgendaWeek
	daysPerColumn := (len(p.Agenda) + 1) / 2
	startDate := parseAgendaDate(p.AgendaDate)
	for column := 0; column < 2; column++ {
		start := column * daysPerColumn
		if start >= len(p.Agenda) {
			break
		}
		end := start + daysPerColumn
		if end > len(p.Agenda) {
			end = len(p.Agenda)
		}
		weekStart, weekEnd := startDate.AddDate(0, 0, start), startDate.AddDate(0, 0, end-1)
		weekLabel := fmt.Sprintf("%s %d–%d, %d", weekStart.Format("Jan"), weekStart.Day(), weekEnd.Day(), weekEnd.Year())
		if weekStart.Month() != weekEnd.Month() {
			weekLabel = fmt.Sprintf("%s %d–%s %d, %d", weekStart.Format("Jan"), weekStart.Day(), weekEnd.Format("Jan"), weekEnd.Day(), weekEnd.Year())
		}

		weeks = append(weeks, webAgendaWeek{Label: weekLabel, Days: p.Agenda[start:end]})
	}
	return weeks
}

func openWebBrowser(address string) error {
	commands := map[string][]string{
		"darwin":  {"open", address},
		"linux":   {"xdg-open", address},
		"windows": {"rundll32", "url.dll,FileProtocolHandler", address},
	}
	command := commands[runtime.GOOS]
	if len(command) == 0 {
		return fmt.Errorf("opening a browser is unsupported on %s", runtime.GOOS)
	}
	return exec.Command(command[0], command[1:]...).Start()
}

//go:embed web.html
var webHTML string

var webTemplate = template.Must(template.New("web").Funcs(template.FuncMap{
	"brightness": previewBrightness,
	"opacity":    func(brightness int) string { return strconv.FormatFloat(float64(brightness)/255, 'f', 3, 64) },
}).Parse(webHTML))
