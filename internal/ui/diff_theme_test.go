package ui

import (
	"testing"

	"svn-tui/internal/theme"
)

func TestAutoDiffThemeFallsBackForMonochromePalette(t *testing.T) {
	t.Setenv("TUI_DIFF_THEME", "")
	p := theme.CatppuccinMocha()
	p.Green, p.Red, p.Yellow, p.Mauve = "#B2B2B2", "#B2B2B2", "#B2B2B2", "#B2B2B2"

	got, appearance := resolveDiffPalette(p)
	want := theme.CatppuccinMocha()
	if appearance != diffAppearanceColor || got.added != want.Green || got.deleted != want.Red || got.modified != want.Yellow || got.conflict != want.Mauve {
		t.Fatalf("auto monochrome palette = %#v / %v, want semantic fallback", got, appearance)
	}
}

func TestWallbashAndMonoDiffOverrides(t *testing.T) {
	p := theme.CatppuccinMocha()
	p.Green, p.Red, p.Yellow, p.Mauve, p.Text = "#AAAAAA", "#AAAAAA", "#AAAAAA", "#AAAAAA", "#EEEEEE"

	t.Setenv("TUI_DIFF_THEME", "wallbash")
	got, appearance := resolveDiffPalette(p)
	if appearance != diffAppearanceColor || got.added != p.Green || got.deleted != p.Red {
		t.Fatalf("wallbash override did not preserve theme colors: %#v / %v", got, appearance)
	}

	t.Setenv("TUI_DIFF_THEME", "mono")
	got, appearance = resolveDiffPalette(p)
	if appearance != diffAppearanceMono || got.added != p.Text || got.deleted != p.Text || got.modified != p.Text || got.conflict != p.Text {
		t.Fatalf("mono override = %#v / %v", got, appearance)
	}
}
