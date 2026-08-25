package ui

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"svn-tui/internal/model"
)

func colorizeSVNLog(out string) string {
	lines := strings.Split(out, "\n")
	var b strings.Builder
	inEntry := false
	inChangedPaths := false
	inMessage := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isSVNLogSeparator(trimmed) {
			b.WriteString(labelMauveStyle.Render(line) + "\n")
			inEntry, inChangedPaths, inMessage = false, false, false
			continue
		}
		if strings.HasPrefix(line, "r") && strings.Contains(line, " | ") {
			b.WriteString(colorizeSVNLogMetaLine(line) + "\n")
			inEntry, inChangedPaths, inMessage = true, false, false
			continue
		}
		if inEntry && trimmed == "Changed paths:" {
			b.WriteString(mutedStyle.Render(line) + "\n")
			inChangedPaths, inMessage = true, false
			continue
		}
		if inChangedPaths {
			if trimmed == "" {
				b.WriteString(line + "\n")
				inChangedPaths, inMessage = false, true
				continue
			}
			b.WriteString(colorizeSVNChangedPathLine(line) + "\n")
			continue
		}
		if inEntry && trimmed == "" {
			b.WriteString(line + "\n")
			inMessage = true
			continue
		}
		if inMessage && trimmed != "" {
			b.WriteString(actionStyle.Render(line) + "\n")
			continue
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func colorizeSVNLogMetaLine(line string) string {
	parts := strings.Split(line, "|")
	if len(parts) < 2 {
		return line
	}
	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			b.WriteString(labelMauveStyle.Render("|"))
		}
		text := part
		trimmed := strings.TrimSpace(part)
		if i == 1 && trimmed != "" {
			text = strings.Replace(part, trimmed, labelYellowStyle.Render(trimmed), 1)
		}
		b.WriteString(text)
	}
	return b.String()
}

func colorizeSVNChangedPathLine(line string) string {
	trimmedLeft := strings.TrimLeft(line, " \t")
	prefix := line[:len(line)-len(trimmedLeft)]
	if trimmedLeft == "" {
		return line
	}
	fields := strings.Fields(trimmedLeft)
	if len(fields) < 2 {
		return mutedStyle.Render(line)
	}
	act := fields[0]
	rest := strings.TrimSpace(strings.TrimPrefix(trimmedLeft, act))
	return prefix + statusStyleForPathAction(act).Render(act) + " " + valueWhiteStyle.Render(rest)
}

func isSVNLogSeparator(line string) bool {
	if len(line) < 8 {
		return false
	}
	for _, r := range line {
		if r != '-' {
			return false
		}
	}
	return true
}

func colorizeUnifiedDiff(out string) string {
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		// Files in this project use CRLF line endings, so svn diff lines end in
		// \r; left in place, that raw \r corrupts terminal rendering (cursor
		// jumps to column 0 without erasing, leaving stray fragments on screen).
		line = strings.TrimSuffix(line, "\r")
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			b.WriteString(labelMauveStyle.Render(line) + "\n")
		case strings.HasPrefix(line, "@@"):
			b.WriteString(diffModifiedStyle.Render(line) + "\n")
		case strings.HasPrefix(line, "+"):
			b.WriteString(diffAddedStyle.Render(line) + "\n")
		case strings.HasPrefix(line, "-"):
			b.WriteString(diffDeletedStyle.Render(line) + "\n")
		default:
			b.WriteString(diffSameStyle.Render(line) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func colorizeSVNUpdateLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) >= 2 {
		status := strings.TrimSpace(trimmed[:1])
		path := strings.TrimSpace(trimmed[1:])
		if status == "A" || status == "D" || status == "U" || status == "G" || status == "C" || status == "E" {
			return statusStyleForUpdateAction(status).Render(status) + " " + valueWhiteStyle.Render(path)
		}
	}
	return mutedStyle.Render(line)
}

func colorizePullStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		status = "U"
	}
	return statusStyleForPathAction(status).Render(padRight(status, 8))
}

func statusStyleForPathAction(action string) lipgloss.Style {
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "A":
		return successStyle
	case "D":
		return errorStyle
	case "M":
		return actionStyle
	case "R":
		return warningStyle
	default:
		return textStyle
	}
}

func statusStyleForUpdateAction(action string) lipgloss.Style {
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "A", "U", "G":
		return successStyle
	case "D":
		return errorStyle
	case "C":
		return diffModifiedStyle
	case "E":
		return warningStyle
	default:
		return textStyle
	}
}

func renderPartialHunkPreview(h model.PartialHunk, maxChangedLines int) []string {
	type pLine struct{ prefix, text, kind string }

	var changed []pLine
	for _, rawLine := range h.Lines {
		if rawLine == "" {
			continue
		}
		prefix := string(rawLine[0])
		if prefix != "+" && prefix != "-" {
			continue
		}
		text := ""
		if len(rawLine) > 1 {
			text = strings.TrimRight(rawLine[1:], "\n")
		}
		kind := "added"
		if prefix == "-" {
			kind = "deleted"
		}
		changed = append(changed, pLine{prefix, strings.TrimSpace(text), kind})
	}

	for i := 0; i < len(changed)-1; i++ {
		if changed[i].prefix == "-" && changed[i+1].prefix == "+" {
			changed[i].kind = "modified"
			changed[i+1].kind = "modified"
			i++
		}
	}

	if len(changed) == 0 {
		return nil
	}
	limit := maxChangedLines
	if limit <= 0 {
		limit = 8
	}
	var out []string
	for i, line := range changed {
		if i >= limit {
			out = append(out, mutedStyle.Render("..."))
			break
		}
		preview := line.prefix + " " + line.text
		switch line.kind {
		case "added":
			out = append(out, diffAddedStyle.Render(preview))
		case "deleted":
			out = append(out, diffDeletedStyle.Render(preview))
		case "modified":
			out = append(out, diffModifiedStyle.Render(preview))
		default:
			out = append(out, mutedStyle.Render(preview))
		}
	}
	return out
}

// historyBlock is one commit entry's line range within rendered history content,
// used to search and navigate between whole commits rather than individual lines.
type historyBlock struct {
	startLine int
	endLine   int // exclusive
	revision  int
}

var historyRevisionLineRe = regexp.MustCompile(`^r(\d+)\s*\|`)

// parseHistoryBlocks splits rendered `svn log` content into per-commit blocks,
// delimited by the "----" separator lines svn prints between entries.
func parseHistoryBlocks(content string) []historyBlock {
	lines := strings.Split(content, "\n")
	var blocks []historyBlock
	current := historyBlock{startLine: -1}
	flush := func(end int) {
		if current.startLine >= 0 && current.revision != 0 {
			current.endLine = end
			blocks = append(blocks, current)
		}
	}
	for i, line := range lines {
		plain := stripANSI(line)
		if isSVNLogSeparator(strings.TrimSpace(plain)) {
			flush(i)
			current = historyBlock{startLine: i + 1}
			continue
		}
		if current.revision == 0 {
			if m := historyRevisionLineRe.FindStringSubmatch(plain); m != nil {
				rev, _ := strconv.Atoi(m[1])
				current.revision = rev
			}
		}
	}
	flush(len(lines))
	return blocks
}

// searchHistoryBlocks returns the indices (into blocks) of commits whose text
// contains query (case-insensitive), in original order.
func searchHistoryBlocks(content string, blocks []historyBlock, query string) []int {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	var matched []int
	for idx, b := range blocks {
		end := min(b.endLine, len(lines))
		for i := b.startLine; i < end; i++ {
			if strings.Contains(strings.ToLower(stripANSI(lines[i])), q) {
				matched = append(matched, idx)
				break
			}
		}
	}
	return matched
}

// highlightHistoryBlock marks b's meta line with a cursor arrow so the currently
// selected commit is visible without disturbing the rest of the colorized content.
func highlightHistoryBlock(content string, b historyBlock) string {
	if b.startLine < 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if b.startLine >= len(lines) {
		return content
	}
	lines[b.startLine] = selectedStyle.Render("▶ ") + lines[b.startLine]
	return strings.Join(lines, "\n")
}

// nextMatchIndex finds the match adjacent to cursor in direction (+1 forward,
// -1 backward) among matches (ascending block indices), wrapping at the ends.
func nextMatchIndex(matches []int, cursor, direction int) (int, bool) {
	if len(matches) == 0 {
		return 0, false
	}
	if direction >= 0 {
		for _, idx := range matches {
			if idx > cursor {
				return idx, true
			}
		}
		return matches[0], true
	}
	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i] < cursor {
			return matches[i], true
		}
	}
	return matches[len(matches)-1], true
}

// indexOfInt returns the position of v within s, if present.
func indexOfInt(s []int, v int) (int, bool) {
	for i, x := range s {
		if x == v {
			return i, true
		}
	}
	return 0, false
}

func stripANSI(s string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(s, "")
}

func formatSVNLogDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return s
}

// padRight pads or truncates s to exactly n visible characters.
func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}
