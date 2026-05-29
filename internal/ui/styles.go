package ui

import "github.com/charmbracelet/lipgloss"

// Catppuccin Mocha
var (
	catMauve     = lipgloss.Color("#cba6f7")
	catRosewater = lipgloss.Color("#f5e0dc")
	catOverlay0  = lipgloss.Color("#6c7086")
	catOverlay1  = lipgloss.Color("#7f849c")
	catRed       = lipgloss.Color("#f38ba8")
	catGreen     = lipgloss.Color("#a6e3a1")
	catYellow    = lipgloss.Color("#f9e2af")
	catText      = lipgloss.Color("#cdd6f4")
	catSubtext0  = lipgloss.Color("#a6adc8")
	catSapphire  = lipgloss.Color("#74c7ec")
	catTeal      = lipgloss.Color("#94e2d5")
	catSurface1  = lipgloss.Color("#45475a")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(catMauve)

	selectedStyle = lipgloss.NewStyle().
			Foreground(catMauve).
			Bold(true)

	mutedStyle = lipgloss.NewStyle().
			Foreground(catOverlay0)

	normalStyle = lipgloss.NewStyle().
			Foreground(catSubtext0)

	textStyle = lipgloss.NewStyle().
			Foreground(catText)

	errorStyle = lipgloss.NewStyle().
			Foreground(catRed).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(catGreen).
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(catYellow).
			Bold(true)

	checkboxStyle = lipgloss.NewStyle().
			Foreground(catOverlay1)

	checkedStyle = lipgloss.NewStyle().
			Foreground(catYellow).
			Bold(true)

	labelMauveStyle = lipgloss.NewStyle().
			Foreground(catMauve).
			Bold(true)

	labelRedStyle = lipgloss.NewStyle().
			Foreground(catRed).
			Bold(true)

	labelYellowStyle = lipgloss.NewStyle().
				Foreground(catYellow).
				Bold(true)

	labelSapphireStyle = lipgloss.NewStyle().
				Foreground(catSapphire).
				Bold(true)

	valueWhiteStyle = lipgloss.NewStyle().
			Foreground(catRosewater)

	actionStyle = lipgloss.NewStyle().
			Foreground(catTeal)

	actionSelectedStyle = lipgloss.NewStyle().
				Foreground(catMauve).
				Bold(true)

	diffAddedStyle = lipgloss.NewStyle().
			Foreground(catGreen)

	diffDeletedStyle = lipgloss.NewStyle().
				Foreground(catRed)

	diffModifiedStyle = lipgloss.NewStyle().
				Foreground(catYellow)

	diffSameStyle = lipgloss.NewStyle().
			Foreground(catSubtext0)

	// Header bar
	headerBarStyle = lipgloss.NewStyle().
			Background(catSurface1).
			Foreground(catText).
			Width(0) // width set per render

	// Content area border (sshm-style)
	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(catSurface1).
			PaddingLeft(1)

	// Status bar at the bottom
	statusBarStyle = lipgloss.NewStyle().
			Foreground(catOverlay0)

	statusKeyStyle = lipgloss.NewStyle().
			Foreground(catMauve).
			Bold(true)
)

// hint formats a single key:description pair for the status bar.
func hint(key, desc string) string {
	return statusKeyStyle.Render(key) + statusBarStyle.Render(":"+desc)
}
