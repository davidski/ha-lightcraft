package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type lightingEditor struct {
	pane            int // sequences or assignments
	selected        int
	steps           bool
	step            int
	form            string
	fields          []textinput.Model
	labels          []string
	focus           int
	sequence        ColorSequence
	assignment      LightingAssignment
	error           string
	picker          bool
	pick            int
	returnDashboard bool
}

type lightingControlMsg struct {
	id, option string
	err        error
}

const (
	assignmentTypeColor = "Color sequence"
	assignmentTypeWLED  = "WLED preset/playlist"
)

const (
	assignmentTypeField = iota
	assignmentNameField
	assignmentSequenceField
	assignmentLightsField
	assignmentFirstDateField
	assignmentLastDateField
	assignmentAllDayField
	assignmentDailyStartField
	assignmentDailyStopField
	assignmentFinishField
	assignmentEnabledField
)

const (
	wledLightField      = assignmentSequenceField
	wledSelectField     = assignmentLightsField
	wledOptionField     = assignmentFirstDateField
	wledFirstDateField  = 5
	wledLastDateField   = 6
	wledAllDayField     = 7
	wledDailyStartField = 8
	wledDailyStopField  = 9
	wledFinishField     = 10
	wledEnabledField    = 11
)

func controlLightingCmd(api StateAPI, id, option string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := controlLighting(ctx, api, id, option)
		return lightingControlMsg{id: id, option: option, err: err}
	}
}

func controlLighting(ctx context.Context, api StateAPI, id, option string) error {
	if api == nil {
		return fmt.Errorf("connect to Home Assistant to control playback")
	}
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
	return err
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
		s = ColorSequence{Name: "New sequence", Repeat: true, Steps: []ColorStep{{Name: "White", XY: []float64{.3127, .3290}, Brightness: 180, Hold: 6, Transition: .5}}}
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
		a = LightingAssignment{Name: "New assignment", Start: "12-01", End: "12-31", On: "sunset", Off: "00:00", Finish: "off", Enabled: true}
		if len(ids) > 0 {
			a.Sequence = ids[0]
		} else {
			a.WLED = &WLEDProgram{}
		}
	}
	m.lighting.assignment = a
	m.openLightingAssignmentForm(a)
}

func (m *statusModel) openLightingAssignmentForm(a LightingAssignment) {
	dailyStart, dailyStop := a.On, a.Off
	if dailyStart == "" {
		dailyStart = "sunset"
	}
	if dailyStop == "" {
		dailyStop = "00:00"
	}
	if a.WLED != nil {
		m.lighting.openForm("assignment", []string{"Program type", "Name", "WLED light", "Preset/playlist", "Program option", "First date", "Last date", "All day", "Daily start", "Daily stop", "When schedule stops", "Schedule"}, []string{assignmentTypeWLED, a.Name, a.WLED.Light, a.WLED.Select, a.WLED.Option, a.Start, a.End, map[bool]string{true: "enabled", false: "disabled"}[a.AllDay], dailyStart, dailyStop, a.Finish, map[bool]string{true: "enabled", false: "disabled"}[a.Enabled]})
		return
	}
	m.lighting.openForm("assignment", []string{"Program type", "Name", "Sequence", "Lights", "First date", "Last date", "All day", "Daily start", "Daily stop", "When schedule stops", "Schedule"}, []string{assignmentTypeColor, a.Name, a.Sequence, strings.Join(a.Targets, ";"), a.Start, a.End, map[bool]string{true: "enabled", false: "disabled"}[a.AllDay], dailyStart, dailyStop, a.Finish, map[bool]string{true: "enabled", false: "disabled"}[a.Enabled]})
}

func (m *statusModel) switchLightingAssignmentKind(wled bool, sequence string) {
	e := &m.lighting
	a := e.assignment
	if len(e.fields) == 12 {
		a.Name, a.Start, a.End = e.fields[assignmentNameField].Value(), e.fields[wledFirstDateField].Value(), e.fields[wledLastDateField].Value()
		a.AllDay, a.On, a.Off = e.fields[wledAllDayField].Value() == "enabled", e.fields[wledDailyStartField].Value(), e.fields[wledDailyStopField].Value()
		a.Finish, a.Enabled = e.fields[wledFinishField].Value(), e.fields[wledEnabledField].Value() == "enabled"
	} else if len(e.fields) >= 9 {
		a.Name, a.Start, a.End, a.Finish = e.fields[1].Value(), e.fields[4].Value(), e.fields[5].Value(), e.fields[9].Value()
		a.AllDay, a.On, a.Off = e.fields[6].Value() == "enabled", e.fields[7].Value(), e.fields[8].Value()
		a.Enabled = e.fields[10].Value() == "enabled"
	}
	if a.AllDay {
		a.On, a.Off = "", ""
	}
	a.Targets = nil
	if wled {
		a.Sequence = ""
		a.WLED = &WLEDProgram{}
	} else {
		if sequence == "" {
			ids := lightingIDs(m.bundle, 0)
			if len(ids) > 0 {
				sequence = ids[0]
			}
		}
		a.Sequence = sequence
		a.WLED = nil
	}
	e.assignment = a
	m.openLightingAssignmentForm(a)
}

func (m *statusModel) switchToWLEDAssignment() {
	m.switchLightingAssignmentKind(true, "")
}

func (e lightingEditor) assignmentWLED() bool {
	return e.form == "assignment" && len(e.fields) > assignmentTypeField && e.fields[assignmentTypeField].Value() == assignmentTypeWLED
}

func (e lightingEditor) assignmentAllDay() bool {
	if e.form != "assignment" {
		return false
	}
	index := e.assignmentAllDayFieldIndex()
	return len(e.fields) > index && e.fields[index].Value() == "enabled"
}

func (e lightingEditor) assignmentAllDayFieldIndex() int {
	if e.assignmentWLED() {
		return wledAllDayField
	}
	return assignmentAllDayField
}

func (e lightingEditor) assignmentDailyField() bool {
	if !e.assignmentAllDay() {
		return false
	}
	if e.assignmentWLED() {
		return e.focus == wledDailyStartField || e.focus == wledDailyStopField
	}
	return e.focus == assignmentDailyStartField || e.focus == assignmentDailyStopField
}

func (e *lightingEditor) restoreDailyDefaults() {
	start, stop := assignmentDailyStartField, assignmentDailyStopField
	if e.assignmentWLED() {
		start, stop = wledDailyStartField, wledDailyStopField
	}
	if e.fields[start].Value() == "" || e.fields[start].Value() == "all day" {
		e.fields[start].SetValue("sunset")
	}
	if e.fields[stop].Value() == "" {
		e.fields[stop].SetValue("00:00")
	}
}

func (m *statusModel) editLightingStep(index int) {
	e := &m.lighting
	e.step = index
	s := ColorStep{Name: "New color", XY: []float64{.3127, .3290}, Brightness: 180, Hold: 6, Transition: .5}
	if index < len(e.sequence.Steps) {
		s = e.sequence.Steps[index]
	}
	colorID := ""
	for id, color := range colorDefinitions(m.bundle) {
		if sameXY(s.XY, []float64{color.X, color.Y}) {
			colorID = id
			break
		}
	}
	e.openForm("step", []string{"Color (catalog)", "Brightness (%)", "Duration (seconds)", "Transition (seconds)"}, []string{colorID, strconv.Itoa(brightnessPercentValue(s.Brightness)), strconv.FormatFloat(s.Hold, 'f', -1, 64), strconv.FormatFloat(s.Transition, 'f', -1, 64)})
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
		catalogColor, colorOK := colorDefinitions(m.bundle)[values[0]]
		brightnessPercent, bErr := strconv.ParseFloat(values[1], 64)
		hold, hErr := strconv.ParseFloat(values[2], 64)
		transition, tErr := strconv.ParseFloat(values[3], 64)
		if !colorOK || bErr != nil || brightnessPercent < 0 || brightnessPercent > 100 || hErr != nil || tErr != nil {
			e.error = "Choose a catalog color and enter 0-100% brightness, duration, and transition."
			return
		}
		s := e.sequence
		s.Steps = append([]ColorStep(nil), s.Steps...)
		step := ColorStep{Name: catalogColor.Name, XY: []float64{catalogColor.X, catalogColor.Y}, Brightness: brightnessFromPercent(brightnessPercent), Hold: hold, Transition: transition}
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
		if e.assignmentWLED() {
			a.Name, a.Start, a.End = values[assignmentNameField], values[wledFirstDateField], values[wledLastDateField]
			a.On, a.Off, a.Finish = values[wledDailyStartField], values[wledDailyStopField], values[wledFinishField]
			a.AllDay = values[wledAllDayField] == "enabled"
			a.Enabled = values[wledEnabledField] == "enabled"
			a.Sequence, a.Targets = "", nil
			a.WLED = &WLEDProgram{Light: values[wledLightField], Select: values[wledSelectField], Option: values[wledOptionField]}
			if a.AllDay {
				a.On, a.Off = "", ""
			}
			status := m.inventoryStatus
			status.Connected = status.Connected || m.stateAPI != nil
			if err = validateConnectedWLEDProgram(status, *a.WLED, m.inventoryStates, m.inventoryMetadata); err != nil {
				e.error = err.Error()
				return
			}
		} else {
			a.Name, a.Sequence, a.Start, a.End, a.Finish = values[assignmentNameField], values[assignmentSequenceField], values[4], values[5], values[9]
			a.AllDay = values[assignmentAllDayField] == "enabled"
			a.On, a.Off = values[assignmentDailyStartField], values[assignmentDailyStopField]
			if a.AllDay {
				a.On, a.Off = "", ""
			}
			a.WLED = nil
			a.Targets = strings.FieldsFunc(values[assignmentLightsField], func(r rune) bool { return r == ';' || r == ',' || r == ' ' })
			if len(m.inventoryStates) > 0 {
				a.Targets, err = resolveLightingTargets(a.Targets, m.inventoryStates)
				if err != nil {
					e.error = err.Error()
					return
				}
			}
			if values[assignmentEnabledField] != "enabled" && values[assignmentEnabledField] != "disabled" {
				e.error = "Schedule must be enabled or disabled."
				return
			}
			a.Enabled = values[assignmentEnabledField] == "enabled"
		}
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
	if e.returnDashboard && e.form == "assignment" {
		m.planner, e.returnDashboard = false, false
	}
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
		if e.form == "step" && e.focus == 0 && key.String() == "enter" {
			e.picker, e.pick = true, 0
			return m, nil
		}
		if e.form == "sequence" && e.focus == 1 && key.String() == "ctrl+u" {
			return m, nil
		}
		switch key.String() {
		case "left", "right", " ":
			options := e.fieldOptions(m.bundle)
			if len(options) > 0 {
				wasWLED := e.assignmentWLED()
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
				value := options[(index+delta+len(options))%len(options)]
				e.fields[e.focus].SetValue(value)
				if e.form == "assignment" && e.focus == assignmentTypeField && (value == assignmentTypeWLED) != wasWLED {
					m.switchLightingAssignmentKind(value == assignmentTypeWLED, "")
				} else if e.form == "assignment" && e.focus == e.assignmentAllDayFieldIndex() && value == "disabled" {
					e.restoreDailyDefaults()
				}
				return m, nil
			}
		case "esc":
			returnDashboard := e.returnDashboard && e.form == "assignment"
			e.form, e.error = "", ""
			e.step = min(e.step, max(0, len(e.sequence.Steps)-1))
			if returnDashboard {
				m.planner, e.returnDashboard = false, false
			}
			return m, nil
		case "s":
			if e.form == "pause" || e.form == "resume" {
				return m.submitLightingControl()
			}
			m.saveLightingForm()
			return m, nil
		case "enter":
			if e.form == "pause" || e.form == "resume" {
				return m.submitLightingControl()
			}
			if e.form == "assignment" && ((e.assignmentWLED() && e.focus == wledDailyStartField) || (!e.assignmentWLED() && e.focus == assignmentDailyStartField)) && !e.assignmentAllDay() {
				if e.error = assignmentDailyStartError(e.fields[e.focus].Value()); e.error != "" {
					return m, nil
				}
			}
			if e.form == "assignment" && ((e.assignmentWLED() && e.focus >= wledLightField && e.focus <= wledOptionField) || (!e.assignmentWLED() && (e.focus == assignmentSequenceField || e.focus == assignmentLightsField))) {
				e.picker, e.pick = true, 0
				if m.stateAPI != nil && len(m.inventoryStates) == 0 {
					m.inventoryMetadata = nil
					m.inventoryStatus = HAInventoryStatus{Connected: true}
					m.inventoryRequest++
					return m, loadInventoryCmd(m.stateAPI, m.inventoryRequest)
				}
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
		if e.form == "assignment" && e.assignmentDailyField() {
			return m, nil
		}
		var cmd tea.Cmd
		if e.form == "assignment" && len(e.fieldOptions(m.bundle)) > 0 {
			return m, nil
		}
		before := e.fields[e.focus].Value()
		e.fields[e.focus], cmd = e.fields[e.focus].Update(key)
		if e.form == "assignment" && e.assignmentWLED() && e.fields[e.focus].Value() != before {
			switch e.focus {
			case wledLightField:
				e.fields[wledSelectField].SetValue("")
				e.fields[wledOptionField].SetValue("")
			case wledSelectField:
				e.fields[wledOptionField].SetValue("")
			}
		}
		if e.form == "assignment" && ((!e.assignmentWLED() && e.focus == assignmentDailyStartField) || (e.assignmentWLED() && e.focus == wledDailyStartField)) {
			e.error = assignmentDailyStartError(e.fields[e.focus].Value())
		}
		return m, cmd
	}
	if e.steps {
		switch key.String() {
		case "esc":
			if e.returnDashboard {
				m.planner, e.returnDashboard = false, false
			} else {
				e.steps = false
			}
		case "up", "k":
			if e.step > -2 {
				e.step--
			}
		case "down", "j":
			if e.step+1 < len(e.sequence.Steps) {
				e.step++
			}
		case "left", "h", "right", "l":
			if e.step == -1 {
				s := e.sequence
				s.Repeat = key.String() == "left" || key.String() == "h"
				if next, err := saveColorSequence(m.bundle, s); err != nil {
					e.error = err.Error()
				} else {
					m.bundle, m.dirty, e.sequence, e.error = next, true, s, ""
				}
			}
		case "a", "n":
			m.editLightingStep(len(e.sequence.Steps))
		case "enter", "e":
			if e.step < 0 {
				focus := e.step + 2
				m.editLightingSequence(e.sequence.ID)
				e.fields[0].Blur()
				e.focus = focus
				return m, e.fields[focus].Focus()
			}
			m.editLightingStep(e.step)
		case "r":
			m.editLightingSequence(e.sequence.ID)
		case "[", "]", "x":
			if e.step < 0 {
				return m, nil
			}
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
				e.fields[assignmentSequenceField].SetValue(sequenceID)
			}
		case "p":
			if len(e.sequence.Steps) > 0 {
				m.sequencePreview, m.previewSequence, m.previewStep, m.previewElapsed = true, e.sequence, 0, 0
				return m, sequencePreviewTickCmd()
			}
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
	case "tab", "left", "right", "h", "l":
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
			m.prompt, m.pending, m.input, m.confirmFocus = "delete-sequence", id, "", 1
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
	}
	return m, nil
}

func (e lightingEditor) fieldOptions(bundle Bundle) []string {
	if e.form == "sequence" && e.focus == 1 {
		return []string{"loop", "once"}
	}
	if e.form == "assignment" && e.focus == e.assignmentAllDayFieldIndex() {
		return []string{"enabled", "disabled"}
	}
	if e.form == "assignment" && ((!e.assignmentWLED() && e.focus == assignmentFinishField) || (e.assignmentWLED() && e.focus == wledFinishField)) {
		return []string{"off", "leave"}
	}
	if e.form == "assignment" && ((!e.assignmentWLED() && e.focus == assignmentEnabledField) || (e.assignmentWLED() && e.focus == wledEnabledField)) {
		return []string{"enabled", "disabled"}
	}
	if e.form == "assignment" && e.focus == assignmentTypeField {
		return []string{assignmentTypeColor, assignmentTypeWLED}
	}
	return nil
}

func assignmentDailyStartError(value string) string {
	if value == "" || value == "sunset" || value == "all day" {
		return ""
	}
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return "Daily start must be HH:MM or sunset."
	}
	return ""
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
	if err := SaveBundleAtWithReferences(m.draftDir, m.colorsDir, m.bundle, m.filePaths); err != nil {
		m.lighting.error = err.Error()
	} else {
		m.dirty = false
		m.message = "Draft saved locally. ESC → d to review; u to publish."
	}
	return m, nil
}

func (m statusModel) lightingPickerIDs() []string {
	if m.lighting.assignmentWLED() {
		switch m.lighting.focus {
		case wledLightField:
			return wledLightIDs(m.inventoryStates, m.inventoryMetadata)
		case wledSelectField:
			return wledSelectorIDs(m.lighting.fields[wledLightField].Value(), m.inventoryStates, m.inventoryMetadata)
		case wledOptionField:
			selected := m.lighting.fields[wledSelectField].Value()
			if containsString(wledSelectorIDs(m.lighting.fields[wledLightField].Value(), m.inventoryStates, m.inventoryMetadata), selected) {
				state := m.inventoryStates[selected]
				return stringList(state.Attribute["options"])
			}
		}
		return nil
	}
	if m.lighting.focus == assignmentSequenceField {
		return lightingIDs(m.bundle, 0)
	}
	seen := map[string]bool{}
	for _, a := range lightingAssignments(m.bundle) {
		for _, id := range a.Targets {
			if state, ok := m.inventoryStates[id]; ok && lightSupportsColor(state) {
				seen[id] = true
			}
		}
	}
	for _, id := range colorLightIDs(m.inventoryStates, m.inventoryLocations) {
		seen[id] = true
	}
	for _, id := range strings.Split(m.lighting.fields[assignmentLightsField].Value(), ";") {
		if state, ok := m.inventoryStates[id]; ok && id != "" && lightSupportsColor(state) {
			seen[id] = true
		}
	}
	ids := []string{}
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		leftGroup := lightIsGroup(m.inventoryStates[ids[i]])
		rightGroup := lightIsGroup(m.inventoryStates[ids[j]])
		if leftGroup != rightGroup {
			return leftGroup
		}
		return ids[i] < ids[j]
	})
	return ids
}

func wledLightPickerLabel(state LightState, id string) string {
	name, _ := state.Attribute["friendly_name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return id
	}
	return name + " · " + id
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
			e.fields[e.focus].SetValue(ids[e.pick])
		} else if (e.focus == assignmentSequenceField || (!e.assignmentWLED() && e.focus == assignmentLightsField) || (e.assignmentWLED() && e.focus >= wledLightField && e.focus <= wledOptionField)) && len(ids) > 0 {
			e.fields[e.focus].SetValue(ids[e.pick])
			if e.assignmentWLED() && e.focus == wledLightField {
				e.fields[wledSelectField].SetValue("")
				e.fields[wledOptionField].SetValue("")
			} else if e.assignmentWLED() && e.focus == wledSelectField {
				e.fields[wledOptionField].SetValue("")
			}
		}
		e.picker = false
	case " ", "space":
		if !e.assignmentWLED() && e.focus == assignmentLightsField && len(ids) > 0 {
			chosen := strings.FieldsFunc(e.fields[assignmentLightsField].Value(), func(r rune) bool { return r == ';' })
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
			e.fields[assignmentLightsField].SetValue(strings.Join(next, ";"))
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
	footer := "Tab/h/l sequences/schedules • ↑/↓ select • Enter edit • n new • s save • ESC back"
	if e.form == "" && !e.steps && e.pane == 0 {
		footer += " • x delete sequence"
	}
	if e.form == "" && e.pane == 0 && e.selected < len(lightingIDs(m.bundle, 0)) {
		footer += "\np preview sequence"
	}
	if e.pane == 1 {
		footer += "\np pause live • r resume live"
	}
	if e.form != "" {
		title = "Edit " + e.form
		if e.form == "assignment" {
			title = "Edit schedule"
		}
		footer = "Tab/↑/↓ fields • ←/→/Space choices • Ctrl+U clear • s save • ESC cancel"
		if e.form == "sequence" {
			footer = "Tab/↑/↓ fields • ←/→/Space choose • s save • ESC cancel"
		}
		if e.form == "step" {
			footer = "Tab/↑/↓ fields • Enter choose catalog color • Ctrl+U clear • s save • ESC cancel"
		}
		if e.form == "pause" || e.form == "resume" {
			title = "Pause live playback"
			if e.form == "resume" {
				title = "Resume live playback"
			}
			footer = "Enter confirm • ESC cancel"
		}
		if e.picker {
			title = "Choose sequence"
			if e.focus == assignmentLightsField {
				title = "Choose lights · Space toggles; Enter accepts"
			}
			if e.form == "step" {
				title = "Choose named color"
			}
			if e.form == "assignment" && e.assignmentWLED() && e.focus == wledLightField {
				title = "Choose WLED light"
			} else if e.form == "assignment" && e.assignmentWLED() && e.focus == wledSelectField {
				title = "Choose WLED preset/playlist selector"
			} else if e.form == "assignment" && e.assignmentWLED() && e.focus == wledOptionField {
				title = "Choose WLED preset/playlist option"
			} else if e.focus == assignmentLightsField {
				title = "Choose lights · groups first · Space toggles; Enter accepts"
			}
			ids := m.lightingPickerIDs()
			if e.form == "step" {
				ids = colorIDs(m.bundle)
			}
			start, end := listWindow(len(ids), e.pick, height-9)
			if len(ids) == 0 {
				message := "No lights discovered. ESC to enter light entity IDs manually."
				if e.form == "assignment" && e.assignmentWLED() {
					switch {
					case !m.inventoryStatus.EntityRegistryReady:
						message = "WLED choices unavailable: reload HA inventory."
					case e.focus == 2:
						message = "No WLED devices found in Home Assistant inventory. Refresh inventory."
					case e.focus == 3:
						message = "No WLED preset or playlist selectors found for this device."
					case e.focus == 4:
						message = "No preset or playlist options found for this selector."
					}
				}
				lines = append(lines, message)
			}
			for i := start; i < end; i++ {
				var label string
				if e.form == "step" {
					label = colorDefinitions(m.bundle)[ids[i]].Name + "  (" + ids[i] + ")"
				} else if !e.assignmentWLED() && e.focus == assignmentSequenceField {
					label = colorSequences(m.bundle)[ids[i]].Name + "  (" + ids[i] + ")"
				} else if e.assignmentWLED() && e.focus == wledOptionField {
					label = ids[i]
				} else if e.assignmentWLED() && e.focus == wledLightField {
					mark := "  "
					if e.fields[e.focus].Value() == ids[i] {
						mark = "❯ "
					}
					label = mark + wledLightPickerLabel(m.inventoryStates[ids[i]], ids[i])
				} else if e.assignmentWLED() && e.focus == wledSelectField {
					mark := "  "
					if e.fields[e.focus].Value() == ids[i] {
						mark = "❯ "
					}
					label = mark + lightName(m.inventoryStates[ids[i]], ids[i]) + " · " + ids[i]
				} else {
					mark := "[ ] "
					if containsString(strings.Split(e.fields[assignmentLightsField].Value(), ";"), ids[i]) {
						mark = "[x] "
					}
					label = mark + lightName(m.inventoryStates[ids[i]], ids[i]) + " · " + ids[i]
					if lightIsGroup(m.inventoryStates[ids[i]]) {
						label = mark + "GROUP · " + lightName(m.inventoryStates[ids[i]], ids[i]) + " · " + ids[i]
					}
				}
				if i == e.pick {
					label = selectedStyle.Render("❯ " + label)
				} else {
					label = "  " + label
				}
				lines = append(lines, label)
			}
			if e.form == "assignment" && e.assignmentWLED() {
				footer = "↑/↓ select • Enter choose • ESC cancel"
			} else {
				footer = "↑/↓ select • Space toggle light • Enter accept • ESC back"
			}
		} else {
			if e.form == "step" {
				lines = append(lines, sectionStyle.Render("SEQUENCE")+"  "+e.sequence.Name, "")
				for i, step := range e.sequence.Steps {
					prefix := "  "
					if i == e.step {
						prefix = "❯ "
					}
					rgb, _, _ := displayRGB(map[string]any{"xy_color": []any{step.XY[0], step.XY[1]}})
					lines = append(lines, fmt.Sprintf("%s%d. %s  XY %.3f, %.3f  #%02X%02X%02X", prefix, i+1, step.Name, step.XY[0], step.XY[1], rgb[0], rgb[1], rgb[2]))
				}
				if e.step == len(e.sequence.Steps) {
					lines = append(lines, "❯ "+fmt.Sprintf("%d. New step", e.step+1))
				}
				lines = append(lines, "")
			}
			if e.form == "sequence" || e.form == "step" {
				lines = append(lines, "Set the sequence name and playback mode.", "")
				if e.form == "step" {
					lines[len(lines)-2] = "Set the color, brightness, and timing for this step."
				}
			}
			start, end := listWindow(len(e.fields), e.focus, max(1, (height-10)/2))
			if e.form == "assignment" {
				if e.assignmentWLED() {
					start, end = listWindow(len(e.fields), e.focus, max(1, height-11))
				} else {
					start, end = 0, len(e.fields)
				}
			}
			for i := start; i < end; i++ {
				field := e.fields[i]
				field.Width = max(12, width-19)
				label := e.labels[i]
				labelWidth := 12
				if e.form == "assignment" {
					labelWidth = 20
				}
				prefix := "  "
				if i == e.focus {
					prefix = "❯ "
				}
				if e.form == "assignment" && i == assignmentTypeField {
					color, wled := "[ ] "+assignmentTypeColor, "[ ] "+assignmentTypeWLED
					if field.Value() == assignmentTypeColor {
						color = "[x] " + assignmentTypeColor
					} else {
						wled = "[x] " + assignmentTypeWLED
					}
					line := fmt.Sprintf("%s%-20s │ %s  %s", prefix, label, color, wled)
					if i == e.focus {
						line = selectedStyle.Render(line)
					}
					lines = append(lines, line)
				} else if e.form == "assignment" && i == e.assignmentAllDayFieldIndex() {
					enabled, disabled := "[ ] enabled", "[ ] disabled"
					if field.Value() == "enabled" {
						enabled = "[x] enabled"
					} else {
						disabled = "[x] disabled"
					}
					line := fmt.Sprintf("%s%-20s │ %s  %s", prefix, label, enabled, disabled)
					if i == e.focus {
						line = selectedStyle.Render(line)
					}
					lines = append(lines, line)
				} else if e.form == "assignment" && e.assignmentAllDay() && ((!e.assignmentWLED() && (i == assignmentDailyStartField || i == assignmentDailyStopField)) || (e.assignmentWLED() && (i == wledDailyStartField || i == wledDailyStopField))) {
					line := fmt.Sprintf("%s%-20s │ ignored while all day is enabled", prefix, label)
					lines = append(lines, mutedStyle.Render(line))
				} else if e.form == "assignment" && ((!e.assignmentWLED() && i == assignmentFinishField) || (e.assignmentWLED() && i == wledFinishField)) {
					off, leave := "[ ] Turn lights off", "[ ] Leave current light state"
					if field.Value() == "off" {
						off = "[x] Turn lights off"
					} else {
						leave = "[x] Leave current light state"
					}
					line := fmt.Sprintf("%s%-20s │ %s  %s", prefix, label, off, leave)
					if i == e.focus {
						line = selectedStyle.Render(line)
					}
					lines = append(lines, line)
				} else if e.form == "assignment" && ((!e.assignmentWLED() && i == assignmentEnabledField) || (e.assignmentWLED() && i == wledEnabledField)) {
					enabled, disabled := "[ ] enabled", "[ ] disabled"
					if field.Value() == "enabled" {
						enabled = "[x] enabled"
					} else {
						disabled = "[x] disabled"
					}
					line := fmt.Sprintf("%s%-20s │ %s  %s", prefix, label, enabled, disabled)
					if i == e.focus {
						line = selectedStyle.Render(line)
					}
					lines = append(lines, line)
				} else if e.form == "sequence" || e.form == "step" {
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
					line := fmt.Sprintf("%s%-*s │ %s", prefix, labelWidth, label, field.View())
					if i == e.focus {
						line = selectedStyle.Render(line)
					}
					lines = append(lines, line)
				}
			}
			if e.form == "assignment" {
				var hints []string
				if e.assignmentWLED() {
					hints = []string{"Choose Color sequence or WLED preset/playlist.", "Enter a descriptive schedule name.", "Enter chooses a WLED light.", "Enter chooses a WLED preset/playlist selector.", "Enter chooses an option from that selector.", "Use M-D or MM-DD, or YYYY-M-D or YYYY-MM-DD.", "Use the same format as First date; this date is included.", "Choose enabled or disabled. Daily fields are ignored when enabled.", "Use HH:MM or sunset.", "Use HH:MM; ignored when all day is enabled.", "Choose Turn lights off or Leave current light state.", "Choose enabled or disabled."}
				} else {
					hints = []string{"Choose Color sequence or WLED preset/playlist.", "Enter a descriptive schedule name.", "Enter chooses a color sequence.", "Enter chooses lights; separate typed entity IDs with ;.", "Use M-D or MM-DD, or YYYY-M-D or YYYY-MM-DD.", "Use the same format as First date; this date is included.", "Choose enabled or disabled. Daily fields are ignored when enabled.", "Use HH:MM or sunset.", "Use HH:MM; ignored when all day is enabled.", "Choose Turn lights off or Leave current light state.", "Choose enabled or disabled."}
				}
				lines = append(lines, hints[e.focus])
				lines = append(lines, "Dates include the last day. All-day assignments ignore daily hours.")
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
		for i, line := range []string{"  Name: " + e.sequence.Name, "  Playback: " + loop + "  " + once} {
			if e.step == i-2 {
				line = selectedStyle.Render("❯ " + strings.TrimPrefix(line, "  "))
			}
			lines = append(lines, line)
		}
		lines = append(lines, "", sectionStyle.Render("COLOR STEPS"))
		start, end := listWindow(len(e.sequence.Steps), e.step, height-10)
		for i := start; i < end; i++ {
			s := e.sequence.Steps[i]
			rgb, _, _ := displayRGB(map[string]any{"xy_color": []any{s.XY[0], s.XY[1]}})
			hex := fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
			swatch := lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render("  ")
			marker := "  "
			if i == e.step {
				marker = "❯ "
			}
			line := fmt.Sprintf("%s%d. %s %s XY %.3f, %.3f %s · brightness %s · %gs / fade %gs", marker, i+1, swatch, s.Name, s.XY[0], s.XY[1], hex, previewBrightness(s.Brightness), s.Hold, s.Transition)
			if i == e.step {
				line = selectedStyle.Render(line)
			}
			lines = append(lines, line)
		}
		footer = "↑/↓ select • Enter edit"
		if e.step == -1 {
			footer += " • ←/h loop • →/l once"
		}
		footer += " • a add • x remove • [ and ] move\nt assign to lights • p preview • s save draft • ESC back"
	} else {
		ids := lightingIDs(m.bundle, e.pane)
		start, end := listWindow(len(ids), e.selected, height-12)
		if len(ids) == 0 {
			if e.pane == 1 {
				lines = append(lines, "No schedules defined. Press n to create a color or WLED schedule.")
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
			daily := a.On + " → " + a.Off
			if a.AllDay {
				daily = "all day"
			}
			targets := strings.Join(a.Targets, ", ")
			if a.WLED != nil {
				targets = "WLED: " + a.WLED.Light + " · " + a.WLED.Select + " = " + a.WLED.Option
			}
			lines = append(lines, "", daily+" · stop: "+a.Finish, targets, "Pause stops playback; resume follows the date and time rules.")
		} else if e.pane == 0 {
			lines = append(lines, "", "Define colors here; Tab opens Schedules.")
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
	titleLine := titleStyle.Render(title)
	if status := m.draftStatus(); status != "" {
		titleLine += "  " + mutedStyle.Render("• "+status)
	}
	return titleLine + "\n\n" + strings.Join(lines, "\n") + "\n\n" + footerStyle.Render(footer) + "\n"
}
