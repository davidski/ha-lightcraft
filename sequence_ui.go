package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m statusModel) updateSequenceView(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	holidays := sequenceHolidays(m.bundle)
	if m.sequenceAddView {
		ids := SceneIDs(m.bundle)
		switch key.String() {
		case "esc", "a":
			m.sequenceAddView = false
		case "up", "k":
			if m.sequenceAddSelected > 0 {
				m.sequenceAddSelected--
			}
		case "down", "j":
			if m.sequenceAddSelected+1 < len(ids) {
				m.sequenceAddSelected++
			}
		case "enter":
			if len(ids) > 0 && len(holidays) > 0 {
				holiday := holidays[m.sequenceHoliday]
				values := append([]string(nil), holidaySequences(m.bundle)[holiday]...)
				values = append(values, ids[m.sequenceAddSelected])
				if next, err := replaceHolidaySequence(m.bundle, holiday, values); err != nil {
					m.message = "Sequence update failed: " + err.Error()
				} else {
					m.bundle, m.dirty, m.message = next, true, "Added "+ids[m.sequenceAddSelected]+" to "+holiday+"."
					m.sequenceSelected = len(values) - 1
				}
				m.sequenceAddView = false
			}
		case "q":
			return m, tea.Quit
		}
		return m, nil
	}
	if len(holidays) == 0 {
		if key.String() == "n" {
			m.prompt, m.input = "sequence-new", ""
		}
		if key.String() == "esc" || key.String() == "h" {
			m.sequenceView = false
		}
		return m, nil
	}
	if m.sequenceHoliday >= len(holidays) {
		m.sequenceHoliday = len(holidays) - 1
	}
	values := holidaySequences(m.bundle)[holidays[m.sequenceHoliday]]
	switch key.String() {
	case "esc", "h":
		m.sequenceView = false
	case "left", "up":
		if key.String() == "left" && m.sequenceHoliday > 0 {
			m.sequenceHoliday--
			m.sequenceSelected = 0
		} else if key.String() == "up" && m.sequenceSelected > 0 {
			m.sequenceSelected--
		}
	case "right":
		if m.sequenceHoliday+1 < len(holidays) {
			m.sequenceHoliday++
			m.sequenceSelected = 0
		}
	case "down", "j":
		if m.sequenceSelected+1 < len(values) {
			m.sequenceSelected++
		}
	case "a":
		if len(SceneIDs(m.bundle)) > 0 {
			m.sequenceAddView, m.sequenceAddSelected = true, 0
		}
	case "n":
		m.prompt, m.input = "sequence-new", ""
	case "x", "backspace":
		if m.sequenceSelected < len(values) {
			values = append(values[:m.sequenceSelected], values[m.sequenceSelected+1:]...)
			if next, err := replaceHolidaySequence(m.bundle, holidays[m.sequenceHoliday], values); err != nil {
				m.message = "Sequence update failed: " + err.Error()
			} else {
				m.bundle, m.dirty, m.message = next, true, "Removed entry from "+holidays[m.sequenceHoliday]+"."
				if m.sequenceSelected >= len(values) && m.sequenceSelected > 0 {
					m.sequenceSelected--
				}
			}
		}
	case "[", "]":
		if m.sequenceSelected < len(values) {
			other := m.sequenceSelected - 1
			if key.String() == "]" {
				other = m.sequenceSelected + 1
			}
			if other >= 0 && other < len(values) {
				values[m.sequenceSelected], values[other] = values[other], values[m.sequenceSelected]
				if next, err := replaceHolidaySequence(m.bundle, holidays[m.sequenceHoliday], values); err != nil {
					m.message = "Sequence update failed: " + err.Error()
				} else {
					m.bundle, m.dirty, m.message = next, true, "Reordered "+holidays[m.sequenceHoliday]+"."
					m.sequenceSelected = other
				}
			}
		}
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m statusModel) renderSequenceScreen() string {
	holidays := sequenceHolidays(m.bundle)
	if m.sequenceAddView {
		return m.renderSequenceAddScreen(holidays)
	}
	var result strings.Builder
	result.WriteString(titleStyle.Render("Holiday sequences") + "\n")
	result.WriteString(mutedStyle.Render("Managed by the holiday_lights HA script; order and duplicates are significant") + "\n\n")
	if m.message != "" {
		result.WriteString(m.message + "\n\n")
	}
	if len(holidays) == 0 {
		result.WriteString(mutedStyle.Render("No sequences defined. Press b on the dashboard to install managed holiday infrastructure.") + "\n")
		return result.String() + "\n" + footerStyle.Render("ESC/h back  q quit") + "\n"
	}
	if m.sequenceHoliday >= len(holidays) {
		m.sequenceHoliday = len(holidays) - 1
	}
	holiday := holidays[m.sequenceHoliday]
	result.WriteString(sectionStyle.Render("HOLIDAY") + "  " + valueStyle.Render(holiday) + fmt.Sprintf("  (%d/%d)\n\n", m.sequenceHoliday+1, len(holidays)))
	values := holidaySequences(m.bundle)[holiday]
	if len(values) == 0 {
		result.WriteString(mutedStyle.Render("  Empty sequence") + "\n")
	}
	for i, sceneID := range values {
		line := fmt.Sprintf("  %d. %-28s", i+1, sequenceSceneName(m.bundle, sceneID))
		if colorRef := colorRefForScene(m.bundle, sceneID); colorRef != "" {
			if color, ok := colorDefinitions(m.bundle)[colorRef]; ok {
				rgb, _, _ := sceneDisplayRGB(map[string]any{"xy_color": []any{color.X, color.Y}})
				hex := fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
				line += "  " + lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render(" ") + " " + color.Name
			}
		}
		if i == m.sequenceSelected {
			line = "❯" + strings.TrimPrefix(line, " ")
			result.WriteString(selectedStyle.Render(line) + "\n")
		} else {
			result.WriteString(line + "\n")
		}
	}
	result.WriteString("\n" + footerStyle.Render("←/→ holiday  j/k entry  [/] move  ESC back  q quit\n"+"a add  x remove  n new") + "\n")
	return result.String()
}

func (m statusModel) renderSequenceAddScreen(holidays []string) string {
	ids := SceneIDs(m.bundle)
	var result strings.Builder
	result.WriteString(titleStyle.Render("Add scene to sequence") + "\n\n")
	if len(holidays) > 0 {
		result.WriteString("Holiday: " + valueStyle.Render(holidays[m.sequenceHoliday]) + "\n\n")
	}
	for i, id := range ids {
		prefix := "  "
		if i == m.sequenceAddSelected {
			prefix = "❯ "
		}
		line := prefix + sequenceSceneName(m.bundle, id)
		if i == m.sequenceAddSelected {
			result.WriteString(selectedStyle.Render(line) + "\n")
		} else {
			result.WriteString(line + "\n")
		}
	}
	result.WriteString("\n" + footerStyle.Render("j/k select  Enter add  ESC cancel") + "\n")
	return result.String()
}

func sequenceSceneName(bundle Bundle, sceneID string) string {
	config, ok := bundle.Files[Scenes]
	if ok {
		if values, ok := config.Data.([]any); ok {
			for _, raw := range values {
				if entry, ok := raw.(map[string]any); ok && entry["id"] == sceneID {
					if name, ok := entry["name"].(string); ok && name != "" {
						return name
					}
				}
			}
		}
	}
	return sceneID
}
