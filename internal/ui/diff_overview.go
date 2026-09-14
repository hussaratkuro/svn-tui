package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// diffLineKind describes the strongest change represented by one rendered
// line. The overview uses the same colours as the full-size diff.
type diffLineKind uint8

const (
	diffLineSame diffLineKind = iota
	diffLineAdded
	diffLineDeleted
	diffLineModified
)

// classifyDiffLines extracts change locations from both side-by-side and
// unified diffs. For side-by-side output, the marker column is discovered from
// the table header, so file contents containing separator characters do not
// confuse the classifier.
func classifyDiffLines(content string) []diffLineKind {
	lines := strings.Split(content, "\n")
	kinds := make([]diffLineKind, len(lines))
	markerColumn := sideBySideMarkerColumn(lines)

	for i, styledLine := range lines {
		line := stripANSI(styledLine)
		if markerColumn >= 0 &&
			visualRuneAt(line, markerColumn-2) == '│' &&
			visualRuneAt(line, markerColumn+2) == '│' {
			switch visualRuneAt(line, markerColumn) {
			case '+':
				kinds[i] = diffLineAdded
			case '-':
				kinds[i] = diffLineDeleted
			case '~':
				kinds[i] = diffLineModified
			case ' ':
				// Wrapped continuations deliberately have a blank marker and
				// line number. Keep them in the same change block as their first
				// visual line so Alt navigation advances by changes, not wraps.
				if i > 0 {
					kinds[i] = kinds[i-1]
				}
			}
			continue
		}

		// Unified diff fallback. File headers are metadata rather than changed
		// content and deliberately stay out of the minimap.
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			kinds[i] = diffLineAdded
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			kinds[i] = diffLineDeleted
		}
	}
	return kinds
}

func sideBySideMarkerColumn(lines []string) int {
	const separator = " │ Δ │ "
	for _, styledLine := range lines {
		line := stripANSI(styledLine)
		if byteIndex := strings.Index(line, separator); byteIndex >= 0 {
			return lipgloss.Width(line[:byteIndex]) + 3
		}
	}
	return -1
}

func visualRuneAt(s string, column int) rune {
	if column < 0 {
		return 0
	}
	position := 0
	for _, r := range s {
		width := lipgloss.Width(string(r))
		if width <= 0 {
			width = 1
		}
		if column >= position && column < position+width {
			return r
		}
		position += width
	}
	return 0
}

func diffChangeStarts(kinds []diffLineKind) []int {
	var starts []int
	inChange := false
	for i, kind := range kinds {
		if kind != diffLineSame {
			if !inChange {
				starts = append(starts, i)
			}
			inChange = true
		} else {
			inChange = false
		}
	}
	return starts
}

// nextDiffChange returns the adjacent change block in the requested direction.
// It does not wrap: at either end the viewport remains on the boundary change.
func nextDiffChange(starts []int, offset, direction int) (int, bool) {
	if direction >= 0 {
		for _, start := range starts {
			if start > offset {
				return start, true
			}
		}
		return 0, false
	}
	for i := len(starts) - 1; i >= 0; i-- {
		if starts[i] < offset {
			return starts[i], true
		}
	}
	return 0, false
}

// renderDiffOverview renders a one-cell-wide, full-document minimap. The
// +, - and ~ markers remain meaningful without colour; the heavier grey
// section marks the current viewport and the thin grey line the rest.
func renderDiffOverview(kinds []diffLineKind, height, offset, visible int) string {
	if height <= 0 {
		return ""
	}
	total := max(1, len(kinds))
	viewStart := clamp(offset*height/total, 0, height-1)
	viewEnd := clamp(divCeil((offset+visible)*height, total)-1, viewStart, height-1)

	var b strings.Builder
	for row := range height {
		from := row * total / height
		to := max(from+1, divCeil((row+1)*total, height))
		to = min(to, len(kinds))

		kind := diffLineSame
		for i := from; i < to; i++ {
			kind = strongerDiffKind(kind, kinds[i])
		}

		glyph := mutedStyle.Render("│")
		if row >= viewStart && row <= viewEnd {
			glyph = checkboxStyle.Render("┃")
		}
		switch kind {
		case diffLineAdded:
			glyph = diffAddedStyle.Render("+")
		case diffLineDeleted:
			glyph = diffDeletedStyle.Render("-")
		case diffLineModified:
			glyph = diffModifiedStyle.Render("~")
		}

		if row > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(glyph)
	}
	return b.String()
}

func strongerDiffKind(a, b diffLineKind) diffLineKind {
	if a == diffLineSame {
		return b
	}
	if b == diffLineSame || a == b {
		return a
	}
	// When compression puts different change kinds on the same minimap row,
	// show the combined region as a modification instead of hiding one colour.
	return diffLineModified
}

func divCeil(n, d int) int {
	if d <= 0 || n <= 0 {
		return 0
	}
	return (n + d - 1) / d
}
