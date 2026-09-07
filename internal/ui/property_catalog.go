package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"svn-tui/internal/model"
)

// ── Catalog ───────────────────────────────────────────────────────────────────

// propertyTargetKind narrows the catalog: SVN refuses svn:executable on a
// directory and svn:ignore on a file, so neither is worth offering there.
type propertyTargetKind int

const (
	propertyForAny propertyTargetKind = iota
	propertyForDir
	propertyForFile
)

// propertyValueChoice is one ready-made value of a property whose values are a
// fixed set, so setting it never needs the keyboard.
type propertyValueChoice struct {
	Value   string
	Label   string
	Summary string
}

// propertyDef describes one well-known SVN property: what it is for, and what
// values it takes.
type propertyDef struct {
	Name        string
	Summary     string
	Kind        propertyTargetKind
	Values      []propertyValueChoice
	Placeholder string
}

// propertyCatalog lists the properties SVN itself documents. Anything outside
// it is still reachable through the "custom name" row of the picker.
var propertyCatalog = []propertyDef{
	{
		Name:        "svn:ignore",
		Summary:     "Names not reported as unversioned in this directory",
		Kind:        propertyForDir,
		Placeholder: `Names, one per line (\n makes a new line), e.g. *.log\nbuild`,
	},
	{
		Name:        "svn:global-ignores",
		Summary:     "Ignore patterns inherited by every subdirectory",
		Kind:        propertyForDir,
		Placeholder: `Patterns, one per line (\n makes a new line)`,
	},
	{
		Name:        "svn:externals",
		Summary:     "Other repository paths checked out inside this directory",
		Kind:        propertyForDir,
		Placeholder: `One external per line: "^/path/to/dir localdir"`,
	},
	{
		Name:        "svn:auto-props",
		Summary:     "Properties applied automatically to newly added files",
		Kind:        propertyForDir,
		Placeholder: `One rule per line: "*.sh = svn:executable=*"`,
	},
	{
		Name:        "svn:mergeinfo",
		Summary:     "Merge tracking — SVN normally maintains this itself",
		Kind:        propertyForAny,
		Placeholder: `One line per source: "/branches/x:12-34"`,
	},
	{
		Name:    "svn:eol-style",
		Summary: "Line endings written into the working copy",
		Kind:    propertyForFile,
		Values: []propertyValueChoice{
			{Value: "native", Label: "native", Summary: "the line ending of the checkout platform"},
			{Value: "LF", Label: "LF", Summary: "always \\n, whatever the platform"},
			{Value: "CRLF", Label: "CRLF", Summary: "always \\r\\n"},
			{Value: "CR", Label: "CR", Summary: "always \\r (classic Mac)"},
		},
	},
	{
		Name:    "svn:executable",
		Summary: "Checked out with the executable bit set",
		Kind:    propertyForFile,
		Values: []propertyValueChoice{
			{Value: "*", Label: "*", Summary: "the only value SVN stores for this property"},
		},
	},
	{
		Name:    "svn:needs-lock",
		Summary: "Read-only until locked, for files that cannot be merged",
		Kind:    propertyForFile,
		Values: []propertyValueChoice{
			{Value: "*", Label: "*", Summary: "the only value SVN stores for this property"},
		},
	},
	{
		Name:    "svn:keywords",
		Summary: "Keywords SVN substitutes inside the file",
		Kind:    propertyForFile,
		Values: []propertyValueChoice{
			{Value: "Id", Label: "Id", Summary: "compact: file, revision, date, author"},
			{Value: "Id Date Author", Label: "Id Date Author", Summary: "Id plus the separate date and author keywords"},
			{Value: "Date Author Revision URL", Label: "Date Author Revision URL", Summary: "every keyword except Id and Header"},
			{Value: "Header", Label: "Header", Summary: "like Id, but with the full URL"},
		},
	},
	{
		Name:    "svn:mime-type",
		Summary: "Content type; anything non-text turns off merging",
		Kind:    propertyForFile,
		Values: []propertyValueChoice{
			{Value: "text/plain", Label: "text/plain", Summary: "plain text, diffable and mergeable"},
			{Value: "text/html", Label: "text/html", Summary: "HTML source"},
			{Value: "application/json", Label: "application/json", Summary: "JSON source"},
			{Value: "application/octet-stream", Label: "application/octet-stream", Summary: "binary: no diff, no merge"},
		},
	},
}

func propertyDefFor(name string) (propertyDef, bool) {
	for _, def := range propertyCatalog {
		if def.Name == name {
			return def, true
		}
	}
	return propertyDef{}, false
}

// propertyChoice is one row of the name picker: a catalog entry, or the escape
// hatch to type a name SVN does not document.
type propertyChoice struct {
	Name    string
	Summary string
	Values  []propertyValueChoice
	Current string
	IsSet   bool
	Custom  bool
}

// propertyChoicesFor builds the picker rows for one target: the properties that
// fit its kind, with the ones already set marked and their value carried along
// so picking them edits instead of starting from scratch.
func propertyChoicesFor(isDir bool, existing []model.PropertyItem) []propertyChoice {
	current := map[string]string{}
	for _, item := range existing {
		current[item.Name] = item.Value
	}

	choices := make([]propertyChoice, 0, len(propertyCatalog)+len(existing)+1)
	for _, def := range propertyCatalog {
		if (def.Kind == propertyForDir && !isDir) || (def.Kind == propertyForFile && isDir) {
			continue
		}
		value, ok := current[def.Name]
		choices = append(choices, propertyChoice{
			Name:    def.Name,
			Summary: def.Summary,
			Values:  def.Values,
			Current: value,
			IsSet:   ok,
		})
	}
	// A property already on the path is always offered, even when it is not in
	// the catalog or does not match the kind — it is on the path either way.
	for _, item := range existing {
		if containsPropertyChoice(choices, item.Name) {
			continue
		}
		choices = append(choices, propertyChoice{
			Name:    item.Name,
			Summary: "already set on this path",
			Current: item.Value,
			IsSet:   true,
		})
	}
	return append(choices, propertyChoice{
		Name:    "Other property…",
		Summary: "type a name SVN does not document, or your own",
		Custom:  true,
	})
}

func containsPropertyChoice(choices []propertyChoice, name string) bool {
	for _, c := range choices {
		if c.Name == name {
			return true
		}
	}
	return false
}

// pathIsDir reports whether a working-copy-relative path is a directory, which
// decides half of the catalog.
func pathIsDir(r model.Repo, path string) bool {
	info, err := os.Stat(filepath.Join(r.Path, filepath.FromSlash(path)))
	return err == nil && info.IsDir()
}

// ── Entering the pickers ──────────────────────────────────────────────────────

// startPropertyPick opens the name picker for a target. existing is what the
// path already carries, so those rows can be marked and pre-filled.
func (m Model) startPropertyPick(target string, isDir bool, existing []model.PropertyItem, fromBrowse bool) Model {
	m.propertyTarget = target
	m.propertyTargetIsDir = isDir
	m.propertyAddFromBrowse = fromBrowse
	m.propertyEditing = false
	m.propertyName = ""
	m.propertyNotice = ""
	m.propertyNameChoices = propertyChoicesFor(isDir, existing)
	m.propertyNameCursor, m.propertyNameOffset = 0, 0
	m.propertyDeleteIdx = -1
	m.screen = model.ScreenPropertyNameSelect
	return m
}

// startPropertyValuePick opens the value picker for a property whose values are
// a fixed set, with the cursor on the value the path carries today.
func (m Model) startPropertyValuePick(name string, values []propertyValueChoice, current string) Model {
	m.propertyName = name
	m.propertyValueChoices = values
	m.propertyValueCursor, m.propertyValueOffset = 0, 0
	for i, choice := range values {
		if choice.Value == strings.TrimRight(current, "\n") {
			m.propertyValueCursor = i
			break
		}
	}
	m.screen = model.ScreenPropertyValueSelect
	return m
}

// startPropertyValueInput opens the free-text value editor, prefilled with the
// current value when there is one.
func (m Model) startPropertyValueInput(name, current string) Model {
	m.propertyName = name
	m.input.Reset()
	if current != "" {
		m.input.SetValue(collapsePropertyValue(current))
	}
	placeholder := `Property value (\n makes a new line)`
	if def, ok := propertyDefFor(name); ok && def.Placeholder != "" {
		placeholder = def.Placeholder
	}
	m.input.Placeholder = placeholder
	m.input.Focus()
	m.screen = model.ScreenPropertyValueInput
	return m
}

// openPropertyChoice is what Enter does on a name-picker row.
func (m Model) openPropertyChoice(choice propertyChoice) (tea.Model, tea.Cmd) {
	if choice.Custom {
		m.propertyName = ""
		m.input.Reset()
		m.input.Placeholder = "Property name, e.g. svn:ignore"
		m.input.Focus()
		m.screen = model.ScreenPropertyNameInput
		return m, nil
	}
	if len(choice.Values) > 0 {
		return m.startPropertyValuePick(choice.Name, choice.Values, choice.Current), nil
	}
	return m.startPropertyValueInput(choice.Name, choice.Current), nil
}

// ── Key handling ──────────────────────────────────────────────────────────────

func (m Model) updatePropertyNameSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(10)
	m.propertyNameCursor = navigateCursor(m.propertyNameCursor, len(m.propertyNameChoices), visible, msg.String())

	switch msg.String() {
	case "enter":
		if len(m.propertyNameChoices) == 0 {
			break
		}
		return m.openPropertyChoice(m.propertyNameChoices[m.propertyNameCursor])
	case "delete":
		if len(m.propertyNameChoices) == 0 {
			break
		}
		choice := m.propertyNameChoices[m.propertyNameCursor]
		if !choice.IsSet || choice.Custom {
			break
		}
		if m.propertyDeleteIdx != m.propertyNameCursor {
			m.propertyDeleteIdx = m.propertyNameCursor
			break
		}
		m.propertyDeleteIdx = -1
		m.screen, m.runningTitle = model.ScreenRunning, "Deleting "+choice.Name+"..."
		return m, deletePropertyCmd(m.activeRepo, m.propertyTarget, choice.Name)
	}
	if msg.String() != "delete" {
		m.propertyDeleteIdx = -1
	}

	m.propertyNameOffset = adjustOffset(m.propertyNameOffset, m.propertyNameCursor, visible)
	return m, nil
}

func (m Model) updatePropertyValueSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(10)
	m.propertyValueCursor = navigateCursor(m.propertyValueCursor, len(m.propertyValueChoices)+1, visible, msg.String())

	if msg.String() == "enter" {
		// The row past the fixed values is the way to a value SVN documents no
		// list for.
		if m.propertyValueCursor >= len(m.propertyValueChoices) {
			current := ""
			if def, ok := propertyDefFor(m.propertyName); ok && len(def.Values) > 0 {
				current = m.propertyCurrentValue()
			}
			return m.startPropertyValueInput(m.propertyName, current), nil
		}
		choice := m.propertyValueChoices[m.propertyValueCursor]
		m.propertyEditing = false
		m.screen, m.runningTitle = model.ScreenRunning, "Setting "+m.propertyName+"..."
		return m, setPropertyCmd(m.activeRepo, m.propertyTarget, m.propertyName, choice.Value)
	}

	m.propertyValueOffset = adjustOffset(m.propertyValueOffset, m.propertyValueCursor, visible)
	return m, nil
}

// propertyCurrentValue is the value the target carries for the property being
// edited, if any.
func (m Model) propertyCurrentValue() string {
	for _, item := range m.propertyItems {
		if item.Name == m.propertyName {
			return item.Value
		}
	}
	for _, choice := range m.propertyNameChoices {
		if choice.Name == m.propertyName {
			return choice.Current
		}
	}
	return ""
}

// ── Views ─────────────────────────────────────────────────────────────────────

func (m Model) viewPropertyNameSelect() string {
	var b strings.Builder
	b.WriteString(m.compactHeader("Property for " + m.propertyTarget))

	items := max(3, m.listInnerHeight()-4)
	end := min(len(m.propertyNameChoices), m.propertyNameOffset+items)

	kind := "file"
	if m.propertyTargetIsDir {
		kind = "directory"
	}

	var c strings.Builder
	c.WriteString(textStyle.Render("Pick a property — the list covers what SVN documents for a "+kind+":") + "\n")
	c.WriteString(mutedStyle.Render("────────────────────────────────────────────────────────────────") + "\n")

	nameWidth := 22
	for i := m.propertyNameOffset; i < end; i++ {
		choice := m.propertyNameChoices[i]
		cursor := " "
		if i == m.propertyNameCursor {
			cursor = ">"
		}
		mark := " "
		if choice.IsSet {
			mark = "●"
		}
		summary := choice.Summary
		if choice.IsSet && choice.Current != "" {
			summary = "set: " + compactPropertyValue(choice.Current)
		}
		line := fmt.Sprintf("%s %s %s %s", cursor, mark, padRightVisual(choice.Name, nameWidth), summary)
		line = truncateVisual(line, max(20, m.width-6))

		switch {
		case i == m.propertyDeleteIdx:
			c.WriteString(errorStyle.Render(line+"   DELETE!") + "\n")
		case i == m.propertyNameCursor:
			c.WriteString(selectedStyle.Render(line) + "\n")
		case choice.Custom:
			c.WriteString(actionStyle.Render(line) + "\n")
		case choice.IsSet:
			c.WriteString(checkedStyle.Render(line) + "\n")
		default:
			c.WriteString(normalStyle.Render(line) + "\n")
		}
	}
	c.WriteString("\n")
	c.WriteString(mutedStyle.Render(scrollHint(m.propertyNameOffset, end, len(m.propertyNameChoices))))

	b.WriteString(m.listBox(c.String()))
	if m.propertyDeleteIdx >= 0 {
		b.WriteString("\n" + warningStyle.Render("Del again: remove this property — or move cursor to cancel"))
		return b.String()
	}
	b.WriteString(statusBar(hint("↑↓/jk", "move"), hint("Enter", "pick"), hint("Del", "remove set one"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

func (m Model) viewPropertyValueSelect() string {
	var b strings.Builder
	b.WriteString(m.compactHeader(m.propertyName + " on " + m.propertyTarget))

	rows := len(m.propertyValueChoices) + 1
	items := max(3, m.listInnerHeight()-4)
	end := min(rows, m.propertyValueOffset+items)

	var c strings.Builder
	c.WriteString(textStyle.Render("Pick a value — it is set as soon as you press Enter:") + "\n")
	c.WriteString(mutedStyle.Render("─────────────────────────────────────────────────────") + "\n")

	labelWidth := 26
	for i := m.propertyValueOffset; i < end; i++ {
		cursor := " "
		if i == m.propertyValueCursor {
			cursor = ">"
		}
		label, summary, custom := "Other value…", "type a value by hand", true
		if i < len(m.propertyValueChoices) {
			choice := m.propertyValueChoices[i]
			label, summary, custom = choice.Label, choice.Summary, false
		}
		line := fmt.Sprintf("%s %s %s", cursor, padRightVisual(label, labelWidth), summary)
		line = truncateVisual(line, max(20, m.width-6))
		switch {
		case i == m.propertyValueCursor:
			c.WriteString(selectedStyle.Render(line) + "\n")
		case custom:
			c.WriteString(actionStyle.Render(line) + "\n")
		default:
			c.WriteString(normalStyle.Render(line) + "\n")
		}
	}
	if current := m.propertyCurrentValue(); current != "" {
		c.WriteString("\n" + mutedStyle.Render("Current value: "+compactPropertyValue(current)) + "\n")
	}

	b.WriteString(m.listBox(c.String()))
	b.WriteString(statusBar(hint("↑↓/jk", "move"), hint("Enter", "set"), hint("i", "info"), hint("Esc", "back")))
	return b.String()
}

// compactPropertyValue squeezes a value onto one line for a list row.
func compactPropertyValue(value string) string {
	one := strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimRight(value, "\n"), "\n", " ")), " ")
	return truncateVisual(one, 60)
}
