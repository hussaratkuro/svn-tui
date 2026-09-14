package ui

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"svn-tui/internal/theme"
)

type diffAppearance uint8

const (
	diffAppearanceColor diffAppearance = iota
	diffAppearanceMono
)

type diffPalette struct {
	added, deleted, modified, conflict lipgloss.Color
}

func resolveDiffPalette(p theme.Palette) (diffPalette, diffAppearance) {
	themed := diffPalette{added: p.Green, deleted: p.Red, modified: p.Yellow, conflict: p.Mauve}
	fallbackTheme := theme.CatppuccinMocha()
	fallback := diffPalette{
		added: fallbackTheme.Green, deleted: fallbackTheme.Red,
		modified: fallbackTheme.Yellow, conflict: fallbackTheme.Mauve,
	}

	switch strings.ToLower(strings.TrimSpace(os.Getenv("TUI_DIFF_THEME"))) {
	case "wallbash", "theme":
		return themed, diffAppearanceColor
	case "semantic", "color", "colour":
		return fallback, diffAppearanceColor
	case "mono", "monochrome":
		mono := diffPalette{added: p.Text, deleted: p.Text, modified: p.Text, conflict: p.Text}
		return mono, diffAppearanceMono
	default:
		if diffPaletteIsIndistinct(themed) {
			return fallback, diffAppearanceColor
		}
		return themed, diffAppearanceColor
	}
}

func diffPaletteIsIndistinct(p diffPalette) bool {
	colors := []lipgloss.Color{p.added, p.deleted, p.modified, p.conflict}
	maximumChroma := 0
	for _, color := range colors {
		rgb, ok := parseRGB(color)
		if !ok {
			return true
		}
		maximumChroma = max(maximumChroma, max(rgb[0], rgb[1], rgb[2])-min(rgb[0], rgb[1], rgb[2]))
	}
	if maximumChroma < 32 {
		return true
	}
	for left := 0; left < len(colors); left++ {
		for right := left + 1; right < len(colors); right++ {
			if colorDistanceSquared(colors[left], colors[right]) < 20*20 {
				return true
			}
		}
	}
	return false
}

func parseRGB(color lipgloss.Color) ([3]int, bool) {
	value := strings.TrimPrefix(string(color), "#")
	if len(value) != 6 {
		return [3]int{}, false
	}
	parsed, err := strconv.ParseUint(value, 16, 24)
	if err != nil {
		return [3]int{}, false
	}
	return [3]int{int(parsed >> 16), int(parsed >> 8 & 0xff), int(parsed & 0xff)}, true
}

func colorDistanceSquared(left, right lipgloss.Color) int {
	l, lok := parseRGB(left)
	r, rok := parseRGB(right)
	if !lok || !rok {
		return 0
	}
	dr, dg, db := l[0]-r[0], l[1]-r[1], l[2]-r[2]
	return dr*dr + dg*dg + db*db
}
