package ui

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"svn-tui/internal/model"
)

func TestSplitLinesWithMixedLFAndCRLF(t *testing.T) {
	lines, crlf := splitLinesWithCRLF("alpha\r\nbeta\ngamma\r\ndelta")
	if want := []string{"alpha", "beta", "gamma", "delta"}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %#v, want %#v", lines, want)
	}
	if want := []bool{true, false, true, false}; !reflect.DeepEqual(crlf, want) {
		t.Fatalf("CRLF flags = %#v, want %#v", crlf, want)
	}
}

func TestSideBySideDiffStaysWithinViewportForMixedEOL(t *testing.T) {
	oldText := "alpha\r\nbeta\twith a longer value\ngamma\r\n"
	newText := "alpha\nbeta\tchanged\r\ngamma\n"

	for _, width := range []int{20, 40, 55, 80, 121} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			rendered := stripANSI(renderSideBySideBody(oldText, newText, "OLD", "NEW", width))
			for lineNumber, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
				if got := lipgloss.Width(line); got != width {
					t.Fatalf("line %d has visible width %d, want %d:\n%q", lineNumber+1, got, width, line)
				}
			}
		})
	}
}

func TestCRLFBadgeDoesNotDiscardWrappedText(t *testing.T) {
	// At an 80-column viewport each left cell used to be 33 columns wide.
	// Adding the three-column CR badge after wrapping silently dropped XYZ.
	oldText := strings.Repeat("a", 30) + "XYZ\r\n"
	newText := "short\n"
	rendered := stripANSI(renderSideBySideBody(oldText, newText, "OLD", "NEW", 80))
	if !strings.Contains(rendered, "XYZ") {
		t.Fatalf("rendered CRLF line lost its suffix:\n%s", rendered)
	}
}

func TestHasFinalNewlineAcceptsSupportedLineEndings(t *testing.T) {
	for _, text := range []string{"line\n", "line\r\n", "line\r"} {
		if !hasFinalNewline(text) {
			t.Fatalf("hasFinalNewline(%q) = false, want true", text)
		}
	}
	if hasFinalNewline("line") {
		t.Fatal("hasFinalNewline without a line ending = true, want false")
	}
}

func TestSideBySideDiffKeepsEntireFile(t *testing.T) {
	oldText := "first\n" + strings.Repeat("unchanged\n", 12) + "old\nlast\n"
	newText := "first\n" + strings.Repeat("unchanged\n", 12) + "new\nlast\n"
	rendered := stripANSI(renderSideBySideBody(oldText, newText, "OLD", "NEW", 80))

	if strings.Contains(rendered, "...") {
		t.Fatalf("full-file diff unexpectedly contains a collapsed section:\n%s", rendered)
	}
	if got := strings.Count(rendered, "unchanged"); got != 24 {
		t.Fatalf("rendered unchanged cell count = %d, want 24", got)
	}
}

func TestClassifySideBySideDiffLines(t *testing.T) {
	rendered := renderSideBySideBody(
		"same\nremoved\nanchor\nchanged old\nlast\n",
		"same\nanchor\nchanged new\nadded\nlast\n",
		"OLD", "NEW", 80,
	)
	kinds := classifyDiffLines(rendered)

	var added, deleted, modified int
	for _, kind := range kinds {
		switch kind {
		case diffLineAdded:
			added++
		case diffLineDeleted:
			deleted++
		case diffLineModified:
			modified++
		}
	}
	if added != 1 || deleted != 1 || modified != 1 {
		t.Fatalf("change counts = added %d, deleted %d, modified %d; want 1 each", added, deleted, modified)
	}
}

func TestNextDiffChange(t *testing.T) {
	starts := []int{5, 12, 20}
	for _, tc := range []struct {
		name      string
		offset    int
		direction int
		want      int
		ok        bool
	}{
		{"first down", 0, 1, 5, true},
		{"next down", 5, 1, 12, true},
		{"current up", 14, -1, 12, true},
		{"previous up", 12, -1, 5, true},
		{"no next", 20, 1, 0, false},
		{"no previous", 5, -1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := nextDiffChange(starts, tc.offset, tc.direction)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("nextDiffChange() = (%d, %t), want (%d, %t)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestDiffOverviewUsesChangeColours(t *testing.T) {
	kinds := []diffLineKind{diffLineSame, diffLineAdded, diffLineSame, diffLineDeleted, diffLineModified}
	overview := renderDiffOverview(kinds, len(kinds), 0, 2)

	if !strings.Contains(overview, diffAddedStyle.Render("+")) {
		t.Fatal("overview does not contain an added marker")
	}
	if !strings.Contains(overview, diffDeletedStyle.Render("-")) {
		t.Fatal("overview does not contain a deleted marker")
	}
	if !strings.Contains(overview, diffModifiedStyle.Render("~")) {
		t.Fatal("overview does not contain a modified marker")
	}
}

func TestWrappedDiffLineStaysInItsChangeBlock(t *testing.T) {
	rendered := renderSideBySideBody(
		"a very long old value that must wrap\nanchor\n",
		"a very long new value that must wrap\nanchor\n",
		"OLD", "NEW", 30,
	)
	kinds := classifyDiffLines(rendered)
	starts := diffChangeStarts(kinds)
	if len(starts) != 1 {
		t.Fatalf("wrapped modification produced %d change blocks, want 1", len(starts))
	}
}

func TestAltNavigationRemembersChangeNearViewportBottom(t *testing.T) {
	kinds := make([]diffLineKind, 100)
	kinds[10] = diffLineAdded
	kinds[99] = diffLineDeleted
	vp := viewport.New(74, 13)
	vp.SetContent(strings.Repeat("line\n", 100))
	m := Model{
		screen:           model.ScreenDiff,
		width:            80,
		height:           20,
		viewport:         vp,
		diffLineKinds:    kinds,
		diffChangeCursor: -1,
	}

	pressAlt := func(key tea.KeyType) {
		updated, _ := m.updateDiffScreen(tea.KeyMsg{Type: key, Alt: true})
		m = updated.(Model)
	}
	pressAlt(tea.KeyDown)
	if m.viewport.YOffset != 10 {
		t.Fatalf("first Alt+Down offset = %d, want 10", m.viewport.YOffset)
	}
	pressAlt(tea.KeyDown)
	if m.diffChangeCursor != 1 {
		t.Fatalf("last change cursor = %d, want 1", m.diffChangeCursor)
	}
	if m.viewport.YOffset >= 99 {
		t.Fatalf("test setup did not clamp the last change offset: got %d", m.viewport.YOffset)
	}
	pressAlt(tea.KeyUp)
	if m.viewport.YOffset != 10 {
		t.Fatalf("Alt+Up from clamped last change offset = %d, want 10", m.viewport.YOffset)
	}
}

func TestOpenSideBySideDiffRerendersWhenWindowWidthChanges(t *testing.T) {
	m := NewModel([]model.Repo{{Path: t.TempDir()}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(Model)

	source := &model.SideBySideDiffSource{
		Prefix:   "Path: long.txt\nStatus: M\n\n",
		OldText:  strings.Repeat("old value ", 16) + "\n",
		NewText:  strings.Repeat("new value ", 16) + "\n",
		OldLabel: "OLD",
		NewLabel: "NEW",
	}
	// Simulate the terminal being resized while the asynchronous diff command
	// was still running: Output has a stale width, Source does not.
	updated, _ = m.Update(model.DiffLoadedMsg{
		Output: renderSideBySideSource(source, 160),
		Path:   "long.txt",
		Source: source,
	})
	m = updated.(Model)
	wideLineCount := m.viewport.TotalLineCount()
	wantWideLineCount := len(strings.Split(renderSideBySideSource(source, m.diffViewportWidth()), "\n"))
	if wideLineCount != wantWideLineCount {
		t.Fatalf("loaded diff has %d lines, want %d at current width", wideLineCount, wantWideLineCount)
	}

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 24})
	m = updated.(Model)
	narrowLineCount := m.viewport.TotalLineCount()
	if narrowLineCount <= wideLineCount {
		t.Fatalf("narrow diff has %d lines, want more than wide diff's %d", narrowLineCount, wideLineCount)
	}
	if m.viewport.Width != m.diffViewportWidth() {
		t.Fatalf("viewport width = %d, want %d", m.viewport.Width, m.diffViewportWidth())
	}
	if m.viewport.YOffset != 0 {
		t.Fatalf("diff that originally fit the viewport jumped to offset %d after narrowing", m.viewport.YOffset)
	}

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(Model)
	if got := m.viewport.TotalLineCount(); got != wideLineCount {
		t.Fatalf("widened diff has %d lines, want original %d", got, wideLineCount)
	}
}

func TestDiffResizeKeepsSelectedChangeAndBottomPosition(t *testing.T) {
	m := NewModel([]model.Repo{{Path: t.TempDir()}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 14})
	m = updated.(Model)

	source := &model.SideBySideDiffSource{
		Prefix:   "Path: changes.txt\nStatus: M\n\n",
		OldText:  "first old\n" + strings.Repeat("same\n", 15) + strings.Repeat("second old ", 12) + "\n",
		NewText:  "first new\n" + strings.Repeat("same\n", 15) + strings.Repeat("second new ", 12) + "\n",
		OldLabel: "OLD", NewLabel: "NEW",
	}
	updated, _ = m.Update(model.DiffLoadedMsg{
		Output: renderSideBySideSource(source, m.diffViewportWidth()),
		Path:   "changes.txt",
		Source: source,
	})
	m = updated.(Model)
	starts := diffChangeStarts(m.diffLineKinds)
	if len(starts) != 2 {
		t.Fatalf("change starts = %v, want two blocks", starts)
	}
	m.diffChangeCursor = 1
	m.viewport.SetYOffset(starts[1])

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 48, Height: 14})
	m = updated.(Model)
	starts = diffChangeStarts(m.diffLineKinds)
	wantOffset := min(starts[1], max(0, m.viewport.TotalLineCount()-m.viewport.VisibleLineCount()))
	if m.diffChangeCursor != 1 || m.viewport.YOffset != wantOffset {
		t.Fatalf("selected change after resize = cursor %d offset %d, want cursor 1 offset %d", m.diffChangeCursor, m.viewport.YOffset, wantOffset)
	}

	m.diffChangeCursor = -1
	m.viewport.GotoBottom()
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 18})
	m = updated.(Model)
	if !m.viewport.AtBottom() {
		t.Fatalf("diff left the bottom after resize: offset %d of %d lines", m.viewport.YOffset, m.viewport.TotalLineCount())
	}
}
