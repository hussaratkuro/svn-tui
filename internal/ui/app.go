package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"svn-tui/internal/diff"
	"svn-tui/internal/model"
	"svn-tui/internal/svn"
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

	actions        []string
	actionCursor   int
	actionOffset   int
	selectedAction model.Action

	branches          []model.Branch
	branchCursor      int
	branchOffset      int
	branchNumberInput string

	shelves     []string
	shelfCursor int
	shelfOffset int

	fileHistoryQuery  string
	fileHistoryItems  []string
	fileHistoryCursor int
	fileHistoryOffset int

	commitItems      []model.CommitItem
	commitCursor     int
	commitOffset     int
	deleteConfirmIdx int

	partialItem       model.CommitItem
	partialHunks      []model.PartialHunk
	partialHunkCursor int
	partialHunkOffset int
	partialCommit     bool

	conflictItems  []model.ConflictItem
	conflictCursor int
	conflictOffset int

	input    textinput.Model
	viewport viewport.Model

	historyTitle   string
	historyContent string
	historySearch  string
	runningTitle   string
	runningLines   []string
	runningOffset  int
	runningPinTail bool
	result         string
	err            error
	showInfo       bool
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
			"Shelve local changes", "Unshelve changes", "Switch to trunk",
			"Checkout revision", "Resolve conflicts",
			"Commit history", "File history", "Revision tree", "Quit",
		},
		input:            input,
		viewport:         vp,
		runningPinTail:   true,
		deleteConfirmIdx: -1,
	}

	if len(repos) == 0 {
		m.screen = model.ScreenResult
		m.err = fmt.Errorf("no usable SVN repositories found")
		m.result = svn.HelpText()
		m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
	}

	return m
}

func (m Model) Init() tea.Cmd { return nil }

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = max(20, msg.Width-4)
		m.viewport.Height = max(5, msg.Height-6)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case model.BranchesLoadedMsg:
		if msg.Err != nil {
			return m.showError("Failed to load branches.", msg.Err.Error()), nil
		}
		m.branches = msg.Branches
		m.branchCursor, m.branchOffset, m.branchNumberInput = 0, 0, ""
		m.screen = model.ScreenBranchSelect
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
		m.screen = model.ScreenShelfSelect
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
			m.viewport.SetContent(m.result)
			return m, nil
		}
		m.commitItems = msg.Items
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
		m.screen = model.ScreenConflictSelect
		return m, nil

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
		m.viewport.SetContent(m.historyContent)
		m.viewport.GotoTop()
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
		if msg.Err != nil {
			m.viewport.SetContent("Failed to load side-by-side diff for:\n" + msg.Path + "\n\n" + msg.Output + "\n\n" + msg.Err.Error())
		} else if strings.TrimSpace(msg.Output) == "" {
			m.viewport.SetContent("No diff found for:\n" + msg.Path)
		} else {
			m.viewport.SetContent(msg.Output)
		}
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
		m.branchCursor = clamp(m.branchCursor+steps, 0, len(m.branches)-1)
		m.branchOffset = adjustOffset(m.branchOffset, m.branchCursor, m.branchListVisibleCount())

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
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ── Key handlers ──────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "i":
		if m.screen != model.ScreenRepoSelect && !m.inputActive() {
			m.showInfo = !m.showInfo
			return m, nil
		}

	case "/", "s":
		if m.screen == model.ScreenHistory && !m.inputActive() {
			m.input.Reset()
			m.input.Placeholder = "Revision number, e.g. 2225"
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
			m.screen = model.ScreenRepoSelect
		case model.ScreenDiff:
			switch m.selectedAction {
			case model.ActionPull:
				m.screen = model.ScreenPullSelect
			case model.ActionRevertFiles:
				m.screen = model.ScreenRevertSelect
			case model.ActionShelveChanges:
				m.screen = model.ScreenShelveSelect
			default:
				m.screen = model.ScreenCommitSelect
			}
		case model.ScreenHistorySearch:
			m.screen = model.ScreenHistory
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
	case model.ScreenFileHistorySearch:
		return m.updateFileHistorySearch(msg)
	case model.ScreenFileHistorySelect:
		return m.updateFileHistorySelect(msg)
	case model.ScreenBranchSelect:
		return m.updateBranchSelect(msg)
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
			m.screen = model.ScreenActionSelect
			m.result = ""
			m.err = nil
			m.viewport.SetContent("")
		}
	case model.ScreenRunning:
		return m.updateRunningScroll(msg), nil
	case model.ScreenHistory, model.ScreenDiff:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
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

func (m Model) updateHistorySearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		query := strings.TrimSpace(m.input.Value())
		m.historySearch = query
		m.result = ""
		m.screen = model.ScreenHistory
		if query == "" {
			return m, nil
		}
		line, ok := findRevisionLine(m.historyContent, query)
		if !ok {
			m.result = fmt.Sprintf("Revision r%s was not found in the currently loaded history.", strings.TrimPrefix(query, "r"))
			return m, nil
		}
		m.viewport.YOffset = clamp(line, 0, max(0, len(strings.Split(m.historyContent, "\n"))-1))
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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
	m.actionCursor = navigateCursor(m.actionCursor, len(m.actions), visible, msg.String())

	if msg.String() == "enter" {
		m.selectedAction = model.Action(m.actionCursor)
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
			m.input.Placeholder = "Revision number, e.g. 12345"
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
	}

	m.actionOffset = adjustOffset(m.actionOffset, m.actionCursor, visible)
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

func (m Model) updateCheckoutRevisionInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		revision := strings.TrimSpace(m.input.Value())
		if revision == "" {
			return m.showError("Please enter an SVN revision number.", "revision number is required"), nil
		}
		for _, ch := range revision {
			if ch < '0' || ch > '9' {
				return m.showError("Revision must be a number, e.g. 12345.", fmt.Sprintf("invalid revision number: %s", revision)), nil
			}
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Checking out revision..."
		return m, checkoutRevisionCmd(m.activeRepo, revision)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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

func (m Model) updateBranchSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.branchListVisibleCount()
	key := msg.String()

	switch key {
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
		if m.selectedAction == model.ActionMergeBranch {
			m.screen, m.runningTitle = model.ScreenRunning, "Merging branch..."
			return m, mergeBranchCmd(m.activeRepo, selected.Name)
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Switching to branch..."
		return m, switchBranchCmd(m.activeRepo, selected.Name)

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

func (m Model) updateShelfSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(7)
	m.shelfCursor = navigateCursor(m.shelfCursor, len(m.shelves), visible, msg.String())

	if msg.String() == "enter" && len(m.shelves) > 0 {
		selected := m.shelves[m.shelfCursor]
		m.screen, m.runningTitle = model.ScreenRunning, "Unshelving changes..."
		return m, unshelveChangesCmd(m.activeRepo, selected)
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
			return m, diffCmd(m.activeRepo, m.commitItems[m.commitCursor], m.width-4)
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
		return m, diffCmd(m.activeRepo, item, m.width-4)
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
			return m, diffCmd(m.activeRepo, m.commitItems[m.commitCursor], m.width-4)
		}
	case "enter":
		paths := selectedCommitPaths(m.commitItems)
		if len(paths) == 0 {
			return m.showError("Select at least one file with Space before reverting.", "no files selected"), nil
		}
		m.screen, m.runningTitle = model.ScreenRunning, "Reverting selected files..."
		return m, revertCmd(m.activeRepo, paths)
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
	m.conflictCursor = navigateCursor(m.conflictCursor, len(m.conflictItems), visible, msg.String())

	if msg.String() == "enter" && len(m.conflictItems) > 0 {
		selected := m.conflictItems[m.conflictCursor]
		m.screen, m.runningTitle = model.ScreenRunning, "Resolving conflict with Meld..."
		return m, resolveConflictWithMeldCmd(m.activeRepo, selected.Path)
	}

	m.conflictOffset = adjustOffset(m.conflictOffset, m.conflictCursor, visible)
	return m, nil
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
	switch m.screen {
	case model.ScreenCreateBranchInput,
		model.ScreenCheckoutRevisionInput,
		model.ScreenCommitMessageInput,
		model.ScreenFileHistorySearch,
		model.ScreenHistorySearch:
		return true
	}
	return false
}

func (m Model) branchListVisibleCount() int {
	return m.visibleListCount(14)
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
