package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type webApp struct {
	mu            sync.Mutex
	draftDir      string
	bundle        Bundle
	baseline      Bundle
	persistedHash string
	token         string
	message       string
}

var errWebDraftChanged = errors.New("draft files changed outside the web editor; restart to load them")

type webColor struct {
	ID   string
	Name string
	X    float64
	Y    float64
	Hex  string
}

type webScene struct {
	ID         string
	Name       string
	Color      string
	Entities   string
	Brightness int
}

type webStep struct {
	Name       string
	Color      string
	Brightness int
	Hold       float64
	Transition float64
}

type webSequence struct {
	ID     string
	Name   string
	Repeat bool
	Steps  []webStep
}

type webAssignment struct {
	ID       string
	Name     string
	Sequence string
	Targets  string
	Start    string
	End      string
	On       string
	Off      string
	Finish   string
	Enabled  bool
}

type webPage struct {
	View        string
	Message     string
	Token       string
	Hash        string
	Dirty       bool
	Scenes      []webScene
	Colors      []webColor
	Sequences   []webSequence
	Assignments []webAssignment
	Scene       webScene
	Color       webColor
	Sequence    webSequence
	Assignment  webAssignment
	YAML        string
	Diff        string
}

func runWeb(args []string) error {
	flags := flag.NewFlagSet("web", flag.ContinueOnError)
	draftDir := flags.String("draft", ".", "directory containing native HA YAML drafts")
	baselineDir := flags.String("against", "", "baseline draft directory for diff")
	configPath := flags.String("config", "", "YAML file containing Home Assistant settings")
	port := flags.Int("port", 8080, "local web server port")
	open := flags.Bool("open", true, "open the web interface in a browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("web port must be 1-65535")
	}
	if *configPath != "" {
		if _, err := loadProjectConfig(*configPath); err != nil {
			return err
		}
	}
	app, err := newWebApp(*draftDir, *baselineDir)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return fmt.Errorf("start web server: %w", err)
	}
	address := "http://" + listener.Addr().String()
	fmt.Printf("Holiday Lighting Designer: %s\n", address)
	if *open {
		go func() { _ = openWebBrowser(address) }()
	}
	return http.Serve(listener, app.handler())
}

func newWebApp(draftDir, baselineDir string) (*webApp, error) {
	bundle, err := LoadBundle(draftDir)
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
		baseline, err = LoadBundle(baselineDir)
		if err != nil {
			return nil, err
		}
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("create web session: %w", err)
	}
	return &webApp{draftDir: draftDir, bundle: bundle, baseline: baseline, persistedHash: bundle.Hash, token: hex.EncodeToString(tokenBytes)}, nil
}

func (a *webApp) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.index)
	mux.HandleFunc("POST /scene", a.saveScene)
	mux.HandleFunc("POST /sequence", a.saveSequence)
	mux.HandleFunc("POST /schedule", a.saveSchedule)
	mux.HandleFunc("POST /color", a.saveColor)
	mux.HandleFunc("POST /delete", a.deleteDraftItem)
	mux.HandleFunc("POST /save", a.saveDraft)
	return mux
}

func (a *webApp) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !validWebHost(r.Host) {
		http.Error(w, "invalid host", http.StatusForbidden)
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "scenes"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	page, err := a.page(view, r.URL.Query().Get("edit"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webTemplate.Execute(w, page); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *webApp) page(view, edit string) (webPage, error) {
	page := webPage{View: view, Message: a.message, Token: a.token, Hash: a.bundle.Hash, Dirty: a.bundle.Hash != a.persistedHash}
	a.message = ""
	for _, id := range colorIDs(a.bundle) {
		color := colorDefinitions(a.bundle)[id]
		page.Colors = append(page.Colors, webColor{ID: id, Name: color.Name, X: color.X, Y: color.Y, Hex: cieRGBHex(CIEColor{X: color.X, Y: color.Y})})
	}
	for _, id := range SceneIDs(a.bundle) {
		values := sceneFormValues(a.bundle, id)
		brightness, _ := strconv.Atoi(values[3])
		page.Scenes = append(page.Scenes, webScene{ID: id, Name: values[0], Color: values[1], Entities: values[2], Brightness: brightness})
	}
	for _, id := range lightingIDs(a.bundle, 0) {
		sequence := colorSequences(a.bundle)[id]
		item := webSequence{ID: id, Name: sequence.Name, Repeat: sequence.Repeat}
		for _, step := range sequence.Steps {
			item.Steps = append(item.Steps, webStep{Name: step.Name, Color: webStepColor(a.bundle, step), Brightness: step.Brightness, Hold: step.Hold, Transition: step.Transition})
		}
		page.Sequences = append(page.Sequences, item)
	}
	for _, id := range lightingIDs(a.bundle, 1) {
		assignment := lightingAssignments(a.bundle)[id]
		page.Assignments = append(page.Assignments, webAssignment{ID: id, Name: assignment.Name, Sequence: assignment.Sequence, Targets: strings.Join(assignment.Targets, ";"), Start: assignment.Start, End: assignment.End, On: assignment.On, Off: assignment.Off, Finish: assignment.Finish, Enabled: assignment.Enabled})
	}
	page.Scene = webScene{Brightness: 180}
	page.Color = webColor{X: .3127, Y: .3290, Hex: "#FFFFFF"}
	page.Sequence = webSequence{Repeat: true, Steps: []webStep{{Brightness: 255, Hold: 6, Transition: .5}}}
	page.Assignment = webAssignment{On: "sunset", Off: "00:00", Finish: "off", Enabled: true}
	if edit != "" {
		for _, item := range page.Scenes {
			if item.ID == edit {
				page.Scene = item
			}
		}
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
	page.YAML = formatWebYAML(a.bundle)
	changes, err := PublishDiff(a.baseline, a.bundle)
	if err != nil {
		return webPage{}, err
	}
	page.Diff = FormatChanges(changes)
	return page, nil
}

func (a *webApp) post(w http.ResponseWriter, r *http.Request, view string, mutate func(Bundle, url.Values) (Bundle, error)) {
	if !validWebHost(r.Host) {
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
	next, err := mutate(a.bundle, r.Form)
	if errors.Is(err, errWebDraftChanged) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		a.message = "Could not update draft: " + err.Error()
	} else {
		a.bundle = next
		a.message = "Draft updated in memory. Save when ready."
		if view == "yaml" {
			a.message = "Draft saved to disk."
		}
	}
	http.Redirect(w, r, "/?view="+url.QueryEscape(view), http.StatusSeeOther)
}

func (a *webApp) saveScene(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "scenes", func(bundle Bundle, form url.Values) (Bundle, error) {
		name, colorID := strings.TrimSpace(form.Get("name")), form.Get("color")
		color, ok := colorDefinitions(bundle)[colorID]
		brightness, err := strconv.Atoi(form.Get("brightness"))
		if !ok || err != nil || brightness < 1 || brightness > 255 || name == "" {
			return Bundle{}, fmt.Errorf("choose a name, catalog color, and brightness from 1-255")
		}
		entities := splitWebValues(form.Get("entities"))
		if len(entities) == 0 {
			return Bundle{}, fmt.Errorf("add at least one light entity")
		}
		for _, entity := range entities {
			if !strings.HasPrefix(entity, "light.") || !entityName.MatchString(strings.TrimPrefix(entity, "light.")) {
				return Bundle{}, fmt.Errorf("invalid light %q", entity)
			}
		}
		id := form.Get("id")
		if id == "" {
			id = SuggestSceneID(name, colorID)
		}
		scene := NewXYScene(id, name+" "+color.Name, entities, color.X, color.Y, brightness)
		scene["color_ref"] = colorID
		next, err := UpsertScene(bundle, scene)
		if err != nil {
			return Bundle{}, err
		}
		return UpsertHolidayColor(next, "holiday_lights", name, id)
	})
}

func (a *webApp) saveSequence(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "sequences", func(bundle Bundle, form url.Values) (Bundle, error) {
		sequence := ColorSequence{ID: form.Get("id"), Name: strings.TrimSpace(form.Get("name")), Repeat: form.Get("repeat") == "on"}
		if sequence.ID == "" {
			sequence.ID = colorID(sequence.Name)
			if _, exists := colorSequences(bundle)[sequence.ID]; exists {
				return Bundle{}, fmt.Errorf("a sequence with that name already exists")
			}
		}
		names, colors := form["step_name"], form["step_color"]
		brightnesses, holds, transitions := form["step_brightness"], form["step_hold"], form["step_transition"]
		if len(names) == 0 || len(names) != len(colors) || len(names) != len(brightnesses) || len(names) != len(holds) || len(names) != len(transitions) {
			return Bundle{}, fmt.Errorf("each color step must be complete")
		}
		old := colorSequences(bundle)[sequence.ID]
		for i := range names {
			if strings.TrimSpace(names[i]) == "" && colors[i] == "" && strings.TrimSpace(brightnesses[i]) == "" {
				continue
			}
			color, ok := colorDefinitions(bundle)[colors[i]]
			brightness, bErr := strconv.Atoi(brightnesses[i])
			hold, hErr := strconv.ParseFloat(holds[i], 64)
			transition, tErr := strconv.ParseFloat(transitions[i], 64)
			if !ok || bErr != nil || hErr != nil || tErr != nil {
				return Bundle{}, fmt.Errorf("step %d needs a name, catalog color, and numeric values", i+1)
			}
			rgb, _, _ := sceneDisplayRGB(map[string]any{"xy_color": []any{color.X, color.Y}})
			step := ColorStep{Name: strings.TrimSpace(names[i]), RGB: []int{rgb[0], rgb[1], rgb[2]}, Brightness: brightness, Hold: hold, Transition: transition}
			if i < len(old.Steps) && stringMustJSON(step.RGB) == stringMustJSON(old.Steps[i].RGB) {
				step.Color = old.Steps[i].Color
			}
			sequence.Steps = append(sequence.Steps, step)
		}
		return saveColorSequence(bundle, sequence)
	})
}

func (a *webApp) saveSchedule(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "schedules", func(bundle Bundle, form url.Values) (Bundle, error) {
		assignment := LightingAssignment{
			ID: form.Get("id"), Name: strings.TrimSpace(form.Get("name")), Sequence: form.Get("sequence"),
			Targets: splitWebValues(form.Get("targets")), Start: strings.TrimSpace(form.Get("start")), End: strings.TrimSpace(form.Get("end")),
			On: strings.TrimSpace(form.Get("on")), Off: strings.TrimSpace(form.Get("off")), Finish: strings.TrimSpace(form.Get("finish")), Enabled: form.Get("enabled") == "on",
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

func (a *webApp) saveColor(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "colors", func(bundle Bundle, form url.Values) (Bundle, error) {
		name := strings.TrimSpace(form.Get("name"))
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
	a.post(w, r, "", func(bundle Bundle, form url.Values) (Bundle, error) {
		id := form.Get("id")
		switch form.Get("kind") {
		case "scenes":
			next, _, err := DeleteScene(bundle, id)
			if err != nil {
				return Bundle{}, err
			}
			return RemoveHolidayColor(next, "holiday_lights", id)
		case "sequences":
			return deleteColorSequence(bundle, id)
		case "colors":
			if colorReferenced(bundle, id) {
				return Bundle{}, fmt.Errorf("color %q is still referenced by a scene", id)
			}
			return deleteColor(bundle, id)
		default:
			return Bundle{}, fmt.Errorf("unsupported draft item")
		}
	})
}

func (a *webApp) saveDraft(w http.ResponseWriter, r *http.Request) {
	a.post(w, r, "yaml", func(bundle Bundle, _ url.Values) (Bundle, error) {
		if err := ValidateBundle(bundle); err != nil {
			return Bundle{}, err
		}
		disk, err := LoadBundle(a.draftDir)
		if err != nil {
			return Bundle{}, err
		}
		if disk.Hash != a.persistedHash {
			return Bundle{}, errWebDraftChanged
		}
		if err := SaveBundle(a.draftDir, bundle); err != nil {
			return Bundle{}, err
		}
		a.persistedHash = bundle.Hash
		return bundle, nil
	})
}

func splitWebValues(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' })
}

func validWebHost(hostport string) bool {
	host := hostport
	if parsed, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func webStepColor(bundle Bundle, step ColorStep) string {
	for id, color := range colorDefinitions(bundle) {
		rgb, _, _ := sceneDisplayRGB(map[string]any{"xy_color": []any{color.X, color.Y}})
		if len(step.RGB) == 3 && rgb[0] == step.RGB[0] && rgb[1] == step.RGB[1] && rgb[2] == step.RGB[2] {
			return id
		}
	}
	return ""
}

func formatWebYAML(bundle Bundle) string {
	var result strings.Builder
	for _, kind := range bundle.Kinds() {
		data, _ := yaml.Marshal(bundle.Files[kind].Data)
		fmt.Fprintf(&result, "# %s\n%s\n", filenameForKind(kind), data)
	}
	return result.String()
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

var webTemplate = template.Must(template.New("web").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Holiday Lighting Designer</title>
<style>
:root{color-scheme:dark;--bg:#11131a;--panel:#1c202b;--line:#343b4c;--text:#f5f6fa;--muted:#a8b0c2;--accent:#78d6c6;--danger:#ff9b9b}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:15px/1.45 system-ui,sans-serif}header{padding:24px max(20px,calc((100% - 1100px)/2));border-bottom:1px solid var(--line);display:flex;gap:20px;align-items:center;justify-content:space-between}h1{font-size:22px;margin:0}nav{display:flex;gap:8px;flex-wrap:wrap}nav a,.button{display:inline-block;padding:8px 12px;border:1px solid var(--line);border-radius:8px;color:var(--text);text-decoration:none;background:#252b39}nav a.active{border-color:var(--accent);color:var(--accent)}main{max-width:1100px;margin:0 auto;padding:24px 20px 60px}.notice{background:#183d37;border:1px solid #2b7165;padding:10px 14px;border-radius:8px;margin-bottom:18px}.status{color:var(--muted);font-size:13px}.dirty{color:#ffd479}.grid{display:grid;grid-template-columns:minmax(240px,1fr) minmax(360px,2fr);gap:20px}.panel{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:18px}h2{font-size:18px;margin:0 0 16px}h3{font-size:15px;margin:22px 0 10px}.items{list-style:none;padding:0;margin:0}.items a{display:block;padding:10px;border-radius:7px;color:var(--text);text-decoration:none}.items a:hover{background:#292f3e}.meta{display:block;color:var(--muted);font-size:12px}label{display:block;color:var(--muted);font-size:13px;margin:12px 0 5px}input,textarea,select{width:100%;border:1px solid var(--line);border-radius:7px;background:#11151f;color:var(--text);padding:9px 10px;font:inherit}textarea{min-height:76px;resize:vertical}input[type=checkbox]{width:auto;margin-right:8px}.check{color:var(--text)}button{border:0;border-radius:8px;background:var(--accent);color:#10211e;font-weight:700;padding:9px 14px;cursor:pointer;margin-top:16px}.step{border-top:1px solid var(--line);margin-top:16px;padding-top:4px}.step-grid{display:grid;grid-template-columns:2fr 2fr 1fr 1fr 1fr;gap:8px}pre{overflow:auto;background:#0d1016;border:1px solid var(--line);border-radius:8px;padding:14px;font:12px/1.5 ui-monospace,monospace;max-height:560px}.save{display:flex;align-items:center;gap:12px;margin-bottom:20px}.save button{margin:0}@media(max-width:760px){header{align-items:flex-start;flex-direction:column}.grid{grid-template-columns:1fr}.step-grid{grid-template-columns:1fr 1fr}.step-grid label:first-child{grid-column:1/-1}}
.danger{background:transparent;color:var(--danger);border:1px solid #824747}.step-actions{grid-column:1/-1}.step-actions button{margin:4px 6px 0 0;padding:5px 9px;background:#303747;color:var(--text)}
input[type=color]{height:52px;padding:4px;cursor:pointer}
</style>
</head>
<body>
<header><div><h1>Holiday Lighting Designer</h1><div class="status">Local draft editor {{if .Dirty}}<span class="dirty">• Unsaved changes</span>{{end}}</div></div><nav>
<a href="/?view=scenes" class="{{if eq .View "scenes"}}active{{end}}">Scenes</a>
<a href="/?view=sequences" class="{{if eq .View "sequences"}}active{{end}}">Sequences</a>
<a href="/?view=schedules" class="{{if eq .View "schedules"}}active{{end}}">Schedules</a>
<a href="/?view=colors" class="{{if eq .View "colors"}}active{{end}}">Colors</a>
<a href="/?view=yaml" class="{{if eq .View "yaml"}}active{{end}}">YAML / Diff</a>
</nav></header>
<main>{{if .Message}}<div class="notice" role="status">{{.Message}}</div>{{end}}
{{if eq .View "scenes"}}<div class="grid"><section class="panel"><h2>Scenes</h2><ul class="items">{{range .Scenes}}<li><a href="/?view=scenes&edit={{.ID}}">{{.Name}} <span class="meta">{{.ID}} · {{.Color}}</span></a></li>{{else}}<li class="status">No scenes</li>{{end}}</ul><a class="button" href="/?view=scenes">New scene</a></section><section class="panel"><h2>{{if .Scene.ID}}Edit scene{{else}}New scene{{end}}</h2><form method="post" action="/scene"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="id" value="{{.Scene.ID}}"><input type="hidden" name="kind" value="scenes"><label for="scene-name">Holiday / name prefix</label><input id="scene-name" name="name" required value="{{.Scene.Name}}"><label for="scene-color">Catalog color</label><select id="scene-color" name="color" required><option value="">Choose…</option>{{range .Colors}}<option value="{{.ID}}" {{if eq $.Scene.Color .ID}}selected{{end}}>{{.Name}}</option>{{end}}</select><label for="scene-entities">Light entities</label><textarea id="scene-entities" name="entities" required placeholder="light.front_door; light.porch">{{.Scene.Entities}}</textarea><label for="scene-brightness">Brightness (1–255)</label><input id="scene-brightness" name="brightness" type="number" min="1" max="255" required value="{{.Scene.Brightness}}"><button>Update draft</button>{{if .Scene.ID}} <button class="danger" formaction="/delete" formnovalidate onclick="return confirm('Delete this scene from the draft?')">Delete from draft</button>{{end}}</form></section></div>{{end}}
{{if eq .View "colors"}}<div class="grid"><section class="panel"><h2>Colors</h2><ul class="items">{{range .Colors}}<li><a href="/?view=colors&edit={{.ID}}">{{.Name}} <span class="meta">{{.ID}} · {{printf "%.4f" .X}}, {{printf "%.4f" .Y}}</span></a></li>{{else}}<li class="status">No colors</li>{{end}}</ul><a class="button" href="/?view=colors">New color</a></section><section class="panel"><h2>{{if .Color.ID}}Edit color{{else}}New color{{end}}</h2><form method="post" action="/color"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="id" value="{{.Color.ID}}"><input type="hidden" name="kind" value="colors"><label for="color-name">Name</label><input id="color-name" name="name" required value="{{.Color.Name}}"><label for="color-picker">Color picker</label><input id="color-picker" type="color" value="{{.Color.Hex}}" oninput="setColorXY(this)"><label for="color-x">CIE x</label><input id="color-x" name="x" type="number" step="any" required value="{{.Color.X}}"><label for="color-y">CIE y</label><input id="color-y" name="y" type="number" step="any" required value="{{.Color.Y}}"><button>Update draft</button>{{if .Color.ID}} <button class="danger" formaction="/delete" formnovalidate onclick="return confirm('Delete this color from the draft?')">Delete from draft</button>{{end}}</form></section></div><script>function setColorXY(picker){const hex=picker.value,rgb=[1,3,5].map(i=>parseInt(hex.slice(i,i+2),16)/255).map(v=>v<=.04045?v/12.92:Math.pow((v+.055)/1.055,2.4)),x=.4124*rgb[0]+.3576*rgb[1]+.1805*rgb[2],y=.2126*rgb[0]+.7152*rgb[1]+.0722*rgb[2],z=.0193*rgb[0]+.1192*rgb[1]+.9505*rgb[2],sum=x+y+z;if(!sum){picker.setCustomValidity('Choose a non-black color.');return}picker.setCustomValidity('');document.querySelector('#color-x').value=(x/sum).toFixed(6);document.querySelector('#color-y').value=(y/sum).toFixed(6)}</script>{{end}}
{{if eq .View "sequences"}}<div class="grid"><section class="panel"><h2>Sequences</h2><ul class="items">{{range .Sequences}}<li><a href="/?view=sequences&edit={{.ID}}">{{.Name}} <span class="meta">{{.ID}} · {{len .Steps}} steps</span></a></li>{{else}}<li class="status">No sequences</li>{{end}}</ul><a class="button" href="/?view=sequences">New sequence</a></section><section class="panel"><h2>{{if .Sequence.ID}}Edit sequence{{else}}New sequence{{end}}</h2><form method="post" action="/sequence"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="id" value="{{.Sequence.ID}}"><input type="hidden" name="kind" value="sequences"><label for="sequence-name">Name</label><input id="sequence-name" name="name" required value="{{.Sequence.Name}}"><label class="check"><input name="repeat" type="checkbox" {{if .Sequence.Repeat}}checked{{end}}>Loop sequence</label><h3>Color steps</h3><div id="steps">{{range .Sequence.Steps}}{{$step := .}}<div class="step step-grid"><label>Name<input name="step_name" required value="{{.Name}}"></label><label>Catalog color<select name="step_color" required><option value="">Choose…</option>{{range $.Colors}}<option value="{{.ID}}" {{if eq $step.Color .ID}}selected{{end}}>{{.Name}}</option>{{end}}</select></label><label>Brightness<input name="step_brightness" type="number" min="1" max="255" required value="{{.Brightness}}"></label><label>Seconds<input name="step_hold" type="number" min="1" step="any" required value="{{.Hold}}"></label><label>Transition<input name="step_transition" type="number" min="0" step="any" required value="{{.Transition}}"></label><div class="step-actions"><button type="button" onclick="moveStep(this,-1)">↑</button><button type="button" onclick="moveStep(this,1)">↓</button><button type="button" onclick="removeStep(this)">Remove</button></div></div>{{end}}</div><button type="button" class="button" onclick="addStep()">Add color step</button> <button>Update draft</button>{{if .Sequence.ID}} <button class="danger" formaction="/delete" formnovalidate onclick="return confirm('Delete this sequence from the draft?')">Delete from draft</button>{{end}}</form></section></div><template id="step-template"><div class="step step-grid"><label>Name<input name="step_name" required></label><label>Catalog color<select name="step_color" required><option value="">Choose…</option>{{range .Colors}}<option value="{{.ID}}">{{.Name}}</option>{{end}}</select></label><label>Brightness<input name="step_brightness" type="number" min="1" max="255" required value="255"></label><label>Seconds<input name="step_hold" type="number" min="1" step="any" required value="6"></label><label>Transition<input name="step_transition" type="number" min="0" step="any" required value="0.5"></label><div class="step-actions"><button type="button" onclick="moveStep(this,-1)">↑</button><button type="button" onclick="moveStep(this,1)">↓</button><button type="button" onclick="removeStep(this)">Remove</button></div></div></template><script>function addStep(){document.querySelector('#steps').append(document.querySelector('#step-template').content.cloneNode(true))}function removeStep(button){button.closest('.step').remove()}function moveStep(button,direction){const step=button.closest('.step'),other=direction<0?step.previousElementSibling:step.nextElementSibling;if(other){step.parentNode.insertBefore(direction<0?step:other,direction<0?other:step)}}</script>{{end}}
{{if eq .View "schedules"}}<div class="grid"><section class="panel"><h2>Schedules</h2><ul class="items">{{range .Assignments}}<li><a href="/?view=schedules&edit={{.ID}}">{{.Name}} <span class="meta">{{.Start}} – {{.End}} · {{if .Enabled}}enabled{{else}}disabled{{end}}</span></a></li>{{else}}<li class="status">No schedules</li>{{end}}</ul><a class="button" href="/?view=schedules">New schedule</a></section><section class="panel"><h2>{{if .Assignment.ID}}Edit schedule{{else}}New schedule{{end}}</h2><form method="post" action="/schedule"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="id" value="{{.Assignment.ID}}"><label for="schedule-name">Name</label><input id="schedule-name" name="name" required value="{{.Assignment.Name}}"><label for="schedule-sequence">Sequence</label><select id="schedule-sequence" name="sequence" required><option value="">Choose…</option>{{range .Sequences}}<option value="{{.ID}}" {{if eq $.Assignment.Sequence .ID}}selected{{end}}>{{.Name}}</option>{{end}}</select><label for="schedule-targets">Light targets</label><textarea id="schedule-targets" name="targets" required>{{.Assignment.Targets}}</textarea><div class="step-grid"><label>Start (MM-DD or YYYY-MM-DD)<input name="start" required value="{{.Assignment.Start}}"></label><label>End<input name="end" required value="{{.Assignment.End}}"></label><label>Start time or sunset<input name="on" required value="{{.Assignment.On}}"></label><label>Stop time<input name="off" required value="{{.Assignment.Off}}"></label><label>At stop<input name="finish" required value="{{.Assignment.Finish}}"></label></div><label class="check"><input name="enabled" type="checkbox" {{if .Assignment.Enabled}}checked{{end}}>Enabled</label><button>Update draft</button></form></section></div>{{end}}
{{if eq .View "yaml"}}<form class="save" method="post" action="/save"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="hash" value="{{.Hash}}"><button>Save draft files</button><span class="status">Writes the current in-memory draft to disk.</span></form><div class="grid"><section class="panel"><h2>Draft YAML</h2><pre>{{.YAML}}</pre></section><section class="panel"><h2>Diff</h2><pre>{{.Diff}}</pre></section></div>{{end}}
</main></body></html>`))
