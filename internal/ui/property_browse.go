package ui

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"svn-tui/internal/model"
	"svn-tui/internal/svn"
)

// ── Loading ───────────────────────────────────────────────────────────────────

// browsePropertyDirCmd lists one directory of the working copy for the property
// browser. Properties of the directory and of every child are read in the same
// pass, so moving the cursor never has to run svn again.
func browsePropertyDirCmd(r model.Repo, dir, selectPath, notice string) tea.Cmd {
	return func() tea.Msg {
		entries, err := loadPropertyBrowseEntries(r, dir)
		return model.PropertyBrowseLoadedMsg{
			Dir:     cleanBrowseDir(dir),
			Entries: entries,
			Select:  selectPath,
			Err:     err,
			Notice:  notice,
		}
	}
}

func loadPropertyBrowseEntries(r model.Repo, dir string) ([]model.PropertyBrowseEntry, error) {
	dir = cleanBrowseDir(dir)
	read, err := os.ReadDir(filepath.Join(r.Path, filepath.FromSlash(dir)))
	if err != nil {
		return nil, fmt.Errorf("could not list %s: %w", dir, err)
	}

	props := browsePropertiesByPath(r, dir)
	unversioned := browseUnversionedPaths(r, dir)
	ignores := svn.Ignores()

	// The directory itself leads the list: properties live on directories far
	// more often than on files, and the root is where a merge writes
	// svn:mergeinfo.
	entries := []model.PropertyBrowseEntry{{
		Label:       browseSelfLabel(dir),
		Path:        dir,
		Kind:        model.PropertyBrowseSelf,
		Props:       props[dir],
		Unversioned: unversioned[dir],
	}}
	if dir != "." {
		entries = append(entries, model.PropertyBrowseEntry{
			Label: "../",
			Path:  browseParentDir(dir),
			Kind:  model.PropertyBrowseParent,
		})
	}

	var dirs, files []model.PropertyBrowseEntry
	for _, de := range read {
		name := de.Name()
		if name == ".svn" || name == model.ShelvesDir || ignores.HidesName(name) {
			continue
		}
		rel := name
		if dir != "." {
			rel = dir + "/" + name
		}
		if ignores.HidesPath(rel) {
			continue
		}
		entry := model.PropertyBrowseEntry{
			Label:       name,
			Path:        rel,
			Kind:        model.PropertyBrowseFile,
			Props:       props[rel],
			Unversioned: unversioned[rel],
		}
		if de.IsDir() {
			entry.Label = name + "/"
			entry.Kind = model.PropertyBrowseDir
			dirs = append(dirs, entry)
		} else {
			files = append(files, entry)
		}
	}
	sortBrowseEntries(dirs)
	sortBrowseEntries(files)

	entries = append(entries, dirs...)
	entries = append(entries, files...)
	return entries, nil
}

func sortBrowseEntries(entries []model.PropertyBrowseEntry) {
	sort.Slice(entries, func(i, j int) bool {
		li, lj := strings.ToLower(entries[i].Label), strings.ToLower(entries[j].Label)
		if li == lj {
			return entries[i].Label < entries[j].Label
		}
		return li < lj
	})
}

// browsePropertiesByPath reads the properties of dir and of its immediate
// children in one svn call. An unversioned directory simply has none, so the
// error is swallowed: the browser still has to list it.
func browsePropertiesByPath(r model.Repo, dir string) map[string][]model.PropertyItem {
	out, err := svn.Run(r, "proplist", "-v", "--xml", "--depth", "immediates", dir)
	if err != nil {
		return nil
	}
	var parsed model.SVNPropListXML
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		return nil
	}
	byPath := map[string][]model.PropertyItem{}
	for _, t := range parsed.Targets {
		items := make([]model.PropertyItem, 0, len(t.Properties))
		for _, prop := range t.Properties {
			items = append(items, model.PropertyItem{Name: prop.Name, Value: prop.Value})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		byPath[normalizeBrowsePath(t.Path)] = items
	}
	return byPath
}

// browseUnversionedPaths collects the children SVN does not track, so the
// browser can say why a path takes no properties instead of failing on propset.
func browseUnversionedPaths(r model.Repo, dir string) map[string]bool {
	out, err := svn.Run(r, "status", "--xml", "--depth", "immediates", dir)
	if err != nil {
		return nil
	}
	var parsed model.SVNStatusXML
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		return nil
	}
	paths := map[string]bool{}
	for _, t := range parsed.Targets {
		for _, e := range t.Entries {
			switch e.WCStatus.Item {
			case "unversioned", "ignored", "external":
				paths[normalizeBrowsePath(e.Path)] = true
			}
		}
	}
	return paths
}

// normalizeBrowsePath brings an svn path into the same shape as the
// working-copy-relative paths the browser builds from the file system.
func normalizeBrowsePath(path string) string {
	p := strings.TrimPrefix(strings.TrimSpace(filepath.ToSlash(path)), "./")
	if p == "" {
		return "."
	}
	return p
}

func cleanBrowseDir(dir string) string {
	d := normalizeBrowsePath(dir)
	if d == "" || d == "/" {
		return "."
	}
	return strings.TrimSuffix(d, "/")
}

func browseParentDir(dir string) string {
	dir = cleanBrowseDir(dir)
	idx := strings.LastIndex(dir, "/")
	if idx < 0 {
		return "."
	}
	return dir[:idx]
}

func browseSelfLabel(dir string) string {
	if dir == "." {
		return "./  (working copy root)"
	}
	return "./  (this directory)"
}

// ── Key handling ──────────────────────────────────────────────────────────────

func (m Model) updatePropertyBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.propertyBrowseVisibleCount()
	prevCursor := m.propertyBrowseCursor
	m.propertyBrowseCursor = navigateCursor(m.propertyBrowseCursor, len(m.propertyBrowseEntries), visible, msg.String())
	if m.propertyBrowseCursor != prevCursor {
		m.propertyBrowseNotice = ""
	}

	entry, ok := m.propertyBrowseEntry()
	switch msg.String() {
	case "enter", "right", "l":
		if !ok {
			break
		}
		switch entry.Kind {
		case model.PropertyBrowseDir:
			return m.openPropertyBrowseDir(entry.Path, "")
		case model.PropertyBrowseParent:
			return m.openPropertyBrowseDir(entry.Path, m.propertyBrowseDir)
		default:
			return m.openPropertyListFor(entry)
		}
	case "left", "h", "backspace":
		if m.propertyBrowseDir == "." {
			break
		}
		return m.openPropertyBrowseDir(browseParentDir(m.propertyBrowseDir), m.propertyBrowseDir)
	case "p":
		if !ok || entry.Kind == model.PropertyBrowseParent {
			break
		}
		return m.openPropertyListFor(entry)
	case "a":
		if !ok || entry.Kind == model.PropertyBrowseParent {
			break
		}
		if entry.Unversioned {
			m.propertyBrowseNotice = entry.Path + " is not versioned — svn add it before setting properties."
			break
		}
		// The picker needs to know the kind: SVN takes svn:ignore only on a
		// directory and svn:executable only on a file.
		isDir := entry.Kind == model.PropertyBrowseDir || entry.Kind == model.PropertyBrowseSelf
		m.propertyTargets = nil
		m.propertyItems = nil
		m.propertyCursor, m.propertyOffset = 0, 0
		return m.startPropertyPick(entry.Path, isDir, entry.Props, true), nil
	case "r":
		return m.openPropertyBrowseDir(m.propertyBrowseDir, m.propertyBrowseTargetPath())
	case "/":
		m.input.Reset()
		m.input.Placeholder = "Path to search, e.g. inc/config"
		m.input.Focus()
		m.screen = model.ScreenPropertyTargetInput
		return m, nil
	}

	m.propertyBrowseOffset = adjustOffset(m.propertyBrowseOffset, m.propertyBrowseCursor, visible)
	return m, nil
}

func (m Model) openPropertyBrowseDir(dir, selectPath string) (tea.Model, tea.Cmd) {
	m.propertyBrowseNotice = ""
	return m, browsePropertyDirCmd(m.activeRepo, dir, selectPath, "")
}

func (m Model) openPropertyListFor(entry model.PropertyBrowseEntry) (tea.Model, tea.Cmd) {
	if entry.Unversioned {
		m.propertyBrowseNotice = entry.Path + " is not versioned — svn add it before setting properties."
		return m, nil
	}
	// An empty target list is what sends Esc back to the browser instead of the
	// path search results.
	m.propertyTargets = nil
	m.propertyCursor, m.propertyOffset = 0, 0
	m.screen, m.runningTitle = model.ScreenRunning, "Reading properties of "+entry.Path+"..."
	return m, loadPropertiesCmd(m.activeRepo, entry.Path, "")
}

func (m Model) propertyBrowseEntry() (model.PropertyBrowseEntry, bool) {
	if m.propertyBrowseCursor < 0 || m.propertyBrowseCursor >= len(m.propertyBrowseEntries) {
		return model.PropertyBrowseEntry{}, false
	}
	return m.propertyBrowseEntries[m.propertyBrowseCursor], true
}

// propertyBrowseTargetPath is the path the cursor stands on, used to restore
// the cursor after a reload.
func (m Model) propertyBrowseTargetPath() string {
	if entry, ok := m.propertyBrowseEntry(); ok && entry.Kind != model.PropertyBrowseParent {
		return entry.Path
	}
	return ""
}

func (m Model) propertyBrowseVisibleCount() int {
	return max(3, m.listInnerHeight()-4)
}

// ── View ──────────────────────────────────────────────────────────────────────

// propertyBrowseColumns splits the list box into the path list and the property
// pane. A narrow terminal gets the list alone, with the properties folded into
// the rows below it.
func (m Model) propertyBrowseColumns() (int, int) {
	inner := max(20, m.width-5)
	left := clamp(inner*2/5, 26, 56)
	left = min(left, inner-2)
	right := inner - left - 3
	// Below this the pane would truncate every property value, so the list
	// takes the whole box and folds the property names under each row.
	if right < 34 {
		return inner, 0
	}
	return left, right
}

func (m Model) viewPropertyBrowse() string {
	var b strings.Builder
	b.WriteString(m.compactHeader("Properties — " + m.propertyBrowseDir))

	leftWidth, rightWidth := m.propertyBrowseColumns()
	height := m.listInnerHeight()
	// One column of the left block is taken by its right padding.
	listWidth := leftWidth
	if rightWidth > 0 {
		listWidth--
	}
	left := m.propertyBrowseList(listWidth, rightWidth == 0)

	content := left
	if rightWidth > 0 {
		leftBlock := lipgloss.NewStyle().
			Width(leftWidth).
			Height(height).
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(catSurface1).
			PaddingRight(1).
			Render(left)
		rightBlock := lipgloss.NewStyle().
			Width(rightWidth).
			Height(height).
			PaddingLeft(1).
			Render(m.propertyBrowsePane(rightWidth-1, height))
		content = lipgloss.JoinHorizontal(lipgloss.Top, leftBlock, rightBlock)
	}

	b.WriteString(m.listBox(content))
	b.WriteString(statusBar(
		hint("↑↓/jk", "move"),
		hint("→/Enter", "open"),
		hint("←", "up"),
		hint("p", "properties"),
		hint("a", "add"),
		hint("/", "search path"),
		hint("Esc", "back"),
	))
	return b.String()
}

// propertyBrowseList renders the path column. compact folds the property names
// under each row, for terminals too narrow for the side pane.
func (m Model) propertyBrowseList(width int, compact bool) string {
	entries := m.propertyBrowseEntries
	visible := m.propertyBrowseVisibleCount()
	if compact {
		visible = max(2, visible/2)
	}
	end := min(len(entries), m.propertyBrowseOffset+visible)

	var c strings.Builder
	c.WriteString(textStyle.Render(truncateVisual("Path: "+m.propertyBrowseDir, width)) + "\n")
	c.WriteString(mutedStyle.Render(strings.Repeat("─", max(3, min(width, 40)))) + "\n")

	for i := m.propertyBrowseOffset; i < end; i++ {
		entry := entries[i]
		cursor := " "
		if i == m.propertyBrowseCursor {
			cursor = ">"
		}
		marker := ""
		switch {
		case entry.Unversioned:
			marker = "?"
		case len(entry.Props) > 0:
			marker = fmt.Sprintf("● %d", len(entry.Props))
		}
		nameWidth := max(4, width-lipgloss.Width(marker)-3)
		line := fmt.Sprintf("%s %s %s", cursor, padRightVisual(entry.Label, nameWidth), marker)
		c.WriteString(browseEntryStyle(entry, i == m.propertyBrowseCursor).Render(line) + "\n")

		if compact && len(entry.Props) > 0 {
			names := make([]string, 0, len(entry.Props))
			for _, prop := range entry.Props {
				names = append(names, prop.Name)
			}
			c.WriteString(mutedStyle.Render(truncateVisual("     "+strings.Join(names, ", "), width)) + "\n")
		}
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.propertyBrowseOffset, end, len(entries))))
	return c.String()
}

func browseEntryStyle(entry model.PropertyBrowseEntry, selected bool) lipgloss.Style {
	switch {
	case selected:
		return selectedStyle
	case entry.Unversioned:
		return mutedStyle
	case entry.Kind == model.PropertyBrowseSelf:
		return labelSapphireStyle
	case entry.Kind == model.PropertyBrowseParent:
		return mutedStyle
	case entry.Kind == model.PropertyBrowseDir:
		return actionStyle
	}
	return normalStyle
}

// propertyBrowsePane shows what the highlighted path already carries and what
// can be done with it, so the properties are visible without opening anything.
func (m Model) propertyBrowsePane(width, height int) string {
	entry, ok := m.propertyBrowseEntry()
	if !ok {
		return mutedStyle.Render("Empty directory.")
	}

	var c strings.Builder
	used := 0
	writeLine := func(s string) {
		c.WriteString(s + "\n")
		used++
	}

	if m.propertyBrowseNotice != "" {
		writeLine(warningStyle.Render(truncateVisual(m.propertyBrowseNotice, width)))
		writeLine("")
	}

	if entry.Kind == model.PropertyBrowseParent {
		writeLine(labelYellowStyle.Render("Parent directory"))
		writeLine("")
		writeLine(mutedStyle.Render("Enter or ← steps one level up."))
		return c.String()
	}

	writeLine(labelYellowStyle.Render("Properties of ") + valueWhiteStyle.Render(truncateVisual(entry.Path, max(4, width-14))))
	writeLine(mutedStyle.Render(strings.Repeat("─", max(3, min(width, 44)))))

	options := propertyBrowseOptions(entry)
	// Two lines for the blank line and the "Options" title, plus the options
	// themselves, are kept free at the bottom of the pane.
	budget := max(2, height-used-len(options)-3)

	switch {
	case entry.Unversioned:
		writeLine(warningStyle.Render("not versioned"))
		writeLine(mutedStyle.Render("svn add it first — SVN keeps properties on versioned paths only."))
	case len(entry.Props) == 0:
		writeLine(mutedStyle.Render("no properties on this path"))
	default:
		for _, prop := range entry.Props {
			if used >= budget {
				writeLine(mutedStyle.Render("…"))
				break
			}
			writeLine(checkedStyle.Render(truncateVisual(prop.Name, width)))
			for _, line := range propertyValuePreview(prop.Value, 2) {
				if used >= budget {
					break
				}
				writeLine(mutedStyle.Render(truncateVisual("  "+line, width)))
			}
		}
	}

	writeLine("")
	writeLine(labelYellowStyle.Render("Options"))
	for _, opt := range options {
		writeLine(mutedStyle.Render(truncateVisual("  "+opt, width)))
	}
	return c.String()
}

// propertyBrowseOptions lists what the highlighted row can do, so the pane
// answers "and now what?" without a trip to the status bar.
func propertyBrowseOptions(entry model.PropertyBrowseEntry) []string {
	if entry.Unversioned {
		return []string{"←    back to the parent directory", "/    search for another path"}
	}
	switch entry.Kind {
	case model.PropertyBrowseDir:
		return []string{
			"Enter  step into the directory",
			"p      open its property manager",
			"a      pick a property to set on it",
		}
	case model.PropertyBrowseSelf:
		return []string{
			"Enter  open the property manager (edit, delete)",
			"a      pick a property for this directory",
			"←      back to the parent directory",
		}
	}
	return []string{
		"Enter  open the property manager (edit, delete)",
		"a      pick a property for this file",
	}
}
