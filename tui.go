package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	sectionStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	valueStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	footerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	borderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

type statusModel struct {
	bundle                 Bundle
	draftDir               string
	baseline               *Bundle
	diff                   bool
	simulate               bool
	selected               int
	prompt                 string
	input                  string
	message                string
	stateAPI               StateAPI
	preview                Preview
	store                  ConfigPublisher
	refs                   []ConfigRef
	deletes                []ConfigRef
	backup                 string
	pending                string
	width                  int
	height                 int
	inventory              bool
	inventoryStates        map[string]LightState
	inventoryLocations     map[string]LightLocation
	inventorySelected      int
	inventoryFocus         int
	inventoryDetailScroll  int
	inventorySection       int
	inventoryCollapsed     map[string]bool
	inventoryLocationError string
	inventoryLoading       bool
	inventoryRequest       uint64
	colorView              bool
	colorSelected          int
	sequenceView           bool
	sequenceHoliday        int
	sequenceSelected       int
	sequenceAddView        bool
	sequenceAddSelected    int
	bootstrapLoading       bool
	lightIDs               []string
	lightLoading           bool
	pendingSceneAction     string
	pendingSceneID         string
	targetSelected         int
	selectorActive         bool
	formValues             []string
	formField              int
	scheduleValues         []string
	scheduleField          int
	scheduleModeActive     bool
	deleteID               string
	deleteRefs             []ConfigRef
	deleteLoading          bool
	deleteInspectionErr    string
	deleteScroll           int
	operationLoading       bool
	ciePicker              bool
	ciePickerX             float64
	ciePickerY             float64
	dashboardFocus         int
	dashboardWorkspace     int // 0 scenes, 1 sequences, 2 colors
	contentView            bool
	contentKind            int
	contentScroll          int
	dirty                  bool
	helpView               bool
	diffScroll             int
	simulationScroll       int
}

type deleteInspectionMsg struct {
	id   string
	refs []ConfigRef
	err  error
}

type inventoryResultMsg struct {
	states      map[string]LightState
	locations   map[string]LightLocation
	request     uint64
	locationErr error
	err         error
}

type operationResultMsg struct {
	operation string
	baseline  *Bundle
	warning   string
	err       error
}

type bootstrapResultMsg struct {
	bundle Bundle
	err    error
}

func (m statusModel) Init() tea.Cmd { return nil }

func (m statusModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if inspected, ok := message.(deleteInspectionMsg); ok {
		if inspected.id == m.deleteID {
			m.deleteLoading = false
			m.deleteRefs = inspected.refs
			if inspected.err != nil {
				m.deleteInspectionErr = inspected.err.Error()
			}
		}
		return m, nil
	}
	if inventory, ok := message.(inventoryResultMsg); ok {
		if inventory.request != m.inventoryRequest {
			return m, nil
		}
		if m.lightLoading {
			m.lightLoading = false
			if inventory.err != nil {
				m.message = "Light discovery failed: " + inventory.err.Error()
				return m, nil
			}
			m.inventoryStates, m.inventoryLocations = inventory.states, inventory.locations
			m.lightIDs = inventoryIDs(inventory.states, inventory.locations)
			if len(m.lightIDs) == 0 {
				m.message = "Home Assistant returned no light entities."
				return m, nil
			}
			if m.pendingSceneAction == "new" {
				ids := colorIDs(m.bundle)
				if len(ids) == 0 {
					if next, err := upsertColor(m.bundle, "warm_white", ColorDefinition{Name: "Warm white", X: .45, Y: .41}); err == nil {
						m.bundle, m.dirty, ids = next, true, colorIDs(next)
					}
				}
				m.prompt, m.formField, m.pending = "new", 0, ""
				m.formValues = []string{"Halloween", ids[0], m.lightIDs[0], "255"}
				m.input = m.formValues[0]
			} else {
				m.prompt, m.pending, m.formField = "edit", m.pendingSceneID, 0
				m.formValues = sceneFormValues(m.bundle, m.pendingSceneID)
				m.input = m.formValues[0]
			}
			m.pendingSceneAction, m.pendingSceneID = "", ""
			return m, nil
		}
		if !m.inventory {
			return m, nil
		}
		m.inventoryLoading = false
		if inventory.err != nil {
			m.message = "Inventory failed: " + inventory.err.Error()
			return m, nil
		}
		m.inventoryStates, m.inventoryLocations = inventory.states, inventory.locations
		m.lightIDs = inventoryIDs(inventory.states, inventory.locations)
		m.inventorySelected, m.inventoryFocus, m.inventoryDetailScroll, m.inventorySection = 0, 0, 0, 0
		m.inventoryCollapsed = map[string]bool{}
		for id, state := range inventory.states {
			if len(inventorySections(state, inventory.locations[id])[1].lines) > 8 {
				m.inventoryCollapsed["color"] = true
				break
			}
		}
		m.inventoryLocationError = ""
		if inventory.locationErr != nil {
			m.inventoryLocationError = inventory.locationErr.Error()
		}
		m.message = ""
		return m, nil
	}
	if result, ok := message.(operationResultMsg); ok {
		m.operationLoading = false
		if result.err != nil {
			m.message = result.err.Error()
			if result.operation == "restore" {
				m.prompt, m.input = "", ""
			}
			return m, nil
		} else if result.operation == "preview" {
			m.message = "Preview applied. Press r to restore captured state."
			m.prompt, m.input = "", ""
		} else if result.operation == "restore" {
			m.message = "Preview state restored."
			m.prompt, m.input = "", ""
		} else {
			m.message = "Published, verified, and backed up."
			if result.warning != "" {
				m.message += " " + result.warning
			}
			if m.draftDir != "" {
				if err := SaveBundle(m.draftDir, m.bundle); err != nil {
					m.message += " Local draft save failed: " + err.Error()
				} else {
					baseline := m.bundle
					m.baseline = &baseline
					m.dirty = false
					m.deletes = nil
				}
			} else {
				if result.baseline != nil {
					m.baseline = result.baseline
				}
				m.dirty = false
				m.deletes = nil
			}
			m.prompt, m.input = "", ""
		}
		return m, nil
	}
	if result, ok := message.(bootstrapResultMsg); ok {
		m.bootstrapLoading = false
		if result.err != nil {
			m.message = "HA infrastructure sync failed: " + result.err.Error()
		} else {
			m.bundle, m.dirty = result.bundle, true
			m.message = "Managed holiday infrastructure staged in the draft. Review the diff before publishing."
		}
		return m, nil
	}
	if size, ok := message.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		return m, nil
	}
	if key, ok := message.(tea.KeyMsg); ok {
		if key.String() == "q" && m.dirty && m.prompt == "" {
			m.prompt, m.input = "quit-confirm", ""
			return m, nil
		}
		if key.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.lightLoading {
			if key.String() == "esc" {
				m.lightLoading, m.pendingSceneAction, m.pendingSceneID = false, "", ""
			}
			return m, nil
		}
		if m.helpView {
			if key.String() == "esc" || key.String() == "?" {
				m.helpView = false
			}
			return m, nil
		}
		if m.ciePicker {
			return m.updateCIEPicker(key)
		}
		if m.bootstrapLoading {
			return m, nil
		}
		if m.sequenceView {
			return m.updateSequenceView(key)
		}
		if m.inventory {
			ids := inventoryIDs(m.inventoryStates, m.inventoryLocations)
			switch key.String() {
			case "esc", "i":
				m.inventory = false
			case "tab":
				m.inventoryFocus = 1 - m.inventoryFocus
			case "enter":
				m.inventoryFocus = 1
			case "up", "k":
				if m.inventoryFocus == 0 {
					if m.inventorySelected > 0 {
						m.inventorySelected--
						m.inventoryDetailScroll = 0
					}
				} else if m.inventoryDetailScroll > 0 {
					m.inventoryDetailScroll--
				}
			case "down", "j":
				if m.inventoryFocus == 0 {
					if m.inventorySelected+1 < len(ids) {
						m.inventorySelected++
						m.inventoryDetailScroll = 0
					}
				} else {
					m.inventoryDetailScroll++
				}
			case "[":
				if m.inventorySection > 0 {
					m.inventorySection--
				}
				m.inventoryFocus = 1
			case "/", "]":
				if m.inventorySection < 1 {
					m.inventorySection++
				}
				m.inventoryFocus = 1
			case " ", "space":
				if m.inventoryFocus == 1 {
					key := inventorySectionKey(m.inventorySection)
					m.inventoryCollapsed[key] = !m.inventoryCollapsed[key]
				}
			case "q":
				return m, tea.Quit
			}
			return m, nil
		}
		if m.colorView {
			ids := colorIDs(m.bundle)
			switch key.String() {
			case "esc", "c":
				m.colorView = false
			case "up", "k":
				if m.colorSelected > 0 {
					m.colorSelected--
				}
			case "down", "j":
				if m.colorSelected+1 < len(ids) {
					m.colorSelected++
				}
			case "n":
				m.colorView = false
				m.prompt, m.pending, m.formField = "color", "", 0
				m.formValues = []string{"New color", "0.500", "0.333"}
				m.input = m.formValues[0]
			case "e", "enter":
				if m.colorSelected < len(ids) {
					m.colorView = false
					id := ids[m.colorSelected]
					color := colorDefinitions(m.bundle)[id]
					m.prompt, m.pending, m.formField = "color", id, 0
					m.formValues = []string{color.Name, fmt.Sprintf("%.3f", color.X), fmt.Sprintf("%.3f", color.Y)}
					m.input = m.formValues[0]
				}
			case "x", "backspace":
				if m.colorSelected < len(ids) {
					id := ids[m.colorSelected]
					if colorReferenced(m.bundle, id) {
						m.message = "Cannot delete a color still referenced by a scene."
						break
					}
					if next, err := deleteColor(m.bundle, id); err != nil {
						m.message = "Color delete failed: " + err.Error()
					} else {
						m.bundle, m.dirty, m.message = next, true, "Deleted color "+id+"."
						if m.colorSelected >= len(colorIDs(next)) && m.colorSelected > 0 {
							m.colorSelected--
						}
					}
				}
			}
			return m, nil
		}
		if m.contentView {
			kinds := m.bundle.Kinds()
			switch key.String() {
			case "esc", "c":
				m.contentView = false
			case "tab", "right", "l":
				if len(kinds) > 0 {
					m.contentKind = (m.contentKind + 1) % len(kinds)
					m.contentScroll = 0
				}
			case "left", "h":
				if len(kinds) > 0 {
					m.contentKind = (m.contentKind + len(kinds) - 1) % len(kinds)
					m.contentScroll = 0
				}
			case "up", "k":
				if m.contentScroll > 0 {
					m.contentScroll--
				}
			case "down", "j":
				m.contentScroll++
			case "q", "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}
		if m.diff || m.simulate {
			switch key.String() {
			case "q":
				return m, tea.Quit
			case "esc", "d":
				m.diff = false
				m.simulate = false
			case "v":
				m.simulate = false
			case "up", "k":
				if m.diff && m.diffScroll > 0 {
					m.diffScroll--
				}
				if m.simulate && m.simulationScroll > 0 {
					m.simulationScroll--
				}
			case "down", "j":
				if m.diff {
					m.diffScroll++
				}
				if m.simulate {
					m.simulationScroll++
				}
			}
			return m, nil
		}
		if m.prompt != "" {
			if m.prompt == "delete-modal" {
				return m.updateDeleteModal(key)
			}
			if m.prompt == "color" {
				return m.updateColorForm(key)
			}
			if m.prompt == "schedule" {
				return m.updateScheduleForm(key)
			}
			if m.prompt == "new" || m.prompt == "edit" {
				return m.updateSceneForm(key)
			}
			return m.updatePrompt(key)
		}
		switch key.String() {
		case "?":
			m.helpView = true
		case "q":
			return m, tea.Quit
		case "d":
			if m.baseline != nil {
				m.diff = !m.diff
				m.diffScroll = 0
			}
		case "v":
			m.simulate = !m.simulate
			m.simulationScroll = 0
		case "i":
			if m.stateAPI == nil {
				m.message = "Inventory unavailable: configure HA URL and token."
				break
			}
			m.inventory, m.inventoryLoading = true, true
			m.inventoryRequest++
			m.message = ""
			return m, loadInventoryCmd(m.stateAPI, m.inventoryRequest)
		case "c":
			m.dashboardWorkspace = 2
			m.colorView, m.colorSelected = true, 0
		case "h":
			m.dashboardWorkspace = 1
			m.sequenceView, m.sequenceHoliday, m.sequenceSelected = true, 0, 0
		case "1", "2", "3", "4":
			m.dashboardWorkspace = int(key.String()[0] - '1')
			m.dashboardFocus = 1
		case "left":
			if m.dashboardWorkspace > 0 {
				m.dashboardWorkspace--
				m.dashboardFocus = 1
			}
		case "right":
			if m.dashboardWorkspace < 3 {
				m.dashboardWorkspace++
				m.dashboardFocus = 1
			}
		case "b":
			if store, ok := m.store.(interface {
				ReadAll(context.Context) (Bundle, error)
			}); ok {
				m.bootstrapLoading, m.message = true, ""
				return m, runBootstrapCmd(store, m.bundle)
			} else {
				m.message = "HA infrastructure sync requires the configured Home Assistant SSH connection."
			}
		case "tab":
			m.dashboardFocus = 1 - m.dashboardFocus
		case "up", "k":
			if m.dashboardFocus == 0 {
				if m.dashboardWorkspace > 0 {
					m.dashboardWorkspace--
				}
			} else {
				switch m.dashboardWorkspace {
				case 1:
					if m.sequenceHoliday > 0 {
						m.sequenceHoliday--
					}
				case 2:
					if m.colorSelected > 0 {
						m.colorSelected--
					}
				case 3:
					if m.contentKind > 0 {
						m.contentKind--
					}
				case 0:
					if m.selected > 0 {
						m.selected--
					}
				}
			}
		case "down", "j":
			if m.dashboardFocus == 0 {
				if m.dashboardWorkspace < 3 {
					m.dashboardWorkspace++
				}
			} else {
				switch m.dashboardWorkspace {
				case 1:
					if m.sequenceHoliday+1 < len(sequenceHolidays(m.bundle)) {
						m.sequenceHoliday++
					}
				case 2:
					if m.colorSelected+1 < len(colorIDs(m.bundle)) {
						m.colorSelected++
					}
				case 3:
					if m.contentKind+1 < len(m.bundle.Kinds()) {
						m.contentKind++
					}
				case 0:
					if m.selected+1 < len(SceneIDs(m.bundle)) {
						m.selected++
					}
				}
			}
		case "enter":
			if m.dashboardFocus == 0 {
				m.dashboardFocus = 1
			} else if m.dashboardWorkspace == 3 {
				m.contentView, m.contentScroll = true, 0
			} else if m.dashboardWorkspace == 1 {
				m.sequenceView, m.sequenceHoliday, m.sequenceSelected = true, 0, 0
			} else if m.dashboardWorkspace == 2 {
				m.colorView, m.colorSelected = true, 0
			} else if id := m.selectedScene(); id != "" {
				if !m.refreshLightIDs() {
					if m.stateAPI != nil {
						m.lightLoading, m.pendingSceneAction, m.pendingSceneID = true, "edit", id
						m.inventoryRequest++
						return m, loadInventoryCmd(m.stateAPI, m.inventoryRequest)
					}
					m.message = "No Home Assistant light entities available."
					break
				}
				m.prompt, m.pending, m.formField = "edit", id, 0
				m.formValues = sceneFormValues(m.bundle, id)
				m.input = m.formValues[0]
			}
		case "n":
			if m.dashboardWorkspace == 3 {
				m.message = "YAML draft is read-only here; use the structured workspaces to edit it."
				break
			}
			if m.dashboardWorkspace == 1 {
				m.prompt, m.input = "sequence-new", ""
				break
			}
			if m.dashboardWorkspace == 2 {
				m.colorView, m.colorSelected = false, 0
				m.prompt, m.pending, m.formField = "color", "", 0
				m.formValues = []string{"New color", "0.500", "0.333"}
				m.input = m.formValues[0]
				break
			}
			if !m.refreshLightIDs() {
				if m.stateAPI != nil {
					m.lightLoading, m.pendingSceneAction = true, "new"
					m.inventoryRequest++
					return m, loadInventoryCmd(m.stateAPI, m.inventoryRequest)
				}
				m.message = "No Home Assistant light entities available."
				break
			}
			ids := colorIDs(m.bundle)
			if len(ids) == 0 {
				if next, err := upsertColor(m.bundle, "warm_white", ColorDefinition{Name: "Warm white", X: .45, Y: .41}); err == nil {
					m.bundle = next
					m.dirty = true
					ids = colorIDs(m.bundle)
				}
			}
			m.prompt, m.formField = "new", 0
			m.pending = ""
			color := "warm_white"
			if len(ids) > 0 {
				color = ids[0]
			}
			m.formValues = []string{"Halloween", color, m.lightIDs[0], "255"}
			m.input = m.formValues[0]
		case "t":
			m.prompt, m.scheduleField = "schedule", 0
			m.scheduleValues = []string{"holiday_lighting_schedule", "Holiday lighting", "Halloween", "relative", "10", "31", "-16", "2"}
			m.input = m.scheduleValues[0]
		case "e":
			if m.dashboardWorkspace == 1 {
				m.sequenceView, m.sequenceSelected = true, 0
			} else if m.dashboardWorkspace == 2 {
				m.colorView = true
			} else if m.dashboardWorkspace == 3 {
				m.contentView, m.contentScroll = true, 0
			} else if id := m.selectedScene(); id != "" {
				if !m.refreshLightIDs() {
					if m.stateAPI != nil {
						m.lightLoading, m.pendingSceneAction, m.pendingSceneID = true, "edit", id
						m.inventoryRequest++
						return m, loadInventoryCmd(m.stateAPI, m.inventoryRequest)
					}
					m.message = "No Home Assistant light entities available."
					break
				}
				m.prompt, m.pending, m.formField = "edit", id, 0
				m.formValues = sceneFormValues(m.bundle, id)
				m.input = m.formValues[0]
			}
		case "x":
			if m.dashboardWorkspace == 1 {
				m.sequenceView, m.sequenceSelected = true, 0
			} else if m.dashboardWorkspace == 2 {
				m.colorView = true
			} else if m.dashboardWorkspace == 3 {
				m.message = "YAML draft is read-only here; use the structured workspaces to edit it."
			} else if id := m.selectedScene(); id != "" {
				refs := AffectedReferences(m.bundle, id)
				if finder, ok := m.store.(ConfigReferenceFinder); ok {
					m.deleteID, m.deleteRefs, m.deleteLoading, m.deleteInspectionErr = id, refs, true, ""
					m.prompt = "delete-modal"
					return m, inspectDeleteCmd(finder, id)
				}
				m.deleteID, m.deleteRefs, m.deleteLoading, m.deleteInspectionErr = id, refs, false, ""
				m.prompt = "delete-modal"
			}
		case "p":
			if m.stateAPI != nil && m.selectedScene() != "" {
				m.prompt, m.input = "preview", ""
			} else if m.selectedScene() == "" {
				m.message = "Preview unavailable: select a scene first."
			} else {
				m.message = "Preview unavailable: configure HA URL and token."
			}
		case "r":
			if m.stateAPI != nil {
				m.prompt, m.operationLoading = "restore", true
				return m, runRestoreCmd(m.preview)
			}
		case "u":
			if m.store != nil && m.baseline != nil {
				changes, err := Diff(*m.baseline, materializeNativeBundle(m.bundle))
				if err != nil {
					m.message = "Diff failed: " + err.Error()
				} else if len(changes) == 0 {
					m.message = "No changes to publish."
				} else {
					m.prompt, m.input = "publish", ""
				}
			} else {
				m.message = "Publish requires an imported baseline and HA credentials."
			}
		case "s":
			if err := SaveBundle(m.draftDir, m.bundle); err != nil {
				m.message = "Save failed: " + err.Error()
			} else {
				m.dirty = false
				m.message = "Draft saved."
			}
		}
	}
	return m, nil
}

func (m statusModel) View() string {
	if m.prompt != "" {
		return RenderPrompt(m)
	}
	if m.lightLoading {
		width, height := m.width, m.height
		if width < 1 {
			width = 80
		}
		if height < 1 {
			height = 24
		}
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, titleStyle.Render("Loading Home Assistant light entities…\n\nEsc cancel  Ctrl+C quit"))
	}
	if m.bootstrapLoading {
		width, height := m.width, m.height
		if width < 1 {
			width = 80
		}
		if height < 1 {
			height = 24
		}
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, titleStyle.Render("Reading Home Assistant infrastructure…\n\nPlease wait. Ctrl+C quits."))
	}
	if m.helpView {
		return renderHelp(m.width, m.height)
	}
	if m.contentView {
		return m.renderContentScreen()
	}
	if m.diff && m.baseline != nil {
		changes, err := Diff(*m.baseline, materializeNativeBundle(m.bundle))
		if err != nil {
			return "Diff error: " + err.Error() + "\n"
		}
		return m.renderScrollable("Draft diff", FormatChanges(changes), m.diffScroll, "d/Esc back  j/k scroll  q quit")
	}
	if m.simulate {
		return m.renderScrollable("Simulation: "+m.selectedScene(), RenderSimulation(m.bundle, m.selectedScene()), m.simulationScroll, "v/Esc back  j/k scroll  q quit")
	}
	if m.inventory {
		return m.renderInventoryScreen()
	}
	if m.sequenceView {
		return m.renderSequenceScreen()
	}
	if m.colorView {
		return m.renderColorScreen()
	}
	return m.withMessage(m.renderDashboard())
}

func renderHelp(width, height int) string {
	text := titleStyle.Render("Holiday Lighting Designer — Help") + "\n\n" +
		sectionStyle.Render("Dashboard") + "\n" +
		"  Tab           focus workspace navigation / active pane\n" +
		"  j/k           navigate the focused list\n" +
		"  1/2/3/4       Scenes / Sequences / Colors / YAML Draft\n" +
		"  Enter         open or edit the selected entry\n" +
		"  n             create a scene\n" +
		"  c             color catalog   h holiday sequences\n" +
		"  t             create a schedule\n" +
		"  x             delete selected scene\n" +
		"  v             simulation   p preview   u publish   b sync HA infra\n" +
		"  s             save draft    d diff\n\n" +
		sectionStyle.Render("Editors") + "\n" +
		"  Tab/↑↓        move between fields\n" +
		"  Enter         enter/accept selectors; save final field\n" +
		"  p             open the CIE xy color picker from a color form\n" +
		"  Space         toggle a light in the target selector\n" +
		"  Esc           cancel or go back\n\n" +
		mutedStyle.Render("Draft-only changes are marked *unsaved.  Press ? or Esc to return.")
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 3).Render(text))
}

func (m statusModel) renderScrollable(title, content string, scroll int, footer string) string {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	visible := m.height - 5
	if visible < 1 {
		visible = 20
	}
	if scroll > len(lines)-visible {
		scroll = len(lines) - visible
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + visible
	if end > len(lines) {
		end = len(lines)
	}
	return titleStyle.Render(title) + "\n\n" + strings.Join(lines[scroll:end], "\n") + "\n\n" + footerStyle.Render(footer+"\n")
}

func (m statusModel) renderDashboard() string {
	width := m.width
	if width < 60 {
		width = 80
	}
	leftWidth := width / 3
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := width - leftWidth - 3
	if rightWidth < 28 {
		rightWidth = 28
	}

	var left strings.Builder
	leftTitle := "WORKSPACES"
	if m.dashboardFocus == 0 {
		leftTitle = "❯ WORKSPACES"
	}
	left.WriteString(sectionStyle.Render(leftTitle) + "\n\n")
	workspaces := []struct {
		label string
		count int
	}{
		{"Scenes", len(SceneIDs(m.bundle))},
		{"Sequences", len(sequenceHolidays(m.bundle))},
		{"Colors", len(colorIDs(m.bundle))},
		{"YAML Draft", len(m.bundle.Kinds())},
	}
	for i, workspace := range workspaces {
		line := fmt.Sprintf("  %d  %-10s (%d)", i+1, workspace.label, workspace.count)
		if m.dashboardWorkspace == i {
			line = selectedStyle.Render(fmt.Sprintf("❯ %d  %-10s (%d)", i+1, workspace.label, workspace.count))
		}
		left.WriteString(line + "\n")
	}
	var right strings.Builder
	right.WriteString(m.renderDashboardWorkspace(rightWidth))

	draftLabel := "draft: " + m.draftDir
	if m.dirty {
		draftLabel += " *unsaved"
	}
	header := titleStyle.Render("Holiday Lighting Designer") + "  " + mutedStyle.Render(draftLabel)
	rule := borderStyle.Render(strings.Repeat("─", width))
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).PaddingRight(2).Render(left.String()),
		borderStyle.Render("│ "),
		lipgloss.NewStyle().Width(rightWidth).Render(right.String()),
	)
	footer := footerStyle.Render("Tab focus  ←/→ workspace  1/2/3/4 jump  Enter open/edit  ? help  q quit\n" +
		"n new  x del  c colors  h seq  b sync infra  v sim  p prev  i inv  s save  d diff  u pub")
	footer += "\n" + dashboardHelpView(width)
	return header + "\n" + rule + "\n" + body + "\n" + rule + "\n" + footer + "\n"
}

func (m statusModel) renderDashboardWorkspace(width int) string {
	var result strings.Builder
	switch m.dashboardWorkspace {
	case 1:
		holidays := sequenceHolidays(m.bundle)
		result.WriteString(sectionStyle.Render("SEQUENCES") + "\n")
		result.WriteString(mutedStyle.Render("Ordered scene cycles; duplicates and order are significant.") + "\n\n")
		for i, holiday := range holidays {
			values := holidaySequences(m.bundle)[holiday]
			line := fmt.Sprintf("  %-24s %d steps", holiday, len(values))
			if i == m.sequenceHoliday && m.dashboardFocus == 1 {
				line = selectedStyle.Render("❯ " + strings.TrimPrefix(line, "  "))
			}
			result.WriteString(line + "\n")
		}
		if len(holidays) == 0 {
			result.WriteString(mutedStyle.Render("  No sequences. Press n to create one or b to sync HA infrastructure.") + "\n")
		}
		if len(holidays) > 0 && m.sequenceHoliday < len(holidays) {
			selected := holidays[m.sequenceHoliday]
			result.WriteString("\n" + sectionStyle.Render("SELECTED SEQUENCE") + "  " + valueStyle.Render(selected) + "\n")
			for i, sceneID := range holidaySequences(m.bundle)[selected] {
				result.WriteString(fmt.Sprintf("  %d. %s\n", i+1, sequenceSceneName(m.bundle, sceneID)))
			}
		}
	case 2:
		ids := colorIDs(m.bundle)
		result.WriteString(sectionStyle.Render("COLORS") + "\n")
		result.WriteString(mutedStyle.Render("Named CIE xy colors reusable by scenes.") + "\n\n")
		for i, id := range ids {
			color := colorDefinitions(m.bundle)[id]
			line := renderCatalogColor(color)
			if i == m.colorSelected && m.dashboardFocus == 1 {
				line = selectedStyle.Render("❯ " + line)
			}
			result.WriteString(line + "\n")
		}
		if len(ids) == 0 {
			result.WriteString(mutedStyle.Render("  No colors. Press n to create one.") + "\n")
		}
	case 3:
		kinds := m.bundle.Kinds()
		result.WriteString(sectionStyle.Render("YAML DRAFT") + "\n")
		result.WriteString(mutedStyle.Render("Proposed native Home Assistant YAML; select a file and press Enter to inspect it.") + "\n\n")
		for i, kind := range kinds {
			line := fmt.Sprintf("  %-14s %d entries", filenameForKind(kind), draftEntryCount(m.bundle.Files[kind].Data))
			if i == m.contentKind && m.dashboardFocus == 1 {
				line = selectedStyle.Render("❯ " + strings.TrimPrefix(line, "  "))
			}
			result.WriteString(line + "\n")
		}
		if len(kinds) == 0 {
			result.WriteString(mutedStyle.Render("  No YAML draft files.") + "\n")
		}
	default:
		ids := SceneIDs(m.bundle)
		result.WriteString(sectionStyle.Render("SCENES") + "\n")
		result.WriteString(mutedStyle.Render("Groups of lights with reusable color assignments.") + "\n\n")
		for i, id := range ids {
			line := "  " + sequenceSceneName(m.bundle, id)
			if i == m.selected && m.dashboardFocus == 1 {
				line = selectedStyle.Render("❯ " + strings.TrimPrefix(line, "  "))
			}
			result.WriteString(line + "\n")
		}
		if len(ids) == 0 {
			result.WriteString(mutedStyle.Render("  No scenes. Press n to create one.") + "\n")
		}
		id := m.selectedScene()
		if id != "" {
			result.WriteString("\n" + sectionStyle.Render("SCENE DETAILS") + "  " + titleStyle.Render(id) + "\n\n")
			for _, entityID := range mapKeys(SceneValues(m.bundle, id)) {
				value := SceneValues(m.bundle, id)[entityID]
				fmt.Fprintf(&result, "  %s\n    State: %v    Brightness: %s\n    %s\n", entityID, value["state"], formatBrightness(value["brightness"]), renderSceneColor(value))
			}
		}
	}
	return result.String()
}

func (m statusModel) renderContentScreen() string {
	kinds := m.bundle.Kinds()
	if len(kinds) == 0 {
		return "No draft content.\n\nEsc back\n"
	}
	contentKind := m.contentKind
	if contentKind < 0 {
		contentKind = 0
	}
	if contentKind >= len(kinds) {
		contentKind = len(kinds) - 1
	}
	kind := kinds[contentKind]
	data, err := yaml.Marshal(m.bundle.Files[kind].Data)
	if err != nil {
		return "Cannot render draft content: " + err.Error() + "\n\nEsc back\n"
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	visible := m.height - 7
	if visible < 1 {
		visible = 18
	}
	start := m.contentScroll
	if start > len(lines)-visible {
		start = len(lines) - visible
	}
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}
	var body strings.Builder
	body.WriteString(titleStyle.Render("Draft content: "+filenameForKind(kind)) + "\n")
	body.WriteString(mutedStyle.Render(fmt.Sprintf("%s · %d entries · working copy only", kind, draftEntryCount(m.bundle.Files[kind].Data))) + "\n\n")
	for _, line := range lines[start:end] {
		body.WriteString(line + "\n")
	}
	body.WriteString("\n" + footerStyle.Render("j/k scroll  Tab/h/l change file  Esc back  q quit") + "\n")
	return body.String()
}

func draftEntryCount(value any) int {
	switch value := value.(type) {
	case []any:
		return len(value)
	case map[string]any:
		return len(value)
	default:
		return 0
	}
}

func renderSceneColor(value map[string]any) string {
	rgb, source, ok := sceneDisplayRGB(value)
	if !ok {
		return mutedStyle.Render("Color: not specified")
	}
	hex := fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
	luminance := (0.299*float64(rgb[0]) + 0.587*float64(rgb[1]) + 0.114*float64(rgb[2])) / 255
	foreground := "255"
	if luminance > 0.55 {
		foreground = "0"
	}
	swatch := lipgloss.NewStyle().Foreground(lipgloss.Color(foreground)).Background(lipgloss.Color(hex)).Padding(0, 1).Render(" " + hex + " ")
	return "Color: " + swatch + "  " + valueStyle.Render(source)
}

func renderCatalogColor(color ColorDefinition) string {
	rgb, _, _ := sceneDisplayRGB(map[string]any{"xy_color": []any{color.X, color.Y}})
	hex := fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
	luminance := (0.299*float64(rgb[0]) + 0.587*float64(rgb[1]) + 0.114*float64(rgb[2])) / 255
	foreground := "255"
	if luminance > 0.55 {
		foreground = "0"
	}
	swatch := lipgloss.NewStyle().Foreground(lipgloss.Color(foreground)).Background(lipgloss.Color(hex)).Padding(0, 1).Render(" " + hex + " ")
	return "Color: " + swatch + "  " + valueStyle.Render(color.Name) + fmt.Sprintf(" (XY %.3f, %.3f)", color.X, color.Y)
}

func sceneDisplayRGB(value map[string]any) ([3]int, string, bool) {
	if raw, ok := value["rgb_color"].([]any); ok && len(raw) >= 3 {
		return [3]int{clampByte(number(raw[0])), clampByte(number(raw[1])), clampByte(number(raw[2]))}, "RGB", true
	}
	if raw, ok := value["xy_color"].([]any); ok && len(raw) >= 2 {
		x, y := number(raw[0]), number(raw[1])
		rgb, valid := xyYToSRGB(CIEColor{X: x, Y: y}, 1)
		if !valid {
			return [3]int{}, "XY invalid", false
		}
		return [3]int{int(rgb[0]), int(rgb[1]), int(rgb[2])}, fmt.Sprintf("XY (%.3f, %.3f)", x, y), true
	}
	if raw, ok := value["hs_color"].([]any); ok && len(raw) >= 2 {
		xy := hsToXY(number(raw[0]), number(raw[1]))
		rgb, _, valid := sceneDisplayRGB(map[string]any{"xy_color": []any{xy[0], xy[1]}})
		return rgb, fmt.Sprintf("HS (%.0f°, %.0f%%)", number(raw[0]), number(raw[1])), valid
	}
	return [3]int{}, "", false
}

func number(value any) float64 {
	switch value := value.(type) {
	case int:
		return float64(value)
	case float64:
		return value
	case float32:
		return float64(value)
	}
	return 0
}

func formatBrightness(value any) string {
	var brightness float64
	switch value := value.(type) {
	case nil:
		return "n/a / 255 (n/a)"
	case string:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return "invalid / 255 (n/a)"
		}
		brightness = parsed
	default:
		brightness = number(value)
	}
	if brightness < 0 || brightness > 255 {
		return "invalid / 255 (n/a)"
	}
	return fmt.Sprintf("%d / 255 (%.0f%%)", int(math.Round(brightness)), brightness/255*100)
}

func gamma(value float64) float64 {
	value = math.Max(0, value)
	if value <= 0.0031308 {
		return 12.92 * value
	}
	return 1.055*math.Pow(value, 1/2.4) - 0.055
}

func clampColor(value float64) int {
	return int(math.Round(math.Max(0, math.Min(1, value)) * 255))
}

func clampByte(value float64) int {
	return int(math.Round(math.Max(0, math.Min(255, value))))
}

func (m statusModel) withMessage(view string) string {
	if m.message == "" {
		return view
	}
	return m.message + "\n\n" + view
}

func RenderStatus(bundle Bundle) string {
	var result strings.Builder
	fmt.Fprintf(&result, "Holiday Lighting Designer\n\nDraft fingerprint: %s\n\n", bundle.Hash)
	for _, kind := range bundle.Kinds() {
		fmt.Fprintf(&result, "  %s\n", kind)
	}
	result.WriteString("\nScenes:\n")
	for _, id := range SceneIDs(bundle) {
		fmt.Fprintf(&result, "  %s\n", id)
	}
	result.WriteString("\nCommands: Tab focus | Enter open/edit | q quit\n")
	result.WriteString("Scenes: n new | Enter/e edit | x delete | v simulate | p preview | i inventory | j/k select | s save\n")
	result.WriteString("Publish: u publish draft\n")
	result.WriteString("Schedule: t new schedule\n")
	return result.String()
}

func (m *statusModel) showInventory() {
	if m.stateAPI == nil {
		m.message = "Inventory unavailable: configure HA URL and token."
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	states, err := m.stateAPI.States(ctx, nil)
	if err != nil {
		cancel()
		m.message = "Inventory failed: " + err.Error()
		return
	}
	locations := map[string]LightLocation{}
	var locationErr error
	if api, ok := m.stateAPI.(interface {
		LightLocations(context.Context) (map[string]LightLocation, error)
	}); ok {
		locations, locationErr = api.LightLocations(ctx)
	}
	cancel()
	m.inventoryStates = states
	m.inventoryLocations = locations
	m.inventorySelected = 0
	m.inventoryFocus = 0
	m.inventoryDetailScroll = 0
	m.inventorySection = 0
	m.inventoryCollapsed = map[string]bool{}
	for id, state := range states {
		if len(inventorySections(state, locations[id])[1].lines) > 8 {
			m.inventoryCollapsed["color"] = true
			break
		}
	}
	m.inventoryLocationError = ""
	if locationErr != nil {
		m.inventoryLocationError = locationErr.Error()
	}
	m.inventory = true
	m.message = ""
}

func loadInventoryCmd(api StateAPI, request uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		states, err := api.States(ctx, nil)
		if err != nil {
			cancel()
			return inventoryResultMsg{request: request, err: err}
		}
		locations := map[string]LightLocation{}
		var locationErr error
		if client, ok := api.(interface {
			LightLocations(context.Context) (map[string]LightLocation, error)
		}); ok {
			locations, locationErr = client.LightLocations(ctx)
		}
		cancel()
		return inventoryResultMsg{states: states, locations: locations, request: request, locationErr: locationErr}
	}
}

func runBootstrapCmd(store interface {
	ReadAll(context.Context) (Bundle, error)
}, current Bundle) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		source, err := store.ReadAll(ctx)
		if err != nil {
			return bootstrapResultMsg{err: err}
		}
		bundle, err := bootstrapHolidayInfrastructure(current, source)
		return bootstrapResultMsg{bundle: bundle, err: err}
	}
}

func (m *statusModel) refreshLightIDs() bool {
	if len(m.lightIDs) > 0 {
		return true
	}
	if ids := draftLightIDs(m.bundle); len(ids) > 0 {
		// Draft targets are a useful offline fallback, but a live HA session
		// must discover the current light inventory before opening the form.
		if m.stateAPI == nil {
			m.lightIDs = ids
			return true
		}
	}
	return false
}

func draftLightIDs(bundle Bundle) []string {
	seen := map[string]bool{}
	for _, sceneID := range SceneIDs(bundle) {
		for entityID := range SceneValues(bundle, sceneID) {
			if strings.HasPrefix(entityID, "light.") {
				seen[entityID] = true
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func inventoryIDs(states map[string]LightState, locationMaps ...map[string]LightLocation) []string {
	locations := map[string]LightLocation{}
	if len(locationMaps) > 0 {
		locations = locationMaps[0]
	}
	ids := make([]string, 0, len(states))
	for entityID := range states {
		if strings.HasPrefix(entityID, "light.") {
			ids = append(ids, entityID)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := locations[ids[i]], locations[ids[j]]
		leftKey := strings.ToLower(left.Floor + "\x00" + left.Area + "\x00" + lightName(states[ids[i]], ids[i]))
		rightKey := strings.ToLower(right.Floor + "\x00" + right.Area + "\x00" + lightName(states[ids[j]], ids[j]))
		return leftKey < rightKey
	})
	return ids
}

func (m statusModel) renderInventoryScreen() string {
	if m.inventoryLoading {
		width, height := m.width, m.height
		if width < 1 {
			width = 80
		}
		if height < 1 {
			height = 24
		}
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, titleStyle.Render("Loading Home Assistant light inventory…\n\nEsc cancel  Ctrl+C quit"))
	}
	ids := inventoryIDs(m.inventoryStates, m.inventoryLocations)
	width := m.width
	if width < 60 {
		width = 80
	}
	leftWidth := width / 3
	if leftWidth < 28 {
		leftWidth = 28
	}
	rightWidth := width - leftWidth - 3
	if rightWidth < 28 {
		rightWidth = 28
	}

	var left strings.Builder
	left.WriteString(sectionStyle.Render("LIGHTS") + "\n\n")
	selectedID := ""
	if len(ids) > 0 && m.inventorySelected < len(ids) {
		selectedID = ids[m.inventorySelected]
	}
	tree := inventoryTree(ids, m.inventoryLocations, m.inventoryStates, selectedID)
	selectedLine := 0
	for i, line := range tree {
		if line.entityID == selectedID {
			selectedLine = i
			break
		}
	}
	visible := m.height - 10
	if visible < 1 {
		visible = 16
	}
	start := selectedLine - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(tree) {
		start = len(tree) - visible
		if start < 0 {
			start = 0
		}
	}
	end := start + visible
	if end > len(tree) {
		end = len(tree)
	}
	for _, line := range tree[start:end] {
		left.WriteString(line.text + "\n")
	}
	if len(ids) == 0 {
		left.WriteString(mutedStyle.Render("  No lights reported.") + "\n")
	}

	var right strings.Builder
	right.WriteString(sectionStyle.Render("LIGHT CAPABILITIES") + "\n\n")
	if len(ids) == 0 {
		right.WriteString(mutedStyle.Render("Home Assistant returned no light entities.") + "\n")
	} else {
		id := ids[m.inventorySelected]
		state := m.inventoryStates[id]
		right.WriteString(titleStyle.Render(lightName(state, id)) + "\n")
		right.WriteString(mutedStyle.Render(id) + "\n\n")
		sections := inventorySections(state, m.inventoryLocations[id])
		var lines []string
		for i, section := range sections {
			marker := " "
			if i == m.inventorySection && m.inventoryFocus == 1 {
				marker = "❯"
			}
			collapsed := m.inventoryCollapsed[section.key]
			lines = append(lines, marker+" "+sectionStyle.Render(section.title)+" ["+map[bool]string{true: "+", false: "-"}[collapsed]+"]")
			if !collapsed {
				for _, line := range section.lines {
					lines = append(lines, "  "+line)
				}
			}
		}
		maxLines := m.height - 10
		if maxLines < 1 {
			maxLines = len(lines)
		}
		detailScroll := m.inventoryDetailScroll
		if detailScroll > len(lines)-maxLines {
			detailScroll = len(lines) - maxLines
		}
		if detailScroll < 0 {
			detailScroll = 0
		}
		end := detailScroll + maxLines
		if end > len(lines) {
			end = len(lines)
		}
		for _, line := range lines[detailScroll:end] {
			right.WriteString(line + "\n")
		}
	}
	if m.inventoryLocationError != "" {
		right.WriteString("\n" + mutedStyle.Render("Location lookup failed: "+m.inventoryLocationError) + "\n")
	}

	rule := borderStyle.Render(strings.Repeat("─", width))
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).PaddingRight(2).Render(left.String()),
		borderStyle.Render("│ "),
		lipgloss.NewStyle().Width(rightWidth).Render(right.String()),
	)
	footer := footerStyle.Render("Tab focus  j/k move/scroll  / section  Space collapse  i/Esc back  q quit")
	return titleStyle.Render("Home Assistant Light Inventory") + "\n" + mutedStyle.Render("Inspect each light's capabilities for scene design") + "\n" + rule + "\n" + body + "\n" + rule + "\n" + footer + "\n"
}

type inventoryTreeLine struct {
	entityID string
	text     string
}

func inventoryTree(ids []string, locations map[string]LightLocation, states map[string]LightState, selectedID string) []inventoryTreeLine {
	lines := []inventoryTreeLine{}
	lastFloor, lastArea := "", ""
	for _, id := range ids {
		location := locations[id]
		floor := location.Floor
		if floor == "" {
			floor = "Unassigned floor"
		}
		area := location.Area
		if area == "" {
			area = "Unassigned area"
		}
		if floor != lastFloor {
			lines = append(lines, inventoryTreeLine{text: sectionStyle.Render("▾ " + floor)})
			lastFloor, lastArea = floor, ""
		}
		if area != lastArea {
			lines = append(lines, inventoryTreeLine{text: mutedStyle.Render("  ▾ " + area)})
			lastArea = area
		}
		text := "    " + lightName(states[id], id)
		if id == selectedID {
			text = selectedStyle.Render("  ❯ " + lightName(states[id], id))
		}
		lines = append(lines, inventoryTreeLine{entityID: id, text: text})
	}
	return lines
}

func lightName(state LightState, fallback string) string {
	if name, ok := state.Attribute["friendly_name"].(string); ok && name != "" {
		return name
	}
	return fallback
}

type inventorySection struct {
	key   string
	title string
	lines []string
}

func inventorySectionKey(index int) string {
	if index == 1 {
		return "color"
	}
	return "overview"
}

func inventorySections(state LightState, location LightLocation) []inventorySection {
	modes := stringList(state.Attribute["supported_color_modes"])
	effects := stringList(state.Attribute["effect_list"])
	colorLines := []string{fmt.Sprintf("Color modes (%d): %s", len(modes), listOrNone(modes)), fmt.Sprintf("Effects (%d):", len(effects))}
	for _, effect := range effects {
		colorLines = append(colorLines, "- "+effect)
	}
	colorLines = append(colorLines, renderSceneColor(state.Attribute))
	return []inventorySection{
		{key: "overview", title: "OVERVIEW", lines: []string{
			"State: " + state.State,
			"Brightness: " + formatBrightness(state.Attribute["brightness"]),
			"Location: " + locationLabel(location),
		}},
		{key: "color", title: "COLOR CAPABILITIES", lines: colorLines},
	}
}

func locationLabel(location LightLocation) string {
	if location.Floor == "" && location.Area == "" {
		return "Unassigned"
	}
	if location.Floor == "" {
		return location.Area
	}
	if location.Area == "" {
		return location.Floor
	}
	return location.Floor + " / " + location.Area
}

func RenderInventory(states map[string]LightState) string {
	ids := inventoryIDs(states)
	var result strings.Builder
	result.WriteString("HA LIGHT INVENTORY\n")
	result.WriteString("Capabilities available when designing scenes:\n\n")
	for _, entityID := range ids {
		state := states[entityID]
		modes := stringList(state.Attribute["supported_color_modes"])
		effects := stringList(state.Attribute["effect_list"])
		result.WriteString(valueStyle.Render(entityID) + "\n")
		fmt.Fprintf(&result, "  State: %s\n", state.State)
		fmt.Fprintf(&result, "  Color modes (%d): %s\n", len(modes), listOrNone(modes))
		fmt.Fprintf(&result, "  Effects (%d): %s\n\n", len(effects), listOrNone(effects))
	}
	if len(ids) == 0 {
		result.WriteString("  No lights reported.\n")
	}
	return result.String()
}

func stringList(value any) []string {
	var result []string
	switch values := value.(type) {
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok {
				result = append(result, text)
			}
		}
	case []string:
		result = append(result, values...)
	}
	return result
}

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func RenderSimulation(bundle Bundle, sceneID string) string {
	if sceneID == "" {
		return "Simulation\n\nNo scene selected."
	}
	values := SceneValues(bundle, sceneID)
	var result strings.Builder
	fmt.Fprintf(&result, "Simulation: %s\n\n", sceneID)
	colorRef := colorRefForScene(bundle, sceneID)
	for _, entityID := range mapKeys(values) {
		value := values[entityID]
		color := renderSceneColor(value)
		if definition, ok := colorDefinitions(bundle)[colorRef]; ok {
			color = renderCatalogColor(definition)
		}
		fmt.Fprintf(&result, "  %s\n    state=%v brightness=%s\n", entityID, value["state"], formatBrightness(value["brightness"]))
		if effect := value["effect"]; effect != nil && effect != "" {
			fmt.Fprintf(&result, "    effect=%v\n", effect)
		}
		fmt.Fprintf(&result, "    %s\n", color)
	}
	if len(values) == 0 {
		result.WriteString("  (no light entities)\n")
	}
	return result.String()
}

func previewConfirmation(bundle Bundle, sceneID string, states map[string]LightState) string {
	values := SceneValues(bundle, sceneID)
	var result strings.Builder
	result.WriteString("Preview targets:\n")
	if len(values) == 0 {
		return result.String() + "  (no light entities)\n"
	}
	if ref := colorRefForScene(bundle, sceneID); ref != "" {
		if color, ok := colorDefinitions(bundle)[ref]; ok {
			result.WriteString("  " + renderCatalogColor(color) + "\n")
		}
	}
	for _, entityID := range mapKeys(values) {
		value := values[entityID]
		fmt.Fprintf(&result, "  %s · %s\n", lightName(states[entityID], entityID), formatBrightness(value["brightness"]))
	}
	return result.String()
}

func (m statusModel) selectedScene() string {
	ids := SceneIDs(m.bundle)
	if m.selected < 0 || m.selected >= len(ids) {
		return ""
	}
	return ids[m.selected]
}

func (m statusModel) updatePrompt(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.operationLoading {
		return m, nil
	}
	switch key.String() {
	case "esc":
		if m.prompt == "delete-ha" {
			m.prompt, m.input, m.pending, m.deleteID, m.deleteRefs = "", "", "", "", nil
			return m, nil
		}
		m.prompt, m.input = "", ""
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case "enter":
		if m.prompt == "preview" && m.input == "PREVIEW" {
			m.operationLoading = true
			return m, runPreviewCmd(m.preview, m.bundle, m.selectedScene())
		}
		if m.prompt == "publish" && m.input == "PUBLISH" {
			m.operationLoading = true
			return m, runPublishCmd(m.store, m.bundle, *m.baseline, m.refs, m.deletes, m.backup)
		}
		if m.prompt == "quit-confirm" && (m.input == "y" || m.input == "Y") {
			return m, tea.Quit
		}
		if m.prompt == "quit-confirm" {
			m.prompt, m.input = "", ""
			return m, nil
		}
		m.finishPrompt()
	default:
		m.input += keyText(key)
	}
	return m, nil
}

func keyText(key tea.KeyMsg) string {
	if len(key.Runes) > 0 {
		return string(key.Runes)
	}
	if len(key.String()) == 1 {
		return key.String()
	}
	return ""
}

func runPreviewCmd(preview Preview, bundle Bundle, sceneID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		values := SceneValues(bundle, sceneID)
		if len(values) == 0 {
			return operationResultMsg{operation: "preview", err: fmt.Errorf("Preview failed: scene has no light targets")}
		}
		if err := preview.Capture(ctx, mapKeys(values)); err != nil {
			return operationResultMsg{operation: "preview", err: fmt.Errorf("Preview failed: %w", err)}
		}
		if err := preview.Apply(ctx, values); err != nil {
			if restoreErr := preview.Restore(ctx); restoreErr != nil {
				err = fmt.Errorf("%w; automatic restore failed: %v", err, restoreErr)
			}
			return operationResultMsg{operation: "preview", err: fmt.Errorf("Preview failed: %w", err)}
		}
		return operationResultMsg{operation: "preview"}
	}
}

func runPublishCmd(store ConfigPublisher, draft, baseline Bundle, refs, deletes []ConfigRef, backup string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := store.Publish(ctx, draft, baseline, refs, deletes, backup); err != nil {
			return operationResultMsg{operation: "publish", err: fmt.Errorf("Publish failed: %w", err)}
		}
		result := operationResultMsg{operation: "publish"}
		if importer, ok := store.(interface {
			Import(context.Context, []ConfigRef) (Bundle, error)
		}); ok {
			refreshed, err := importer.Import(ctx, refs)
			if err != nil {
				result.warning = "Could not refresh the publish baseline: " + err.Error()
			} else {
				result.baseline = &refreshed
			}
		}
		return result
	}
}

func runRestoreCmd(preview Preview) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := preview.Restore(ctx); err != nil {
			return operationResultMsg{operation: "restore", err: fmt.Errorf("Restore failed: %w", err)}
		}
		return operationResultMsg{operation: "restore"}
	}
}

func inspectDeleteCmd(finder ConfigReferenceFinder, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		refs, err := finder.AffectedReferences(ctx, id)
		cancel()
		return deleteInspectionMsg{id: id, refs: refs, err: err}
	}
}

func (m statusModel) updateDeleteModal(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.deleteLoading {
		if key.String() == "esc" {
			m.prompt, m.deleteLoading, m.deleteInspectionErr = "", false, ""
		}
		return m, nil
	}
	switch key.String() {
	case "esc", "n":
		m.prompt, m.deleteID, m.deleteRefs, m.deleteInspectionErr = "", "", nil, ""
	case "enter", "y":
		if m.deleteInspectionErr != "" {
			return m, nil
		}
		entries := bundleEntries(m.bundle)
		for _, ref := range m.deleteRefs {
			if _, exists := entries[ref]; !exists {
				m.message = "Import affected entries before deleting: " + formatRefs([]ConfigRef{ref})
				return m, nil
			}
		}
		if next, refs, err := DeleteScene(m.bundle, m.deleteID); err != nil {
			m.message = "Delete failed: " + err.Error()
		} else {
			m.bundle = next
			m.dirty = true
			if cleaned, cleanupErr := RemoveHolidayColor(m.bundle, "holiday_lights", m.deleteID); cleanupErr == nil {
				m.bundle = cleaned
			}
			m.message = "Removed from draft. HA scene not deleted."
			if len(refs) > 0 {
				m.message += " Affected: " + formatRefs(refs)
			}
			if m.store != nil {
				m.pending = m.deleteID
				m.prompt = "delete-ha"
				m.input = ""
				return m, nil
			}
		}
		m.prompt, m.deleteID, m.deleteRefs, m.deleteInspectionErr = "", "", nil, ""
	case "up", "k":
		if m.deleteScroll > 0 {
			m.deleteScroll--
		}
	case "down", "j":
		m.deleteScroll++
	}
	return m, nil
}

func (m statusModel) updateSceneForm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if (m.formField == 1 || m.formField == 2) && key.String() == "enter" && !m.selectorActive {
		m.selectorActive = true
		if m.formField == 2 {
			selected := strings.FieldsFunc(m.formValues[2], func(r rune) bool { return r == ';' })
			for i, id := range m.lightIDs {
				for _, chosen := range selected {
					if id == chosen {
						m.targetSelected = i
						break
					}
				}
			}
		}
		return m, nil
	}
	if m.formField == 2 && m.selectorActive {
		if len(m.lightIDs) == 0 {
			switch key.String() {
			case "esc":
				m.selectorActive = false
			case "enter":
				m.selectorActive = false
				m.formField = (m.formField + 1) % len(m.formValues)
				m.input = m.formValues[m.formField]
			}
			return m, nil
		}
		switch key.String() {
		case "left", "up":
			m.targetSelected = (m.targetSelected + len(m.lightIDs) - 1) % len(m.lightIDs)
			return m, nil
		case "right", "down":
			m.targetSelected = (m.targetSelected + 1) % len(m.lightIDs)
			return m, nil
		case " ", "space":
			selected := strings.FieldsFunc(m.formValues[2], func(r rune) bool { return r == ';' })
			candidate := m.lightIDs[m.targetSelected]
			found := false
			for i, id := range selected {
				if id == candidate {
					selected = append(selected[:i], selected[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				selected = append(selected, candidate)
			}
			m.formValues[2] = strings.Join(selected, ";")
			m.input = m.formValues[2]
			return m, nil
		case "enter":
			m.selectorActive = false
			m.formField = (m.formField + 1) % len(m.formValues)
			m.input = m.formValues[m.formField]
			return m, nil
		case "esc":
			m.selectorActive = false
			return m, nil
		default:
			return m, nil
		}
	}
	if m.formField == 1 && m.selectorActive {
		switch key.String() {
		case "left", "right", "up", "down":
			ids := colorIDs(m.bundle)
			if len(ids) == 0 {
				return m, nil
			}
			index := 0
			for i, id := range ids {
				if id == m.formValues[1] {
					index = i
					break
				}
			}
			if key.String() == "left" || key.String() == "up" {
				index = (index + len(ids) - 1) % len(ids)
			} else {
				index = (index + 1) % len(ids)
			}
			m.formValues[1] = ids[index]
			m.input = m.formValues[1]
			return m, nil
		case "backspace":
			return m, nil
		case "enter":
			m.selectorActive = false
			m.formField = (m.formField + 1) % len(m.formValues)
			m.input = m.formValues[m.formField]
			return m, nil
		case "esc":
			m.selectorActive = false
			return m, nil
		default:
			return m, nil
		}
	}
	saveField := func() {
		m.formValues[m.formField] = m.input
	}
	switch key.String() {
	case "esc":
		if m.selectorActive {
			m.selectorActive = false
		} else {
			m.prompt, m.input, m.pending = "", "", ""
		}
	case "tab", "down", "enter":
		saveField()
		if key.String() == "enter" && m.formField == len(m.formValues)-1 {
			m.finishSceneForm()
			return m, nil
		}
		m.formField = (m.formField + 1) % len(m.formValues)
		m.input = m.formValues[m.formField]
		m.selectorActive = false
	case "shift+tab", "up":
		saveField()
		m.formField = (m.formField + len(m.formValues) - 1) % len(m.formValues)
		m.input = m.formValues[m.formField]
		m.selectorActive = false
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		m.input += keyText(key)
	}
	return m, nil
}

func (m *statusModel) finishPrompt() {
	if m.prompt == "quit-confirm" {
		if m.input == "" || m.input == "y" || m.input == "Y" {
			return
		}
		if m.input == "n" || m.input == "N" {
			m.prompt, m.input = "", ""
		}
		return
	}
	if m.prompt == "new" || m.prompt == "edit" {
		m.finishSceneForm()
		return
	}
	if m.prompt == "sequence-new" {
		holiday := strings.TrimSpace(m.input)
		if holiday == "" {
			m.message = "Enter a holiday name."
			return
		}
		for _, existing := range sequenceHolidays(m.bundle) {
			if strings.EqualFold(existing, holiday) {
				m.message = "That holiday sequence already exists."
				return
			}
		}
		if next, err := replaceHolidaySequence(m.bundle, holiday, nil); err != nil {
			m.message = "Sequence creation failed: " + err.Error()
		} else {
			m.bundle, m.dirty, m.message = next, true, "Created empty "+holiday+" sequence."
		}
		m.prompt, m.input = "", ""
		return
	}
	if strings.HasPrefix(m.prompt, "delete ") {
		if m.input != "DELETE" {
			m.message = "Deletion cancelled."
		} else if next, refs, err := DeleteScene(m.bundle, strings.TrimPrefix(m.prompt, "delete ")); err != nil {
			m.message = "Delete failed: " + err.Error()
		} else {
			m.bundle = next
			m.dirty = true
			if cleaned, cleanupErr := RemoveHolidayColor(m.bundle, "holiday_lights", strings.TrimPrefix(m.prompt, "delete ")); cleanupErr == nil {
				m.bundle = cleaned
			}
			m.message = "Removed from draft. HA scene not deleted."
			if len(refs) > 0 {
				m.message += " Affected: " + formatRefs(refs)
			}
			if m.store != nil {
				m.pending = strings.TrimPrefix(m.prompt, "delete ")
				m.prompt, m.input = "delete-ha", ""
				return
			}
		}
		m.prompt, m.input = "", ""
		return
	}
	if m.prompt == "preview" {
		if m.input != "PREVIEW" {
			m.message = "Preview cancelled."
		} else if id := m.selectedScene(); id != "" {
			values := SceneValues(m.bundle, id)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := m.preview.Capture(ctx, mapKeys(values))
			if err == nil {
				err = m.preview.Apply(ctx, values)
				if err != nil {
					restoreErr := m.preview.Restore(ctx)
					if restoreErr != nil {
						err = fmt.Errorf("%w; automatic restore failed: %v", err, restoreErr)
					}
				}
			}
			cancel()
			if err != nil {
				m.message = "Preview failed: " + err.Error()
			} else {
				m.message = "Preview applied. Press r to restore captured state."
			}
		} else {
			m.message = "No scene selected."
		}
		m.prompt, m.input = "", ""
		return
	}
	if m.prompt == "publish" {
		if m.input != "PUBLISH" {
			m.message = "Publish cancelled."
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := m.store.Publish(ctx, m.bundle, *m.baseline, m.refs, m.deletes, m.backup)
			cancel()
			if err != nil {
				m.message = "Publish failed: " + err.Error()
			} else {
				m.message = "Published, verified, and backed up."
			}
		}
		m.prompt, m.input = "", ""
		return
	}
	if m.prompt == "delete-ha" {
		if m.input == "DELETE HA" {
			m.deletes = append(m.deletes, ConfigRef{Kind: Scenes, ID: m.pending})
			m.message = "HA scene deletion approved for the next publish."
		} else {
			m.message = "HA scene retained."
		}
		m.prompt, m.input, m.pending = "", "", ""
		m.deleteID, m.deleteRefs = "", nil
		return
	}
	if m.prompt == "schedule" {
		parts := strings.Split(m.input, ",")
		if len(parts) != 8 {
			m.message = "Use id,name,selector,relative|fixed,month,day,start,end"
			return
		}
		month, merr := strconv.Atoi(parts[4])
		day, derr := strconv.Atoi(parts[5])
		schedule := Schedule{ID: parts[0], Name: parts[1], Selector: parts[2], Mode: parts[3], AnchorMonth: month, AnchorDay: day}
		if parts[3] == "fixed" {
			schedule.StartDate, schedule.EndDate = parts[6], parts[7]
		} else {
			start, serr := strconv.Atoi(parts[6])
			end, eerr := strconv.Atoi(parts[7])
			if serr != nil || eerr != nil {
				m.message = "Relative schedule offsets must be numbers."
				return
			}
			schedule.StartDays, schedule.EndDays = start, end
		}
		if merr != nil || derr != nil {
			m.message = "Schedule anchor month and day must be numbers."
			return
		}
		automation, err := schedule.Automation()
		if err != nil {
			m.message = err.Error()
			return
		}
		next, err := UpsertAutomation(m.bundle, automation)
		if err != nil {
			m.message = err.Error()
		} else {
			m.bundle = next
			m.dirty = true
			m.message = "Schedule updated in draft."
		}
		m.prompt, m.input = "", ""
		return
	}
	parts := strings.Split(m.input, ",")
	if len(parts) != 8 {
		m.message = "Use id,holiday,color,entity,r,g,b,brightness"
		return
	}
	rgb, colorErr := sceneRGB(parts[1], parts[2], parts[4:7])
	brightness := 180
	if parts[7] != "" {
		var err error
		brightness, err = strconv.Atoi(parts[7])
		if err != nil {
			colorErr = err
		}
	}
	if colorErr != nil || brightness < 1 || brightness > 255 {
		m.message = "Use valid RGB values, a known holiday color, and brightness 1-255."
		return
	}
	id := parts[0]
	if id == "" {
		id = SuggestSceneID(parts[1], parts[2])
	}
	scene := NewScene(id, parts[1]+" "+parts[2], strings.FieldsFunc(parts[3], func(r rune) bool { return r == ';' }), rgb, brightness)
	next, err := UpsertScene(m.bundle, scene)
	if err != nil {
		m.message = err.Error()
	} else {
		m.bundle = next
		m.dirty = true
		if integrated, integrationErr := UpsertHolidayColor(m.bundle, "holiday_lights", parts[1], id); integrationErr == nil {
			m.bundle = integrated
		}
		m.message = "Scene updated in draft."
	}
	m.prompt, m.input = "", ""
}

func (m *statusModel) finishScheduleForm() {
	if len(m.scheduleValues) != 8 {
		m.message = "Schedule form is incomplete."
		return
	}
	month, monthErr := strconv.Atoi(m.scheduleValues[4])
	day, dayErr := strconv.Atoi(m.scheduleValues[5])
	schedule := Schedule{ID: m.scheduleValues[0], Name: m.scheduleValues[1], Selector: m.scheduleValues[2], Mode: m.scheduleValues[3], AnchorMonth: month, AnchorDay: day}
	if schedule.Mode == "fixed" {
		schedule.StartDate, schedule.EndDate = m.scheduleValues[6], m.scheduleValues[7]
	} else {
		var startErr, endErr error
		schedule.StartDays, startErr = strconv.Atoi(m.scheduleValues[6])
		schedule.EndDays, endErr = strconv.Atoi(m.scheduleValues[7])
		if startErr != nil || endErr != nil {
			m.message = "Relative window values must be whole numbers of days."
			return
		}
	}
	if monthErr != nil || dayErr != nil {
		m.message = "Anchor month and day must be numbers."
		return
	}
	automation, err := schedule.Automation()
	if err != nil {
		m.message = err.Error()
		return
	}
	next, err := UpsertAutomation(m.bundle, automation)
	if err != nil {
		m.message = err.Error()
		return
	}
	m.bundle, m.dirty, m.message = next, true, "Schedule updated in draft."
	m.prompt, m.input, m.scheduleValues = "", "", nil
}

func (m statusModel) updateColorForm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "p" {
		m.ciePicker = true
		m.ciePickerX, m.ciePickerY = 0.5, 0.333
		if len(m.formValues) > 2 {
			if x, err := strconv.ParseFloat(m.formValues[1], 64); err == nil {
				m.ciePickerX = x
			}
			if y, err := strconv.ParseFloat(m.formValues[2], 64); err == nil {
				m.ciePickerY = y
			}
		}
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.prompt, m.input, m.pending = "", "", ""
	case "tab", "down", "enter":
		m.formValues[m.formField] = m.input
		if key.String() == "enter" && m.formField == len(m.formValues)-1 {
			m.finishColorForm()
			return m, nil
		}
		m.formField = (m.formField + 1) % len(m.formValues)
		m.input = m.formValues[m.formField]
	case "shift+tab", "up":
		m.formValues[m.formField] = m.input
		m.formField = (m.formField + len(m.formValues) - 1) % len(m.formValues)
		m.input = m.formValues[m.formField]
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		m.input += keyText(key)
	}
	return m, nil
}

func (m statusModel) updateCIEPicker(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	step := 0.001
	if strings.HasPrefix(key.String(), "shift+") || strings.HasPrefix(key.String(), "ctrl+") {
		step = 0.01
	}
	switch key.String() {
	case "left", "shift+left", "ctrl+left":
		m.ciePickerX = math.Max(0.001, m.ciePickerX-step)
	case "right", "shift+right", "ctrl+right":
		m.ciePickerX = math.Min(1-m.ciePickerY, m.ciePickerX+step)
	case "up", "shift+up", "ctrl+up":
		m.ciePickerY = math.Min(0.999, m.ciePickerY+step)
	case "down", "shift+down", "ctrl+down":
		m.ciePickerY = math.Max(0.001, m.ciePickerY-step)
	case "enter":
		m.formValues[1] = fmt.Sprintf("%.4f", m.ciePickerX)
		m.formValues[2] = fmt.Sprintf("%.4f", m.ciePickerY)
		m.formField = 1
		m.input = m.formValues[1]
		m.ciePicker = false
	case "esc":
		m.ciePicker = false
	}
	return m, nil
}

func (m statusModel) updateScheduleForm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.scheduleField == 3 && key.String() == "enter" && !m.scheduleModeActive {
		m.scheduleModeActive = true
		return m, nil
	}
	if m.scheduleField == 3 && m.scheduleModeActive {
		switch key.String() {
		case "left", "up", "right", "down":
			if key.String() == "left" || key.String() == "up" {
				m.scheduleValues[3] = "fixed"
			} else {
				m.scheduleValues[3] = "relative"
			}
			if m.scheduleValues[3] == "fixed" {
				m.scheduleValues[6], m.scheduleValues[7] = "10-15", "11-02"
			}
			if m.scheduleValues[3] == "relative" {
				m.scheduleValues[6], m.scheduleValues[7] = "-16", "2"
			}
			m.input = m.scheduleValues[3]
			return m, nil
		case "enter":
			m.scheduleModeActive = false
			m.scheduleField++
			m.input = m.scheduleValues[m.scheduleField]
			return m, nil
		case "esc":
			m.scheduleModeActive = false
			return m, nil
		default:
			return m, nil
		}
	}
	switch key.String() {
	case "esc":
		m.prompt, m.input, m.scheduleValues = "", "", nil
	case "tab", "down", "enter":
		m.scheduleValues[m.scheduleField] = m.input
		if key.String() == "enter" && m.scheduleField == len(m.scheduleValues)-1 {
			m.finishScheduleForm()
			return m, nil
		}
		m.scheduleField = (m.scheduleField + 1) % len(m.scheduleValues)
		m.input = m.scheduleValues[m.scheduleField]
		m.scheduleModeActive = false
	case "shift+tab", "up":
		m.scheduleValues[m.scheduleField] = m.input
		m.scheduleField = (m.scheduleField + len(m.scheduleValues) - 1) % len(m.scheduleValues)
		m.input = m.scheduleValues[m.scheduleField]
		m.scheduleModeActive = false
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		m.input += keyText(key)
	}
	return m, nil
}

func colorID(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var result strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			result.WriteRune(r)
		} else if result.Len() > 0 {
			result.WriteByte('_')
		}
	}
	return strings.Trim(result.String(), "_")
}

func (m *statusModel) finishColorForm() {
	if len(m.formValues) != 3 {
		m.message = "Color form is incomplete."
		return
	}
	x, xErr := strconv.ParseFloat(m.formValues[1], 64)
	y, yErr := strconv.ParseFloat(m.formValues[2], 64)
	id := m.pending
	if id == "" {
		id = colorID(m.formValues[0])
	}
	if xErr != nil || yErr != nil || id == "" || x < 0 || y <= 0 || x+y > 1 || m.formValues[0] == "" {
		m.message = "Use a name and valid XY coordinates."
		return
	}
	next, err := upsertColor(m.bundle, id, ColorDefinition{Name: m.formValues[0], X: x, Y: y})
	if err != nil {
		m.message = err.Error()
		return
	}
	if m.pending != "" && m.pending != id {
		if config, ok := next.Files[Colors]; ok {
			if values, ok := config.Data.(map[string]any); ok {
				delete(values, m.pending)
				config.Data = values
				next, _ = withHash(Bundle{Files: next.Files, SourceHash: next.SourceHash})
			}
		}
		if config, ok := next.Files[Scenes]; ok {
			if values, ok := config.Data.([]any); ok {
				for _, raw := range values {
					if scene, ok := raw.(map[string]any); ok && scene["color_ref"] == m.pending {
						scene["color_ref"] = id
					}
				}
				config.Data = values
				next.Files[Scenes] = config
				next, _ = withHash(Bundle{Files: next.Files, SourceHash: next.SourceHash})
			}
		}
	}
	m.bundle, m.message = next, "Color saved: "+id
	m.dirty = true
	m.prompt, m.input, m.pending, m.formValues = "", "", "", nil
}

func (m *statusModel) finishSceneForm() {
	if len(m.formValues) != 4 {
		m.message = "Scene form is incomplete."
		return
	}
	color, ok := colorDefinitions(m.bundle)[m.formValues[1]]
	brightness, brightnessErr := strconv.Atoi(m.formValues[3])
	entities := strings.FieldsFunc(m.formValues[2], func(r rune) bool { return r == ';' })
	known := map[string]bool{}
	for _, entity := range m.lightIDs {
		known[entity] = true
	}
	for _, entity := range entities {
		if !known[entity] {
			ok = false
			break
		}
	}
	if !ok || brightnessErr != nil || brightness < 1 || brightness > 255 || m.formValues[0] == "" || len(entities) == 0 {
		m.message = "Select a catalog color and use brightness 1-255."
		return
	}
	id := m.pending
	if id == "" {
		id = SuggestSceneID(m.formValues[0], m.formValues[1])
	}
	scene := NewXYScene(id, m.formValues[0]+" "+color.Name, entities, color.X, color.Y, brightness)
	scene["color_ref"] = m.formValues[1]
	next, err := UpsertScene(m.bundle, scene)
	if err != nil {
		m.message = err.Error()
		return
	}
	m.bundle = next
	m.dirty = true
	m.message = "Scene updated in draft: " + id
	if integrated, integrationErr := UpsertHolidayColor(m.bundle, "holiday_lights", m.formValues[0], id); integrationErr == nil {
		m.bundle = integrated
	} else {
		m.message += "; holiday script integration failed: " + integrationErr.Error()
	}
	m.prompt, m.input, m.pending, m.formValues = "", "", "", nil
}

func sceneFormValues(bundle Bundle, sceneID string) []string {
	values := SceneValues(bundle, sceneID)
	entities := mapKeys(values)
	h, _ := sceneLabels(bundle, sceneID)
	color := colorRefForScene(bundle, sceneID)
	if color == "" {
		color = nearestColor(bundle, values)
	}
	brightness := 180
	if len(entities) > 0 {
		value := values[entities[0]]
		if value, ok := value["brightness"].(int); ok {
			brightness = value
		}
	}
	return []string{h, color, strings.Join(entities, ";"), strconv.Itoa(brightness)}
}

func nearestColor(bundle Bundle, values map[string]map[string]any) string {
	ids := colorIDs(bundle)
	if len(ids) == 0 {
		return ""
	}
	if len(values) == 0 {
		return ids[0]
	}
	xy, ok := sceneXY(map[string]any{"entities": values})
	if !ok {
		return ids[0]
	}
	best := ids[0]
	distance := math.MaxFloat64
	for _, id := range ids {
		c := colorDefinitions(bundle)[id]
		d := math.Abs(c.X-xy[0]) + math.Abs(c.Y-xy[1])
		if d < distance {
			best, distance = id, d
		}
	}
	return best
}

func formXY(values []string) (float64, float64) {
	if len(values) < 5 {
		return 0.5, 0.5
	}
	x, xErr := strconv.ParseFloat(values[3], 64)
	y, yErr := strconv.ParseFloat(values[4], 64)
	if xErr != nil || yErr != nil {
		return 0.5, 0.5
	}
	return x, y
}

func clampXY(value float64) float64 {
	if value < 0.001 {
		return 0.001
	}
	if value > 0.999 {
		return 0.999
	}
	return value
}

func sceneRGB(holiday, color string, values []string) ([3]int, error) {
	if len(values) != 3 {
		return [3]int{}, fmt.Errorf("RGB requires three values")
	}
	if values[0] == "" && values[1] == "" && values[2] == "" {
		if rgb, ok := SuggestedRGB(holiday, color); ok {
			return rgb, nil
		}
		return [3]int{}, fmt.Errorf("unknown holiday color")
	}
	rgb := [3]int{}
	for i, value := range values {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 255 {
			return [3]int{}, fmt.Errorf("RGB out of range")
		}
		rgb[i] = parsed
	}
	return rgb, nil
}

func RenderPrompt(m statusModel) string {
	if m.ciePicker {
		return renderCIEPicker(CIEColor{X: m.ciePickerX, Y: m.ciePickerY})
	}
	message := m.message
	if message != "" {
		message += "\n\n"
	}
	if m.prompt == "new" || m.prompt == "edit" {
		return message + RenderSceneForm(m)
	}
	if m.prompt == "color" {
		return message + RenderColorForm(m)
	}
	if m.prompt == "delete-modal" {
		return m.renderDeleteModal()
	}
	if m.prompt == "quit-confirm" {
		return message + "Unsaved draft changes will be lost. Quit? [y/N]\n> " + m.input + "\n\nEnter confirm | Esc cancel\n"
	}
	if m.prompt == "schedule" {
		return message + RenderScheduleForm(m)
	}
	if m.prompt == "sequence-new" {
		return message + titleStyle.Render("New holiday sequence") + "\n\nEnter the holiday/selector name.\n> " + m.input + "▏\n\nEnter save  Esc cancel\n"
	}
	if m.prompt == "preview" {
		if m.operationLoading {
			return "Applying preview to real lights…\n\nPlease wait. Ctrl+C quits.\n"
		}
		return message + previewConfirmation(m.bundle, m.selectedScene(), m.inventoryStates) + "\nType PREVIEW to apply the selected scene to real lights\n> " + m.input + "\n\nEnter confirm | Esc cancel\n"
	}
	if m.prompt == "restore" {
		return "Restoring captured light state…\n\nPlease wait.\n"
	}
	if m.prompt == "publish" {
		if m.operationLoading {
			return "Publishing draft to Home Assistant…\n\nValidating, backing up, writing, and verifying.\n"
		}
		diff := ""
		if m.baseline != nil {
			if changes, err := Diff(*m.baseline, materializeNativeBundle(m.bundle)); err == nil {
				diff = "\n" + FormatChanges(changes)
			}
		}
		return message + diff + "\nType PUBLISH to publish the draft to Home Assistant\n> " + m.input + "\n\nEnter confirm | Esc cancel\n"
	}
	if m.prompt == "delete-ha" {
		return m.renderDeleteHAModal()
	}
	return message + m.prompt + "\n> " + m.input + "\n\nEnter confirm | Esc cancel\n"
}

func RenderScheduleForm(m statusModel) string {
	labels := []string{"ID", "Name", "Holiday selector", "Window mode", "Anchor month", "Anchor day", "Window start", "Window end"}
	var result strings.Builder
	result.WriteString(titleStyle.Render("Design schedule") + "\n")
	result.WriteString(mutedStyle.Render("All times use Home Assistant's configured timezone") + "\n\n")
	for i, label := range labels {
		value := ""
		if i < len(m.scheduleValues) {
			value = m.scheduleValues[i]
		}
		if i == m.scheduleField {
			value = m.input
		}
		if i == 6 || i == 7 {
			mode := "relative"
			if len(m.scheduleValues) > 3 {
				mode = m.scheduleValues[3]
			}
			if mode == "fixed" {
				label += " (MM-DD)"
			} else {
				label += " (days)"
			}
		}
		if i == 3 && m.scheduleField == i {
			if m.scheduleModeActive {
				value += "  [selecting]"
			} else {
				value += "  [Enter to choose]"
			}
		}
		prefix := "  "
		if i == m.scheduleField {
			prefix = "❯ "
		}
		line := prefix + label + ": " + value
		if i == m.scheduleField {
			result.WriteString(selectedStyle.Render(line+"▏") + "\n")
		} else {
			result.WriteString(line + "\n")
		}
	}
	result.WriteString("\n" + footerStyle.Render("Tab/↑↓ move  Enter edit/next/save  Esc cancel") + "\n")
	return result.String()
}

func RenderSceneForm(m statusModel) string {
	labels := []string{"Holiday", "Color", "Target light(s)", "Brightness"}
	var result strings.Builder
	result.WriteString(titleStyle.Render("Design scene") + "\n")
	result.WriteString(mutedStyle.Render("The scene ID is generated from holiday + color.") + "\n\n")
	for i, label := range labels {
		value := ""
		if i < len(m.formValues) {
			value = m.formValues[i]
		}
		if i == m.formField {
			value = m.input
		}
		if i == 1 {
			if color, ok := colorDefinitions(m.bundle)[value]; ok {
				rgb, _, _ := sceneDisplayRGB(map[string]any{"xy_color": []any{color.X, color.Y}})
				hex := fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
				value = lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render(" ") + " " + color.Name + fmt.Sprintf(" (%.3f, %.3f)", color.X, color.Y)
			}
		}
		if i == 3 {
			value = value + " / 255 (" + formatBrightnessPercent(value) + ")"
		}
		if i == 2 && !m.selectorActive {
			value = targetNames(value, m.inventoryStates)
		}
		if i == 1 || i == 2 {
			if m.formField == i {
				if m.selectorActive {
					value += "  [selecting]"
				} else {
					value += "  [Enter to choose]"
				}
			}
		}
		if i == m.formField {
			result.WriteString(selectedStyle.Render("❯ "+label+": "+value+"▏") + "\n")
		} else {
			result.WriteString("  " + label + ": " + value + "\n")
		}
	}
	id := m.pending
	if id == "" && len(m.formValues) >= 2 {
		id = SuggestSceneID(m.formValues[0], m.formValues[1])
	}
	result.WriteString("\n  Generated ID: " + valueStyle.Render(id) + "\n\n")
	if m.formField == 2 && m.selectorActive {
		result.WriteString(sectionStyle.Render("SELECT LIGHTS") + "\n")
		selected := map[string]bool{}
		for _, entity := range strings.FieldsFunc(m.formValues[2], func(r rune) bool { return r == ';' }) {
			selected[entity] = true
		}
		visible := m.height - 14
		if visible < 4 {
			visible = 4
		}
		start := m.targetSelected - visible/2
		if start < 0 {
			start = 0
		}
		if start+visible > len(m.lightIDs) {
			start = len(m.lightIDs) - visible
			if start < 0 {
				start = 0
			}
		}
		end := start + visible
		if end > len(m.lightIDs) {
			end = len(m.lightIDs)
		}
		for i, entity := range m.lightIDs[start:end] {
			index := start + i
			marker := "[ ]"
			if selected[entity] {
				marker = "[x]"
			}
			prefix := "  "
			if index == m.targetSelected {
				prefix = "❯ "
			}
			result.WriteString(prefix + marker + " " + lightName(m.inventoryStates[entity], entity) + "\n")
		}
		result.WriteString(footerStyle.Render("↑/↓ choose  Space toggle  Enter accept  Esc cancel") + "\n")
	} else {
		result.WriteString(footerStyle.Render("Tab/↑↓ move  Enter choose/next/save  Esc cancel") + "\n")
	}
	return result.String()
}

func formatBrightnessPercent(value string) string {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 || parsed > 255 {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%%", parsed/255*100)
}

func targetNames(value string, states map[string]LightState) string {
	ids := strings.FieldsFunc(value, func(r rune) bool { return r == ';' })
	for i, id := range ids {
		ids[i] = lightName(states[id], id)
	}
	return strings.Join(ids, "; ")
}

func (m statusModel) renderColorScreen() string {
	ids := colorIDs(m.bundle)
	var result strings.Builder
	result.WriteString(titleStyle.Render("Color catalog") + "\n")
	result.WriteString(mutedStyle.Render("Reusable XY colors; scene references stay local to this draft") + "\n\n")
	if len(ids) == 0 {
		result.WriteString(mutedStyle.Render("No colors defined. Press n to add one.\n"))
	}
	visible := m.height - 7
	if visible < 1 {
		visible = 18
	}
	start := m.colorSelected - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(ids) {
		start = len(ids) - visible
		if start < 0 {
			start = 0
		}
	}
	end := start + visible
	if end > len(ids) {
		end = len(ids)
	}
	for i, id := range ids[start:end] {
		realIndex := start + i
		c := colorDefinitions(m.bundle)[id]
		rgb, _, _ := sceneDisplayRGB(map[string]any{"xy_color": []any{c.X, c.Y}})
		hex := fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
		swatch := lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render("  ")
		line := fmt.Sprintf("%s %-24s %-18s (%.3f, %.3f)", swatch, c.Name, id, c.X, c.Y)
		if realIndex == m.colorSelected {
			result.WriteString(selectedStyle.Render("❯ "+line) + "\n")
		} else {
			result.WriteString("  " + line + "\n")
		}
	}
	result.WriteString("\n" + footerStyle.Render("j/k select  Enter/e edit  n new  c/Esc back  q quit") + "\n")
	return result.String()
}

func RenderColorForm(m statusModel) string {
	labels := []string{"Name", "X", "Y"}
	var result strings.Builder
	result.WriteString(titleStyle.Render("Define color") + "\n")
	result.WriteString(mutedStyle.Render("Home Assistant receives XY values; this name is draft-only") + "\n\n")
	for i, label := range labels {
		value := m.formValues[i]
		if i == m.formField {
			value = m.input
		}
		if i == m.formField {
			result.WriteString(selectedStyle.Render("❯ "+label+": "+value+"▏") + "\n")
		} else {
			result.WriteString("  " + label + ": " + value + "\n")
		}
	}
	result.WriteString("\n" + footerStyle.Render("Tab/↑↓ move  p picker  Enter next/save  Esc cancel") + "\n")
	return result.String()
}

func (m statusModel) renderDeleteModal() string {
	var modal strings.Builder
	modal.WriteString(titleStyle.Render("Delete scene") + "\n\n")
	if m.deleteLoading {
		modal.WriteString("Inspecting Home Assistant references…\n")
	} else if m.deleteInspectionErr != "" {
		modal.WriteString("Cannot inspect Home Assistant references:\n")
		modal.WriteString(mutedStyle.Render("  "+m.deleteInspectionErr) + "\n\n")
		modal.WriteString("Deletion is disabled until HA can be inspected.\n")
		modal.WriteString("\nEsc cancel\n")
	} else {
		modal.WriteString("Delete " + valueStyle.Render(m.deleteID) + " from the draft?\n\n")
		if len(m.deleteRefs) == 0 {
			modal.WriteString(mutedStyle.Render("No affected HA entries found.") + "\n")
		} else {
			modal.WriteString("Affected entries:\n")
			visible := m.height - 10
			if visible < 3 {
				visible = 3
			}
			start := m.deleteScroll
			if start > len(m.deleteRefs)-visible {
				start = len(m.deleteRefs) - visible
			}
			if start < 0 {
				start = 0
			}
			end := start + visible
			if end > len(m.deleteRefs) {
				end = len(m.deleteRefs)
			}
			for _, ref := range m.deleteRefs[start:end] {
				modal.WriteString("  • " + string(ref.Kind) + ":" + ref.ID + "\n")
			}
			if len(m.deleteRefs) > visible {
				modal.WriteString(mutedStyle.Render(fmt.Sprintf("  showing %d-%d of %d", start+1, end, len(m.deleteRefs))) + "\n")
			}
		}
		modal.WriteString("\nEnter/y confirm  Esc/n cancel\n")
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 3).Render(modal.String())
	width, height := m.width, m.height
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	background := m.renderDashboard()
	boxWidth, boxHeight := lipgloss.Width(box), lipgloss.Height(box)
	left := (width-boxWidth)/2 + 1
	top := (height-boxHeight)/2 + 1
	if left < 1 {
		left = 1
	}
	if top < 1 {
		top = 1
	}
	return background + fmt.Sprintf("\x1b[%d;%dH%s\x1b[%d;1H", top, left, box, height)
}

func (m statusModel) renderDeleteHAModal() string {
	var modal strings.Builder
	modal.WriteString(titleStyle.Render("Delete from Home Assistant") + "\n\n")
	modal.WriteString("The draft scene was removed. Also delete " + valueStyle.Render(m.pending) + " from HA?\n\n")
	if len(m.deleteRefs) > 0 {
		modal.WriteString("Affected entries:\n")
		for _, ref := range m.deleteRefs {
			modal.WriteString("  • " + string(ref.Kind) + ":" + ref.ID + "\n")
		}
		modal.WriteString("\n")
	}
	modal.WriteString("Type DELETE HA to approve the HA deletion\n> " + m.input + "\n\nEnter confirm  Esc retain HA scene\n")
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 3).Render(modal.String())
	width, height := m.width, m.height
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	background := m.renderDashboard()
	left := (width-lipgloss.Width(box))/2 + 1
	top := (height-lipgloss.Height(box))/2 + 1
	if left < 1 {
		left = 1
	}
	if top < 1 {
		top = 1
	}
	return background + fmt.Sprintf("\x1b[%d;%dH%s\x1b[%d;1H", top, left, box, height)
}

func (m *statusModel) restorePreview() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	err := m.preview.Restore(ctx)
	cancel()
	if err != nil {
		m.message = "Restore failed: " + err.Error()
	} else {
		m.message = "Preview state restored."
	}
}

func formatRefs(refs []ConfigRef) string {
	refs = append([]ConfigRef(nil), refs...)
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind == refs[j].Kind {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].Kind < refs[j].Kind
	})
	values := make([]string, 0, len(refs))
	for _, ref := range refs {
		values = append(values, string(ref.Kind)+":"+ref.ID)
	}
	return strings.Join(values, ", ")
}
