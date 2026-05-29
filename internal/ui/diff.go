package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"svn-tui/internal/model"
	"svn-tui/internal/svn"
)

func buildSideBySideDiff(r model.Repo, item model.CommitItem, width int) (string, error) {
	if width <= 0 {
		width = 160
	}

	usableWidth := max(72, width)
	separatorWidth := lipgloss.Width(" │ Δ │ ")
	numWidth := 4
	numColOverhead := 2 * (numWidth + lipgloss.Width(" │ "))
	cellSpace := usableWidth - separatorWidth - numColOverhead
	if cellSpace < 48 {
		cellSpace = 48
	}
	leftWidth := max(24, cellSpace/2)
	rightWidth := max(24, cellSpace-leftWidth)

	if isLikelyDir(r, item.Path) {
		out, err := svn.Run(r, "diff", "--", item.Path)
		if err != nil {
			return "", err
		}
		return "Directory diff uses unified SVN diff output:\n\n" + colorizeUnifiedDiff(out), nil
	}

	var oldText, newText string
	var err error

	switch {
	case item.Unversioned || strings.HasPrefix(item.Status, "?") || strings.HasPrefix(item.Status, "A"):
		oldText = ""
		newText, err = readWorkingFile(r, item.Path)
		if err != nil {
			newText, err = readRepoHeadFile(r, item.Path)
		}
		if err != nil {
			return "", err
		}
	case strings.HasPrefix(item.Status, "D"):
		oldText, err = readBaseFile(r, item.Path)
		if err != nil {
			return "", err
		}
		newText = ""
	default:
		oldText, err = readBaseFile(r, item.Path)
		if err != nil {
			return "", err
		}
		newText, err = readWorkingFile(r, item.Path)
		if err != nil {
			return "", err
		}
	}

	oldEOL := detectEOLStyle(oldText)
	newEOL := detectEOLStyle(newText)
	oldHasEOF := hasFinalNewline(oldText)
	newHasEOF := hasFinalNewline(newText)

	// Normalise for comparison: files may differ only in line endings
	oldNorm := normalizeEOL(oldText)
	newNorm := normalizeEOL(newText)

	if oldNorm == newNorm && !item.Unversioned &&
		!strings.HasPrefix(item.Status, "?") &&
		!strings.HasPrefix(item.Status, "A") &&
		!strings.HasPrefix(item.Status, "D") {
		// Check if there is a real SVN property diff
		out, serr := svn.Run(r, "diff", "--", item.Path)
		eolNote := buildEOLNote(oldEOL, newEOL, oldHasEOF, newHasEOF)
		if serr == nil && strings.TrimSpace(out) != "" {
			return "Path: " + item.Path + "\nStatus: " + item.Status + "\n" + eolNote + "\nContent is identical — SVN reports a property diff:\n\n" + colorizeUnifiedDiff(out), nil
		}
		if serr == nil {
			return "Path: " + item.Path + "\nStatus: " + item.Status + "\n" + eolNote + "\nNo text or property diff found.", nil
		}
	}

	oldLines, oldCRLF := splitLinesWithCRLF(oldText)
	newLines, newCRLF := splitLinesWithCRLF(newText)

	maxLines := max(len(oldLines), len(newLines))
	if maxLines < 1 {
		maxLines = 1
	}
	numWidth = len(fmt.Sprintf("%d", maxLines))
	if numWidth < 1 {
		numWidth = 1
	}
	numColOverhead = 2 * (numWidth + lipgloss.Width(" │ "))
	cellSpace = usableWidth - separatorWidth - numColOverhead
	if cellSpace < 48 {
		cellSpace = 48
	}
	leftWidth = max(24, cellSpace/2)
	rightWidth = max(24, cellSpace-leftWidth)

	var b strings.Builder
	b.WriteString("Path:   " + item.Path + "\n")
	b.WriteString("Status: " + item.Status + "\n")
	b.WriteString(buildEOLNote(oldEOL, newEOL, oldHasEOF, newHasEOF))
	if item.Unversioned {
		b.WriteString(mutedStyle.Render("Note: unversioned file — will be svn add-ed before commit if selected.") + "\n")
	}
	if strings.HasPrefix(item.Status, "A") {
		b.WriteString(mutedStyle.Render("Note: added file — left side is empty.") + "\n")
	}
	b.WriteString("\n")
	b.WriteString(renderDiffHeader(leftWidth, rightWidth, numWidth))

	rows := sideBySideRows(oldLines, newLines, oldCRLF, newCRLF)
	if len(rows) == 0 {
		b.WriteString(renderDiffLine("", "", "=", leftWidth, rightWidth, 0, 0, numWidth, false, false) + "\n")
		return b.String(), nil
	}

	rows = compactUnchangedRows(rows, 4)
	for _, row := range rows {
		leftWrapped := wrapLineForDiff(row.Left, leftWidth)
		rightWrapped := wrapLineForDiff(row.Right, rightWidth)

		maxParts := max(len(leftWrapped), len(rightWrapped))
		for i := range maxParts {
			left, right := "", ""
			if i < len(leftWrapped) {
				left = leftWrapped[i]
			}
			if i < len(rightWrapped) {
				right = rightWrapped[i]
			}
			marker, lNum, rNum := row.Marker, row.LeftNum, row.RightNum
			if i > 0 {
				marker, lNum, rNum = " ", 0, 0
			}
			// Show CRLF badge on the last sub-line of a wrapped cell
			showLCR := row.LeftCRLF && (i == len(leftWrapped)-1)
			showRCR := row.RightCRLF && (i == len(rightWrapped)-1)
			b.WriteString(renderDiffLine(left, right, marker, leftWidth, rightWidth, lNum, rNum, numWidth, showLCR, showRCR) + "\n")
		}
	}
	return b.String(), nil
}

func renderDiffHeader(leftWidth, rightWidth, numWidth int) string {
	numPad := strings.Repeat(" ", numWidth)
	numSep := mutedStyle.Render(" │ ")
	left := labelMauveStyle.Render(padRightVisual("OLD / BASE", leftWidth))
	right := labelMauveStyle.Render(padRightVisual("NEW / WORKING COPY", rightWidth))
	sep := mutedStyle.Render(" │ Δ │ ")
	rule := mutedStyle.Render(
		strings.Repeat("─", numWidth) + "─┼─" +
			strings.Repeat("─", leftWidth) + "─┼───┼─" +
			strings.Repeat("─", numWidth) + "─┼─" +
			strings.Repeat("─", rightWidth),
	)
	return mutedStyle.Render(numPad) + numSep + left + sep + mutedStyle.Render(numPad) + numSep + right + "\n" + rule + "\n"
}

// crBadgeW is the visible width of the CRLF badge " CR".
const crBadgeW = 3

func renderDiffLine(left, right, marker string, leftWidth, rightWidth, leftNum, rightNum, numWidth int, leftCRLF, rightCRLF bool) string {
	left = expandTabsForDisplay(left)
	right = expandTabsForDisplay(right)

	style := diffStyleForMarker(marker)

	buildCell := func(text string, width int, hasCR bool) string {
		if hasCR {
			cw := max(1, width-crBadgeW)
			content := style.Render(padRightVisual(truncateVisual(text, cw), cw))
			return content + labelMauveStyle.Render(" CR")
		}
		return style.Render(padRightVisual(text, width))
	}

	leftCell := buildCell(left, leftWidth, leftCRLF)
	rightCell := buildCell(right, rightWidth, rightCRLF)

	markerCell := diffStyleForMarker(marker).Render(marker)
	if marker == " " {
		markerCell = mutedStyle.Render(marker)
	}

	leftNumStr, rightNumStr := strings.Repeat(" ", numWidth), strings.Repeat(" ", numWidth)
	if leftNum > 0 {
		leftNumStr = fmt.Sprintf("%*d", numWidth, leftNum)
	}
	if rightNum > 0 {
		rightNumStr = fmt.Sprintf("%*d", numWidth, rightNum)
	}

	numSep := mutedStyle.Render(" │ ")
	return mutedStyle.Render(leftNumStr) + numSep + leftCell + mutedStyle.Render(" │ ") + markerCell + mutedStyle.Render(" │ ") + mutedStyle.Render(rightNumStr) + numSep + rightCell
}

func diffStyleForMarker(marker string) lipgloss.Style {
	switch marker {
	case "+":
		return diffAddedStyle
	case "-":
		return diffDeletedStyle
	case "~":
		return diffModifiedStyle
	case "=":
		return diffSameStyle
	default:
		return mutedStyle
	}
}

func sideBySideRows(oldLines, newLines []string, oldCRLF, newCRLF []bool) []model.DiffRow {
	crlfAt := func(sl []bool, i int) bool {
		if i >= 0 && i < len(sl) {
			return sl[i]
		}
		return false
	}

	n, m := len(oldLines), len(newLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var rows []model.DiffRow
	i, j := 0, 0
	for i < n && j < m {
		if oldLines[i] == newLines[j] {
			lc, rc := crlfAt(oldCRLF, i), crlfAt(newCRLF, j)
			marker := "="
			if lc != rc {
				marker = "~" // content same, but line ending differs
			}
			rows = append(rows, model.DiffRow{Left: oldLines[i], Right: newLines[j], Marker: marker, LeftNum: i + 1, RightNum: j + 1, LeftCRLF: lc, RightCRLF: rc})
			i++
			j++
			continue
		}
		if i+1 < n && oldLines[i+1] == newLines[j] {
			rows = append(rows, model.DiffRow{Left: oldLines[i], Marker: "-", LeftNum: i + 1, LeftCRLF: crlfAt(oldCRLF, i)})
			i++
			continue
		}
		if j+1 < m && oldLines[i] == newLines[j+1] {
			rows = append(rows, model.DiffRow{Right: newLines[j], Marker: "+", RightNum: j + 1, RightCRLF: crlfAt(newCRLF, j)})
			j++
			continue
		}
		if dp[i+1][j] > dp[i][j+1] {
			rows = append(rows, model.DiffRow{Left: oldLines[i], Marker: "-", LeftNum: i + 1, LeftCRLF: crlfAt(oldCRLF, i)})
			i++
		} else if dp[i][j+1] > dp[i+1][j] {
			rows = append(rows, model.DiffRow{Right: newLines[j], Marker: "+", RightNum: j + 1, RightCRLF: crlfAt(newCRLF, j)})
			j++
		} else {
			rows = append(rows, model.DiffRow{Left: oldLines[i], Right: newLines[j], Marker: "~", LeftNum: i + 1, RightNum: j + 1, LeftCRLF: crlfAt(oldCRLF, i), RightCRLF: crlfAt(newCRLF, j)})
			i++
			j++
		}
	}
	for i < n {
		rows = append(rows, model.DiffRow{Left: oldLines[i], Marker: "-", LeftNum: i + 1, LeftCRLF: crlfAt(oldCRLF, i)})
		i++
	}
	for j < m {
		rows = append(rows, model.DiffRow{Right: newLines[j], Marker: "+", RightNum: j + 1, RightCRLF: crlfAt(newCRLF, j)})
		j++
	}
	return compactUnchangedRows(rows, 4)
}

func compactUnchangedRows(rows []model.DiffRow, context int) []model.DiffRow {
	if len(rows) == 0 {
		return rows
	}
	changed := make([]bool, len(rows))
	hasChanges := false
	for i, row := range rows {
		if row.Marker != "=" {
			hasChanges = true
			from := max(0, i-context)
			to := min(len(rows)-1, i+context)
			for j := from; j <= to; j++ {
				changed[j] = true
			}
		}
	}
	if !hasChanges {
		return rows
	}
	var out []model.DiffRow
	hidden := false
	for i, row := range rows {
		if changed[i] {
			if hidden {
				out = append(out, model.DiffRow{Left: "...", Right: "...", Marker: " "})
				hidden = false
			}
			out = append(out, row)
		} else {
			hidden = true
		}
	}
	if hidden {
		out = append(out, model.DiffRow{Left: "...", Right: "...", Marker: " "})
	}
	return out
}

func wrapLineForDiff(s string, width int) []string {
	s = expandTabsForDisplay(s)
	if width <= 0 {
		return []string{s}
	}
	if s == "" {
		return []string{""}
	}
	var parts []string
	var current strings.Builder
	currentWidth := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if rw <= 0 {
			rw = 1
		}
		if currentWidth > 0 && currentWidth+rw > width {
			parts = append(parts, current.String())
			current.Reset()
			currentWidth = 0
		}
		current.WriteRune(r)
		currentWidth += rw
	}
	return append(parts, current.String())
}

func padRightVisual(s string, width int) string {
	s = truncateVisual(s, width)
	visible := lipgloss.Width(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

func truncateVisual(s string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if rw <= 0 {
			rw = 1
		}
		if used+rw > width {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String()
}

func expandTabsForDisplay(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			spaces := 4 - (col % 4)
			if spaces == 0 {
				spaces = 4
			}
			b.WriteString(strings.Repeat(" ", spaces))
			col += spaces
			continue
		}
		b.WriteRune(r)
		w := lipgloss.Width(string(r))
		if w <= 0 {
			w = 1
		}
		col += w
	}
	return b.String()
}

func splitLinesForDiff(text string) []string {
	lines, _ := splitLinesWithCRLF(text)
	return lines
}

// splitLinesWithCRLF splits text into lines and records which were \r\n terminated.
func splitLinesWithCRLF(text string) (lines []string, crlf []bool) {
	if text == "" {
		return nil, nil
	}
	var cur strings.Builder
	i := 0
	for i < len(text) {
		b := text[i]
		switch {
		case b == '\r' && i+1 < len(text) && text[i+1] == '\n':
			lines = append(lines, cur.String())
			crlf = append(crlf, true)
			cur.Reset()
			i += 2
		case b == '\r' || b == '\n':
			lines = append(lines, cur.String())
			crlf = append(crlf, false)
			cur.Reset()
			i++
		default:
			cur.WriteByte(b)
			i++
		}
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
		crlf = append(crlf, false)
	}
	return lines, crlf
}

// normalizeEOL converts all line endings to \n for content comparison.
func normalizeEOL(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// detectEOLStyle returns "CRLF", "LF", "Mixed", or "" for empty/no-newline text.
func detectEOLStyle(text string) string {
	if text == "" {
		return ""
	}
	hasCRLF := strings.Contains(text, "\r\n")
	// LF that is not part of a CRLF pair
	hasLF := strings.Contains(normalizeEOL(strings.ReplaceAll(text, "\r\n", "")), "\n")
	switch {
	case hasCRLF && hasLF:
		return "Mixed"
	case hasCRLF:
		return "CRLF"
	case hasLF:
		return "LF"
	default:
		return ""
	}
}

// hasFinalNewline reports whether text ends with a newline (LF or CRLF).
func hasFinalNewline(text string) bool {
	return strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r\n")
}

// buildEOLNote returns a styled one-line summary of EOL/EOF differences.
func buildEOLNote(oldEOL, newEOL string, oldEOF, newEOF bool) string {
	eofStr := func(has bool) string {
		if has {
			return "✓ EOF"
		}
		return "✗ EOF"
	}
	oldLabel := func(s string) string {
		if s == "" {
			return mutedStyle.Render("—")
		}
		return warningStyle.Render(s)
	}
	newLabel := func(s string) string {
		if s == "" {
			return mutedStyle.Render("—")
		}
		if s == "CRLF" {
			return successStyle.Render(s)
		}
		return actionStyle.Render(s)
	}
	oldEOFStr := mutedStyle.Render(eofStr(oldEOF))
	if !oldEOF {
		oldEOFStr = warningStyle.Render(eofStr(oldEOF))
	}
	newEOFStr := mutedStyle.Render(eofStr(newEOF))
	if !newEOF {
		newEOFStr = warningStyle.Render(eofStr(newEOF))
	}

	return mutedStyle.Render("EOL     OLD: ") + oldLabel(oldEOL) + mutedStyle.Render("  ") + oldEOFStr + mutedStyle.Render("   NEW: ") + newLabel(newEOL) + mutedStyle.Render("  ") + newEOFStr + "\n"
}

// toCRLF converts all line endings in s to \r\n.
func toCRLF(s string) string {
	s = normalizeEOL(s)
	return strings.ReplaceAll(s, "\n", "\r\n")
}

func readBaseFile(r model.Repo, path string) (string, error) {
	out, err := svn.Run(r, "cat", path)
	if err != nil {
		return "", err
	}
	return out, nil
}

func readWorkingFile(r model.Repo, path string) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.Path, filepath.FromSlash(path)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readRepoHeadFile(r model.Repo, path string) (string, error) {
	out, err := svn.Run(r, "cat", "-r", "HEAD", path)
	if err != nil {
		return "", err
	}
	return out, nil
}

func isSVNDir(r model.Repo, path string) bool {
	if isLikelyDir(r, path) {
		return true
	}
	out, err := svn.Run(r, "info", "--", path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "Node Kind: directory") {
			return true
		}
	}
	return false
}

func isLikelyDir(r model.Repo, path string) bool {
	info, err := os.Stat(filepath.Join(r.Path, filepath.FromSlash(path)))
	if err != nil {
		return false
	}
	return info.IsDir()
}
