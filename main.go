package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"svn-tui/internal/credential"
	"svn-tui/internal/svn"
	"svn-tui/internal/ui"
)

func main() {
	configs := svn.LoadRepoConfigs()
	if credential.Required(configs) {
		password, err := credential.PromptVaultPassword()
		if err != nil {
			fmt.Fprintln(os.Stderr, "svntui:", err)
			os.Exit(1)
		}
		configs, err = credential.Resolve(configs, password)
		password = ""
		if err != nil {
			fmt.Fprintln(os.Stderr, "svntui:", err)
			os.Exit(1)
		}
	}
	repos := svn.BuildRepos(configs)

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
