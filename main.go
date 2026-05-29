package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"svn-tui/internal/svn"
	"svn-tui/internal/ui"
)

func main() {
	repos := svn.LoadRepos()

	p := tea.NewProgram(
		ui.NewModel(repos),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "svntui:", err)
		os.Exit(1)
	}
}
