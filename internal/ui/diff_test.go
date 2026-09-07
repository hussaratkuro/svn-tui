package ui

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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
