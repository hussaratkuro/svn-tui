package diff

import (
	"fmt"
	"regexp"
	"strings"

	"svn-tui/internal/model"
)

var unifiedHunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// ParseHunks parses a unified diff text into individual hunks.
func ParseHunks(diffText string) ([]model.PartialHunk, error) {
	lines := strings.Split(normalize(diffText), "\n")
	var hunks []model.PartialHunk
	var current *model.PartialHunk

	flush := func() {
		if current == nil {
			return
		}
		current.PreviewText = BuildPreview(*current, 4)
		hunks = append(hunks, *current)
		current = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "@@ ") {
			flush()
			os, oc, ns, nc, err := parseHeader(line)
			if err != nil {
				return nil, err
			}
			current = &model.PartialHunk{
				Header:   line,
				OldStart: os, OldCount: oc,
				NewStart: ns, NewCount: nc,
			}
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, `\ No newline at end of file`) {
			continue
		}
		if line == "" {
			current.Lines = append(current.Lines, " ")
			current.Context++
			continue
		}
		sign := line[0]
		if sign != ' ' && sign != '+' && sign != '-' {
			continue
		}
		current.Lines = append(current.Lines, line)
		switch sign {
		case '+':
			current.Added++
		case '-':
			current.Removed++
		case ' ':
			current.Context++
		}
	}
	flush()
	return hunks, nil
}

func parseHeader(header string) (oldStart, oldCount, newStart, newCount int, err error) {
	m := unifiedHunkHeaderRe.FindStringSubmatch(header)
	if len(m) == 0 {
		return 0, 0, 0, 0, fmt.Errorf("invalid hunk header: %s", header)
	}
	return atoiDef(m[1], 0), atoiDef(m[2], 1), atoiDef(m[3], 0), atoiDef(m[4], 1), nil
}

func atoiDef(s string, def int) int {
	if s == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return def
	}
	return n
}

// BuildPreview returns a short text summary of a hunk's changed lines.
func BuildPreview(h model.PartialHunk, maxLines int) string {
	var out []string
	for _, line := range h.Lines {
		if len(out) >= maxLines {
			out = append(out, "...")
			break
		}
		if line == "" {
			out = append(out, "")
			continue
		}
		prefix := string(line[0])
		text := ""
		if len(line) > 1 {
			text = line[1:]
		}
		if prefix == " " {
			continue
		}
		out = append(out, prefix+" "+strings.TrimSpace(text))
	}
	return strings.Join(out, "\n")
}

// Selected returns only the selected hunks.
func Selected(hunks []model.PartialHunk) []model.PartialHunk {
	var out []model.PartialHunk
	for _, h := range hunks {
		if h.Selected {
			out = append(out, h)
		}
	}
	return out
}

// ApplyToBase applies the selected hunks to baseLines and returns the result.
func ApplyToBase(baseLines []string, hunks []model.PartialHunk) ([]string, error) {
	result := append([]string(nil), baseLines...)

	// Apply in reverse order to keep offsets valid.
	for i := len(hunks) - 1; i >= 0; i-- {
		h := hunks[i]
		start := h.OldStart - 1
		if start < 0 {
			start = 0
		}
		end := start + h.OldCount
		if end > len(result) {
			return nil, fmt.Errorf("hunk range is outside file: %s", h.Header)
		}
		newLines := make([]string, 0, h.NewCount)
		for _, line := range h.Lines {
			if line == "" {
				newLines = append(newLines, "")
				continue
			}
			sign := line[0]
			text := ""
			if len(line) > 1 {
				text = line[1:]
			}
			switch sign {
			case ' ', '+':
				newLines = append(newLines, text)
			}
		}
		updated := make([]string, 0, len(result)-h.OldCount+len(newLines))
		updated = append(updated, result[:start]...)
		updated = append(updated, newLines...)
		updated = append(updated, result[end:]...)
		result = updated
	}
	return result, nil
}

// SplitLines splits text into lines for patching.
func SplitLines(text string) []string {
	text = normalize(text)
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// JoinLines joins lines back into text.
func JoinLines(lines []string, finalNewline bool) string {
	out := strings.Join(lines, "\n")
	if finalNewline {
		out += "\n"
	}
	return out
}

// HasFinalNewline returns true if any of the given strings ends with a newline.
func HasFinalNewline(values ...string) bool {
	for _, v := range values {
		if strings.HasSuffix(v, "\n") {
			return true
		}
	}
	return false
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
