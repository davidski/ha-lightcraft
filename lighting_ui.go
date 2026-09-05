package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type lightingEditor struct {
	pane       int // sequences or assignments
	selected   int
	steps      bool
	step       int
	form       string
	fields     []textinput.Model
	labels     []string
	focus      int
	sequence   ColorSequence
	assignment LightingAssignment
	error      string
	picker     bool
	pick       int
}

type lightingControlMsg struct {
	id, option string
	err        error
}

func controlLightingCmd(api StateAPI, id, option string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		entity := "input_select." + assignmentPrefix + id
		states, err := api.States(ctx, []string{entity})
		if err == nil {
			if state, ok := states[entity]; !ok || state.State == "unknown" || state.State == "unavailable" {
				err = fmt.Errorf("schedule is not available in HA; publish it first")
			}
		}
		if err == nil {
			err = api.CallService(ctx, "input_select", "select_option", entity, map[string]any{"option": option})
		}
		return lightingControlMsg{id: id, option: option, err: err}
	}
}

func lightingIDs(bundle Bundle, pane int) []string {
	ids := []string{}
	if pane == 0 {
		for id := range colorSequences(bundle) {
			ids = append(ids, id)
		}
	} else {
		for id := range lightingAssignments(bundle) {
			ids = append(ids, id)
		}
	}
	return sortedStrings(ids)
}

func (m *statusModel) openLighting(pane int) {
	m.planner = true
	m.lighting = lightingEditor{pane: pane}
	m.message = ""
}

func (e *lightingEditor) openForm(kind string, labels, values []string) {
	e.form, e.labels, e.focus, e.error, e.picker = kind, labels, 0, "", false
	e.fields = make([]textinput.Model, len(values))
	for i, value := range values {
		input := textinput.New()
		input.SetValue(value)
		input.CharLimit = 2048
		input.Prompt = ""
		e.fields[i] = input
	}
	e.fields[0].Focus()
}

func (m *statusModel) editLightingSequence(id string) {
	s, exists := colorSequences(m.bundle)[id]
	if !exists {
		s = ColorSequence{Name: "New sequence", Repeat: true, Steps: []ColorStep{{Name: "White", RGB: []int{255, 255, 255}, Brightness: 180, Hold: 6, Transition: .5}}}
	}
	// Forms own a copy until saved or cancelled.
	_ = decodeLighting(jsonValue(s), &m.lighting.sequence)
	m.lighting.sequence.ID, m.lighting.sequence.Name = s.ID, s.Name
	m.lighting.openForm("sequence", []string{"Name", "Playback"}, []string{s.Name, map[bool]string{true: "loop", false: "once"}[s.Repeat]})
}

func (m *statusModel) editLightingAssignment(id string) {
	a, exists := lightingAssignments(m.bundle)[id]
	if !exists {
		ids := lightingIDs(m.bundle, 0)
		if len(ids) == 0 {
			m.lighting.error = "No color sequences exist. Switch to Sequences, create one, then return here."
			return
		}
		a = LightingAssignment{Name: "New assignment", Sequence: ids[0], Start: "12-01", End: "12-31", On: "sunset", Off: "00:00", Finish: "off", Enabled: true}
	}
	m.lighting.assignment = a
	m.lighting.openForm("assignment", []string{"Name", "Sequence (Enter to choose)", "Lights (Enter to choose; or type light IDs separated by ;)", "First date (MM-DD or YYYY-MM-DD)", "Last date (inclusive; same format)", "Daily start (HH:MM or sunset)", "Daily stop (HH:MM)", "At stop (off / leave / scene.entity_id)", "Schedule (enabled / disabled)"}, []string{a.Name, a.Sequence, strings.Join(a.Targets, ";"), a.Start, a.End, a.On, a.Off, a.Finish, map[bool]string{true: "enabled", false: "disabled"}[a.Enabled]})
}

func (m *statusModel) editLightingStep(index int) {
	e := &m.lighting
	e.step = index
	s := ColorStep{Name: "New color", RGB: []int{255, 255, 255}, Brightness: 180, Hold: 6, Transition: .5}
	if index < len(e.sequence.Steps) {
		s = e.sequence.Steps[index]
	}
	colorID := ""
	for id, color := range colorDefinitions(m.bundle) {
		rgb, _, _ := sceneDisplayRGB(map[string]any{"xy_color": []any{color.X, color.Y}})
		if rgb[0] == s.RGB[0] && rgb[1] == s.RGB[1] && rgb[2] == s.RGB[2] {
			colorID = id
			break
		}
	}
	e.openForm("step", []string{"Name", "Color (catalog)", "Brightness", "Duration (seconds)", "Transition (seconds)"}, []string{s.Name, colorID, strconv.Itoa(s.Brightness), strconv.FormatFloat(s.Hold, 'f', -1, 64), strconv.FormatFloat(s.Transition, 'f', -1, 64)})
}

func (m *statusModel) saveLightingForm() {
	e := &m.lighting
	values := make([]string, len(e.fields))
	for i, field := range e.fields {
		values[i] = strings.TrimSpace(field.Value())
	}
	var next Bundle
	var err error
	switch e.form {
	case "sequence":
		s := e.sequence
		s.Name = values[0]
		if values[1] != "loop" && values[1] != "once" {
			e.error = "Playback must be loop or once (holds the final color until stopped)."
			return
		}
		s.Repeat = values[1] == "loop"
		if s.ID == "" {
			s.ID = colorID(s.Name)
			if _, exists := colorSequences(m.bundle)[s.ID]; exists {
				e.error = "A sequence with that name already exists."
				return
			}
		}
		next, err = saveColorSequence(m.bundle, s)
		if err == nil {
			e.sequence, e.steps, e.step = s, true, 0
		}
	case "step":
		catalogColor, colorOK := colorDefinitions(m.bundle)[values[1]]
		brightness, bErr := strconv.Atoi(values[2])
		hold, hErr := strconv.ParseFloat(values[3], 64)
		transition, tErr := strconv.ParseFloat(values[4], 64)
		if !colorOK || bErr != nil || hErr != nil || tErr != nil {
			e.error = "Choose a catalog color and enter numeric brightness, duration, and transition."
			return
		}
		rgb, _, _ := sceneDisplayRGB(map[string]any{"xy_color": []any{catalogColor.X, catalogColor.Y}})
		s := e.sequence
		s.Steps = append([]ColorStep(nil), s.Steps...)
		step := ColorStep{Name: values[0], RGB: []int{rgb[0], rgb[1], rgb[2]}, Brightness: brightness, Hold: hold, Transition: transition}
		if e.step < len(s.Steps) && stringMustJSON(step.RGB) == stringMustJSON(s.Steps[e.step].RGB) {
			step.Color = s.Steps[e.step].Color
		}
		if e.step == len(s.Steps) {
			s.Steps = append(s.Steps, step)
		} else {
			s.Steps[e.step] = step
		}
		next, err = saveColorSequence(m.bundle, s)
		if err == nil {
			e.sequence = s
		}
	case "assignment":
		a := e.assignment
		a.Name, a.Sequence, a.Start, a.End, a.On, a.Off, a.Finish = values[0], values[1], values[3], values[4], values[5], values[6], values[7]
		a.Targets = strings.FieldsFunc(values[2], func(r rune) bool { return r == ';' || r == ',' || r == ' ' })
		if len(m.inventoryStates) > 0 {
			a.Targets, err = resolveLightingTargets(a.Targets, m.inventoryStates)
			if err != nil {
				e.error = err.Error()
				return
			}
		}
		if values[8] != "enabled" && values[8] != "disabled" {
			e.error = "Schedule must be enabled or disabled."
			return
		}
		a.Enabled = values[8] == "enabled"
		if a.ID == "" {
			a.ID = colorID(a.Name)
			if _, exists := lightingAssignments(m.bundle)[a.ID]; exists {
				e.error = "An assignment with that name already exists."
				return
			}
		}
		next, err = saveLightingAssignment(m.bundle, a)
		if err == nil {
			e.assignment = a
		}
	}
	if err != nil {
		e.error = err.Error()
		return
	}
	m.bundle, m.dirty, m.message = next, true, "Saved to draft. Review the diff and publish to apply in Home Assistant."
	e.form, e.error = "", ""
}

func (m statusModel) updateLighting(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &m.lighting
	if m.operationLoading {
		return m, nil
	}
	if e.form != "" {
		if e.picker {
			return m.updateLightingPicker(key)
		}
		if e.form == "step" && e.focus == 1 && key.String() == "enter" {
			e.picker, e.pick = true, 0
			return m, nil
		}
		if e.form == "sequence" && e.focus == 1 && key.String() == "ctrl+u" {
			return m, nil
		}
		switch key.String() {
		case "left", "right":
			options := e.fieldOptions()
			if len(options) > 0 {
				index := 0
				for i, value := range options {
					if value == e.fields[e.focus].Value() {
						index = i
					}
				}
				delta := 1
				if key.String() == "left" {
					delta = -1
				}
				e.fields[e.focus].SetValue(options[(index+delta+len(options))%len(options)])
				return m, nil
			}
		case "esc":
			e.form, e.error = "", ""
			e.step = min(e.step, max(0, len(e.sequence.Steps)-1))
			return m, nil
		case "ctrl+s":
			if e.form == "pause" || e.form == "resume" {
				return m.submitLightingControl()
			}
			m.saveLightingForm()
			return m, nil
		case "enter":
			if e.form == "pause" || e.form == "resume" {
				return m.submitLightingControl()
			}
			if e.form == "assignment" && (e.focus == 1 || e.focus == 2) {
				e.picker, e.pick = true, 0
				if e.focus == 2 && m.stateAPI != nil && len(m.inventoryStates) == 0 {
					m.inventoryRequest++
					return m, loadInventoryCmd(m.stateAPI, m.inventoryRequest)
				}
				return m, nil
			}
			if e.focus == len(e.fields)-1 {
				m.saveLightingForm()
				return m, nil
			}
			fallthrough
		case "tab", "down":
			e.fields[e.focus].Blur()
			e.focus = (e.focus + 1) % len(e.fields)
			return m, e.fields[e.focus].Focus()
		case "shift+tab", "up":
			e.fields[e.focus].Blur()
			e.focus = (e.focus + len(e.fields) - 1) % len(e.fields)
			return m, e.fields[e.focus].Focus()
		}
		var cmd tea.Cmd
		e.fields[e.focus], cmd = e.fields[e.focus].Update(key)
		return m, cmd
	}
	if e.steps {
		switch key.String() {
		case "esc":
			e.steps = false
		case "up", "k":
			if e.step > 0 {
				e.step--
			}
		case "down", "j":
			if e.step+1 < len(e.sequence.Steps) {
				e.step++
			}
		case "a", "n":
			m.editLightingStep(len(e.sequence.Steps))
		case "enter", "e":
			m.editLightingStep(e.step)
		case "r":
			m.editLightingSequence(e.sequence.ID)
		case "[", "]", "x":
			s := e.sequence
			s.Steps = append([]ColorStep(nil), s.Steps...)
			if key.String() == "x" {
				if len(s.Steps) == 1 {
					e.error = "Keep at least one step; edit it to change the color."
					return m, nil
				}
				s.Steps = append(s.Steps[:e.step], s.Steps[e.step+1:]...)
				if e.step >= len(s.Steps) {
					e.step--
				}
			} else {
				other := e.step - 1
				if key.String() == "]" {
					other = e.step + 1
				}
				if other < 0 || other >= len(s.Steps) {
					return m, nil
				}
				s.Steps[other], s.Steps[e.step] = s.Steps[e.step], s.Steps[other]
				e.step = other
			}
			if next, err := saveColorSequence(m.bundle, s); err != nil {
				e.error = err.Error()
			} else {
				m.bundle, m.dirty, e.sequence, e.error = next, true, s, ""
			}
		case "t":
			sequenceID := e.sequence.ID
			e.steps, e.pane = false, 1
			m.editLightingAssignment("")
			if e.form == "assignment" {
				e.fields[1].SetValue(sequenceID)
			}
		case "s":
			return m.saveLightingDraft()
		}
		return m, nil
	}
	ids := lightingIDs(m.bundle, e.pane)
	if e.selected >= len(ids) {
		e.selected = max(0, len(ids)-1)
	}
	id := ""
	if len(ids) > 0 {
		id = ids[e.selected]
	}
	switch key.String() {
	case "esc":
		m.planner = false
	case "tab", "left", "right":
		e.pane, e.selected, e.error = 1-e.pane, 0, ""
	case "up", "k":
		if e.selected > 0 {
			e.selected--
		}
	case "down", "j":
		if e.selected+1 < len(ids) {
			e.selected++
		}
	case "n":
		if e.pane == 0 {
			m.editLightingSequence("")
		} else {
			m.editLightingAssignment("")
		}
	case "enter", "e":
		if id != "" {
			if e.pane == 0 {
				e.sequence, e.steps, e.step = colorSequences(m.bundle)[id], true, 0
			} else {
				m.editLightingAssignment(id)
			}
		}
	case "x", "backspace":
		if e.pane == 0 && id != "" {
			if next, err := deleteColorSequence(m.bundle, id); err != nil {
				e.error = err.Error()
			} else {
				m.bundle, m.dirty, e.error = next, true, ""
				if e.selected >= len(lightingIDs(m.bundle, e.pane)) && e.selected > 0 {
					e.selected--
				}
			}
		}
	case "p", "r":
		if e.pane == 1 && id != "" {
			if m.stateAPI == nil {
				e.error = "Connect to Home Assistant to pause or resume playback."
				return m, nil
			}
			a := lightingAssignments(m.bundle)[id]
			if m.baseline == nil || stringMustJSON(lightingAssignments(*m.baseline)[id]) != stringMustJSON(a) {
				e.error = "Publish this schedule before controlling its live playback."
				return m, nil
			}
			e.assignment = a
			action := "pause"
			if key.String() == "r" {
				action = "resume"
			}
			e.openForm(action, []string{"Type " + strings.ToUpper(action) + " to " + action + " " + a.Name + " in Home Assistant"}, []string{""})
		}
	case "s":
		return m.saveLightingDraft()
	case "l":
		m.planner, m.sequenceView = false, true
	}
	return m, nil
}

func (e lightingEditor) fieldOptions() []string {
	if e.form == "sequence" && e.focus == 1 {
		return []string{"loop", "once"}
	}
	if e.form == "assignment" && e.focus == 8 {
		return []string{"enabled", "disabled"}
	}
	if e.form == "assignment" && e.focus == 7 {
		return []string{"off", "leave"}
	}
	return nil
}

func (m statusModel) submitLightingControl() (tea.Model, tea.Cmd) {
	e := &m.lighting
	if e.fields[0].Value() != strings.ToUpper(e.form) {
		e.error = "Type " + strings.ToUpper(e.form) + " to confirm, or ESC to cancel."
		return m, nil
	}
	option := "paused"
	if e.form == "resume" {
		option = "idle"
	}
	m.operationLoading = true
	return m, controlLightingCmd(m.stateAPI, e.assignment.ID, option)
}

func (m statusModel) saveLightingDraft() (tea.Model, tea.Cmd) {
	if err := SaveBundle(m.draftDir, m.bundle); err != nil {
		m.lighting.error = err.Error()
	} else {
		m.dirty = false
		m.message = "Draft saved locally. ESC → d to review; u to publish."
	}
	return m, nil
}

func (m statusModel) lightingPickerIDs() []string {
	if m.lighting.focus == 1 {
		return lightingIDs(m.bundle, 0)
	}
	seen := map[string]bool{}
	for _, id := range draftLightIDs(m.bundle) {
		seen[id] = true
	}
	for _, a := range lightingAssignments(m.bundle) {
		for _, id := range a.Targets {
			seen[id] = true
		}
	}
	for id := range m.inventoryStates {
		if strings.HasPrefix(id, "light.") {
			seen[id] = true
		}
	}
	for _, id := range strings.Split(m.lighting.fields[2].Value(), ";") {
		if id != "" {
			seen[id] = true
		}
	}
	ids := []string{}
	for id := range seen {
		ids = append(ids, id)
	}
	return sortedStrings(ids)
}

func (m statusModel) updateLightingPicker(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &m.lighting
	ids := m.lightingPickerIDs()
	if e.form == "step" {
		ids = colorIDs(m.bundle)
	}
	switch key.String() {
	case "esc":
		e.picker = false
	case "up", "k":
		if e.pick > 0 {
			e.pick--
		}
	case "down", "j":
		if e.pick+1 < len(ids) {
			e.pick++
		}
	case "enter":
		if e.form == "step" && len(ids) > 0 {
			color := colorDefinitions(m.bundle)[ids[e.pick]]
			rgb, _, _ := sceneDisplayRGB(map[string]any{"xy_color": []any{color.X, color.Y}})
			e.fields[e.focus].SetValue(fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2]))
		} else if e.focus == 1 && len(ids) > 0 {
			e.fields[e.focus].SetValue(ids[e.pick])
		}
		e.picker = false
	case " ", "space":
		if e.focus == 2 && len(ids) > 0 {
			chosen := strings.FieldsFunc(e.fields[2].Value(), func(r rune) bool { return r == ';' })
			id := ids[e.pick]
			next := []string{}
			found := false
			for _, value := range chosen {
				if value == id {
					found = true
				} else {
					next = append(next, value)
				}
			}
			if !found {
				next = append(next, id)
			}
			e.fields[2].SetValue(strings.Join(next, ";"))
		}
	}
	return m, nil
}

func listWindow(length, selected, height int) (int, int) {
	height = max(1, height)
	start := max(0, selected-height+1)
	return start, min(length, start+height)
}

func (m statusModel) renderLighting() string {
	e := m.lighting
	width, height := m.width, m.height
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}
	var lines []string
	title := "Lighting · Sequences"
	if e.pane == 1 {
		title = "Schedules"
	}
	footer := "Tab sequences/schedules  ↑/↓ select  Enter edit  n new  s save  ESC back"
	if e.pane == 1 {
		footer += "\np pause live  r resume live"
	}
	if e.form != "" {
		title = "Edit " + e.form
		if e.form == "assignment" {
			title = "Edit schedule"
		}
		footer = "Tab/↑/↓ fields  ←/→ choices  Ctrl+U clear  Ctrl+S save  ESC cancel"
		if e.form == "sequence" {
			footer = "Tab/↑/↓ fields  ←/→ choose  Ctrl+S save  ESC cancel"
		}
		if e.form == "step" {
			footer = "Tab/↑/↓ fields  Enter choose catalog color  Ctrl+U clear  Ctrl+S save  ESC cancel"
		}
		if e.form == "pause" || e.form == "resume" {
			title = strings.Title(e.form) + " live playback"
			footer = "Enter confirm  ESC cancel"
		}
		if e.picker {
			title = "Choose sequence"
			if e.focus == 2 {
				title = "Choose lights · Space toggles; Enter accepts"
			}
			if e.form == "step" {
				title = "Choose named color"
			}
			ids := m.lightingPickerIDs()
			if e.form == "step" {
				ids = colorIDs(m.bundle)
			}
			start, end := listWindow(len(ids), e.pick, height-9)
			if len(ids) == 0 {
				lines = append(lines, "No lights discovered. ESC to enter light entity IDs manually.")
			}
			for i := start; i < end; i++ {
				label := ids[i]
				if e.form == "step" {
					label = colorDefinitions(m.bundle)[ids[i]].Name + "  (" + ids[i] + ")"
				} else if e.focus == 1 {
					label = colorSequences(m.bundle)[ids[i]].Name + "  (" + ids[i] + ")"
				} else {
					mark := "[ ] "
					if containsString(strings.Split(e.fields[2].Value(), ";"), ids[i]) {
						mark = "[x] "
					}
					label = mark + ids[i]
					if name, ok := m.inventoryStates[ids[i]].Attribute["friendly_name"].(string); ok {
						label += " · " + name
					}
				}
				if i == e.pick {
					label = "❯ " + label
				} else {
					label = "  " + label
				}
				lines = append(lines, label)
			}
			footer = "↑/↓ select  Space toggle light  Enter accept  ESC back"
		} else {
			if e.form == "step" {
				lines = append(lines, sectionStyle.Render("SEQUENCE")+"  "+e.sequence.Name, "")
				for i, step := range e.sequence.Steps {
					prefix := "  "
					if i == e.step {
						prefix = "❯ "
					}
					lines = append(lines, fmt.Sprintf("%s%d. %s  #%02X%02X%02X", prefix, i+1, step.Name, step.RGB[0], step.RGB[1], step.RGB[2]))
				}
				if e.step == len(e.sequence.Steps) {
					lines = append(lines, "❯ "+fmt.Sprintf("%d. New step", e.step+1))
				}
				lines = append(lines, "")
			}
			start, end := listWindow(len(e.fields), e.focus, max(1, (height-10)/2))
			if e.form == "sequence" || e.form == "step" {
				lines = append(lines, "Set the sequence name and playback mode.", "")
				if e.form == "step" {
					lines[len(lines)-2] = "Set the color, brightness, and timing for this step."
				}
			}
			for i := start; i < end; i++ {
				field := e.fields[i]
				field.Width = max(12, width-6)
				label := e.labels[i]
				prefix := "  "
				if i == e.focus {
					prefix = "❯ "
				}
				if e.form == "sequence" || e.form == "step" {
					var line string
					if e.form == "sequence" && i == 1 {
						value := field.Value()
						loop, once := "[ ] loop", "[ ] once"
						if value == "loop" {
							loop = "[x] loop"
						} else {
							once = "[x] once"
						}
						line = prefix + label + ": " + loop + "  " + once
					} else {
						line = prefix + label + ": " + field.View()
					}
					if i == e.focus {
						line = selectedStyle.Render(line)
					}
					lines = append(lines, line)
				} else {
					lines = append(lines, prefix+label, "  "+field.View())
				}
			}
			lines = append(lines, fmt.Sprintf("Field %d/%d · Enter advances; final Enter saves", e.focus+1, len(e.fields)))
			if e.form == "assignment" {
				lines = append(lines, "Dates include the last day. Overnight hours use the current date.")
			}
			if e.form == "pause" {
				lines = append(lines, "Stops this player and applies its stop behavior: "+e.assignment.Finish)
			}
			if e.form == "resume" {
				lines = append(lines, "Resumes only when its dates and daily hours permit playback.")
			}
		}
	} else if e.steps {
		title = "Edit sequence"
		loop, once := "[ ] loop", "[ ] once"
		if e.sequence.Repeat {
			loop = "[x] loop"
		} else {
			once = "[x] once"
		}
		lines = append(lines, "Name: "+e.sequence.Name, "Playback: "+loop+"  "+once, "", sectionStyle.Render("COLOR STEPS"))
		start, end := listWindow(len(e.sequence.Steps), e.step, height-10)
		for i := start; i < end; i++ {
			s := e.sequence.Steps[i]
			hex := fmt.Sprintf("#%02X%02X%02X", s.RGB[0], s.RGB[1], s.RGB[2])
			swatch := lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render("  ")
			line := fmt.Sprintf("%d. %s %s %s · %d/255 · %gs / fade %gs", i+1, swatch, s.Name, hex, s.Brightness, s.Hold, s.Transition)
			if i == e.step {
				line = "❯ " + line
			}
			lines = append(lines, line)
		}
		footer = "↑/↓ step  Enter edit  a add  x remove  [/] move  r rename/mode\nt assign to lights  s save draft  ESC back"
	} else {
		ids := lightingIDs(m.bundle, e.pane)
		start, end := listWindow(len(ids), e.selected, height-12)
		if len(ids) == 0 {
			if e.pane == 1 {
				lines = append(lines, "No schedules defined. Create a sequence first, then add a schedule here.")
			} else {
				lines = append(lines, "No color sequences defined. Press n to create one.")
			}
		}
		for i := start; i < end; i++ {
			var line string
			if e.pane == 0 {
				s := colorSequences(m.bundle)[ids[i]]
				line = fmt.Sprintf("%s · %d steps", s.Name, len(s.Steps))
			} else {
				a := lightingAssignments(m.bundle)[ids[i]]
				state := "enabled"
				if !a.Enabled {
					state = "disabled"
				}
				line = fmt.Sprintf("%s · %s · %s → %s", a.Name, state, a.Start, a.End)
			}
			if i == e.selected {
				line = "❯ " + line
			} else {
				line = "  " + line
			}
			lines = append(lines, line)
		}
		if e.pane == 1 && e.selected < len(ids) {
			a := lightingAssignments(m.bundle)[ids[e.selected]]
			lines = append(lines, "", a.On+" → "+a.Off+" · stop: "+a.Finish, "Lights: "+strings.Join(a.Targets, ", "), "Pause stops playback; resume follows the date and time rules.")
		} else if e.pane == 0 {
			lines = append(lines, "", "Define colors here; Tab opens Schedules. l opens legacy sequences.")
		}
	}
	if e.error != "" {
		lines = append(lines, "Error: "+e.error)
	} else if m.message != "" && e.form == "" {
		lines = append(lines, m.message)
	}
	if m.operationLoading {
		lines = append(lines, "Updating playback in Home Assistant…")
	}
	// Truncate long entity lists without letting terminal wrapping hide controls.
	for i, line := range lines {
		lines[i] = lipgloss.NewStyle().MaxWidth(width).Render(line)
	}
	return titleStyle.Render(title) + "\n\n" + strings.Join(lines, "\n") + "\n\n" + footerStyle.Render(footer) + "\n"
}
