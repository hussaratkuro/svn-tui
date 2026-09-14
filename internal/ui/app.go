package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"svn-tui/internal/diff"
	"svn-tui/internal/model"
	"svn-tui/internal/svn"
	"svn-tui/internal/theme"
)

// resolveConfirmKind tracks which destructive resolve is armed and waiting for
// a second key press on the conflict screen.
type resolveConfirmKind int

const (
	resolveConfirmNone resolveConfirmKind = iota
	resolveConfirmAllTree
	resolveConfirmMine
	resolveConfirmTheirs
)

// Model is the main bubbletea model.
type Model struct {
	screen model.Screen
	width  int
	height int

	repos      []model.Repo
	repoCursor int
	repoOffset int
	activeRepo model.Repo

	actions          []string
	actionCursor     int
	actionOffset     int
	actionFilter     string
	actionFilterMode bool
	selectedAction   model.Action

	branches          []model.Branch
	branchCursor      int
	branchOffset      int
	branchNumberInput string
	branchFilter      string
	branchFilterMode  bool
	mergeBranch       model.Branch
	mergeRevisions    []model.BranchMergeRevision
	mergeCursor       int
	mergeOffset       int

	checkoutRevisions     []model.CheckoutRevision
	checkoutRevQuery      string
	checkoutRevFilterMode bool
	checkoutRevCursor     int
	checkoutRevOffset     int

	shelves        []string
	shelfCursor    int
	shelfOffset    int
	shelfDeleteIdx int

	fileHistoryQuery  string
	fileHistoryItems  []string
	fileHistoryCursor int
	fileHistoryOffset int

	propertyBrowseDir     string
	propertyBrowseEntries []model.PropertyBrowseEntry
	propertyBrowseCursor  int
	propertyBrowseOffset  int
	propertyBrowseNotice  string
	// propertyAddFromBrowse marks an "a" pressed in the browser: Esc out of the
	// name input then has no property list to return to.
	propertyAddFromBrowse bool

	propertyTargets      []string
	propertyTargetCursor int
	propertyTargetOffset int
	propertyTarget       string
	propertyItems        []model.PropertyItem
	propertyCursor       int
	propertyOffset       int
	propertyName         string
	propertyEditing      bool
	propertyTargetIsDir  bool
	propertyNameChoices  []propertyChoice
	propertyNameCursor   int
	propertyNameOffset   int
	propertyValueChoices []propertyValueChoice
	propertyValueCursor  int
	propertyValueOffset  int
	propertyDeleteIdx    int
	propertyNotice       string

	commitItems      []model.CommitItem
	commitConflicts  []string
	commitCursor     int
	commitOffset     int
	deleteConfirmIdx int

	partialItem       model.CommitItem
	partialHunks      []model.PartialHunk
	partialHunkCursor int
	partialHunkOffset int
	partialCommit     bool

	branchDelete model.BranchDeleteInfo

	branchDiffCtx    model.BranchDiffContext
	branchDiffItems  []model.BranchDiffItem
	branchDiffCursor int
	branchDiffOffset int

	conflictItems  []model.ConflictItem
	conflictCursor int
	conflictOffset int
	conflictNotice string
	resolveConfirm resolveConfirmKind

	input            textinput.Model
	viewport         viewport.Model
	diffLineKinds    []diffLineKind
	diffChangeCursor int

	historyTitle   string
	historyContent string
	historyBlocks  []historyBlock
	historySearch  string
	historyMatches []int
	historyCursor  int
	resultExpanded bool
	runningTitle   string
	runningLines   []string
	runningOffset  int
	runningPinTail bool
	result         string
	err            error
	showInfo       bool
	commands       commandPalette
}

func NewModel(repos []model.Repo) Model {
	input := textinput.New()
	input.Placeholder = "e.g. ASD-123 or create-branch-test"
	input.Focus()
	input.CharLimit = 200
	input.Width = 60

	vp := viewport.New(100, 30)

	m := Model{
		screen: model.ScreenRepoSelect,
		repos:  repos,
		actions: []string{
			"Pull", "Status", "Revert files", "Commit",
			"Create branch", "Switch to branch", "Merge branch",
			"Branch diff (since branch point)", "Branch diff (vs trunk HEAD)",
			"Delete branch",
			"Shelve local changes", "Unshelve changes", "Switch to trunk",
			"Checkout revision", "Resolve conflicts", "Cleanup", "Properties",
			"Commit history", "File history", "Revision tree", "Quit",
		},
		input:             input,
		viewport:          vp,
		runningPinTail:    true,
		deleteConfirmIdx:  -1,
		shelfDeleteIdx:    -1,
		propertyDeleteIdx: -1,
	}

	if len(repos) == 0 {
		m.screen = model.ScreenResult
		m.err = fmt.Errorf("no usable SVN repositories found")
		m.result = svn.HelpText()
		m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
	}

	return m
}

func (m Model) Init() tea.Cmd { return theme.Watch() }

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		applyTheme(msg.Palette)
		return m, theme.Watch()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = max(20, msg.Width-4)
		m.viewport.Height = max(5, msg.Height-6)
		if m.screen == model.ScreenDiff {
			m.syncDiffViewportSize()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case readOnlyDiffPreparedMsg:
		if msg.err != nil {
			if msg.tempDir != "" {
				_ = os.RemoveAll(msg.tempDir)
			}
			return m.showError("Failed to open merger.", msg.err.Error()), nil
		}
		m.screen = msg.returnScreen
		return m, runPreparedMerger(msg)

	case readOnlyDiffExitedMsg:
		m.screen = msg.returnScreen
		if msg.err != nil {
			return m.showError("merger exited with an error.", msg.err.Error()), nil
		}
		return m, nil

	case model.BranchesLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to load branches.", msg.Err.Error()), nil
		}
		m.branches = msg.Branches
		m.branchCursor, m.branchOffset, m.branchNumberInput = 0, 0, ""
		m.branchFilter, m.branchFilterMode = "", false
		// A branch diff nearly always targets the branch that is checked out,
		// so start the cursor there when the working copy is on one.
		if m.isBranchDiffAction() {
			m.branchCursor = currentBranchIndex(msg.Branches, m.activeRepo.CurrentLocation)
			m.branchOffset = adjustOffset(0, m.branchCursor, m.branchListVisibleCount())
		}
		m.screen = model.ScreenBranchSelect
		return m, nil

	case model.BranchMergeRevisionsLoadedMsg:
		if msg.Err != nil {
			content := "Failed to load branch revisions."
			if strings.TrimSpace(msg.Output) != "" {
				content += "\n\n" + msg.Output
			}
			content += "\n\n" + msg.Err.Error()
			return m.showErrorContent("Failed to load branch revisions.", content), nil
		}
		m.mergeBranch = msg.Branch
		m.mergeRevisions = msg.Revisions
		m.mergeCursor, m.mergeOffset = 0, 0
		m.screen = model.ScreenBranchMergeSelect
		return m, nil

	case model.BranchDeleteInfoLoadedMsg:
		if msg.Err != nil {
			return m.showErrorContent("Failed to load branch details.", "Failed to load branch details.\n\n"+msg.Err.Error()), nil
		}
		m.branchDelete = msg.Info
		m.input.Reset()
		m.input.Placeholder = branchDeleteConfirmWord
		m.input.Focus()
		m.screen = model.ScreenDeleteBranchConfirm
		return m, nil

	case model.BranchDiffLoadedMsg:
		if msg.Err != nil {
			content := "Failed to compare the branch."
			if strings.TrimSpace(msg.Context.Summary) != "" {
				content += "\n\n" + msg.Context.Summary
			}
			content += "\n\n" + msg.Err.Error()
			return m.showErrorContent("Failed to compare the branch.", content), nil
		}
		m.branchDiffCtx = msg.Context
		m.branchDiffItems = msg.Items
		m.branchDiffCursor, m.branchDiffOffset = 0, 0
		if len(msg.Items) == 0 {
			m.screen = model.ScreenResult
			m.err = nil
			m.result = "No differences found.\n\n" + msg.Context.Old.Label + "  ->  " + msg.Context.New.Label
			if strings.TrimSpace(msg.Context.Summary) != "" {
				m.result += "\n" + msg.Context.Summary
			}
			m.viewport.SetContent(m.result)
			return m, nil
		}
		m.screen = model.ScreenBranchDiffSelect
		return m, nil

	case model.ShelvesLoadedMsg:
		if msg.Err != nil {
			content := "Failed to load shelves."
			if strings.TrimSpace(msg.Output) != "" {
				content += "\n\n" + msg.Output
			}
			content += "\n\n" + msg.Err.Error()
			return m.showErrorContent("Failed to load shelves.", content), nil
		}
		if len(msg.Shelves) == 0 {
			result := "No shelves found."
			if strings.TrimSpace(msg.Output) != "" {
				result += "\n\n" + msg.Output
			}
			m.screen = model.ScreenResult
			m.err = nil
			m.result = result
			m.viewport.SetContent(result)
			return m, nil
		}
		m.shelves = msg.Shelves
		m.shelfCursor, m.shelfOffset = 0, 0
		m.shelfDeleteIdx = -1
		m.screen = model.ScreenShelfSelect
		return m, nil

	case model.PropertyTargetsLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to search paths.", msg.Err.Error()), nil
		}
		if len(msg.Items) == 0 {
			return m.showError("No path matches "+msg.Query+".", "no property target found"), nil
		}
		m.propertyTargets = msg.Items
		m.propertyTargetCursor, m.propertyTargetOffset = 0, 0
		m.screen = model.ScreenPropertyTargetSelect
		return m, nil

	case model.PropertyBrowseLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to list "+msg.Dir+".", msg.Err.Error()), nil
		}
		m.propertyBrowseDir = msg.Dir
		m.propertyBrowseEntries = msg.Entries
		m.propertyBrowseNotice = msg.Notice
		m.propertyBrowseCursor = 0
		if msg.Select != "" {
			for i, entry := range msg.Entries {
				if entry.Path == msg.Select && entry.Kind != model.PropertyBrowseParent {
					m.propertyBrowseCursor = i
					break
				}
			}
		}
		m.propertyBrowseOffset = adjustOffset(0, m.propertyBrowseCursor, m.propertyBrowseVisibleCount())
		m.screen = model.ScreenPropertyBrowse
		return m, nil

	case model.PropertiesLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to read properties of "+msg.Target+".", msg.Err.Error()), nil
		}
		m.propertyTarget = msg.Target
		m.propertyItems = msg.Items
		m.propertyNotice = msg.Notice
		m.propertyDeleteIdx = -1
		m.propertyCursor = clamp(m.propertyCursor, 0, max(0, len(msg.Items)-1))
		m.propertyOffset = adjustOffset(m.propertyOffset, m.propertyCursor, m.visibleListCount(10))
		m.screen = model.ScreenPropertyList
		return m, nil

	case model.PullItemsLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to load incoming pull changes.", msg.Err.Error()), nil
		}
		if len(msg.Items) == 0 {
			m.screen = model.ScreenResult
			m.result = "No incoming changes found. Working copy is already up to date."
			m.viewport.SetContent(m.result)
			return m, nil
		}
		m.commitItems = msg.Items
		m.commitCursor, m.commitOffset = 0, 0
		m.screen = model.ScreenPullSelect
		return m, nil

	case model.CommitItemsLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to load working copy changes.", msg.Err.Error()), nil
		}
		if len(msg.Items) == 0 {
			m.screen = model.ScreenResult
			m.result = "No committable changes found."
			if len(msg.Conflicted) > 0 {
				m.result += "\n\nStill in conflict, resolve these first:\n  " +
					strings.Join(msg.Conflicted, "\n  ")
			}
			m.viewport.SetContent(m.result)
			return m, nil
		}
		m.commitItems = msg.Items
		m.commitConflicts = msg.Conflicted
		m.commitCursor, m.commitOffset = 0, 0
		m.deleteConfirmIdx = -1
		m.screen = model.ScreenCommitSelect
		return m, nil

	case model.RevertItemsLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to load working copy changes.", msg.Err.Error()), nil
		}
		if len(msg.Items) == 0 {
			result := "No revertable changes found."
			if m.selectedAction == model.ActionShelveChanges {
				result = "No local changes found to shelve."
			}
			m.screen = model.ScreenResult
			m.result = result
			m.viewport.SetContent(result)
			return m, nil
		}
		m.commitItems = msg.Items
		m.commitCursor, m.commitOffset = 0, 0
		if m.selectedAction == model.ActionShelveChanges {
			m.screen = model.ScreenShelveSelect
		} else {
			m.screen = model.ScreenRevertSelect
		}
		return m, nil

	case model.ConflictItemsLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to load conflicts.", msg.Err.Error()), nil
		}
		if len(msg.Items) == 0 {
			m.screen = model.ScreenResult
			m.result = "No conflicts found."
			m.viewport.SetContent(m.result)
			return m, nil
		}
		m.conflictItems = msg.Items
		m.conflictCursor, m.conflictOffset = 0, 0
		m.conflictNotice = ""
		m.resolveConfirm = resolveConfirmNone
		m.screen = model.ScreenConflictSelect
		return m, nil

	case conflictToolPreparedMsg:
		if msg.err != nil {
			m.screen = model.ScreenConflictSelect
			m.conflictNotice = msg.err.Error()
			return m, nil
		}
		command := exec.Command(msg.executable, msg.args...)
		return m, tea.ExecProcess(command, func(err error) tea.Msg {
			return conflictToolExitedMsg{prepared: msg, err: err}
		})

	case conflictToolExitedMsg:
		if msg.err != nil {
			m.screen = model.ScreenConflictSelect
			if conflictToolCancelled(msg.err) {
				m.conflictNotice = "Merge cancelled; the SVN conflict is still unresolved."
			} else {
				m.conflictNotice = msg.prepared.tool + " exited with an error: " + msg.err.Error()
			}
			return m, nil
		}
		m.conflictNotice = ""
		m.screen, m.runningTitle = model.ScreenRunning, "Marking merged file as resolved..."
		return m, resolveConflictAfterToolCmd(msg.prepared)

	case model.HistoryLoadedMsg:
		m.screen = model.ScreenHistory
		m.err = msg.Err
		m.historyTitle = msg.Title
		if strings.TrimSpace(m.historyTitle) == "" {
			m.historyTitle = "Commit history"
		}
		if msg.Err != nil {
			m.historyContent = "Failed to load " + strings.ToLower(m.historyTitle) + ".\n\n" + msg.Output + "\n\n" + msg.Err.Error()
		} else {
			m.historyContent = msg.Output
		}
		m.historySearch = ""
		m.historyBlocks = parseHistoryBlocks(m.historyContent)
		m.historyMatches = nil
		m.historyCursor = 0
		m.viewport.SetContent(m.historyContent)
		m.viewport.GotoTop()
		return m, nil

	case model.CheckoutRevisionsLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to load revisions to search.", msg.Err.Error()), nil
		}
		m.checkoutRevisions = msg.Items
		m.checkoutRevFilterMode = false
		m.checkoutRevCursor, m.checkoutRevOffset = 0, 0
		m.screen = model.ScreenCheckoutRevisionSelect
		return m, nil

	case model.FileHistoryMatchesLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to search files.", msg.Err.Error()), nil
		}
		if len(msg.Items) == 0 {
			m.screen = model.ScreenResult
			m.result = fmt.Sprintf("No files found for search: %s", msg.Query)
			m.viewport.SetContent(m.result)
			return m, nil
		}
		m.fileHistoryQuery = msg.Query
		m.fileHistoryItems = msg.Items
		m.fileHistoryCursor, m.fileHistoryOffset = 0, 0
		m.screen = model.ScreenFileHistorySelect
		return m, nil

	case model.DiffLoadedMsg:
		m.screen = model.ScreenDiff
		m.err = msg.Err
		content := msg.Output
		if msg.Err != nil {
			content = "Failed to load side-by-side diff for:\n" + msg.Path + "\n\n" + msg.Output + "\n\n" + msg.Err.Error()
		} else if strings.TrimSpace(msg.Output) == "" {
			content = "No diff found for:\n" + msg.Path
		}
		m.diffLineKinds = classifyDiffLines(content)
		m.diffChangeCursor = -1
		m.syncDiffViewportSize()
		m.viewport.SetContent(content)
		m.viewport.GotoTop()
		return m, nil

	case model.PartialHunksLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to load partial commit hunks.", msg.Err.Error()), nil
		}
		if len(msg.Hunks) == 0 {
			m.screen = model.ScreenResult
			m.result = "No selectable hunks found for partial commit."
			m.viewport.SetContent(m.result)
			return m, nil
		}
		m.partialItem = msg.Item
		m.partialHunks = msg.Hunks
		m.partialHunkCursor, m.partialHunkOffset = 0, 0
		m.partialCommit = false
		m.screen = model.ScreenPartialHunkSelect
		return m, nil

	case model.StreamOutputMsg:
		m.runningLines = append(m.runningLines, msg.Line)
		if m.runningPinTail {
			m.runningOffset = max(0, len(m.runningLines)-m.visibleListCount(6))
		}
		return m, readNextSVNStream(msg.Ch)

	case model.CommandResult:
		m.runningLines = nil
		m.runningOffset = 0
		m.runningPinTail = true
		m.screen = model.ScreenResult
		m.result = msg.Output
		m.err = msg.Err
		m.resultExpanded = false

		location := svn.FirstNonEmpty(msg.CurrentLocation, svn.GetCurrentLocation(m.activeRepo))
		currentURL := svn.FirstNonEmpty(msg.URL, svn.GetCurrentURL(m.activeRepo))
		revision := svn.FirstNonEmpty(msg.CurrentRevision, svn.GetCurrentRevision(m.activeRepo))

		if location != "" {
			m.activeRepo.CurrentLocation = location
		}
		if currentURL != "" {
			m.activeRepo.URL = currentURL
		}
		if revision != "" {
			m.activeRepo.CurrentRevision = revision
		}
		for i := range m.repos {
			if m.repos[i].Path == m.activeRepo.Path {
				m.repos[i].CurrentLocation = m.activeRepo.CurrentLocation
				m.repos[i].URL = m.activeRepo.URL
				m.repos[i].CurrentRevision = m.activeRepo.CurrentRevision
				break
			}
		}

		content := msg.Output
		if msg.Err != nil {
			content += "\n\nError:\n" + msg.Err.Error()
		}
		m.viewport.SetContent(content)
		m.viewport.GotoTop()
		return m, nil
	}

	if m.screen == model.ScreenCreateBranchInput ||
		m.screen == model.ScreenCheckoutRevisionInput ||
		m.screen == model.ScreenCommitMessageInput ||
		m.screen == model.ScreenFileHistorySearch {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	if m.screen == model.ScreenResult || m.screen == model.ScreenHistory || m.screen == model.ScreenDiff {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) showError(result, errText string) Model {
	m.screen = model.ScreenResult
	m.err = fmt.Errorf("%s", errText)
	m.result = result
	m.viewport.SetContent(result + "\n\n" + errText)
	return m
}

func (m Model) showErrorContent(result, content string) Model {
	m.screen = model.ScreenResult
	m.err = fmt.Errorf("%s", result)
	m.result = result
	m.viewport.SetContent(content)
	return m
}

// ── Mouse ─────────────────────────────────────────────────────────────────────

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	steps := 0
	switch msg.Type {
	case tea.MouseWheelUp:
		steps = -3
	case tea.MouseWheelDown:
		steps = 3
	default:
		return m, nil
	}

	switch m.screen {
	case model.ScreenBranchSelect:
		m.branchNumberInput = ""
		filtered := m.filteredBranches()
		m.branchCursor = clamp(m.branchCursor+steps, 0, max(0, len(filtered)-1))
		m.branchOffset = adjustOffset(m.branchOffset, m.branchCursor, m.branchListVisibleCount())

	case model.ScreenBranchMergeSelect:
		total := len(m.mergeRevisions) + 1
		m.mergeCursor = clamp(m.mergeCursor+steps, 0, max(0, total-1))
		m.mergeOffset = adjustOffset(m.mergeOffset, m.mergeCursor, m.branchMergeListVisibleCount())

	case model.ScreenCheckoutRevisionSelect:
		filtered := m.filteredCheckoutRevisions()
		m.checkoutRevCursor = clamp(m.checkoutRevCursor+steps, 0, max(0, len(filtered)-1))
		m.checkoutRevOffset = adjustOffset(m.checkoutRevOffset, m.checkoutRevCursor, m.checkoutRevisionListVisibleCount())

	case model.ScreenBranchDiffSelect:
		m.branchDiffCursor = clamp(m.branchDiffCursor+steps, 0, max(0, len(m.branchDiffItems)-1))
		m.branchDiffOffset = adjustOffset(m.branchDiffOffset, m.branchDiffCursor, m.branchDiffListVisibleCount())

	case model.ScreenShelfSelect:
		m.shelfCursor = clamp(m.shelfCursor+steps, 0, len(m.shelves)-1)
		m.shelfOffset = adjustOffset(m.shelfOffset, m.shelfCursor, m.visibleListCount(7))

	case model.ScreenPullSelect:
		m.commitCursor = clamp(m.commitCursor+steps, 0, len(m.commitItems)-1)
		m.commitOffset = adjustOffset(m.commitOffset, m.commitCursor, m.pullListVisibleCount())

	case model.ScreenShelveSelect:
		m.commitCursor = clamp(m.commitCursor+steps, 0, len(m.commitItems)-1)
		m.commitOffset = adjustOffset(m.commitOffset, m.commitCursor, m.visibleListCount(12))

	case model.ScreenRunning:
		available := m.visibleListCount(6)
		total := len(m.runningLines)
		if steps < 0 {
			m.runningPinTail = false
			m.runningOffset = clamp(m.runningOffset+steps, 0, max(0, total-available))
		} else {
			newOffset := m.runningOffset + steps
			if newOffset >= total-available {
				m.runningPinTail = true
				m.runningOffset = max(0, total-available)
			} else {
				m.runningPinTail = false
				m.runningOffset = newOffset
			}
		}

	case model.ScreenHistory, model.ScreenDiff, model.ScreenResult:
		if m.screen == model.ScreenDiff {
			m.diffChangeCursor = -1
			m.syncDiffViewportSize()
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ── Key handlers ──────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.commands.open {
		return m.updateCommandPalette(msg)
	}
	if (msg.String() == "ctrl+p" || msg.String() == "ctrl+shift+p") && !m.inputActive() && m.screen != model.ScreenRunning {
		m.openCommandPalette()
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "i":
		if m.screen != model.ScreenRepoSelect && !m.inputActive() {
			m.showInfo = !m.showInfo
			if m.screen == model.ScreenDiff {
				m.syncDiffViewportSize()
			}
			return m, nil
		}

	case "/", "s":
		if m.screen == model.ScreenHistory && !m.inputActive() {
			m.input.Reset()
			m.input.Placeholder = "Search text: message, author, path, or revision"
			m.input.Focus()
			m.result = ""
			m.screen = model.ScreenHistorySearch
			return m, nil
		}

	case "a":
		if m.screen == model.ScreenHistory && m.selectedAction == model.ActionRevisionTree && !m.inputActive() {
			m.screen = model.ScreenRunning
			m.runningTitle = "Building full ASCII revision tree..."
			return m, loadRevisionTreeCmd(m.activeRepo, true)
		}

	case "q":
		if !m.inputActive() {
			if m.screen == model.ScreenRepoSelect ||
				m.screen == model.ScreenActionSelect ||
				m.screen == model.ScreenResult ||
				m.screen == model.ScreenHistory {
				return m, tea.Quit
			}
			m.screen = model.ScreenActionSelect
			return m, nil
		}

	case "esc":
		switch m.screen {
		case model.ScreenRepoSelect:
			return m, tea.Quit
		case model.ScreenActionSelect:
			if m.actionFilterMode {
				m.actionFilter, m.actionFilterMode = "", false
				m.actionCursor, m.actionOffset = 0, 0
				return m, nil
			}
			m.screen = model.ScreenRepoSelect
		case model.ScreenDiff:
			switch m.selectedAction {
			case model.ActionPull:
				m.screen = model.ScreenPullSelect
			case model.ActionRevertFiles:
				m.screen = model.ScreenRevertSelect
			case model.ActionShelveChanges:
				m.screen = model.ScreenShelveSelect
			case model.ActionBranchDiffFromStart, model.ActionBranchDiffVsTrunk:
				m.screen = model.ScreenBranchDiffSelect
			case model.ActionCommitHistory, model.ActionFileHistory, model.ActionRevisionTree:
				m.screen = model.ScreenHistory
				m.viewport.SetContent(m.historyContent)
				if len(m.historyBlocks) > 0 {
					m.viewport.YOffset = m.historyBlockOffset(m.historyCursor)
				}
			default:
				m.screen = model.ScreenCommitSelect
			}
		case model.ScreenCheckoutRevisionSelect:
			if m.checkoutRevFilterMode {
				m.checkoutRevFilterMode = false
				return m, nil
			}
			m.input.Focus()
			m.screen = model.ScreenCheckoutRevisionInput
		case model.ScreenBranchDiffSelect:
			m.screen = model.ScreenBranchSelect
		case model.ScreenBranchMergeSelect:
			m.screen = model.ScreenBranchSelect
		case model.ScreenDeleteBranchConfirm:
			m.input.Reset()
			m.screen = model.ScreenBranchSelect
		case model.ScreenHistorySearch:
			m.screen = model.ScreenHistory
		case model.ScreenPropertyTargetInput:
			if m.propertyBrowseDir != "" {
				m.screen = model.ScreenPropertyBrowse
			} else {
				m.screen = model.ScreenActionSelect
			}
		case model.ScreenPropertyTargetSelect:
			m.input.Focus()
			m.screen = model.ScreenPropertyTargetInput
		case model.ScreenPropertyList:
			if len(m.propertyTargets) > 0 {
				m.screen = model.ScreenPropertyTargetSelect
			} else if m.propertyBrowseDir != "" {
				// A property may have been set or deleted, so the browser is
				// reloaded rather than redrawn from stale entries.
				m.screen = model.ScreenPropertyBrowse
				return m, browsePropertyDirCmd(m.activeRepo, m.propertyBrowseDir, m.propertyTarget, "")
			} else {
				m.input.Focus()
				m.screen = model.ScreenPropertyTargetInput
			}
		case model.ScreenPropertyNameSelect:
			m.propertyDeleteIdx = -1
			if m.propertyAddFromBrowse {
				m.propertyAddFromBrowse = false
				m.screen = model.ScreenPropertyBrowse
				break
			}
			m.screen = model.ScreenPropertyList
		case model.ScreenPropertyNameInput:
			// The typed name is only reachable from the picker.
			m.screen = model.ScreenPropertyNameSelect
		case model.ScreenPropertyValueSelect:
			// Editing came from the property list; picking a value came from
			// the name picker.
			if m.propertyEditing {
				m.propertyEditing = false
				m.screen = model.ScreenPropertyList
				break
			}
			m.screen = model.ScreenPropertyNameSelect
		case model.ScreenPropertyValueInput:
			if def, ok := propertyDefFor(m.propertyName); ok && len(def.Values) > 0 {
				// The typed value is only reachable from the value picker.
				m.screen = model.ScreenPropertyValueSelect
				break
			}
			if m.propertyEditing {
				m.propertyEditing = false
				m.screen = model.ScreenPropertyList
				break
			}
			m.screen = model.ScreenPropertyNameSelect
		default:
			m.screen = model.ScreenActionSelect
		}
		return m, nil
	}

	switch m.screen {
	case model.ScreenRepoSelect:
		return m.updateRepoSelect(msg)
	case model.ScreenActionSelect:
		return m.updateActionSelect(msg)
	case model.ScreenCreateBranchInput:
		return m.updateCreateBranchInput(msg)
	case model.ScreenCheckoutRevisionInput:
		return m.updateCheckoutRevisionInput(msg)
	case model.ScreenCheckoutRevisionSelect:
		return m.updateCheckoutRevisionSelect(msg)
	case model.ScreenFileHistorySearch:
		return m.updateFileHistorySearch(msg)
	case model.ScreenFileHistorySelect:
		return m.updateFileHistorySelect(msg)
	case model.ScreenPropertyBrowse:
		return m.updatePropertyBrowse(msg)
	case model.ScreenPropertyTargetInput:
		return m.updatePropertyTargetInput(msg)
	case model.ScreenPropertyTargetSelect:
		return m.updatePropertyTargetSelect(msg)
	case model.ScreenPropertyList:
		return m.updatePropertyList(msg)
	case model.ScreenPropertyNameSelect:
		return m.updatePropertyNameSelect(msg)
	case model.ScreenPropertyNameInput:
		return m.updatePropertyNameInput(msg)
	case model.ScreenPropertyValueSelect:
		return m.updatePropertyValueSelect(msg)
	case model.ScreenPropertyValueInput:
		return m.updatePropertyValueInput(msg)
	case model.ScreenBranchSelect:
		return m.updateBranchSelect(msg)
	case model.ScreenBranchMergeSelect:
		return m.updateBranchMergeSelect(msg)
	case model.ScreenBranchDiffSelect:
		return m.updateBranchDiffSelect(msg)
	case model.ScreenDeleteBranchConfirm:
		return m.updateDeleteBranchConfirm(msg)
	case model.ScreenShelfSelect:
		return m.updateShelfSelect(msg)
	case model.ScreenPullSelect:
		return m.updatePullSelect(msg)
	case model.ScreenShelveSelect:
		return m.updateShelveSelect(msg)
	case model.ScreenCommitSelect:
		return m.updateCommitSelect(msg)
	case model.ScreenCommitMessageInput:
		return m.updateCommitMessageInput(msg)
	case model.ScreenPartialHunkSelect:
		return m.updatePartialHunkSelect(msg)
	case model.ScreenRevertSelect:
		return m.updateRevertSelect(msg)
	case model.ScreenConflictSelect:
		return m.updateConflictSelect(msg)
	case model.ScreenHistorySearch:
		return m.updateHistorySearch(msg)
	case model.ScreenResult:
		if msg.String() == "enter" {
			if m.err == nil && m.isCompactResultAction() && !m.resultExpanded {
				m.resultExpanded = true
				m.viewport.GotoTop()
			} else {
				m.screen = model.ScreenActionSelect
				m.result = ""
				m.err = nil
				m.resultExpanded = false
				m.viewport.SetContent("")
			}
		}
	case model.ScreenRunning:
		return m.updateRunningScroll(msg), nil
	case model.ScreenHistory:
		return m.updateHistoryScreen(msg)
	case model.ScreenDiff:
		return m.updateDiffScreen(msg)
	}
	return m, nil
}

// ── Screen update handlers ────────────────────────────────────────────────────

func (m Model) updateRunningScroll(msg tea.KeyMsg) Model {
	available := m.visibleListCount(6)
	total := len(m.runningLines)

	scroll := func(delta int) {
		m.runningPinTail = false
		m.runningOffset = clamp(m.runningOffset+delta, 0, max(0, total-available))
	}

	switch msg.String() {
	case "up", "k":
		scroll(-1)
	case "down", "j":
		scroll(1)
	case "pgup":
		scroll(-available)
	case "pgdown":
		if m.runningOffset+available >= total-available {
			m.runningPinTail = true
			m.runningOffset = max(0, total-available)
		} else {
			scroll(available)
		}
	case "home":
		m.runningPinTail = false
		m.runningOffset = 0
	case "end":
		m.runningPinTail = true
		m.runningOffset = max(0, total-available)
	}
	return m
}

func (m Model) updateDiffScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.syncDiffViewportSize()
	direction := 0
	switch msg.String() {
	case "alt+down":
		direction = 1
	case "alt+up":
		direction = -1
	}
	if direction != 0 {
		starts := diffChangeStarts(m.diffLineKinds)
		targetIndex := -1
		if m.diffChangeCursor >= 0 && m.diffChangeCursor < len(starts) {
			targetIndex = m.diffChangeCursor + direction
		} else if offset, ok := nextDiffChange(starts, m.viewport.YOffset, direction); ok {
			for i, start := range starts {
				if start == offset {
					targetIndex = i
					break
				}
			}
		}
		if targetIndex >= 0 && targetIndex < len(starts) {
			m.diffChangeCursor = targetIndex
			m.viewport.SetYOffset(starts[targetIndex])
		}
		return m, nil
	}

	m.diffChangeCursor = -1
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) updateHistorySearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		query := strings.TrimSpace(m.input.Value())
		m.historySearch = query
		m.result = ""
		m.screen = model.ScreenHistory
		m.historyMatches = nil
		if query == "" {
			return m, nil
		}
		matches := searchHistoryBlocks(m.historyContent, m.historyBlocks, query)
		if len(matches) == 0 {
			m.result = fmt.Sprintf("No matches found for %q in the currently loaded history.", query)
			return m, nil
		}
		m.historyMatches = matches
		m.historyCursor = matches[0]
		m.viewport.YOffset = m.historyBlockOffset(m.historyCursor)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateHistoryScreen handles the loaded commit history screen. Up/down (and
// j/k, pgup/pgdown, home/end) always step between commits one at a time — the
// default way to browse history — d opens the diff for the currently selected
// commit, and — when a text search is active — n/N jump directly to the
// next/previous matching commit.
func (m Model) updateHistoryScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.historyBlocks) > 0 {
		switch msg.String() {
		case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
			m.historyCursor = navigateCursor(m.historyCursor, len(m.historyBlocks), 5, msg.String())
			m.viewport.YOffset = m.historyBlockOffset(m.historyCursor)
			return m, nil
		case "n":
			if idx, ok := nextMatchIndex(m.historyMatches, m.historyCursor, 1); ok {
				m.historyCursor = idx
				m.viewport.YOffset = m.historyBlockOffset(m.historyCursor)
			}
			return m, nil
		case "N":
			if idx, ok := nextMatchIndex(m.historyMatches, m.historyCursor, -1); ok {
				m.historyCursor = idx
				m.viewport.YOffset = m.historyBlockOffset(m.historyCursor)
			}
			return m, nil
		case "d":
			rev := m.historyBlocks[m.historyCursor].revision
			m.screen, m.runningTitle = model.ScreenRunning, fmt.Sprintf("Loading diff for r%d...", rev)
			return m, commitDiffCmd(m.activeRepo, rev)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// historyBlockOffset returns the viewport line offset for the given commit
// index, clamped to the currently loaded history content.
func (m Model) historyBlockOffset(idx int) int {
	total := len(strings.Split(m.historyContent, "\n"))
	return clamp(m.historyBlocks[idx].startLine, 0, max(0, total-1))
}

func (m Model) updateRepoSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(7)
	m.repoCursor = navigateCursor(m.repoCursor, len(m.repos), visible, msg.String())

	if msg.String() == "enter" {
		m.activeRepo = m.repos[m.repoCursor]
		m.activeRepo.CurrentLocation = svn.GetCurrentLocation(m.activeRepo)
		m.activeRepo.CurrentRevision = svn.GetCurrentRevision(m.activeRepo)
		m.repos[m.repoCursor].CurrentLocation = m.activeRepo.CurrentLocation
		m.repos[m.repoCursor].CurrentRevision = m.activeRepo.CurrentRevision
		m.actionCursor, m.actionOffset = 0, 0
		m.screen = model.ScreenActionSelect
	}

	m.repoOffset = adjustOffset(m.repoOffset, m.repoCursor, visible)
	return m, nil
}

func (m Model) updateActionSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(10)
	key := msg.String()
	filtered := m.filteredActionIndexes()

	if m.actionFilterMode {
		switch key {
		case "enter":
			if len(filtered) == 0 {
				break
			}
			return m.runAction(model.Action(filtered[m.actionCursor]))
		case "backspace", "ctrl+h":
			if len(m.actionFilter) > 0 {
				m.actionFilter = m.actionFilter[:len(m.actionFilter)-1]
				m.actionCursor, m.actionOffset = 0, 0
			}
		case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
			m.actionCursor = navigateCursor(m.actionCursor, len(filtered), visible, key)
		default:
			if len(key) == 1 && key >= " " {
				m.actionFilter += key
				m.actionCursor, m.actionOffset = 0, 0
			}
		}
		filtered = m.filteredActionIndexes()
		m.actionCursor = clamp(m.actionCursor, 0, max(0, len(filtered)-1))
		m.actionOffset = adjustOffset(m.actionOffset, m.actionCursor, visible)
		return m, nil
	}

	m.actionCursor = navigateCursor(m.actionCursor, len(filtered), visible, key)

	switch key {
	case "/":
		m.actionFilterMode = true
		m.actionFilter = ""
		m.actionCursor, m.actionOffset = 0, 0
	case "enter":
		if len(filtered) == 0 {
			break
		}
		return m.runAction(model.Action(filtered[m.actionCursor]))
	}

	m.actionOffset = adjustOffset(m.actionOffset, m.actionCursor, visible)
	return m, nil
}

// filteredActionIndexes returns the positions in m.actions matching the search
// text. The position is the action itself, so the cursor always maps back to
// the right model.Action even while the list is filtered.
func (m Model) filteredActionIndexes() []int {
	if strings.TrimSpace(m.actionFilter) == "" {
		all := make([]int, len(m.actions))
		for i := range m.actions {
			all[i] = i
		}
		return all
	}
	filter := strings.ToLower(m.actionFilter)
	var result []int
	for i, name := range m.actions {
		if strings.Contains(strings.ToLower(name), filter) {
			result = append(result, i)
		}
	}
	return result
}

// runAction starts one action and drops any active search, so coming back from
// it shows the whole menu with the cursor on what was just run.
func (m Model) runAction(action model.Action) (tea.Model, tea.Cmd) {
	m.selectedAction = action
	m.actionFilter, m.actionFilterMode = "", false
	m.actionCursor = int(action)
	m.actionOffset = adjustOffset(0, m.actionCursor, m.visibleListCount(10))

	switch m.selectedAction {
	case model.ActionPull:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading incoming pull changes..."
		return m, loadPullItemsCmd(m.activeRepo)
	case model.ActionStatus:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading status..."
		return m, statusCmd(m.activeRepo)
	case model.ActionRevertFiles:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading revertable files..."
		return m, loadRevertItemsCmd(m.activeRepo)
	case model.ActionCheckoutRevision:
		m.input.Reset()
		m.input.Placeholder = "Revision number, or search text"
		m.input.Focus()
		m.screen = model.ScreenCheckoutRevisionInput
	case model.ActionCreateBranch:
		m.input.Reset()
		m.input.Placeholder = "e.g. ASD-123 or create-branch-test"
		m.input.Focus()
		m.screen = model.ScreenCreateBranchInput
	case model.ActionSwitchBranch:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading branches..."
		return m, loadBranchesCmd(m.activeRepo)
	case model.ActionMergeBranch:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading branches..."
		return m, loadBranchesCmd(m.activeRepo)
	case model.ActionBranchDiffFromStart, model.ActionBranchDiffVsTrunk:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading branches..."
		return m, loadBranchesCmd(m.activeRepo)
	case model.ActionDeleteBranch:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading branches..."
		return m, loadBranchesCmd(m.activeRepo)
	case model.ActionShelveChanges:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading local changes to shelve..."
		return m, loadShelveItemsCmd(m.activeRepo)
	case model.ActionUnshelveChanges:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading shelves..."
		return m, loadShelvesCmd(m.activeRepo)
	case model.ActionSwitchTrunk:
		m.screen, m.runningTitle = model.ScreenRunning, "Switching to trunk..."
		return m, switchTrunkCmd(m.activeRepo)
	case model.ActionCommit:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading working copy changes..."
		return m, loadCommitItemsCmd(m.activeRepo)
	case model.ActionResolveConflicts:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading conflicts..."
		return m, loadConflictItemsCmd(m.activeRepo)
	case model.ActionCleanup:
		m.screen, m.runningTitle = model.ScreenRunning, "Cleaning up working copy..."
		return m, cleanupCmd(m.activeRepo)
	case model.ActionProperties:
		// The browser is the way in: walking the working copy beats typing a
		// path, and the search stays one "/" away.
		m.propertyTargets = nil
		m.propertyTarget, m.propertyNotice, m.propertyBrowseNotice = "", "", ""
		m.propertyBrowseDir = "."
		m.propertyBrowseEntries = nil
		m.propertyBrowseCursor, m.propertyBrowseOffset = 0, 0
		m.screen, m.runningTitle = model.ScreenRunning, "Reading the working copy root..."
		return m, browsePropertyDirCmd(m.activeRepo, ".", "", "")
	case model.ActionCommitHistory:
		m.screen, m.runningTitle = model.ScreenRunning, "Loading commit history..."
		return m, loadHistoryCmd(m.activeRepo)
	case model.ActionFileHistory:
		m.input.Reset()
		m.input.Placeholder = "Search file path, e.g. action.php or inc/config"
		m.input.Focus()
		m.screen = model.ScreenFileHistorySearch
	case model.ActionRevisionTree:
		m.screen, m.runningTitle = model.ScreenRunning, "Building ASCII revision tree..."
		return m, loadRevisionTreeCmd(m.activeRepo, false)
	case model.ActionQuit:
		return m, tea.Quit
	}

	return m, nil
}

func (m Model) updateCreateBranchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		param := strings.TrimSpace(m.input.Value())
		if param == "" {
			return m.showError("Please enter a branch parameter, e.g. ASD-123.", "branch name parameter is required"), nil
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Creating branch..."
		return m, createBranchCmd(m.activeRepo, param)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateCheckoutRevisionInput never checks anything out on its own: Enter
// always opens the revision picker, so what is about to be checked out — the
// commit message and the files it changed — can be read first. An empty box
// lists every loaded revision.
func (m Model) updateCheckoutRevisionInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		return m.startCheckoutRevisionSearch(strings.TrimSpace(m.input.Value()))
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// startCheckoutRevisionSearch opens the picker on the given query, reusing the
// log already loaded in this session so refining a search costs no svn call.
func (m Model) startCheckoutRevisionSearch(query string) (tea.Model, tea.Cmd) {
	m.checkoutRevQuery = query
	m.checkoutRevFilterMode = false
	m.checkoutRevCursor, m.checkoutRevOffset = 0, 0
	if len(m.checkoutRevisions) > 0 {
		m.screen = model.ScreenCheckoutRevisionSelect
		return m, nil
	}
	m.screen, m.runningTitle = model.ScreenRunning, "Loading revisions..."
	return m, loadCheckoutRevisionsCmd(m.activeRepo)
}

// updateCheckoutRevisionSelect drives the revision picker. "/" edits the search
// in place so the list can be narrowed without leaving the screen.
func (m Model) updateCheckoutRevisionSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.checkoutRevisionListVisibleCount()
	key := msg.String()
	filtered := m.filteredCheckoutRevisions()

	if m.checkoutRevFilterMode {
		switch key {
		case "enter":
			if len(filtered) == 0 {
				break
			}
			m.checkoutRevFilterMode = false
			return m.startCheckoutOfRevision(filtered[m.checkoutRevCursor])
		case "backspace", "ctrl+h":
			if len(m.checkoutRevQuery) > 0 {
				m.checkoutRevQuery = m.checkoutRevQuery[:len(m.checkoutRevQuery)-1]
				m.checkoutRevCursor, m.checkoutRevOffset = 0, 0
			}
		case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
			m.checkoutRevCursor = navigateCursor(m.checkoutRevCursor, len(filtered), visible, key)
		default:
			if len(key) == 1 && key >= " " {
				m.checkoutRevQuery += key
				m.checkoutRevCursor, m.checkoutRevOffset = 0, 0
			}
		}
		filtered = m.filteredCheckoutRevisions()
		m.checkoutRevCursor = clamp(m.checkoutRevCursor, 0, max(0, len(filtered)-1))
		m.checkoutRevOffset = adjustOffset(m.checkoutRevOffset, m.checkoutRevCursor, visible)
		return m, nil
	}

	switch key {
	case "/":
		m.checkoutRevFilterMode = true
	case "enter":
		if len(filtered) == 0 {
			break
		}
		return m.startCheckoutOfRevision(filtered[m.checkoutRevCursor])
	default:
		m.checkoutRevCursor = navigateCursor(m.checkoutRevCursor, len(filtered), visible, key)
	}

	m.checkoutRevOffset = adjustOffset(m.checkoutRevOffset, m.checkoutRevCursor, visible)
	return m, nil
}

func (m Model) startCheckoutOfRevision(rev model.CheckoutRevision) (tea.Model, tea.Cmd) {
	revision := strconv.Itoa(rev.Revision)
	m.screen, m.runningTitle = model.ScreenRunning, "Checking out revision r"+revision+"..."
	return m, checkoutRevisionCmd(m.activeRepo, revision)
}

// filteredCheckoutRevisions narrows the loaded log with the current search
// text. Every whitespace-separated term has to match, so adding a word narrows
// the list instead of widening it.
func (m Model) filteredCheckoutRevisions() []model.CheckoutRevision {
	return filterCheckoutRevisions(m.checkoutRevisions, m.checkoutRevQuery)
}

func filterCheckoutRevisions(revisions []model.CheckoutRevision, query string) []model.CheckoutRevision {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return revisions
	}
	var result []model.CheckoutRevision
	for _, rev := range revisions {
		haystack := checkoutRevisionHaystack(rev)
		matches := true
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				matches = false
				break
			}
		}
		if matches {
			result = append(result, rev)
		}
	}
	return result
}

// checkoutRevisionHaystack is what a search term is matched against: the
// revision number (with and without the r prefix), the author, the date, the
// message and every changed path.
func checkoutRevisionHaystack(rev model.CheckoutRevision) string {
	var b strings.Builder
	b.WriteString("r")
	b.WriteString(strconv.Itoa(rev.Revision))
	b.WriteString(" " + rev.Author)
	b.WriteString(" " + rev.Date)
	b.WriteString(" " + rev.Msg)
	for _, p := range rev.Paths {
		b.WriteString(" " + p.Path)
	}
	return strings.ToLower(b.String())
}

// checkoutRevisionMatchedPath returns the changed path that put a row in the
// list, so a search by file name shows which file it hit. It is empty when the
// commit message already carries the term.
func checkoutRevisionMatchedPath(rev model.CheckoutRevision, query string) string {
	msg := strings.ToLower(rev.Msg)
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if strings.Contains(msg, term) {
			continue
		}
		for _, p := range rev.Paths {
			if strings.Contains(strings.ToLower(p.Path), term) {
				return p.Path
			}
		}
	}
	return ""
}

func (m Model) updateFileHistorySearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		query := strings.TrimSpace(m.input.Value())
		if query == "" {
			return m.showError("Please enter a file path search query, e.g. action.php or inc/config.", "file history search query is required"), nil
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Searching files..."
		return m, searchFileHistoryMatchesCmd(m.activeRepo, query)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateBranchMergeSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.mergeRevisions) + 1 // the first row is the merge-through-HEAD action
	visible := m.branchMergeListVisibleCount()
	m.mergeCursor = navigateCursor(m.mergeCursor, total, visible, msg.String())

	if msg.String() == "enter" {
		revision := ""
		m.runningTitle = "Merging branch through HEAD..."
		if m.mergeCursor > 0 {
			revision = strconv.Itoa(m.mergeRevisions[m.mergeCursor-1].Revision)
			m.runningTitle = "Merging branch revision r" + revision + "..."
		}
		m.screen = model.ScreenRunning
		return m, mergeBranchCmd(m.activeRepo, m.mergeBranch.Name, revision)
	}

	m.mergeOffset = adjustOffset(m.mergeOffset, m.mergeCursor, visible)
	return m, nil
}

func (m Model) updateFileHistorySelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(12)
	m.fileHistoryCursor = navigateCursor(m.fileHistoryCursor, len(m.fileHistoryItems), visible, msg.String())

	if msg.String() == "enter" && len(m.fileHistoryItems) > 0 {
		path := m.fileHistoryItems[m.fileHistoryCursor]
		m.screen, m.runningTitle = model.ScreenRunning, "Loading file history..."
		return m, loadFileHistoryCmd(m.activeRepo, path)
	}

	m.fileHistoryOffset = adjustOffset(m.fileHistoryOffset, m.fileHistoryCursor, visible)
	return m, nil
}

func (m Model) filteredBranches() []model.Branch {
	if m.branchFilter == "" {
		return m.branches
	}
	filter := strings.ToLower(m.branchFilter)
	var result []model.Branch
	for _, br := range m.branches {
		if strings.Contains(strings.ToLower(br.Name), filter) {
			result = append(result, br)
		}
	}
	return result
}

func (m Model) updateBranchSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.branchListVisibleCount()
	key := msg.String()
	filtered := m.filteredBranches()

	if m.branchFilterMode {
		switch key {
		case "esc":
			m.branchFilter, m.branchFilterMode = "", false
			m.branchCursor, m.branchOffset = 0, 0
		case "enter":
			if len(filtered) == 0 {
				break
			}
			selected := filtered[m.branchCursor]
			m.branchFilter, m.branchFilterMode = "", false
			return m.startBranchAction(selected)
		case "backspace", "ctrl+h":
			if len(m.branchFilter) > 0 {
				m.branchFilter = m.branchFilter[:len(m.branchFilter)-1]
				m.branchCursor = 0
				m.branchOffset = 0
			}
		case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
			m.branchCursor = navigateCursor(m.branchCursor, len(filtered), visible, key)
		default:
			if len(key) == 1 && key >= " " {
				m.branchFilter += key
				m.branchCursor = 0
				m.branchOffset = 0
			}
		}
		filtered = m.filteredBranches()
		m.branchCursor = clamp(m.branchCursor, 0, max(0, len(filtered)-1))
		m.branchOffset = adjustOffset(m.branchOffset, m.branchCursor, visible)
		return m, nil
	}

	switch key {
	case "/":
		m.branchFilterMode = true
		m.branchFilter = ""
		m.branchCursor, m.branchOffset = 0, 0

	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		m.branchNumberInput = ""
		m.branchCursor = navigateCursor(m.branchCursor, len(m.branches), visible, key)

	case "backspace", "ctrl+h":
		if len(m.branchNumberInput) > 0 {
			m.branchNumberInput = m.branchNumberInput[:len(m.branchNumberInput)-1]
		}

	case "enter":
		selectedIndex := m.branchCursor
		if strings.TrimSpace(m.branchNumberInput) != "" {
			var number int
			if _, err := fmt.Sscanf(m.branchNumberInput, "%d", &number); err != nil || number < 1 || number > len(m.branches) {
				return m.showError(fmt.Sprintf("Branch number must be between 1 and %d.", len(m.branches)), fmt.Sprintf("invalid branch number: %s", m.branchNumberInput)), nil
			}
			selectedIndex = number - 1
		}
		selected := m.branches[selectedIndex]
		m.branchNumberInput = ""
		return m.startBranchAction(selected)

	default:
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			m.branchNumberInput += key
			if len(m.branchNumberInput) > 6 {
				m.branchNumberInput = m.branchNumberInput[len(m.branchNumberInput)-6:]
			}
			var number int
			if _, err := fmt.Sscanf(m.branchNumberInput, "%d", &number); err == nil && number >= 1 && number <= len(m.branches) {
				m.branchCursor = number - 1
			}
		}
	}

	m.branchOffset = adjustOffset(m.branchOffset, m.branchCursor, visible)
	return m, nil
}

// startBranchAction runs the action the branch list was opened for.
func (m Model) startBranchAction(selected model.Branch) (tea.Model, tea.Cmd) {
	switch m.selectedAction {
	case model.ActionMergeBranch:
		m.mergeBranch = selected
		m.screen, m.runningTitle = model.ScreenRunning, "Loading branch revisions..."
		return m, loadBranchMergeRevisionsCmd(m.activeRepo, selected)
	case model.ActionBranchDiffFromStart:
		m.screen, m.runningTitle = model.ScreenRunning, "Comparing branch with its branch point..."
		return m, loadBranchDiffCmd(m.activeRepo, selected.Name, model.BranchDiffSinceBranchPoint)
	case model.ActionBranchDiffVsTrunk:
		m.screen, m.runningTitle = model.ScreenRunning, "Comparing branch with trunk HEAD..."
		return m, loadBranchDiffCmd(m.activeRepo, selected.Name, model.BranchDiffAgainstTrunkHead)
	case model.ActionDeleteBranch:
		// Enter never deletes: it only opens the confirmation screen, which
		// wants the branch details and a typed-out confirmation first.
		m.screen, m.runningTitle = model.ScreenRunning, "Loading branch details..."
		return m, loadBranchDeleteInfoCmd(m.activeRepo, selected.Name)
	}
	m.screen, m.runningTitle = model.ScreenRunning, "Switching to branch..."
	return m, switchBranchCmd(m.activeRepo, selected.Name)
}

func (m Model) isBranchDiffAction() bool {
	return m.selectedAction == model.ActionBranchDiffFromStart || m.selectedAction == model.ActionBranchDiffVsTrunk
}

// currentBranchIndex finds the checked-out branch in the branch list, using the
// working copy location ("branches/<name>"). It returns 0 when on trunk.
func currentBranchIndex(branches []model.Branch, location string) int {
	loc := strings.Trim(strings.TrimSpace(location), "/")
	if !strings.HasPrefix(loc, "branches/") {
		return 0
	}
	name := strings.TrimPrefix(loc, "branches/")
	if slash := strings.Index(name, "/"); slash >= 0 {
		name = name[:slash]
	}
	for i, br := range branches {
		if br.Name == name {
			return i
		}
	}
	return 0
}

func (m Model) updateBranchDiffSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.branchDiffListVisibleCount()
	m.branchDiffCursor = navigateCursor(m.branchDiffCursor, len(m.branchDiffItems), visible, msg.String())

	switch msg.String() {
	case "enter", "m":
		if len(m.branchDiffItems) == 0 {
			break
		}
		item := m.branchDiffItems[m.branchDiffCursor]
		if item.IsDir {
			return m.showError("Select a file to open in merger.", "directory comparison is not available for repository URLs"), nil
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Preparing merger comparison..."
		return m, prepareBranchMergerCmd(m.activeRepo, m.branchDiffCtx, item, model.ScreenBranchDiffSelect)
	case "d":
		if len(m.branchDiffItems) == 0 {
			break
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Loading built-in side-by-side diff..."
		return m, branchFileDiffCmd(m.activeRepo, m.branchDiffCtx, m.branchDiffItems[m.branchDiffCursor], m.diffViewportWidth())
	case "u":
		m.screen, m.runningTitle = model.ScreenRunning, "Loading full unified diff..."
		return m, branchFullDiffCmd(m.activeRepo, m.branchDiffCtx)
	}

	m.branchDiffOffset = adjustOffset(m.branchDiffOffset, m.branchDiffCursor, visible)
	return m, nil
}

func (m Model) updateDeleteBranchConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		typed := strings.ToLower(strings.TrimSpace(m.input.Value()))
		if typed != branchDeleteConfirmWord {
			m.input.Reset()
			return m.showError(
				fmt.Sprintf("Branch %s was NOT deleted. Type %s to confirm.", m.branchDelete.Name, branchDeleteConfirmWord),
				"delete confirmation did not match"), nil
		}
		m.input.Reset()
		m.screen, m.runningTitle = model.ScreenRunning, "Deleting branch..."
		return m, deleteBranchCmd(m.activeRepo, m.branchDelete.Name)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateShelfSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(7)
	prevCursor := m.shelfCursor
	m.shelfCursor = navigateCursor(m.shelfCursor, len(m.shelves), visible, msg.String())
	if m.shelfCursor != prevCursor {
		m.shelfDeleteIdx = -1
	}

	switch msg.String() {
	case "enter":
		if len(m.shelves) == 0 {
			break
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Unshelving changes..."
		return m, unshelveChangesCmd(m.activeRepo, m.shelves[m.shelfCursor])
	case "delete":
		// Deleting a shelf discards saved changes for good, so it arms on the
		// first press and runs on the second.
		if len(m.shelves) == 0 {
			break
		}
		if m.shelfDeleteIdx != m.shelfCursor {
			m.shelfDeleteIdx = m.shelfCursor
			break
		}
		name := m.shelves[m.shelfCursor]
		m.shelfDeleteIdx = -1
		if err := deleteShelf(m.activeRepo, name); err != nil {
			return m.showError("Failed to delete shelf "+name+".", err.Error()), nil
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Deleting shelf "+name+"..."
		return m, loadShelvesCmd(m.activeRepo)
	}

	m.shelfOffset = adjustOffset(m.shelfOffset, m.shelfCursor, visible)
	return m, nil
}

func (m Model) updatePullSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.pullListVisibleCount()
	m.commitCursor = navigateCursor(m.commitCursor, len(m.commitItems), visible, msg.String())

	switch msg.String() {
	case " ":
		if len(m.commitItems) == 0 {
			break
		}
		item := m.commitItems[m.commitCursor]
		if item.IsDir {
			allSel := true
			count := 0
			for i := m.commitCursor + 1; i < len(m.commitItems) && !m.commitItems[i].IsDir; i++ {
				count++
				if !m.commitItems[i].Selected {
					allSel = false
				}
			}
			if count > 0 {
				newVal := !allSel
				for i := m.commitCursor + 1; i < len(m.commitItems) && !m.commitItems[i].IsDir; i++ {
					m.commitItems[i].Selected = newVal
				}
			}
		} else {
			m.commitItems[m.commitCursor].Selected = !m.commitItems[m.commitCursor].Selected
		}
	case "a":
		for i := range m.commitItems {
			if !m.commitItems[i].IsDir {
				m.commitItems[i].Selected = true
			}
		}
	case "n":
		for i := range m.commitItems {
			if !m.commitItems[i].IsDir {
				m.commitItems[i].Selected = false
			}
		}
	case "d":
		if len(m.commitItems) > 0 && !m.commitItems[m.commitCursor].IsDir {
			m.screen, m.runningTitle = model.ScreenRunning, "Loading incoming diff..."
			return m, remoteDiffCmd(m.activeRepo, m.commitItems[m.commitCursor], m.width-4)
		}
	case "m":
		if len(m.commitItems) > 0 && !m.commitItems[m.commitCursor].IsDir {
			m.screen, m.runningTitle = model.ScreenRunning, "Preparing merger comparison..."
			return m, prepareIncomingMergerCmd(m.activeRepo, m.commitItems[m.commitCursor], model.ScreenPullSelect)
		}
	case "enter":
		hasSelected := false
		for _, item := range m.commitItems {
			if !item.IsDir && item.Selected {
				hasSelected = true
				break
			}
		}
		if !hasSelected {
			return m.showError("Select at least one incoming file with Space before pulling. Use 'a' to select all.", "no files selected"), nil
		}
		paths := pullUpdatePaths(m.commitItems)
		m.screen, m.runningTitle = model.ScreenRunning, "Pulling selected files..."
		return m, pullCmd(m.activeRepo, paths)
	}

	m.commitOffset = adjustOffset(m.commitOffset, m.commitCursor, visible)
	return m, nil
}

func (m Model) updateCommitSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(12)
	prevCursor := m.commitCursor
	m.commitCursor = navigateCursor(m.commitCursor, len(m.commitItems), visible, msg.String())
	if m.commitCursor != prevCursor {
		m.deleteConfirmIdx = -1
	}

	switch msg.String() {
	case "delete":
		if len(m.commitItems) == 0 {
			break
		}
		item := m.commitItems[m.commitCursor]
		statusIsA := len(item.Status) > 0 && item.Status[0] == 'A'
		if !item.Unversioned && !statusIsA {
			break
		}
		if m.deleteConfirmIdx != m.commitCursor {
			m.deleteConfirmIdx = m.commitCursor
			break
		}
		m.deleteConfirmIdx = -1
		fullPath := fmt.Sprintf("%s/%s", m.activeRepo.Path, item.Path)
		if statusIsA {
			if _, err := svn.Run(m.activeRepo, "revert", "--depth", "infinity", item.Path); err != nil {
				return m.showError("Failed to revert "+item.Path, err.Error()), nil
			}
		}
		var deleteErr error
		if item.IsDir {
			deleteErr = removeAll(fullPath)
		} else {
			deleteErr = removeFile(fullPath)
		}
		if deleteErr != nil {
			return m.showError("Failed to delete "+item.Path, deleteErr.Error()), nil
		}
		prefix := item.Path + "/"
		kept := m.commitItems[:0:0]
		for _, ci := range m.commitItems {
			if ci.Path != item.Path && !strings.HasPrefix(ci.Path, prefix) {
				kept = append(kept, ci)
			}
		}
		m.commitItems = kept
		if m.commitCursor >= len(m.commitItems) {
			m.commitCursor = max(0, len(m.commitItems)-1)
		}

	case " ":
		if len(m.commitItems) > 0 {
			root := commitSelectionRootIndex(m.commitItems, m.commitCursor)
			setCommitSelection(m.commitItems, root, !m.commitItems[root].Selected)
		}
	case "a":
		for i := range m.commitItems {
			if m.commitItems[i].IncludedByParent == "" {
				setCommitSelection(m.commitItems, i, true)
			}
		}
	case "n":
		for i := range m.commitItems {
			m.commitItems[i].Selected = false
		}
	case "d":
		if len(m.commitItems) > 0 && !m.commitItems[m.commitCursor].IsDir {
			m.screen, m.runningTitle = model.ScreenRunning, "Loading side-by-side diff..."
			return m, diffCmd(m.activeRepo, m.commitItems[m.commitCursor], m.diffViewportWidth())
		}
	case "m":
		if len(m.commitItems) > 0 && !m.commitItems[m.commitCursor].IsDir {
			m.screen, m.runningTitle = model.ScreenRunning, "Preparing merger comparison..."
			return m, prepareWorkingCopyMergerCmd(m.activeRepo, m.commitItems[m.commitCursor], model.ScreenCommitSelect)
		}
	case "p":
		if len(m.commitItems) == 0 {
			break
		}
		item := m.commitItems[m.commitCursor]
		if item.Unversioned || strings.HasPrefix(item.Status, "?") {
			return m.showError("Use normal commit for unversioned files. Partial commit only works for modified versioned files.", "partial commit is not available for unversioned files"), nil
		}
		if !strings.HasPrefix(item.Status, "M") {
			return m.showError("Partial commit currently supports M files only. Added/deleted/replaced files should use normal commit.", "partial commit is only supported for modified versioned files"), nil
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Loading partial commit hunks..."
		return m, loadPartialHunksCmd(m.activeRepo, item)

	case "h":
		if len(m.commitItems) == 0 {
			break
		}
		if m.commitItems[m.commitCursor].IncludedByParent != "" {
			break
		}
		hiddenPath := m.commitItems[m.commitCursor].Path
		kept := m.commitItems[:0:0]
		for _, item := range m.commitItems {
			if item.Path == hiddenPath || item.IncludedByParent == hiddenPath {
				continue
			}
			kept = append(kept, item)
		}
		m.commitItems = kept
		if m.commitCursor >= len(m.commitItems) {
			m.commitCursor = max(0, len(m.commitItems)-1)
		}
		m.deleteConfirmIdx = -1

	case "enter":
		if len(selectedCommitItems(m.commitItems)) == 0 {
			return m.showError("Select at least one file with Space before committing.", "no files selected"), nil
		}
		m.input.Reset()
		m.input.Placeholder = "Commit message"
		m.input.Focus()
		m.screen = model.ScreenCommitMessageInput
	}

	m.commitOffset = adjustOffset(m.commitOffset, m.commitCursor, visible)
	return m, nil
}

func (m Model) updatePartialHunkSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	const maxPreview = 8
	const pageStep = 5
	availHeight := m.visibleListCount(10)

	switch msg.String() {
	case "up", "k":
		if m.partialHunkCursor > 0 {
			m.partialHunkCursor--
		}
	case "down", "j":
		if m.partialHunkCursor < len(m.partialHunks)-1 {
			m.partialHunkCursor++
		}
	case "pgup":
		m.partialHunkCursor = max(0, m.partialHunkCursor-pageStep)
	case "pgdown":
		m.partialHunkCursor = min(len(m.partialHunks)-1, m.partialHunkCursor+pageStep)
	case "home":
		m.partialHunkCursor = 0
	case "end":
		m.partialHunkCursor = len(m.partialHunks) - 1
	case " ":
		if len(m.partialHunks) > 0 {
			m.partialHunks[m.partialHunkCursor].Selected = !m.partialHunks[m.partialHunkCursor].Selected
		}
	case "a":
		for i := range m.partialHunks {
			m.partialHunks[i].Selected = true
		}
	case "n":
		for i := range m.partialHunks {
			m.partialHunks[i].Selected = false
		}
	case "enter":
		if len(diff.Selected(m.partialHunks)) == 0 {
			return m.showError("Select at least one hunk with Space before partial committing.", "no hunks selected"), nil
		}
		m.partialCommit = true
		m.input.Reset()
		m.input.Placeholder = "Partial commit message"
		m.input.Focus()
		m.screen = model.ScreenCommitMessageInput
	}

	m.partialHunkOffset = adjustHunkOffset(m.partialHunkOffset, m.partialHunkCursor, m.partialHunks, availHeight, maxPreview)
	return m, nil
}

func (m Model) updateShelveSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(12)
	m.commitCursor = navigateCursor(m.commitCursor, len(m.commitItems), visible, msg.String())

	switch msg.String() {
	case " ":
		if len(m.commitItems) > 0 {
			m.commitItems[m.commitCursor].Selected = !m.commitItems[m.commitCursor].Selected
		}
	case "a":
		for i := range m.commitItems {
			m.commitItems[i].Selected = true
		}
	case "n":
		for i := range m.commitItems {
			m.commitItems[i].Selected = false
		}
	case "d":
		if len(m.commitItems) == 0 {
			break
		}
		item := m.commitItems[m.commitCursor]
		if item.Unversioned || strings.HasPrefix(strings.TrimSpace(item.Status), "?") {
			return m.showError("This file is unversioned, so SVN has no base version to compare against.", "diff is not available for unversioned files"), nil
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Loading side-by-side diff..."
		return m, diffCmd(m.activeRepo, item, m.diffViewportWidth())
	case "m":
		if len(m.commitItems) == 0 || m.commitItems[m.commitCursor].IsDir {
			break
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Preparing merger comparison..."
		return m, prepareWorkingCopyMergerCmd(m.activeRepo, m.commitItems[m.commitCursor], model.ScreenShelveSelect)
	case "enter":
		items := selectedCommitItems(m.commitItems)
		if len(items) == 0 {
			return m.showError("Select at least one file with Space before shelving.", "no files selected"), nil
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Shelving selected files..."
		return m, shelveChangesCmd(m.activeRepo, items)
	}

	m.commitOffset = adjustOffset(m.commitOffset, m.commitCursor, visible)
	return m, nil
}

func (m Model) updateRevertSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(12)
	m.commitCursor = navigateCursor(m.commitCursor, len(m.commitItems), visible, msg.String())

	switch msg.String() {
	case " ":
		if len(m.commitItems) > 0 {
			m.commitItems[m.commitCursor].Selected = !m.commitItems[m.commitCursor].Selected
		}
	case "a":
		for i := range m.commitItems {
			m.commitItems[i].Selected = true
		}
	case "n":
		for i := range m.commitItems {
			m.commitItems[i].Selected = false
		}
	case "d":
		if len(m.commitItems) > 0 {
			m.screen, m.runningTitle = model.ScreenRunning, "Loading side-by-side diff..."
			return m, diffCmd(m.activeRepo, m.commitItems[m.commitCursor], m.diffViewportWidth())
		}
	case "m":
		if len(m.commitItems) > 0 && !m.commitItems[m.commitCursor].IsDir {
			m.screen, m.runningTitle = model.ScreenRunning, "Preparing merger comparison..."
			return m, prepareWorkingCopyMergerCmd(m.activeRepo, m.commitItems[m.commitCursor], model.ScreenRevertSelect)
		}
	case "enter":
		selected := selectedCommitItems(m.commitItems)
		if len(selected) == 0 {
			return m.showError("Select at least one file with Space before reverting.", "no files selected"), nil
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Reverting selected files..."
		return m, revertCmd(m.activeRepo, selected)
	}

	m.commitOffset = adjustOffset(m.commitOffset, m.commitCursor, visible)
	return m, nil
}

func (m Model) updateCommitMessageInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		message := strings.TrimSpace(m.input.Value())
		if message == "" {
			return m.showError("Please enter a commit message.", "commit message is required"), nil
		}
		if m.partialCommit {
			item := m.partialItem
			hunks := diff.Selected(m.partialHunks)
			m.partialCommit = false
			m.partialItem = model.CommitItem{}
			m.screen, m.runningTitle = model.ScreenRunning, "Committing selected hunks..."
			return m, partialHunkCommitCmd(m.activeRepo, item, hunks, message)
		}
		items := withRequiredParentDirs(selectedCommitItems(m.commitItems), m.commitItems)
		m.screen, m.runningTitle = model.ScreenRunning, "Committing selected files..."
		return m, commitCmd(m.activeRepo, items, message)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateConflictSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(12)
	prevCursor := m.conflictCursor
	m.conflictCursor = navigateCursor(m.conflictCursor, len(m.conflictItems), visible, msg.String())
	if m.conflictCursor != prevCursor {
		m.resolveConfirm = resolveConfirmNone
		m.conflictNotice = ""
	}

	switch msg.String() {
	case "r":
		// Tree conflicts are resolved one path at a time, so "r" runs the whole
		// batch. Arm on the first press, run on the second.
		tree := m.treeConflicts()
		if len(tree) == 0 {
			break
		}
		if m.resolveConfirm != resolveConfirmAllTree {
			m.resolveConfirm = resolveConfirmAllTree
			break
		}
		m.resolveConfirm = resolveConfirmNone
		m.screen, m.runningTitle = model.ScreenRunning, fmt.Sprintf("Resolving %d tree conflict(s)...", len(tree))
		return m, resolveAllTreeConflictsCmd(m.activeRepo, tree)
	case "m", "t":
		// Whole-file resolve for the path under the cursor: keep mine, or take
		// theirs. Tree conflicts only accept --accept=working, so they use "r".
		if len(m.conflictItems) == 0 || m.conflictItems[m.conflictCursor].IsTree {
			break
		}
		item := m.conflictItems[m.conflictCursor]
		want, accept, label := resolveConfirmMine, "mine-full", "current file"
		if msg.String() == "t" {
			want, accept, label = resolveConfirmTheirs, "theirs-full", "incoming file"
		}
		if m.resolveConfirm != want {
			m.resolveConfirm = want
			break
		}
		m.resolveConfirm = resolveConfirmNone
		m.screen, m.runningTitle = model.ScreenRunning, "Keeping the "+label+" for "+item.Path+"..."
		return m, resolveConflictAcceptCmd(m.activeRepo, item, accept, label)
	case "enter":
		if len(m.conflictItems) > 0 {
			item := m.conflictItems[m.conflictCursor]
			m.screen, m.runningTitle = model.ScreenRunning, "Preparing conflict for Merger..."
			return m, prepareDefaultConflictToolCmd(m.activeRepo, item.Path)
		}
	case "M":
		if len(m.conflictItems) > 0 {
			item := m.conflictItems[m.conflictCursor]
			m.screen, m.runningTitle = model.ScreenRunning, "Preparing conflict for Meld..."
			return m, prepareConflictToolCmd(m.activeRepo, item.Path, "meld")
		}
	}

	m.conflictOffset = adjustOffset(m.conflictOffset, m.conflictCursor, visible)
	return m, nil
}

// treeConflicts returns every tree conflict in the loaded conflict list.
func (m Model) treeConflicts() []model.ConflictItem {
	var tree []model.ConflictItem
	for _, item := range m.conflictItems {
		if item.IsTree {
			tree = append(tree, item)
		}
	}
	return tree
}

func (m Model) updatePropertyTargetInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		query := strings.TrimSpace(m.input.Value())
		if query == "" {
			// Empty search goes straight to the working copy root, which is
			// where a merge records its mergeinfo.
			m.propertyTargets = nil
			m.propertyCursor, m.propertyOffset = 0, 0
			m.screen, m.runningTitle = model.ScreenRunning, "Reading properties of the working copy root..."
			return m, loadPropertiesCmd(m.activeRepo, ".", "")
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Searching paths..."
		return m, searchPropertyTargetsCmd(m.activeRepo, query)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updatePropertyTargetSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(10)
	m.propertyTargetCursor = navigateCursor(m.propertyTargetCursor, len(m.propertyTargets), visible, msg.String())

	if msg.String() == "enter" && len(m.propertyTargets) > 0 {
		target := m.propertyTargets[m.propertyTargetCursor]
		m.propertyCursor, m.propertyOffset = 0, 0
		m.screen, m.runningTitle = model.ScreenRunning, "Reading properties of "+target+"..."
		return m, loadPropertiesCmd(m.activeRepo, target, "")
	}

	m.propertyTargetOffset = adjustOffset(m.propertyTargetOffset, m.propertyTargetCursor, visible)
	return m, nil
}

func (m Model) updatePropertyList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(12)
	prevCursor := m.propertyCursor
	m.propertyCursor = navigateCursor(m.propertyCursor, len(m.propertyItems), visible, msg.String())
	if m.propertyCursor != prevCursor {
		m.propertyDeleteIdx = -1
	}

	switch msg.String() {
	case "a":
		return m.startPropertyPick(m.propertyTarget, pathIsDir(m.activeRepo, m.propertyTarget), m.propertyItems, false), nil
	case "e", "enter":
		if len(m.propertyItems) == 0 {
			break
		}
		item := m.propertyItems[m.propertyCursor]
		m.propertyEditing = true
		m.propertyNotice = ""
		// A property with a documented set of values is picked, not typed.
		if def, ok := propertyDefFor(item.Name); ok && len(def.Values) > 0 {
			return m.startPropertyValuePick(item.Name, def.Values, item.Value), nil
		}
		return m.startPropertyValueInput(item.Name, item.Value), nil
	case "delete":
		if len(m.propertyItems) == 0 {
			break
		}
		if m.propertyDeleteIdx != m.propertyCursor {
			m.propertyDeleteIdx = m.propertyCursor
			break
		}
		name := m.propertyItems[m.propertyCursor].Name
		m.propertyDeleteIdx = -1
		m.screen, m.runningTitle = model.ScreenRunning, "Deleting "+name+"..."
		return m, deletePropertyCmd(m.activeRepo, m.propertyTarget, name)
	}

	m.propertyOffset = adjustOffset(m.propertyOffset, m.propertyCursor, visible)
	return m, nil
}

func (m Model) updatePropertyNameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			return m.showError("Please enter a property name, e.g. svn:ignore.", "property name is required"), nil
		}
		m.propertyName = name
		m.input.Reset()
		m.input.Placeholder = `Property value (
 makes a new line)`
		m.input.Focus()
		m.screen = model.ScreenPropertyValueInput
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updatePropertyValueInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		value := expandPropertyValue(m.input.Value())
		name := m.propertyName
		m.propertyEditing = false
		m.screen, m.runningTitle = model.ScreenRunning, "Setting "+name+"..."
		return m, setPropertyCmd(m.activeRepo, m.propertyTarget, name, value)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// ── List helpers ──────────────────────────────────────────────────────────────

func (m Model) visibleListCount(reservedLines int) int {
	if m.height <= 0 {
		return 12
	}
	extra := 0
	if m.showInfo {
		extra = 4
	}
	count := m.height - reservedLines - extra
	if count < 5 {
		return 5
	}
	return count
}

func (m Model) headerLines() int {
	if m.showInfo {
		return 6 // header text + 4 info lines + blank
	}
	return 2 // header text + blank
}

func (m Model) inputActive() bool {
	if m.branchFilterMode || m.actionFilterMode || m.checkoutRevFilterMode {
		return true
	}
	switch m.screen {
	case model.ScreenCreateBranchInput,
		model.ScreenCheckoutRevisionInput,
		model.ScreenDeleteBranchConfirm,
		model.ScreenCommitMessageInput,
		model.ScreenFileHistorySearch,
		model.ScreenHistorySearch,
		model.ScreenPropertyTargetInput,
		model.ScreenPropertyNameInput,
		model.ScreenPropertyValueInput:
		return true
	}
	return false
}

func (m Model) branchListVisibleCount() int {
	return m.visibleListCount(14)
}

func (m Model) branchMergeListVisibleCount() int {
	return max(2, (m.listInnerHeight()-5)/2)
}

// checkoutRevisionListVisibleCount counts rows, not lines: each revision takes
// two lines, below the search line, the hint line and the separator, above the
// blank line, the scroll hint and the detail panel.
func (m Model) checkoutRevisionListVisibleCount() int {
	return max(2, (m.listInnerHeight()-5-m.checkoutRevisionDetailHeight())/2)
}

// checkoutRevisionDetailHeight is how many lines the panel below the list gets
// for the selected revision. It is dropped altogether on a short terminal,
// where the list itself needs every line.
func (m Model) checkoutRevisionDetailHeight() int {
	inner := m.listInnerHeight()
	if inner < 18 {
		return 0
	}
	return clamp(inner/3, 7, 12)
}

func (m Model) branchDiffListVisibleCount() int {
	return m.visibleListCount(15)
}

func (m Model) diffViewportWidth() int {
	// Two cells are reserved to the right of the content for the gap and the
	// full-document change overview.
	return max(20, m.width-6)
}

func (m Model) diffViewportHeight() int {
	return max(3, m.listInnerHeight()-1)
}

func (m *Model) syncDiffViewportSize() {
	m.viewport.Width = m.diffViewportWidth()
	m.viewport.Height = m.diffViewportHeight()
}

func (m Model) pullListVisibleCount() int {
	return m.visibleListCount(15)
}

func navigateCursor(cursor, listLen, pageSize int, key string) int {
	if listLen == 0 {
		return 0
	}
	switch key {
	case "up", "k":
		return max(0, cursor-1)
	case "down", "j":
		return min(listLen-1, cursor+1)
	case "pgup":
		return max(0, cursor-pageSize)
	case "pgdown":
		return min(listLen-1, cursor+pageSize)
	case "home":
		return 0
	case "end":
		return listLen - 1
	}
	return cursor
}

func adjustOffset(offset, cursor, visible int) int {
	if cursor < offset {
		return cursor
	}
	if cursor >= offset+visible {
		return cursor - visible + 1
	}
	return offset
}

func clamp(v, low, high int) int {
	if high < low {
		return low
	}
	return max(low, min(high, v))
}

func scrollHint(offset, end, total int) string {
	if total <= 0 {
		return ""
	}
	return fmt.Sprintf("Showing %d-%d of %d", offset+1, end, total)
}

func hunkPreviewLineCount(h model.PartialHunk, maxLines int) int {
	count := 0
	for _, line := range h.Lines {
		if line == "" || line[0] == ' ' {
			continue
		}
		if line[0] == '+' || line[0] == '-' {
			count++
		}
	}
	if count > maxLines {
		return maxLines + 1
	}
	return count
}

func hunkDisplayLines(h model.PartialHunk, maxPreview int) int {
	return 1 + hunkPreviewLineCount(h, maxPreview)
}

func adjustHunkOffset(offset, cursor int, hunks []model.PartialHunk, availHeight, maxPreview int) int {
	if len(hunks) == 0 {
		return 0
	}
	if cursor < offset {
		return cursor
	}
	used := 0
	for i := offset; i < len(hunks); i++ {
		h := hunkDisplayLines(hunks[i], maxPreview)
		if used+h > availHeight {
			break
		}
		used += h
		if i == cursor {
			return offset
		}
	}
	used = 0
	newOffset := cursor
	for i := cursor; i >= 0; i-- {
		h := hunkDisplayLines(hunks[i], maxPreview)
		if used+h > availHeight {
			newOffset = i + 1
			break
		}
		used += h
		newOffset = i
	}
	return newOffset
}

// ── OS helpers ────────────────────────────────────────────────────────────────

func removeAll(path string) error {
	return os.RemoveAll(path)
}

func removeFile(path string) error {
	return os.Remove(path)
}
