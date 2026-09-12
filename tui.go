package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi/kitty"
)

//go:embed favicon.png
var projectLogoPNG []byte

var projectLogoOnce struct {
	sync.Once
	view string
}

const (
	projectLogoSize    = 128
	projectLogoColumns = 16
	projectLogoRows    = 8
)

var projectLogoID = os.Getpid()

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
	planner                bool
	lighting               lightingEditor
	scheduleSelected       int
	bundle                 Bundle
	draftDir               string
	filePaths              map[ConfigKind][]string
	colorsDir              string
	baseline               *Bundle
	baselineImported       bool
	diff                   bool
	prompt                 string
	input                  string
	confirmFocus           int
	message                string
	stateAPI               StateAPI
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
	inventoryMetadata      map[string]HAEntityMetadata
	inventoryStatus        HAInventoryStatus
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
	sequenceHoliday        int
	lightIDs               []string
	formValues             []string
	formField              int
	operationLoading       bool
	sequencePreview        bool
	previewSequence        ColorSequence
	previewStep            int
	previewElapsed         int
	loadingSpinner         spinner.Model
	ciePicker              bool
	ciePickerX             float64
	ciePickerY             float64
	dashboardFocus         int
	dashboardWorkspace     int // 1 sequences, 4 schedules, 2 colors, 3 YAML
	contentView            bool
	contentKind            int
	contentScroll          int
	agendaView             bool
	agendaDate             time.Time
	dirty                  bool
	helpView               bool
	clearProjectLogo       bool
	diffScroll             int
}

var workspaceOrder = []int{2, 1, 4, 3} // colors, sequences, schedules, YAML

func (m statusModel) saveDraftFiles() (tea.Model, tea.Cmd) {
	if m.draftDir == "" {
		m.message = "Draft directory is not configured."
		return m, nil
	}
	if err := SaveBundleAtWithReferences(m.draftDir, m.colorsDir, m.bundle, m.filePaths); err != nil {
		m.message = "Save failed: " + err.Error()
	} else {
		m.dirty = false
		m.message = "Draft saved."
	}
	return m, nil
}

func (m statusModel) saveWithS() (tea.Model, tea.Cmd) {
	if m.operationLoading || m.lighting.picker {
		return m, nil
	}
	if m.ciePicker {
		m.formValues[1] = fmt.Sprintf("%.4f", m.ciePickerX)
		m.formValues[2] = fmt.Sprintf("%.4f", m.ciePickerY)
		m.ciePicker = false
		m.formField = 2
		m.input = m.formValues[2]
	}
	if m.prompt == "color" {
		m.formValues[m.formField] = m.input
		m.finishColorForm()
		if m.prompt != "" {
			return m, nil
		}
	} else if m.prompt != "" {
		return m, nil
	}
	if m.planner && m.lighting.form != "" {
		if m.lighting.form == "pause" || m.lighting.form == "resume" {
			return m, nil
		}
		m.saveLightingForm()
		if m.lighting.form != "" {
			return m, nil
		}
		return m.saveLightingDraft()
	}
	return m.saveDraftFiles()
}

func workspacePosition(workspace int) int {
	for i, value := range workspaceOrder {
		if value == workspace {
			return i
		}
	}
	return 0
}
func workspaceAt(position int) int {
	if position < 0 {
		position = 0
	}
	if position >= len(workspaceOrder) {
		position = len(workspaceOrder) - 1
	}
	return workspaceOrder[position]
}

func projectLogoSupported() bool {
	return os.Getenv("TERM") == "xterm-ghostty" || os.Getenv("TERM") == "xterm-kitty" || strings.EqualFold(os.Getenv("TERM_PROGRAM"), "ghostty")
}

func projectLogo() string {
	if !projectLogoSupported() {
		return ""
	}
	projectLogoOnce.Do(func() {
		source, err := png.Decode(bytes.NewReader(projectLogoPNG))
		if err != nil {
			return
		}
		source = transparentLogo(source)
		logo := image.NewRGBA(image.Rect(0, 0, projectLogoSize, projectLogoSize))
		for y := range projectLogoSize {
			for x := range projectLogoSize {
				sx := source.Bounds().Min.X + x*source.Bounds().Dx()/projectLogoSize
				sy := source.Bounds().Min.Y + y*source.Bounds().Dy()/projectLogoSize
				logo.Set(x, y, source.At(sx, sy))
			}
		}
		var encoded bytes.Buffer
		if err := kitty.EncodeGraphics(&encoded, logo, &kitty.Options{
			Action:          kitty.TransmitAndPut,
			Format:          kitty.PNG,
			Transmission:    kitty.Direct,
			Chunk:           true,
			ID:              projectLogoID,
			Columns:         projectLogoColumns,
			Rows:            projectLogoRows,
			DoNotMoveCursor: true,
			Quite:           2,
		}); err == nil {
			projectLogoOnce.view = encoded.String()
		}
	})
	return projectLogoOnce.view
}

func transparentLogo(source image.Image) image.Image {
	bounds := source.Bounds()
	logo := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := source.At(x, y).RGBA()
			logo.SetRGBA(x, y, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)})
		}
	}
	queue := make([]image.Point, 0, bounds.Dx()+bounds.Dy())
	remove := func(x, y int) {
		if x < bounds.Min.X || x >= bounds.Max.X || y < bounds.Min.Y || y >= bounds.Max.Y {
			return
		}
		pixel := logo.RGBAAt(x, y)
		if pixel.A == 0 || pixel.R > 12 || pixel.G > 12 || pixel.B > 12 {
			return
		}
		pixel.A = 0
		logo.SetRGBA(x, y, pixel)
		queue = append(queue, image.Point{X: x, Y: y})
	}
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		remove(x, bounds.Min.Y)
		remove(x, bounds.Max.Y-1)
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		remove(bounds.Min.X, y)
		remove(bounds.Max.X-1, y)
	}
	for len(queue) > 0 {
		point := queue[0]
		queue = queue[1:]
		remove(point.X-1, point.Y)
		remove(point.X+1, point.Y)
		remove(point.X, point.Y-1)
		remove(point.X, point.Y+1)
	}
	return logo
}

func clearProjectLogo() string {
	if projectLogoOnce.view == "" {
		return ""
	}
	return fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2;\x1b\\", projectLogoID)
}

type inventoryResultMsg struct {
	states      map[string]LightState
	locations   map[string]LightLocation
	metadata    map[string]HAEntityMetadata
	status      HAInventoryStatus
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

func (m statusModel) Init() tea.Cmd {
	return m.loadingSpinner.Tick
}

func (m statusModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if tick, ok := message.(spinner.TickMsg); ok {
		if len(m.loadingSpinner.Spinner.Frames) == 0 {
			m.loadingSpinner = spinner.New()
		}
		var cmd tea.Cmd
		m.loadingSpinner, cmd = m.loadingSpinner.Update(tick)
		return m, cmd
	}
	if result, ok := message.(lightingControlMsg); ok {
		m.operationLoading = false
		if result.err != nil {
			m.lighting.error = result.err.Error()
		} else {
			m.lighting.form, m.lighting.error = "", ""
			m.message = "Playback " + map[string]string{"paused": "paused", "idle": "resumed according to its schedule"}[result.option] + " in Home Assistant."
		}
		return m, nil
	}
	if inventory, ok := message.(inventoryResultMsg); ok {
		if inventory.request != m.inventoryRequest {
			return m, nil
		}
		if m.planner {
			m.inventoryStates, m.inventoryLocations, m.inventoryMetadata = inventory.states, inventory.locations, inventory.metadata
			m.inventoryStatus = inventory.status
			if inventory.err != nil {
				m.lighting.error = "Discovery failed; enter light IDs manually: " + inventory.err.Error()
			} else if inventory.status.Error != "" {
				m.lighting.error = "Home Assistant entity registry unavailable; refresh inventory: " + inventory.status.Error
			} else {
				m.lighting.error = ""
			}
			return m, nil
		}
		if !m.inventory {
			return m, nil
		}
		m.inventoryLoading = false
		if inventory.err != nil {
			m.inventoryStates, m.inventoryLocations, m.inventoryMetadata = nil, nil, nil
			m.inventoryStatus = inventory.status
			m.message = "Inventory failed: " + inventory.err.Error()
			return m, nil
		}
		m.inventoryStates, m.inventoryLocations, m.inventoryMetadata = inventory.states, inventory.locations, inventory.metadata
		m.inventoryStatus = inventory.status
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
		} else if inventory.status.Error != "" {
			m.inventoryLocationError = inventory.status.Error
		}
		m.message = ""
		return m, nil
	}
	if result, ok := message.(operationResultMsg); ok {
		m.operationLoading = false
		if result.err != nil {
			m.message = result.err.Error()
			return m, nil
		} else {
			m.message = "Published, verified, and backed up."
			if result.baseline != nil {
				m.bundle.SourceHash = result.baseline.SourceHash
				m.refs = bundleRefs(*result.baseline)
			}
			if result.warning != "" {
				m.message += " " + result.warning
			}
			if m.draftDir != "" {
				if err := SaveBundleAtWithReferences(m.draftDir, m.colorsDir, m.bundle, m.filePaths); err != nil {
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
	if _, ok := message.(sequencePreviewTickMsg); ok && m.sequencePreview {
		if len(m.previewSequence.Steps) == 0 {
			m.sequencePreview = false
			return m, nil
		}
		step := m.previewSequence.Steps[m.previewStep]
		m.previewElapsed++
		hold := int(math.Ceil(step.Hold))
		if hold < 1 {
			hold = 1
		}
		if m.previewElapsed >= hold {
			m.previewElapsed = 0
			if m.previewStep+1 < len(m.previewSequence.Steps) {
				m.previewStep++
			} else if m.previewSequence.Repeat {
				m.previewStep = 0
			}
		}
		return m, sequencePreviewTickCmd()
	}
	if size, ok := message.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		return m, nil
	}
	if key, ok := message.(tea.KeyMsg); ok {
		if m.clearProjectLogo {
			m.clearProjectLogo = false
		}
		if key.String() == "q" && m.dirty && m.prompt == "" && (!m.planner || m.lighting.form == "") {
			m.prompt, m.input, m.confirmFocus = "quit-confirm", "", 1
			return m, nil
		}
		if key.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if key.String() == "s" {
			return m.saveWithS()
		}
		if m.sequencePreview {
			if key.String() == "esc" || key.String() == "p" {
				m.sequencePreview = false
			}
			return m, nil
		}
		if m.prompt == "quit-confirm" {
			return m.updatePrompt(key)
		}
		if m.planner && key.String() == "q" && m.lighting.form == "" {
			return m, tea.Quit
		}
		if m.planner {
			if m.prompt != "" {
				return m.updatePrompt(key)
			}
			return m.updateLighting(key)
		}
		if m.helpView {
			if key.String() == "esc" || key.String() == "?" {
				m.helpView, m.clearProjectLogo = false, true
			}
			return m, nil
		}
		if m.ciePicker {
			return m.updateCIEPicker(key)
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
				m.inventorySection = 0
			case "left", "h":
				if m.inventoryFocus == 1 {
					m.inventoryFocus = 0
				}
			case "right", "l":
				if m.inventoryFocus == 0 {
					m.inventoryFocus = 1
					m.inventorySection = 0
				}
			case "up":
				if m.inventoryFocus == 1 {
					if m.inventorySection > 0 {
						m.inventorySection--
					}
				} else if m.inventorySelected > 0 {
					m.inventorySelected--
					m.inventoryDetailScroll = 0
				}
			case "k":
				if m.inventoryFocus == 0 {
					if m.inventorySelected > 0 {
						m.inventorySelected--
						m.inventoryDetailScroll = 0
					}
				} else if m.inventoryDetailScroll > 0 {
					m.inventoryDetailScroll--
				}
			case "down":
				if m.inventoryFocus == 1 {
					if m.inventorySection < 1 {
						m.inventorySection++
					}
				} else if m.inventorySelected+1 < len(ids) {
					m.inventorySelected++
					m.inventoryDetailScroll = 0
				}
			case "j":
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
					m.prompt, m.pending, m.input, m.confirmFocus = "delete-color", id, "", 1
				}
			}
			return m, nil
		}
		if m.agendaView {
			switch key.String() {
			case "esc", "a":
				m.agendaView = false
			case "left", "h":
				m.agendaDate = normalizeAgendaDate(m.agendaDate).AddDate(0, 0, -1)
			case "right", "l":
				m.agendaDate = normalizeAgendaDate(m.agendaDate).AddDate(0, 0, 1)
			case "up", "k":
				m.agendaDate = normalizeAgendaDate(m.agendaDate).AddDate(0, 0, -7)
			case "down", "j":
				m.agendaDate = normalizeAgendaDate(m.agendaDate).AddDate(0, 0, 7)
			case "?":
				m.helpView, m.clearProjectLogo = true, false
			case "q", "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}
		if m.contentView {
			kinds := nativeDraftKinds(m.bundle)
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
		if m.diff {
			switch key.String() {
			case "q":
				return m, tea.Quit
			case "esc", "d":
				m.diff = false
			case "up", "k":
				if m.diffScroll > 0 {
					m.diffScroll--
				}
			case "down", "j":
				m.diffScroll++
			}
			return m, nil
		}
		if m.prompt != "" {
			if m.prompt == "color" {
				return m.updateColorForm(key)
			}
			return m.updatePrompt(key)
		}
		switch key.String() {
		case "?":
			m.helpView, m.clearProjectLogo = true, false
		case "a":
			m.agendaView, m.agendaDate, m.message = true, normalizeAgendaDate(time.Now()), ""
		case "q":
			return m, tea.Quit
		case "d":
			if m.baseline != nil {
				m.diff = !m.diff
				m.diffScroll = 0
			}
		case "i":
			if m.stateAPI == nil {
				m.message = "Inventory unavailable: set HOMEASSISTANT_URL and HOMEASSISTANT_TOKEN."
				break
			}
			m.inventory, m.inventoryLoading = true, true
			m.inventoryRequest++
			m.message = ""
			return m, loadInventoryCmd(m.stateAPI, m.inventoryRequest)
		case "1", "2", "3", "4":
			m.dashboardWorkspace = workspaceAt(int(key.String()[0] - '1'))
			m.dashboardFocus = 1
		case "left", "h":
			if position := workspacePosition(m.dashboardWorkspace); position > 0 {
				m.dashboardWorkspace = workspaceAt(position - 1)
				if m.dashboardFocus != 0 {
					m.dashboardFocus = 1
				}
			}
		case "right", "l":
			if position := workspacePosition(m.dashboardWorkspace); position < len(workspaceOrder)-1 {
				m.dashboardWorkspace = workspaceAt(position + 1)
				if m.dashboardFocus != 0 {
					m.dashboardFocus = 1
				}
			}
		case "tab":
			m.dashboardFocus = 1 - m.dashboardFocus
		case "up", "k":
			if m.dashboardFocus == 0 {
				if position := workspacePosition(m.dashboardWorkspace); position > 0 {
					m.dashboardWorkspace = workspaceAt(position - 1)
				}
			} else {
				switch m.dashboardWorkspace {
				case 4:
					if m.scheduleSelected > 0 {
						m.scheduleSelected--
					}
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
				}
			}
		case "down", "j":
			if m.dashboardFocus == 0 {
				if position := workspacePosition(m.dashboardWorkspace); position < len(workspaceOrder)-1 {
					m.dashboardWorkspace = workspaceAt(position + 1)
				}
			} else {
				switch m.dashboardWorkspace {
				case 4:
					if m.scheduleSelected+1 < len(lightingAssignments(m.bundle)) {
						m.scheduleSelected++
					}
				case 1:
					if m.sequenceHoliday+1 < len(colorSequences(m.bundle)) {
						m.sequenceHoliday++
					}
				case 2:
					if m.colorSelected+1 < len(colorIDs(m.bundle)) {
						m.colorSelected++
					}
				case 3:
					if m.contentKind+1 < len(nativeDraftKinds(m.bundle)) {
						m.contentKind++
					}
				}
			}
		case "enter":
			if m.dashboardFocus == 0 {
				m.dashboardFocus = 1
			} else if m.dashboardWorkspace == 3 {
				m.openPublishPrompt(false)
			} else if m.dashboardWorkspace == 1 {
				ids := lightingIDs(m.bundle, 0)
				if m.sequenceHoliday < len(ids) {
					m.openLighting(0)
					m.lighting.selected = m.sequenceHoliday
					m.lighting.returnDashboard = true
					m.lighting.sequence, m.lighting.steps = colorSequences(m.bundle)[ids[m.sequenceHoliday]], true
				}
			} else if m.dashboardWorkspace == 4 {
				ids := lightingIDs(m.bundle, 1)
				if m.scheduleSelected < len(ids) {
					m.openLighting(1)
					m.lighting.selected = m.scheduleSelected
					m.lighting.returnDashboard = true
					m.editLightingAssignment(ids[m.scheduleSelected])
				}
			} else if m.dashboardWorkspace == 2 {
				ids := colorIDs(m.bundle)
				if m.colorSelected < len(ids) {
					id := ids[m.colorSelected]
					color := colorDefinitions(m.bundle)[id]
					m.colorView = false
					m.prompt, m.pending, m.formField = "color", id, 0
					m.formValues = []string{color.Name, fmt.Sprintf("%.3f", color.X), fmt.Sprintf("%.3f", color.Y)}
					m.input = m.formValues[0]
				}
			}
		case "n":
			if m.dashboardWorkspace == 4 {
				m.openLighting(1)
				m.editLightingAssignment("")
				break
			}
			if m.dashboardWorkspace == 3 {
				m.message = "Publish is read-only here; use Colors, Sequences, and Schedules to edit."
				break
			}
			if m.dashboardWorkspace == 1 {
				m.openLighting(0)
				m.editLightingSequence("")
				break
			}
			if m.dashboardWorkspace == 2 {
				m.colorView, m.colorSelected = false, 0
				m.prompt, m.pending, m.formField = "color", "", 0
				m.formValues = []string{"New color", "0.500", "0.333"}
				m.input = m.formValues[0]
				break
			}
		case "x":
			if m.dashboardWorkspace == 4 {
				m.message = "Open a schedule and set it to disabled to retire it safely."
				break
			}
			if m.dashboardWorkspace == 2 {
				ids := colorIDs(m.bundle)
				if m.colorSelected < len(ids) {
					id := ids[m.colorSelected]
					m.prompt, m.pending, m.input, m.confirmFocus = "delete-color", id, "", 1
				}
				break
			}
			switch m.dashboardWorkspace {
			case 1:
				ids := lightingIDs(m.bundle, 0)
				if m.sequenceHoliday < len(ids) {
					id := ids[m.sequenceHoliday]
					m.prompt, m.pending, m.input, m.confirmFocus = "delete-sequence", id, "", 1
				}
			case 2:
				m.colorView = true
			case 3:
				m.message = "Publish is read-only here; use Colors, Sequences, and Schedules to edit."
			}
		case "u":
			m.openPublishPrompt(true)
		case "p":
			if m.dashboardWorkspace == 1 && m.dashboardFocus == 1 {
				ids := lightingIDs(m.bundle, 0)
				if m.sequenceHoliday < len(ids) {
					m.sequencePreview, m.previewSequence, m.previewStep, m.previewElapsed = true, colorSequences(m.bundle)[ids[m.sequenceHoliday]], 0, 0
					return m, sequencePreviewTickCmd()
				}
				m.message = "No color sequence is selected."
			}
		}
	}
	return m, nil
}

func (m statusModel) View() string {
	if m.prompt != "" {
		return RenderPrompt(m)
	}
	if m.sequencePreview {
		return m.renderSequencePreview()
	}
	if m.planner {
		return m.renderLighting()
	}
	if m.helpView {
		return renderHelp(m.width, m.height)
	}
	if m.agendaView {
		return m.renderAgenda()
	}
	if m.contentView {
		return m.renderContentScreen()
	}
	if m.diff && m.baseline != nil {
		_, err := PublishDiff(*m.baseline, m.bundle)
		if err != nil {
			return "Diff error: " + err.Error() + "\n"
		}
		return m.renderScrollable("Home Assistant publish preview", m.publishPreview(), m.diffScroll, "d/ESC back • j/k scroll • q quit")
	}
	if m.inventory {
		return m.renderInventoryScreen()
	}
	if m.colorView {
		return m.renderColorScreen()
	}
	return m.withMessage(m.renderDashboard())
}

func renderHelp(width, height int) string {
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	text := titleStyle.Render("HA Lightcraft — Help") + "\n\n" +
		sectionStyle.Render("Dashboard") + "\n" +
		"  Tab           focus workspace navigation / active pane\n" +
		"  hjkl/↑↓←→     navigate\n" +
		"  1/2/3/4       select workspaces\n" +
		"  a             open the 14-day schedule agenda\n" +
		"  Enter         open or edit the selected entry\n" +
		"  n             create an entry in the active workspace\n" +
		"  x             delete selected entry\n" +
		"  u             publish to Home Assistant\n" +
		"  s             save draft\n" +
		"  d             show file changes in unified diff format\n\n" +
		sectionStyle.Render("Editors") + "\n" +
		"  Tab/↑↓        move between fields\n" +
		"  Enter         enter/accept selectors; finish the active form\n" +
		"  p             open the CIE xy color picker from a color form\n" +
		"  Space         toggle a light in the target selector\n" +
		"  ESC           cancel or go back\n\n" +
		mutedStyle.Render("Draft-only changes are marked *unsaved.  Press ? or ESC to return.")
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 3).Render(text)
	if height >= 30 {
		if logo := projectLogo(); logo != "" {
			left := (width - projectLogoColumns) / 2
			if left < 0 {
				left = 0
			}
			return fmt.Sprintf("\x1b[%dC", left) + logo + "\r" + strings.Repeat("\n", projectLogoRows) + lipgloss.Place(width, height-projectLogoRows, lipgloss.Center, lipgloss.Center, box)
		}
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
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
	titleLine := titleStyle.Render(title)
	if status := m.draftStatus(); status != "" {
		titleLine += "  " + mutedStyle.Render("• "+status)
	}
	return titleLine + "\n\n" + strings.Join(lines[scroll:end], "\n") + "\n\n" + footerStyle.Render(footer+"\n")
}

func (m statusModel) renderDashboard() string {
	width := m.width
	if width < 60 {
		width = 80
	}
	workspaces := []struct {
		label string
		count int
	}{
		{"Colors", len(colorIDs(m.bundle))},
		{"Sequences", len(colorSequences(m.bundle))},
		{"Schedules", len(lightingAssignments(m.bundle))},
		{"Publish", 0},
	}
	var tabs strings.Builder
	for i, workspace := range workspaces {
		line := workspace.label
		if m.dashboardWorkspace == workspaceOrder[i] {
			if m.dashboardFocus == 0 {
				line = selectedStyle.Render("[" + line + "]")
			} else {
				line = valueStyle.Underline(true).Render(line)
			}
		}
		tabs.WriteString(line)
		if i < len(workspaces)-1 {
			tabs.WriteString("   ")
		}
	}
	workflowBar := sectionStyle.Render("WORKFLOW") + "  " + mutedStyle.Render("Colors → Sequences → Schedules → Agenda → Publish") + "  " + borderStyle.Render("│") + "  " + mutedStyle.Render("Inventory")
	workspaceBar := sectionStyle.Render("EDIT WORKSPACES") + "  " + tabs.String()
	content := m.renderDashboardWorkspace(width)

	draftLabel := "draft: " + m.draftDir
	if status := m.draftStatus(); status != "" {
		draftLabel += " • " + status
	}
	header := titleStyle.Render("HA Lightcraft") + "  " + mutedStyle.Render(draftLabel)
	rule := borderStyle.Render(strings.Repeat("─", width))
	body := lipgloss.NewStyle().Width(width).Render(content)
	footerLines := []string{}
	if m.dashboardWorkspace == 1 && m.dashboardFocus == 1 {
		if ids := lightingIDs(m.bundle, 0); m.sequenceHoliday < len(ids) {
			footerLines = append(footerLines, "p • preview selected sequence")
		}
	}
	actions := "n new • a agenda • "
	if m.dashboardWorkspace == 1 || m.dashboardWorkspace == 2 {
		actions += "x delete • "
	}
	actions += "i inventory • s save • d diff • u publish • ? help • q quit"
	footerLines = append(footerLines,
		"Tab focus • hjkl/↑↓←→ navigate • 1–4 jump • Enter open",
		actions,
	)
	footer := footerStyle.Render(strings.Join(footerLines, "\n"))
	clear := ""
	if m.clearProjectLogo {
		clear = clearProjectLogo()
	}
	return clear + header + "\n" + rule + "\n" + workflowBar + "\n" + workspaceBar + "\n" + rule + "\n" + body + "\n" + rule + "\n" + footer + "\n"
}

func (m statusModel) renderAgenda() string {
	width := m.width
	if width < 60 {
		width = 80
	}
	days := scheduleAgenda(lightingAssignments(m.bundle), normalizeAgendaDate(m.agendaDate))
	var content strings.Builder
	content.WriteString(sectionStyle.Render("SCHEDULE AGENDA") + "\n")
	content.WriteString(mutedStyle.Render("Enabled schedules in the next 14 days") + "\n\n")
	for _, day := range days {
		entries := make([]string, 0, len(day.Items))
		for _, item := range day.Items {
			entry := item.Name + " (" + item.Time
			if item.Targets != "" {
				entry += " · " + item.Targets
			}
			entries = append(entries, entry+")")
		}
		value := mutedStyle.Render("—")
		if len(entries) > 0 {
			value = strings.Join(entries, "  •  ")
		}
		line := fmt.Sprintf("%-3s %-12s  %s", day.Weekday, day.Date, value)
		content.WriteString(lipgloss.NewStyle().MaxWidth(width).Render(line) + "\n")
	}
	footer := "a/ESC back • h/l previous/next day • j/k previous/next week • ? help"
	clear := ""
	if m.clearProjectLogo {
		clear = clearProjectLogo()
	}
	return clear + m.withMessage(m.renderScrollable("Schedule agenda", content.String(), 0, footer))
}

func (m statusModel) draftStatus() string {
	status := []string{}
	if m.dirty {
		status = append(status, "unsaved in memory")
	}
	if m.baselineImported && m.baseline != nil {
		if changes, err := PublishDiff(*m.baseline, m.bundle); err == nil && len(changes) > 0 {
			status = append(status, "unpublished")
		}
	}
	return strings.Join(status, " • ")
}

func (m statusModel) renderDashboardWorkspace(width int) string {
	var result strings.Builder
	switch m.dashboardWorkspace {
	case 4:
		result.WriteString(sectionStyle.Render("SCHEDULES") + "\n\n")
		ids := lightingIDs(m.bundle, 1)
		if len(ids) == 0 {
			result.WriteString(mutedStyle.Render("No schedules defined.") + "\n")
		}
		for i, id := range ids {
			a := lightingAssignments(m.bundle)[id]
			line := fmt.Sprintf("  %s · %s → %s", a.Name, a.Start, a.End)
			if i == m.scheduleSelected && m.dashboardFocus == 1 {
				line = selectedStyle.Render("❯ " + strings.TrimPrefix(line, "  "))
			}
			result.WriteString(line + "\n")
		}
		result.WriteString("\nCreate a color-sequence or WLED schedule with its own dates and hours.\nThe same sequence or WLED program can have several schedules.\n")
		return result.String()
	case 1:
		result.WriteString(sectionStyle.Render("COLOR SEQUENCES") + "\n\n")
		sequences := colorSequences(m.bundle)
		ids := lightingIDs(m.bundle, 0)
		if len(ids) == 0 {
			result.WriteString(mutedStyle.Render("No color sequences defined.") + "\n")
		}
		nameWidth := 0
		for _, id := range ids {
			nameWidth = max(nameWidth, len(sequences[id].Name))
		}
		for i, id := range ids {
			s := sequences[id]
			line := fmt.Sprintf("  %-*s  %d steps", nameWidth, s.Name, len(s.Steps))
			if i == m.sequenceHoliday && m.dashboardFocus == 1 {
				line = selectedStyle.Render("❯ " + strings.TrimPrefix(line, "  "))
			}
			result.WriteString(line + "\n")
		}
		return result.String()
	case 2:
		ids := colorIDs(m.bundle)
		result.WriteString(sectionStyle.Render("COLORS") + "\n")
		result.WriteString(mutedStyle.Render("Named CIE xy colors reusable by sequences.") + "\n\n")
		for i, id := range ids {
			color := colorDefinitions(m.bundle)[id]
			line := "  " + strings.TrimPrefix(renderCatalogColor(color), "Color: ")
			if i == m.colorSelected && m.dashboardFocus == 1 {
				line = strings.Replace(line, color.Name, selectedStyle.Render(color.Name), 1)
				line = selectedStyle.Render("❯ " + strings.TrimPrefix(line, "  "))
			}
			result.WriteString(line + "\n")
		}
		if len(ids) == 0 {
			result.WriteString(mutedStyle.Render("  No colors. Press n to create one.") + "\n")
		}
	case 3:
		packageName, ok := packagePath(m.filePaths)
		if !ok && len(m.filePaths) == 0 {
			packageName, ok = defaultPackagePath, true
		}
		result.WriteString(sectionStyle.Render("PUBLISH") + "\n")
		result.WriteString(mutedStyle.Render("Review the current Home Assistant package and unified diff before publishing.") + "\n\n")
		if ok {
			line := "  " + packageName
			if m.dashboardFocus == 1 {
				line = selectedStyle.Render("❯ " + strings.TrimPrefix(line, "  "))
			}
			result.WriteString(line + "\n")
		} else {
			result.WriteString(mutedStyle.Render("  No Home Assistant package configured.") + "\n")
		}
	}
	return result.String()
}

func (m statusModel) renderContentScreen() string {
	kinds := nativeDraftKinds(m.bundle)
	if len(kinds) == 0 {
		return "No draft content.\n\nESC back\n"
	}
	contentKind := m.contentKind
	if contentKind < 0 {
		contentKind = 0
	}
	if contentKind >= len(kinds) {
		contentKind = len(kinds) - 1
	}
	kind := kinds[contentKind]
	data, err := marshalConfigYAML(kind, m.bundle.Files[kind].Data)
	if err != nil {
		return "Cannot render draft content: " + err.Error() + "\n\nESC back\n"
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
	body.WriteString(titleStyle.Render("Home Assistant file: "+nativeDraftName(kind, m.filePaths)) + "\n")
	body.WriteString(mutedStyle.Render(fmt.Sprintf("%s · %d entries · working copy only", kind, draftEntryCount(m.bundle.Files[kind].Data))) + "\n\n")
	for _, line := range lines[start:end] {
		body.WriteString(line + "\n")
	}
	body.WriteString("\n" + footerStyle.Render("j/k scroll • Tab/h/l change file • ESC back • q quit") + "\n")
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

func renderLightColor(value map[string]any) string {
	rgb, source, ok := displayRGB(value)
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
	rgb, _, _ := displayRGB(map[string]any{"xy_color": []any{color.X, color.Y}})
	hex := fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
	luminance := (0.299*float64(rgb[0]) + 0.587*float64(rgb[1]) + 0.114*float64(rgb[2])) / 255
	foreground := "255"
	if luminance > 0.55 {
		foreground = "0"
	}
	swatch := lipgloss.NewStyle().Foreground(lipgloss.Color(foreground)).Background(lipgloss.Color(hex)).Padding(0, 1).Render(" " + hex + " ")
	return "Color: " + swatch + "  " + color.Name + fmt.Sprintf(" (XY %.3f, %.3f)", color.X, color.Y)
}

func displayRGB(value map[string]any) ([3]int, string, bool) {
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
		rgb, _, valid := displayRGB(map[string]any{"xy_color": []any{xy[0], xy[1]}})
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

func clampByte(value float64) int {
	return int(math.Round(math.Max(0, math.Min(255, value))))
}

func (m statusModel) withMessage(view string) string {
	if m.message == "" {
		return view
	}
	return m.message + "\n\n" + view
}

func loadInventoryCmd(api StateAPI, request uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		states, inventory, status, err := fetchInventory(ctx, api)
		if !status.StatesReady {
			return inventoryResultMsg{request: request, status: status, err: err}
		}
		return inventoryResultMsg{states: states, locations: inventory.Locations, metadata: inventory.Metadata, status: status, request: request, locationErr: err}
	}
}

func sequencePreviewTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return sequencePreviewTickMsg{} })
}

func (m statusModel) renderSequencePreview() string {
	if len(m.previewSequence.Steps) == 0 {
		return "No sequence steps to preview.\n\nESC back\n"
	}
	step := currentCatalogStep(m.bundle, m.previewSequence.Steps[m.previewStep])
	hex := previewRGBHex(step)
	swatch := lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render("          ")
	var result strings.Builder
	result.WriteString(titleStyle.Render("Sequence preview: "+m.previewSequence.Name) + "\n")
	fmt.Fprintf(&result, "Step %d/%d · %s · brightness %s · hold %.1fs\n\n", m.previewStep+1, len(m.previewSequence.Steps), step.Name, previewBrightness(step.Brightness), step.Hold)
	fmt.Fprintf(&result, "Color  %s  XY (%.3f, %.3f)  RGB %s\n\n", swatch, step.XY[0], step.XY[1], hex)
	result.WriteString(sectionStyle.Render("LIGHTS") + "\n")
	ids := sequencePreviewTargetIDs(m.bundle, m.previewSequence.ID)
	if len(ids) == 0 {
		result.WriteString(mutedStyle.Render("No schedules target this sequence yet.") + "\n")
	}
	for _, id := range ids {
		name := id
		if state, ok := m.inventoryStates[id]; ok {
			name = lightName(state, id)
		}
		fmt.Fprintf(&result, "  %-28s %s  brightness %s\n", name, swatch, previewBrightness(step.Brightness))
	}
	result.WriteString("\n" + footerStyle.Render("Colors cycle by their hold times • ESC/p stop") + "\n")
	return result.String()
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
	leftWidth := width / 2
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
	visible := m.height - 12
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
					lines = append(lines, "    "+line)
				}
			}
		}
		maxLines := m.height - 12
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
	footer := footerStyle.Render("Tab focus • ↑/↓ section/list • j/k scroll • ←/→/h/l pane • Space collapse • i/ESC back • q quit")
	return titleStyle.Render("Home Assistant Light Inventory") + "\n" + mutedStyle.Render("Inspect each light's capabilities for sequence schedules") + "\n" + rule + "\n" + body + "\n" + rule + "\n" + footer + "\n"
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
		text := "    " + lightName(states[id], id) + " · " + lightTypeLabel(states[id])
		if id == selectedID {
			text = selectedStyle.Render("  ❯ " + lightName(states[id], id) + " · " + lightTypeLabel(states[id]))
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
	colorLines = append(colorLines, renderLightColor(state.Attribute))
	return []inventorySection{
		{key: "overview", title: "OVERVIEW", lines: []string{
			"State: " + state.State,
			"Brightness: " + formatBrightness(state.Attribute["brightness"]),
			"Type: " + lightTypeLabel(state),
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
	result.WriteString("Capabilities available for scheduled sequences:\n\n")
	for _, entityID := range ids {
		state := states[entityID]
		modes := stringList(state.Attribute["supported_color_modes"])
		effects := stringList(state.Attribute["effect_list"])
		result.WriteString(valueStyle.Render(entityID) + " · " + lightTypeLabel(state) + "\n")
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

func (m statusModel) updatePrompt(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.operationLoading {
		return m, nil
	}
	deletePrompt := m.prompt == "delete-color" || m.prompt == "delete-sequence"
	if m.prompt == "quit-confirm" {
		switch key.String() {
		case "esc":
			m.prompt, m.input, m.confirmFocus = "", "", 0
			return m, nil
		case "left", "h", "up", "k", "shift+tab":
			m.confirmFocus = 0
			return m, nil
		case "right", "l", "down", "j", "tab":
			m.confirmFocus = 1
			return m, nil
		case "enter":
			if m.confirmFocus == 0 {
				return m, tea.Quit
			}
			m.prompt, m.input, m.confirmFocus = "", "", 0
			return m, nil
		default:
			return m, nil
		}
	}
	if deletePrompt {
		switch key.String() {
		case "esc":
			m.prompt, m.pending, m.input, m.confirmFocus = "", "", "", 0
			return m, nil
		case "left", "h", "up", "k", "shift+tab":
			m.confirmFocus = 0
			return m, nil
		case "right", "l", "down", "j", "tab":
			m.confirmFocus = 1
			return m, nil
		case "enter":
			if m.confirmFocus == 1 {
				m.prompt, m.pending, m.input, m.confirmFocus = "", "", "", 0
				return m, nil
			}
		default:
			return m, nil
		}
	}
	switch key.String() {
	case "esc":
		m.prompt, m.input = "", ""
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case "enter":
		if m.prompt == "delete-color" {
			next, err := deleteColor(m.bundle, m.pending)
			if err != nil {
				m.message = "Color delete failed: " + err.Error()
			} else {
				m.bundle, m.dirty, m.message = next, true, "Deleted color "+m.pending+"."
				if m.colorSelected >= len(colorIDs(next)) && m.colorSelected > 0 {
					m.colorSelected--
				}
			}
			m.prompt, m.pending, m.input, m.confirmFocus = "", "", "", 0
			return m, nil
		}
		if m.prompt == "delete-sequence" {
			next, err := deleteColorSequence(m.bundle, m.pending)
			if err != nil {
				m.message = "Sequence delete failed: " + err.Error()
			} else {
				m.bundle, m.dirty, m.message = next, true, "Deleted sequence "+m.pending+"."
				ids := lightingIDs(next, 0)
				if m.planner {
					if m.lighting.selected >= len(ids) && m.lighting.selected > 0 {
						m.lighting.selected--
					}
				} else if m.sequenceHoliday >= len(ids) && m.sequenceHoliday > 0 {
					m.sequenceHoliday--
				}
			}
			m.prompt, m.pending, m.input, m.confirmFocus = "", "", "", 0
			return m, nil
		}
		if m.prompt == "publish" && m.input == "PUBLISH" {
			m.operationLoading = true
			return m, runPublishCmd(m.store, m.bundle, *m.baseline, m.refs, m.deletes, m.backup)
		}
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

func runPublishCmd(store ConfigPublisher, draft, baseline Bundle, refs, deletes []ConfigRef, backup string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := store.Publish(ctx, draft, baseline, refs, deletes, backup); err != nil {
			return operationResultMsg{operation: "publish", err: fmt.Errorf("Publish failed: %w", err)}
		}
		result := operationResultMsg{operation: "publish"}
		if importer, ok := store.(interface {
			ReadAll(context.Context) (Bundle, error)
		}); ok {
			refreshed, err := importer.ReadAll(ctx)
			if err != nil {
				result.warning = "Could not refresh the publish baseline: " + err.Error()
			} else {
				result.baseline = &refreshed
			}
		} else if importer, ok := store.(interface {
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

func (m statusModel) updateColorForm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "p" && m.formField > 0 {
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
	}
	m.bundle, m.message = next, "Color saved: "+id
	m.dirty = true
	m.prompt, m.input, m.pending, m.formValues = "", "", "", nil
}

func (m *statusModel) openPublishPrompt(requireStore bool) {
	if requireStore && (m.store == nil || m.baseline == nil || !m.baselineImported) {
		m.message = "Publish requires an imported baseline and HA credentials."
		return
	}
	if m.baseline == nil {
		m.message = "Publish preview requires a baseline."
		return
	}
	changes, err := PublishDiff(*m.baseline, m.bundle)
	if err != nil {
		m.message = "Diff failed: " + err.Error()
	} else if len(changes) == 0 {
		m.message = "No changes to publish."
	} else {
		m.prompt, m.input = "publish", ""
	}
}

func (m statusModel) publishPreview() string {
	packageName, ok := packagePath(m.filePaths)
	if !ok && len(m.filePaths) == 0 {
		packageName, ok = defaultPackagePath, true
	}
	if !ok {
		return "Publish preview unavailable: configure one Home Assistant package file."
	}
	current, err := marshalPackageYAML(materializeNativeBundle(m.bundle))
	if err != nil {
		return "Publish preview unavailable: " + err.Error()
	}
	diff := "No changes."
	if m.baseline != nil {
		if value, diffErr := formatPackageDiff(*m.baseline, m.bundle, packageName); diffErr == nil {
			diff = value
		} else {
			return "Publish preview unavailable: " + diffErr.Error()
		}
	}
	return titleStyle.Render("File to publish: "+packageName) + "\n\n" + strings.TrimSuffix(string(current), "\n") + "\n\n" + titleStyle.Render("Changes to publish") + "\n\n" + strings.TrimSuffix(diff, "\n")
}

func RenderPrompt(m statusModel) string {
	if m.ciePicker {
		return renderCIEPicker(CIEColor{X: m.ciePickerX, Y: m.ciePickerY})
	}
	message := m.message
	if message != "" {
		message += "\n\n"
	}
	if m.prompt == "color" {
		return message + RenderColorForm(m)
	}
	if m.prompt == "delete-color" || m.prompt == "delete-sequence" {
		kind := "color"
		if m.prompt == "delete-sequence" {
			kind = "sequence"
		}
		button := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
		focusedButton := button.Foreground(lipgloss.Color("212")).Bold(true)
		deleteButton, cancelButton := button.Render("Delete"), button.Render("Cancel")
		if m.confirmFocus == 0 {
			deleteButton = focusedButton.Render("Delete")
		} else {
			cancelButton = focusedButton.Render("Cancel")
		}
		buttons := lipgloss.JoinHorizontal(lipgloss.Top, deleteButton, "  ", cancelButton)
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 3).Render(
			titleStyle.Render("Delete "+kind) + "\n\nDelete " + valueStyle.Render(m.pending) + " from the draft?\n\n" + buttons + "\n\nh/l or ←/→ choose  Enter select  ESC cancel")
		width, height := m.width, m.height
		if width < 1 {
			width = 80
		}
		if height < 1 {
			height = 24
		}
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}
	if m.prompt == "quit-confirm" {
		return m.renderQuitModal()
	}
	if m.prompt == "publish" {
		if m.operationLoading {
			return "Publishing draft to Home Assistant…\n\nValidating, backing up, writing, and verifying.\n"
		}
		return message + "\n" + m.publishPreview() + "\n\nType PUBLISH to publish the draft to Home Assistant\n> " + m.input + "\n\nEnter confirm | ESC cancel\n"
	}
	return message + m.prompt + "\n> " + m.input + "\n\nEnter confirm | ESC cancel\n"
}

func (m statusModel) renderQuitModal() string {
	button := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	focusedButton := button.Foreground(lipgloss.Color("212")).Bold(true)
	quitButton, keepButton := button.Render("Quit"), button.Render("Keep editing")
	if m.confirmFocus == 0 {
		quitButton = focusedButton.Render("Quit")
	} else {
		keepButton = focusedButton.Render("Keep editing")
	}
	buttons := lipgloss.JoinHorizontal(lipgloss.Top, quitButton, "  ", keepButton)
	modal := titleStyle.Render("Quit with unsaved changes") + "\n\n" +
		"Unsaved draft changes will be lost. Quit?\n\n" +
		buttons + "\n\n" +
		"h/l or ←/→ choose  Enter select  ESC cancel"
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 3).Render(modal)
	width, height := m.width, m.height
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func (m statusModel) renderColorScreen() string {
	ids := colorIDs(m.bundle)
	var result strings.Builder
	result.WriteString(titleStyle.Render("Color catalog") + "\n")
	result.WriteString(mutedStyle.Render("Reusable XY colors for sequence steps") + "\n\n")
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
		rgb, _, _ := displayRGB(map[string]any{"xy_color": []any{c.X, c.Y}})
		hex := fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
		swatch := lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render("  ")
		line := fmt.Sprintf("%s %-24s %-18s (%.3f, %.3f)", swatch, c.Name, id, c.X, c.Y)
		if realIndex == m.colorSelected {
			result.WriteString(selectedStyle.Render("❯ "+line) + "\n")
		} else {
			result.WriteString("  " + line + "\n")
		}
	}
	result.WriteString("\n" + footerStyle.Render("j/k select • Enter/e edit • n new • x delete • c/Esc back • q quit") + "\n")
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
	result.WriteString("\n" + footerStyle.Render("Tab/↑↓ move • p picker • Enter next/finish • s save draft • Esc cancel") + "\n")
	return result.String()
}
