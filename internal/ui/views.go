package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"svn-tui/internal/model"
)

// ── Top-level view dispatcher ─────────────────────────────────────────────────

func (m Model) View() string {
	switch m.screen {
	case model.ScreenRepoSelect:
		return m.viewRepoSelect()
	case model.ScreenActionSelect:
		return m.viewActionSelect()
	case model.ScreenCreateBranchInput:
		return m.viewCreateBranchInput()
	case model.ScreenCheckoutRevisionInput:
		return m.viewCheckoutRevisionInput()
	case model.ScreenFileHistorySearch:
		return m.viewFileHistorySearch()
	case model.ScreenFileHistorySelect:
		return m.viewFileHistorySelect()
	case model.ScreenBranchSelect:
		return m.viewBranchSelect()
	case model.ScreenShelfSelect:
		return m.viewShelfSelect()
	case model.ScreenPullSelect:
		return m.viewPullSelect()
	case model.ScreenShelveSelect:
		return m.viewShelveSelect()
	case model.ScreenCommitSelect:
		return m.viewCommitSelect()
	case model.ScreenCommitMessageInput:
		return m.viewCommitMessageInput()
	case model.ScreenPartialHunkSelect:
		return m.viewPartialHunkSelect()
	case model.ScreenRevertSelect:
		return m.viewRevertSelect()
	case model.ScreenConflictSelect:
		return m.viewConflictSelect()
	case model.ScreenHistory, model.ScreenHistorySearch:
		return m.viewHistory()
	case model.ScreenDiff:
		return m.viewDiff()
	case model.ScreenRunning:
		return m.viewRunning()
	case model.ScreenResult:
		return m.viewResult()
	}
	return ""
}

// ── Compact header ────────────────────────────────────────────────────────────
// Compact: path · location · rRev    TITLE
// Expanded (i): + 4 info lines below

func (m Model) compactHeader(title string) string {
	r := m.activeRepo
	path := r.Path
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		path = "~" + path[len(home):]
	}

	left := labelMauveStyle.Render(path) +
		mutedStyle.Render(" · ") +
		labelYellowStyle.Render(r.CurrentLocation) +
		mutedStyle.Render(" · ") +
		successStyle.Render("r"+r.CurrentRevision)

	right := titleStyle.Render(title)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 2 {
		gap = 2
	}

	var b strings.Builder
	b.WriteString(left + strings.Repeat(" ", gap) + right + "\n")

	if m.showInfo {
		b.WriteString(mutedStyle.Render("  Path  ") + valueWhiteStyle.Render(r.Path) + "\n")
		b.WriteString(mutedStyle.Render("  URL   ") + valueWhiteStyle.Render(r.URL) + "\n")
		b.WriteString(mutedStyle.Render("  Root  ") + valueWhiteStyle.Render(r.Root) + "\n")
		b.WriteString(mutedStyle.Render("  Rev   ") + valueWhiteStyle.Render(r.CurrentRevision) + "\n")
	}

	b.WriteString("\n")
	return b.String()
}

func simpleHeader(title string) string {
	return titleStyle.Render("svntui") + "  " + mutedStyle.Render(title) + "\n\n"
}

func statusBar(hints ...string) string {
	return "\n" + statusBarStyle.Render(strings.Join(hints, "  "))
}

// ── List box helpers ─────────────────────────────────────────────────────────

// listInnerHeight returns the usable inner height for a bordered list box.
func (m Model) listInnerHeight() int {
	// total - header - border(2) - status bar(2)
	return max(3, m.height-m.headerLines()-4)
}

// listBox wraps content in a border that fills the available list area.
func (m Model) listBox(content string) string {
	h := m.listInnerHeight()
	return styleBorder.Width(m.width - 2).Height(h).Render(content)
}

// ── Screens ───────────────────────────────────────────────────────────────────

func (m Model) viewRepoSelect() string {
	var b strings.Builder
	b.WriteString(simpleHeader("Select SVN repository"))

	items := max(3, m.listInnerHeight()-2)
	end := min(len(m.repos), m.repoOffset+items)

	var c strings.Builder
	for i := m.repoOffset; i < end; i++ {
		r := m.repos[i]
		cursor := " "
		lineStyle := normalStyle
		if i == m.repoCursor {
			cursor = ">"
			lineStyle = successStyle
		}
		c.WriteString(lineStyle.Render(fmt.Sprintf("%s %s", cursor, r.Path)) + "\n")
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.repoOffset, end, len(m.repos))))

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("↑↓/jk", "move"), hint("Enter", "select"), hint("q", "quit")))
	return b.String()
}

func (m Model) viewActionSelect() string {
	var b strings.Builder
	b.WriteString(m.compactHeader("Select action"))

	items := max(3, m.listInnerHeight()-2)
	end := min(len(m.actions), m.actionOffset+items)

	var c strings.Builder
	for i := m.actionOffset; i < end; i++ {
		cursor := " "
		lineStyle := actionStyle
		if i == m.actionCursor {
			cursor = ">"
			lineStyle = actionSelectedStyle
		}
		c.WriteString(lineStyle.Render(fmt.Sprintf("%s %s", cursor, m.actions[i])) + "\n")
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.actionOffset, end, len(m.actions))))

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("↑↓/jk", "move"), hint("Enter", "run"), hint("i", "info"), hint("Esc", "repos"), hint("q", "quit")))
	return b.String()
}

func (m Model) viewCreateBranchInput() string {
	var b strings.Builder
	b.WriteString(m.compactHeader("Create branch"))
	var c strings.Builder
	c.WriteString(textStyle.Render("Branch parameter:") + "\n")
	c.WriteString(m.input.View() + "\n\n")
	c.WriteString(mutedStyle.Render("Final branch name format: YYYY-MM-DD_username_parameter"))
	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("Enter", "create branch"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewCheckoutRevisionInput() string {
	var b strings.Builder
	b.WriteString(m.compactHeader("Checkout revision"))
	var c strings.Builder
	c.WriteString(mutedStyle.Render("This runs: svn update -r REVISION") + "\n\n")
	c.WriteString(textStyle.Render("Revision number:") + "\n")
	c.WriteString(m.input.View() + "\n\n")
	c.WriteString(warningStyle.Render("Note: Pull will update the working copy back to HEAD."))
	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("Enter", "checkout revision"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewFileHistorySearch() string {
	var b strings.Builder
	b.WriteString(m.compactHeader("File history search"))
	var c strings.Builder
	c.WriteString(mutedStyle.Render("Search by local working-copy file path.") + "\n\n")
	c.WriteString(textStyle.Render("File path search:") + "\n")
	c.WriteString(m.input.View())
	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("Enter", "search"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewFileHistorySelect() string {
	var b strings.Builder
	b.WriteString(m.compactHeader("File history"))

	items := max(3, m.listInnerHeight()-4)
	end := min(len(m.fileHistoryItems), m.fileHistoryOffset+items)

	var c strings.Builder
	c.WriteString(mutedStyle.Render("Search: "+m.fileHistoryQuery) + "\n")
	c.WriteString(mutedStyle.Render("─────────────────") + "\n")
	for i := m.fileHistoryOffset; i < end; i++ {
		cursor := " "
		lineStyle := normalStyle
		if i == m.fileHistoryCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}
		c.WriteString(lineStyle.Render(fmt.Sprintf("%s %s", cursor, m.fileHistoryItems[i])) + "\n")
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.fileHistoryOffset, end, len(m.fileHistoryItems))))

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("↑↓/jk", "move"), hint("Enter", "show history"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewBranchSelect() string {
	title := "Switch to branch"
	enterAction := "switch"
	if m.selectedAction == model.ActionMergeBranch {
		title = "Merge branch"
		enterAction = "merge"
	}

	var b strings.Builder
	b.WriteString(m.compactHeader(title))

	overhead := 4
	if strings.TrimSpace(m.branchNumberInput) != "" {
		overhead = 5
	}
	items := max(3, m.listInnerHeight()-overhead)
	end := min(len(m.branches), m.branchOffset+items)

	var c strings.Builder
	c.WriteString(textStyle.Render("Available SVN branches:") + "\n")
	if strings.TrimSpace(m.branchNumberInput) != "" {
		c.WriteString(labelYellowStyle.Render("Branch number: ") + valueWhiteStyle.Render(m.branchNumberInput) +
			mutedStyle.Render("  Enter: "+enterAction+" | Backspace: edit") + "\n")
	}
	c.WriteString(mutedStyle.Render("─────────────────────────") + "\n")
	for i := m.branchOffset; i < end; i++ {
		br := m.branches[i]
		cursor := " "
		lineStyle := normalStyle
		if i == m.branchCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}
		c.WriteString(lineStyle.Render(fmt.Sprintf("%s %3d) %s", cursor, i+1, br.Name)) + "\n")
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.branchOffset, end, len(m.branches))))

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("↑↓/jk", "move"), hint("0-9", "jump to number"), hint("Enter", enterAction), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewShelfSelect() string {
	var b strings.Builder
	b.WriteString(m.compactHeader("Unshelve changes"))

	items := max(3, m.listInnerHeight()-4)
	end := min(len(m.shelves), m.shelfOffset+items)

	var c strings.Builder
	c.WriteString(textStyle.Render("Available shelves:") + "\n")
	c.WriteString(mutedStyle.Render("──────────────────") + "\n")
	for i := m.shelfOffset; i < end; i++ {
		cursor := " "
		lineStyle := normalStyle
		if i == m.shelfCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}
		c.WriteString(lineStyle.Render(fmt.Sprintf("%s %s", cursor, m.shelves[i])) + "\n")
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.shelfOffset, end, len(m.shelves))))

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("↑↓/jk", "move"), hint("Enter", "unshelve"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewPullSelect() string {
	selectedFiles, totalFiles := 0, 0
	for _, item := range m.commitItems {
		if !item.IsDir {
			totalFiles++
			if item.Selected {
				selectedFiles++
			}
		}
	}

	var b strings.Builder
	b.WriteString(m.compactHeader("Pull incoming changes"))

	items := max(3, m.listInnerHeight()-5)
	end := min(len(m.commitItems), m.commitOffset+items)

	var c strings.Builder
	c.WriteString(mutedStyle.Render(fmt.Sprintf("Selected: %d of %d files", selectedFiles, totalFiles)) + "\n")
	c.WriteString(textStyle.Render("Incoming repository changes:") + "\n")
	c.WriteString(mutedStyle.Render("────────────────────────────") + "\n")

	for i := m.commitOffset; i < end; i++ {
		item := m.commitItems[i]
		isCursor := i == m.commitCursor
		cursor := " "
		if isCursor {
			cursor = ">"
		}

		if item.IsDir {
			allSel, anySel, count := true, false, 0
			for j := i + 1; j < len(m.commitItems) && !m.commitItems[j].IsDir; j++ {
				count++
				if m.commitItems[j].Selected {
					anySel = true
				} else {
					allSel = false
				}
			}
			dirCheckText := "[ ]"
			if count > 0 && allSel {
				dirCheckText = "[x]"
			} else if anySel {
				dirCheckText = "[-]"
			}
			if isCursor {
				c.WriteString(selectedStyle.Render(fmt.Sprintf("%s %s %s", cursor, dirCheckText, item.Path)) + "\n")
				continue
			}
			dirCheck := checkboxStyle.Render(dirCheckText)
			if dirCheckText == "[x]" {
				dirCheck = checkedStyle.Render(dirCheckText)
			} else if dirCheckText == "[-]" {
				dirCheck = mutedStyle.Render(dirCheckText)
			}
			c.WriteString(normalStyle.Render(fmt.Sprintf("%s %s ", cursor, dirCheck)) + labelMauveStyle.Render(item.Path) + "\n")
			continue
		}

		checkText := "[ ]"
		if item.Selected {
			checkText = "[x]"
		}
		if isCursor {
			c.WriteString(selectedStyle.Render(fmt.Sprintf("%s %s %s %s", cursor, checkText, item.Status, item.Path)) + "\n")
			continue
		}
		check := checkboxStyle.Render(checkText)
		if item.Selected {
			check = checkedStyle.Render(checkText)
		}
		status := colorizePullStatus(item.Status)
		if item.Selected {
			c.WriteString(checkedStyle.Render(fmt.Sprintf("%s %s ", cursor, checkText)) + status + " " + valueWhiteStyle.Render(item.Path) + "\n")
		} else {
			c.WriteString(fmt.Sprintf("%s %s ", cursor, check) + status + " " + valueWhiteStyle.Render(item.Path) + "\n")
		}
	}

	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.commitOffset, end, len(m.commitItems))))

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("Space", "select/dir"), hint("a", "all"), hint("n", "none"), hint("d", "diff"), hint("Enter", "pull selected"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewShelveSelect() string {
	selectedCount := len(selectedCommitItems(m.commitItems))
	unversionedCount := len(selectedUnversionedCommitPaths(m.commitItems))

	var b strings.Builder
	b.WriteString(m.compactHeader("Shelve local changes"))

	items := max(3, m.listInnerHeight()-5)
	end := min(len(m.commitItems), m.commitOffset+items)

	var c strings.Builder
	c.WriteString(mutedStyle.Render(fmt.Sprintf("Selected files: %d | Unversioned (will be copied): %d", selectedCount, unversionedCount)) + "\n")
	c.WriteString(textStyle.Render("Working copy changes:") + "\n")
	c.WriteString(mutedStyle.Render("─────────────────────") + "\n")
	for i := m.commitOffset; i < end; i++ {
		item := m.commitItems[i]
		isCursor := i == m.commitCursor
		cursor := " "
		if isCursor {
			cursor = ">"
		}
		checkText := "[ ]"
		if item.Selected {
			checkText = "[x]"
		}
		status := item.Status
		if item.Unversioned {
			status = "? copy"
		}
		line := fmt.Sprintf("%s %s %-8s %s", cursor, checkText, status, item.Path)
		if isCursor {
			c.WriteString(selectedStyle.Render(line) + "\n")
		} else if item.Selected {
			c.WriteString(checkedStyle.Render(line) + "\n")
		} else {
			c.WriteString(normalStyle.Render(line) + "\n")
		}
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.commitOffset, end, len(m.commitItems))))

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("Space", "select"), hint("a", "all"), hint("n", "none"), hint("d", "diff"), hint("Enter", "shelve"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewCommitSelect() string {
	selectedCount := len(selectedCommitItems(m.commitItems))
	unversionedCount := len(selectedUnversionedCommitPaths(m.commitItems))

	var b strings.Builder
	b.WriteString(m.compactHeader("Commit"))

	items := max(3, m.listInnerHeight()-5)
	end := min(len(m.commitItems), m.commitOffset+items)

	var c strings.Builder
	c.WriteString(mutedStyle.Render(fmt.Sprintf("Selected files: %d | Unversioned (will be svn add-ed): %d", selectedCount, unversionedCount)) + "\n")
	c.WriteString(textStyle.Render("Working copy changes:") + "\n")
	c.WriteString(mutedStyle.Render("─────────────────────") + "\n")
	for i := m.commitOffset; i < end; i++ {
		item := m.commitItems[i]
		isCursor := i == m.commitCursor
		cursor := " "
		if isCursor {
			cursor = ">"
		}
		checkText := "[ ]"
		if item.Selected {
			checkText = "[x]"
		}
		armed := m.deleteConfirmIdx == i
		status := item.Status
		if item.Unversioned {
			if armed {
				status = "? DEL!"
			} else {
				status = "? add"
			}
		} else if armed && len(item.Status) > 0 && item.Status[0] == 'A' {
			status = "A DEL!"
		}
		line := fmt.Sprintf("%s %s %-8s %s", cursor, checkText, status, item.Path)
		if armed {
			c.WriteString(errorStyle.Render(line) + "\n")
		} else if isCursor {
			c.WriteString(selectedStyle.Render(line) + "\n")
		} else {
			check := checkboxStyle.Render(checkText)
			if item.Selected {
				check = checkedStyle.Render(checkText)
			}
			line2 := fmt.Sprintf("%s %s %-8s %s", cursor, check, status, item.Path)
			if item.Selected {
				c.WriteString(checkedStyle.Render(line2) + "\n")
			} else {
				c.WriteString(normalStyle.Render(line2) + "\n")
			}
		}
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.commitOffset, end, len(m.commitItems))))

	b.WriteString(m.listBox(c.String()))

	if m.deleteConfirmIdx >= 0 {
		b.WriteString("\n" + warningStyle.Render("Del again: confirm permanent deletion — or move cursor to cancel"))
	} else {
		hints := []string{hint("Space", "select"), hint("a", "all"), hint("n", "none"), hint("d", "diff"), hint("p", "partial hunks"), hint("Enter", "commit message"), hint("i", "info"), hint("Esc", "back")}
		canDelete := func(ci model.CommitItem) bool {
			return ci.Unversioned || (len(ci.Status) > 0 && ci.Status[0] == 'A')
		}
		hasDeletable := false
		for _, ci := range m.commitItems {
			if canDelete(ci) {
				hasDeletable = true
				break
			}
		}
		if hasDeletable {
			if len(m.commitItems) > 0 && canDelete(m.commitItems[m.commitCursor]) {
				hints = append(hints, hint("Del", "delete"))
			} else {
				hints = append(hints, mutedStyle.Render("Del: delete"))
			}
		}
		b.WriteString(statusBar(hints...))
	}
	return b.String()
}

func (m Model) viewPartialHunkSelect() string {
	const maxPreview = 8
	selectedCount := len(m.partialHunks)
	for _, h := range m.partialHunks {
		if !h.Selected {
			selectedCount--
		}
	}
	_ = selectedCount

	var b strings.Builder
	b.WriteString(m.compactHeader("Partial commit hunks"))

	availHeight := max(3, m.listInnerHeight()-5)
	end := m.partialHunkOffset
	linesUsed := 0
	for i := m.partialHunkOffset; i < len(m.partialHunks); i++ {
		need := hunkDisplayLines(m.partialHunks[i], maxPreview)
		if linesUsed+need > availHeight {
			break
		}
		linesUsed += need
		end = i + 1
	}

	var c strings.Builder
	c.WriteString(mutedStyle.Render("File: "+m.partialItem.Path) + "\n")
	c.WriteString(mutedStyle.Render(fmt.Sprintf("Selected hunks: %d of %d", len(m.partialHunks)-countUnselected(m.partialHunks), len(m.partialHunks))) + "\n")
	c.WriteString(textStyle.Render("Choose hunks to commit:") + "\n")
	c.WriteString(mutedStyle.Render("───────────────────────") + "\n")
	for i := m.partialHunkOffset; i < end; i++ {
		hunk := m.partialHunks[i]
		isCursor := i == m.partialHunkCursor
		cursor := " "
		if isCursor {
			cursor = ">"
		}
		checkText := "[ ]"
		if hunk.Selected {
			checkText = "[x]"
		}
		if isCursor {
			summary := fmt.Sprintf("%s %s hunk %d  %s  +%d -%d", cursor, checkText, i+1, hunk.Header, hunk.Added, hunk.Removed)
			c.WriteString(selectedStyle.Render(summary) + "\n")
		} else {
			check := checkboxStyle.Render(checkText)
			if hunk.Selected {
				check = checkedStyle.Render(checkText)
			}
			addInfo := diffAddedStyle.Render(fmt.Sprintf("+%d", hunk.Added))
			removeInfo := diffDeletedStyle.Render(fmt.Sprintf("-%d", hunk.Removed))
			headerText := hunk.Header
			if hunk.Added > 0 && hunk.Removed > 0 {
				headerText = diffModifiedStyle.Render(headerText)
			}
			summary := fmt.Sprintf("%s %s hunk %d  %s  %s %s", cursor, check, i+1, headerText, addInfo, removeInfo)
			if hunk.Selected {
				c.WriteString(checkedStyle.Render(summary) + "\n")
			} else {
				c.WriteString(normalStyle.Render(summary) + "\n")
			}
		}
		for _, previewLine := range renderPartialHunkPreview(hunk, maxPreview) {
			c.WriteString("      " + previewLine + "\n")
		}
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.partialHunkOffset, end, len(m.partialHunks))))

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("Space", "select"), hint("a", "all"), hint("n", "none"), hint("Enter", "commit message"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func countUnselected(hunks []model.PartialHunk) int {
	n := 0
	for _, h := range hunks {
		if !h.Selected {
			n++
		}
	}
	return n
}

func (m Model) viewRevertSelect() string {
	selectedCount := len(selectedCommitPaths(m.commitItems))

	var b strings.Builder
	b.WriteString(m.compactHeader("Revert files"))

	items := max(3, m.listInnerHeight()-6)
	end := min(len(m.commitItems), m.commitOffset+items)

	var c strings.Builder
	c.WriteString(warningStyle.Render("Warning: SVN revert discards local changes for selected versioned files.") + "\n")
	c.WriteString(mutedStyle.Render(fmt.Sprintf("Selected files: %d", selectedCount)) + "\n")
	c.WriteString(textStyle.Render("Working copy changes:") + "\n")
	c.WriteString(mutedStyle.Render("─────────────────────") + "\n")
	for i := m.commitOffset; i < end; i++ {
		item := m.commitItems[i]
		isCursor := i == m.commitCursor
		cursor := " "
		if isCursor {
			cursor = ">"
		}
		checkText := "[ ]"
		if item.Selected {
			checkText = "[x]"
		}
		if isCursor {
			c.WriteString(selectedStyle.Render(fmt.Sprintf("%s %s %-8s %s", cursor, checkText, item.Status, item.Path)) + "\n")
			continue
		}
		check := checkboxStyle.Render(checkText)
		if item.Selected {
			check = checkedStyle.Render(checkText)
		}
		line := fmt.Sprintf("%s %s %-8s %s", cursor, check, item.Status, item.Path)
		if item.Selected {
			c.WriteString(checkedStyle.Render(line) + "\n")
		} else {
			c.WriteString(normalStyle.Render(line) + "\n")
		}
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.commitOffset, end, len(m.commitItems))))

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("Space", "select"), hint("a", "all"), hint("n", "none"), hint("d", "diff"), hint("Enter", "revert selected"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewCommitMessageInput() string {
	var b strings.Builder

	if m.partialCommit {
		selected := m.partialHunks
		var selHunks []model.PartialHunk
		for _, h := range selected {
			if h.Selected {
				selHunks = append(selHunks, h)
			}
		}

		b.WriteString(m.compactHeader("Partial commit message"))
		var cp strings.Builder
		cp.WriteString(mutedStyle.Render("File: "+m.partialItem.Path) + "\n")
		cp.WriteString(mutedStyle.Render(fmt.Sprintf("Selected hunks: %d", len(selHunks))) + "\n\n")
		previewLimit := min(6, len(selHunks))
		for i := range previewLimit {
			cp.WriteString(mutedStyle.Render(fmt.Sprintf("  hunk %d: %s", i+1, selHunks[i].Header)) + "\n")
		}
		if len(selHunks) > previewLimit {
			cp.WriteString(mutedStyle.Render(fmt.Sprintf("  ... and %d more", len(selHunks)-previewLimit)) + "\n")
		}
		cp.WriteString("\n")
		cp.WriteString(textStyle.Render("Commit message:") + "\n")
		cp.WriteString(m.input.View())
		b.WriteString(m.listBox(cp.String()))
		b.WriteString(statusBar(hint("Enter", "commit selected hunks"), hint("i", "info"), hint("Esc", "back")))
		return b.String()
	}

	items := selectedCommitItems(m.commitItems)
	b.WriteString(m.compactHeader("Commit message"))
	var cm strings.Builder
	cm.WriteString(mutedStyle.Render(fmt.Sprintf("Files selected: %d", len(items))) + "\n\n")
	previewLimit := min(8, len(items))
	for i := range previewLimit {
		prefix := "  "
		if items[i].Unversioned {
			prefix = "  + "
		}
		cm.WriteString(mutedStyle.Render(prefix+items[i].Path) + "\n")
	}
	if len(items) > previewLimit {
		cm.WriteString(mutedStyle.Render(fmt.Sprintf("  ... and %d more", len(items)-previewLimit)) + "\n")
	}
	if len(selectedUnversionedCommitPaths(m.commitItems)) > 0 {
		cm.WriteString("\n")
		cm.WriteString(warningStyle.Render("Selected unversioned files will be added with svn add before commit.") + "\n")
	}
	cm.WriteString("\n")
	cm.WriteString(textStyle.Render("Commit message:") + "\n")
	cm.WriteString(m.input.View())
	b.WriteString(m.listBox(cm.String()))
	b.WriteString(statusBar(hint("Enter", "commit"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewConflictSelect() string {
	var b strings.Builder
	b.WriteString(m.compactHeader("Resolve conflicts"))

	items := max(3, m.listInnerHeight()-5)
	end := min(len(m.conflictItems), m.conflictOffset+items)

	var c strings.Builder
	c.WriteString(textStyle.Render("Conflicted files:") + "\n")
	c.WriteString(mutedStyle.Render("─────────────────") + "\n")
	for i := m.conflictOffset; i < end; i++ {
		item := m.conflictItems[i]
		cursor := " "
		lineStyle := normalStyle
		if i == m.conflictCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}
		c.WriteString(lineStyle.Render(fmt.Sprintf("%s %-8s %s", cursor, item.Status, item.Path)) + "\n")
	}
	c.WriteString("\n")
	c.WriteString(warningStyle.Render("File conflicts → Meld. Tree conflicts → --accept=working.") + "\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.conflictOffset, end, len(m.conflictItems))))

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("↑↓/jk", "move"), hint("Enter", "resolve with Meld"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewHistory() string {
	title := strings.TrimSpace(m.historyTitle)
	if title == "" {
		title = "Commit history"
	}

	var b strings.Builder
	b.WriteString(m.compactHeader(title))

	// Overhead inside the box: search input (1) + optional last-search line (1)
	overhead := 0
	if m.screen == model.ScreenHistorySearch {
		overhead = 1
	}
	if strings.TrimSpace(m.historySearch) != "" {
		overhead++
	}
	vp := m.viewport
	vp.Height = max(3, m.listInnerHeight()-overhead)

	var c strings.Builder
	if m.screen == model.ScreenHistorySearch {
		c.WriteString(labelSapphireStyle.Render("Search revision: ") + m.input.View() + "\n")
	}
	if strings.TrimSpace(m.historySearch) != "" {
		c.WriteString(mutedStyle.Render("Last search: r"+strings.TrimPrefix(strings.TrimSpace(m.historySearch), "r")) + "\n")
	}
	c.WriteString(textStyle.Render(vp.View()))
	if strings.TrimSpace(m.result) != "" && strings.Contains(m.result, "was not found") {
		c.WriteString("\n" + warningStyle.Render(m.result))
	}

	b.WriteString(m.listBox(c.String()))

	if m.screen == model.ScreenHistorySearch {
		b.WriteString(statusBar(hint("Enter", "jump to revision"), hint("Esc", "cancel")))
	} else if m.selectedAction == model.ActionRevisionTree {
		b.WriteString(statusBar(hint("↑↓", "scroll"), hint("PgUp/PgDn", "page"), hint("/", "search"), hint("a", "full history"), hint("i", "info"), hint("Esc", "back"), hint("q", "quit")))
	} else {
		b.WriteString(statusBar(hint("↑↓", "scroll"), hint("PgUp/PgDn", "page"), hint("/", "search revision"), hint("i", "info"), hint("Esc", "back"), hint("q", "quit")))
	}
	return b.String()
}

func (m Model) viewDiff() string {
	var b strings.Builder
	b.WriteString(m.compactHeader("Side-by-side diff viewer"))

	vp := m.viewport
	vp.Height = max(3, m.listInnerHeight()-1)

	var c strings.Builder
	c.WriteString(mutedStyle.Render("Legend: = same | - removed/old | + added/new | ~ changed | CR = CRLF line") + "\n")
	c.WriteString(vp.View())

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("↑↓", "scroll"), hint("PgUp/PgDn", "page"), hint("Home/End", "jump"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewRunning() string {
	var b strings.Builder
	b.WriteString(m.compactHeader(m.runningTitle))

	items := max(3, m.listInnerHeight()-1)
	start := clamp(m.runningOffset, 0, max(0, len(m.runningLines)-1))
	end := min(start+items, len(m.runningLines))

	var c strings.Builder
	if len(m.runningLines) == 0 {
		c.WriteString(mutedStyle.Render("SVN is working. It may grumble a little."))
	} else {
		for _, line := range m.runningLines[start:end] {
			c.WriteString(line + "\n")
		}
		if !m.runningPinTail {
			c.WriteString(mutedStyle.Render(fmt.Sprintf(
				"↑/↓/PgUp/PgDn: scroll | Home: top | End: tail  [line %d/%d]",
				start+1, len(m.runningLines),
			)))
		}
	}

	b.WriteString(m.listBox(c.String()))
	return b.String()
}

func (m Model) viewResult() string {
	var b strings.Builder
	if m.err != nil {
		b.WriteString(m.compactHeader("Error"))
	} else {
		b.WriteString(m.compactHeader("Done"))
	}

	vp := m.viewport
	vp.Height = m.listInnerHeight()

	b.WriteString(m.listBox(vp.View()))
	b.WriteString(statusBar(hint("↑↓", "scroll"), hint("PgUp/PgDn", "page"), hint("Enter", "back to menu"), hint("q", "quit")))
	return b.String()
}
