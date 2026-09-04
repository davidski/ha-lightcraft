package main

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type dashboardHelp struct {
	Open key.Binding
	Move key.Binding
	Quit key.Binding
}

func (dashboardHelp) ShortHelp() []key.Binding {
	return []key.Binding{dashboardKeys.Move, dashboardKeys.Open, dashboardKeys.Quit}
}
func (dashboardHelp) FullHelp() [][]key.Binding {
	return [][]key.Binding{{dashboardKeys.Move, dashboardKeys.Open}, {dashboardKeys.Quit}}
}

var dashboardKeys = dashboardHelp{
	Move: key.NewBinding(key.WithKeys("j", "k", "←", "→"), key.WithHelp("j/k ←/→", "navigate")),
	Open: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	Quit: key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}

func dashboardHelpView(width int) string {
	model := help.New()
	model.Width = width
	return model.View(dashboardKeys)
}
