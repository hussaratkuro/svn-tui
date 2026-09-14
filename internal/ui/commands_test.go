package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"svn-tui/internal/model"
)

func TestCommandPaletteFiltersSVNActions(t *testing.T) {
	m := NewModel([]model.Repo{{Path: "/repo"}})
	m.activeRepo = model.Repo{Path: "/repo"}
	m.screen = model.ScreenActionSelect
	m.openCommandPalette()
	m.commands.query = "br df tr"
	items := m.filteredCommands()
	if len(items) != 1 || items[0].kind != commandAction || items[0].index != int(model.ActionBranchDiffVsTrunk) {
		t.Fatalf("branch diff command match = %#v", items)
	}
	updated, _ := m.updateCommandPalette(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).commands.open {
		t.Fatal("Esc did not close command palette")
	}
}
