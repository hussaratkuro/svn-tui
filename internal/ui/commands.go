package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"svn-tui/internal/model"
	"svn-tui/internal/svn"
)

type commandKind uint8

const (
	commandAction commandKind = iota
	commandRepository
	commandShowActions
	commandShowRepositories
	commandToggleInfo
	commandBack
	commandQuit
)

type commandItem struct {
	kind  commandKind
	index int
	label string
	hint  string
}

type commandPalette struct {
	open   bool
	query  string
	cursor int
	items  []commandItem
}

func (m *Model) openCommandPalette() {
	items := []commandItem{
		{kind: commandShowRepositories, label: "Select repository", hint: "repos"},
		{kind: commandToggleInfo, label: "Toggle repository details", hint: "i"},
		{kind: commandBack, label: "Back to previous screen", hint: "Esc"},
		{kind: commandQuit, label: "Quit svn-tui", hint: "Ctrl+C"},
	}
	if m.activeRepo.Path != "" {
		items = append([]commandItem{{kind: commandShowActions, label: "Show action menu", hint: "actions"}}, items...)
		for index, label := range m.actions {
			items = append(items, commandItem{kind: commandAction, index: index, label: "SVN: " + label, hint: "action"})
		}
	} else {
		for index, repo := range m.repos {
			items = append(items, commandItem{kind: commandRepository, index: index, label: "Open repository: " + repo.Path, hint: "repo"})
		}
	}
	m.commands = commandPalette{open: true, items: items}
}

func (m Model) filteredCommands() []commandItem {
	var result []commandItem
	for _, item := range m.commands.items {
		if fuzzyCommandMatch(item.label+" "+item.hint, m.commands.query) {
			result = append(result, item)
		}
	}
	return result
}

func fuzzyCommandMatch(label, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	label = strings.ToLower(label)
	position := 0
	for _, character := range query {
		found := strings.IndexRune(label[position:], character)
		if found < 0 {
			return false
		}
		position += found + 1
	}
	return true
}

func (m Model) updateCommandPalette(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.filteredCommands()
	switch key.String() {
	case "esc", "ctrl+p", "ctrl+shift+p":
		m.commands = commandPalette{}
	case "ctrl+c":
		return m, tea.Quit
	case "up", "ctrl+k":
		m.commands.cursor = max(0, m.commands.cursor-1)
	case "down", "ctrl+j", "tab":
		m.commands.cursor = min(max(0, len(items)-1), m.commands.cursor+1)
	case "backspace":
		query := []rune(m.commands.query)
		if len(query) > 0 {
			m.commands.query = string(query[:len(query)-1])
			m.commands.cursor = 0
		}
	case "enter":
		if len(items) == 0 {
			return m, nil
		}
		selected := items[min(m.commands.cursor, len(items)-1)]
		m.commands = commandPalette{}
		return m.executeCommand(selected)
	default:
		if key.Type == tea.KeyRunes && !key.Alt {
			for _, character := range key.Runes {
				if unicode.IsPrint(character) {
					m.commands.query += string(character)
				}
			}
			m.commands.cursor = 0
		}
	}
	return m, nil
}

func (m Model) executeCommand(item commandItem) (tea.Model, tea.Cmd) {
	switch item.kind {
	case commandAction:
		if m.activeRepo.Path != "" && item.index >= 0 && item.index < len(m.actions) {
			return m.runAction(model.Action(item.index))
		}
	case commandRepository:
		if item.index >= 0 && item.index < len(m.repos) {
			m.repoCursor = item.index
			m.activeRepo = m.repos[item.index]
			m.activeRepo.CurrentLocation = svn.GetCurrentLocation(m.activeRepo)
			m.activeRepo.CurrentRevision = svn.GetCurrentRevision(m.activeRepo)
			m.repos[item.index].CurrentLocation = m.activeRepo.CurrentLocation
			m.repos[item.index].CurrentRevision = m.activeRepo.CurrentRevision
			m.actionCursor, m.actionOffset = 0, 0
			m.screen = model.ScreenActionSelect
		}
	case commandShowActions:
		m.screen = model.ScreenActionSelect
	case commandShowRepositories:
		m.screen = model.ScreenRepoSelect
	case commandToggleInfo:
		if m.screen != model.ScreenRepoSelect {
			m.showInfo = !m.showInfo
			if m.screen == model.ScreenDiff {
				m.syncDiffViewportSize()
			}
		}
	case commandBack:
		return m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	case commandQuit:
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewCommandPalette() string {
	width := min(max(64, m.width*3/4), max(20, m.width-4))
	items := m.filteredCommands()
	limit := min(len(items), max(3, m.height-10))
	start := max(0, min(m.commands.cursor-limit/2, len(items)-limit))
	rows := []string{
		titleStyle.Render("Command palette"),
		mutedStyle.Render("Fuzzy-search repositories and SVN actions"),
		"", labelYellowStyle.Render("> ") + valueWhiteStyle.Render(m.commands.query+"_"), "",
	}
	if len(items) == 0 {
		rows = append(rows, mutedStyle.Render("No matching commands"))
	}
	for index := start; index < start+limit; index++ {
		item := items[index]
		labelWidth := max(8, width-22)
		label := truncateCommand(item.label, labelWidth)
		line := label + strings.Repeat(" ", max(1, labelWidth-lipgloss.Width(label))) + item.hint
		style := normalStyle
		if index == m.commands.cursor {
			style = actionSelectedStyle
		}
		rows = append(rows, style.Width(width-6).Render(line))
	}
	rows = append(rows, "", mutedStyle.Render("Enter runs · ↑/↓ selects · Esc closes"))
	box := styleBorder.Padding(1, 2).Width(width).Render(strings.Join(rows, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func truncateCommand(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return fmt.Sprintf("%s…", string(runes))
}
