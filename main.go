package main

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenRepoSelect screen = iota
	screenActionSelect
	screenCreateBranchInput
	screenCheckoutRevisionInput
	screenBranchSelect
	screenPullSelect
	screenCommitSelect
	screenCommitMessageInput
	screenPartialHunkSelect
	screenRevertSelect
	screenConflictSelect
	screenFileHistorySearch
	screenFileHistorySelect
	screenHistory
	screenDiff
	screenRunning
	screenResult
)

type action int

const (
	actionPull action = iota
	actionStatus
	actionRevertFiles
	actionCommit
	actionCreateBranch
	actionSwitchBranch
	actionSwitchTrunk
	actionCheckoutRevision
	actionResolveConflicts
	actionCommitHistory
	actionFileHistory
	actionRevisionTree
	actionQuit
)

type repoConfig struct {
	Path           string
	Username       string
	Password       string
	BranchUsername string
}

type repo struct {
	Path            string
	URL             string
	Root            string
	Username        string
	Password        string
	BranchUsername  string
	CurrentLocation string
}

type branch struct {
	Name     string
	Revision int
}

type commitItem struct {
	Status      string
	Path        string
	Selected    bool
	Unversioned bool
}

type conflictItem struct {
	Status string
	Path   string
	IsTree bool
}

type commandResult struct {
	Output          string
	Err             error
	CurrentLocation string
}

type branchesLoadedMsg struct {
	Branches []branch
	Err      error
}

type pullItemsLoadedMsg struct {
	Items []commitItem
	Err   error
}

type commitItemsLoadedMsg struct {
	Items []commitItem
	Err   error
}

type conflictItemsLoadedMsg struct {
	Items []conflictItem
	Err   error
}

type historyLoadedMsg struct {
	Output string
	Err    error
	Title  string
}

type fileHistoryMatchesLoadedMsg struct {
	Query string
	Items []string
	Err   error
}

type revertItemsLoadedMsg struct {
	Items []commitItem
	Err   error
}

type diffLoadedMsg struct {
	Output string
	Err    error
	Path   string
}

type partialHunksLoadedMsg struct {
	Item  commitItem
	Hunks []partialHunk
	Err   error
}

type partialHunk struct {
	Header      string
	OldStart    int
	OldCount    int
	NewStart    int
	NewCount    int
	Lines       []string
	Selected    bool
	Added       int
	Removed     int
	Context     int
	PreviewText string
}

type model struct {
	screen screen

	width  int
	height int

	repos      []repo
	repoCursor int
	repoOffset int
	activeRepo repo

	actions        []string
	actionCursor   int
	actionOffset   int
	selectedAction action

	branches          []branch
	branchCursor      int
	branchOffset      int
	branchNumberInput string

	fileHistoryQuery  string
	fileHistoryItems  []string
	fileHistoryCursor int
	fileHistoryOffset int

	commitItems  []commitItem
	commitCursor int
	commitOffset int

	partialItem       commitItem
	partialHunks      []partialHunk
	partialHunkCursor int
	partialHunkOffset int
	partialCommit     bool

	conflictItems  []conflictItem
	conflictCursor int
	conflictOffset int

	input textinput.Model

	viewport viewport.Model

	historyTitle string
	runningTitle string
	result       string
	err          error
}

var (
	catMauve     = lipgloss.Color("#cba6f7")
	catRosewater = lipgloss.Color("#f5e0dc")
	catOverlay0  = lipgloss.Color("#6c7086")
	catOverlay1  = lipgloss.Color("#7f849c")
	catRed       = lipgloss.Color("#f38ba8")
	catGreen     = lipgloss.Color("#a6e3a1")
	catYellow    = lipgloss.Color("#f9e2af")
	catText      = lipgloss.Color("#cdd6f4")
	catSubtext0  = lipgloss.Color("#a6adc8")
	catSapphire  = lipgloss.Color("#74c7ec")
	catTeal      = lipgloss.Color("#94e2d5")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(catMauve).
			MarginBottom(1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(catYellow).
			Bold(true)

	mutedStyle = lipgloss.NewStyle().
			Foreground(catOverlay0)

	normalStyle = lipgloss.NewStyle().
			Foreground(catSubtext0)

	textStyle = lipgloss.NewStyle().
			Foreground(catText)

	errorStyle = lipgloss.NewStyle().
			Foreground(catRed).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(catGreen).
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(catYellow).
			Bold(true)

	checkboxStyle = lipgloss.NewStyle().
			Foreground(catOverlay1)

	checkedStyle = lipgloss.NewStyle().
			Foreground(catYellow).
			Bold(true)

	labelMauveStyle = lipgloss.NewStyle().
			Foreground(catMauve).
			Bold(true)

	labelYellowStyle = lipgloss.NewStyle().
				Foreground(catYellow).
				Bold(true)

	valueWhiteStyle = lipgloss.NewStyle().
			Foreground(catRosewater)

	actionStyle = lipgloss.NewStyle().
			Foreground(catTeal)

	actionSelectedStyle = lipgloss.NewStyle().
				Foreground(catMauve).
				Bold(true)

	diffAddedStyle = lipgloss.NewStyle().
			Foreground(catGreen)

	diffDeletedStyle = lipgloss.NewStyle().
				Foreground(catRed)

	diffModifiedStyle = lipgloss.NewStyle().
				Foreground(catYellow)

	diffSameStyle = lipgloss.NewStyle().
			Foreground(catSubtext0)
)

func main() {
	repos := loadRepos()

	input := textinput.New()
	input.Placeholder = "e.g. ASD-123 or create-branch-test"
	input.Focus()
	input.CharLimit = 200
	input.Width = 60

	vp := viewport.New(100, 30)

	m := model{
		screen: screenRepoSelect,
		repos:  repos,
		actions: []string{
			"Pull",
			"Status",
			"Revert files",
			"Commit",
			"Create branch",
			"Switch to branch",
			"Switch to trunk",
			"Checkout revision",
			"Resolve conflicts",
			"Commit history",
			"File history",
			"Revision tree",
			"Quit",
		},
		input:    input,
		viewport: vp,
	}

	if len(repos) == 0 {
		m.screen = screenResult
		m.err = fmt.Errorf("no usable SVN repositories found")
		m.result = repoHelpText()
		m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
	}

	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

func loadRepos() []repo {
	var configs []repoConfig

	if configDir, err := os.UserConfigDir(); err == nil {
		configPath := filepath.Join(configDir, "svn-tui", "repo.txt")
		configs = append(configs, loadRepoConfigFile(configPath)...)
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		configPath := filepath.Join(homeDir, ".config", "svn-tui", "repo.txt")
		configs = append(configs, loadRepoConfigFile(configPath)...)
	}

	if len(configs) == 0 {
		if len(os.Args) > 1 {
			for _, p := range os.Args[1:] {
				p = strings.TrimSpace(p)
				if p != "" {
					configs = append(configs, repoConfig{
						Path: p,
					})
				}
			}
		}

		if env := strings.TrimSpace(os.Getenv("SVN_TUI_REPOS")); env != "" {
			parts := filepath.SplitList(env)
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					configs = append(configs, repoConfig{
						Path: p,
					})
				}
			}
		}

		if wd, err := os.Getwd(); err == nil {
			configs = append(configs, repoConfig{
				Path: wd,
			})
		}
	}

	seen := map[string]bool{}
	var repos []repo

	for _, cfg := range configs {
		if strings.TrimSpace(cfg.Path) == "" {
			continue
		}

		abs, err := filepath.Abs(cfg.Path)
		if err != nil {
			continue
		}

		if seen[abs] {
			continue
		}

		seen[abs] = true
		cfg.Path = abs

		r, err := buildRepo(cfg)
		if err == nil {
			repos = append(repos, r)
		}
	}

	return repos
}

func loadRepoConfigFile(configPath string) []repoConfig {
	file, err := os.Open(configPath)
	if err != nil {
		return nil
	}
	defer file.Close()

	var configs []repoConfig
	current := repoConfig{}

	flush := func() {
		if strings.TrimSpace(current.Path) != "" {
			configs = append(configs, current)
		}

		current = repoConfig{}
	}

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			flush()
			continue
		}

		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch key {
		case "path", "repo", "working_copy":
			current.Path = value

		case "username", "user":
			current.Username = value

		case "password", "pass":
			current.Password = value

		case "branch_username", "branch_user", "branchname_user":
			current.BranchUsername = value
		}
	}

	flush()

	return configs
}

func buildRepo(cfg repoConfig) (repo, error) {
	r := repo{
		Path:           cfg.Path,
		Username:       cfg.Username,
		Password:       cfg.Password,
		BranchUsername: cfg.BranchUsername,
	}

	root, err := svn(r, "info", "--show-item", "repos-root-url")
	if err != nil {
		return repo{}, err
	}

	currentURL, err := svn(r, "info", "--show-item", "url")
	if err != nil {
		return repo{}, err
	}

	r.Root = strings.TrimSpace(root)
	r.URL = strings.TrimSpace(currentURL)
	r.CurrentLocation = getCurrentLocation(r)

	return r, nil
}

func repoHelpText() string {
	return `Usage:

Config file:

  ~/.config/svn-tui/repo.txt

Example:

  path=/home/user/dev/
  username=user
  password=YOUR_PASSWORD_HERE
  branch_username=user

Important:

  chmod 600 ~/.config/svn-tui/repo.txt

Meld conflict resolve requires:

  sudo pacman -S meld
`
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = max(20, msg.Width-4)
		m.viewport.Height = max(5, msg.Height-8)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case branchesLoadedMsg:
		if msg.Err != nil {
			m.screen = screenResult
			m.err = msg.Err
			m.result = "Failed to load branches."
			m.viewport.SetContent(m.result + "\n\n" + msg.Err.Error())
			return m, nil
		}

		m.branches = msg.Branches
		m.branchCursor = 0
		m.branchOffset = 0
		m.branchNumberInput = ""
		m.screen = screenBranchSelect
		return m, nil

	case pullItemsLoadedMsg:
		if msg.Err != nil {
			m.screen = screenResult
			m.err = msg.Err
			m.result = "Failed to load incoming pull changes."
			m.viewport.SetContent(m.result + "\n\n" + msg.Err.Error())
			return m, nil
		}

		if len(msg.Items) == 0 {
			m.screen = screenResult
			m.err = nil
			m.result = "No incoming changes found. Working copy is already up to date."
			m.viewport.SetContent(m.result)
			return m, nil
		}

		m.commitItems = msg.Items
		m.commitCursor = 0
		m.commitOffset = 0
		m.screen = screenPullSelect
		return m, nil

	case commitItemsLoadedMsg:
		if msg.Err != nil {
			m.screen = screenResult
			m.err = msg.Err
			m.result = "Failed to load working copy changes."
			m.viewport.SetContent(m.result + "\n\n" + msg.Err.Error())
			return m, nil
		}

		if len(msg.Items) == 0 {
			m.screen = screenResult
			m.err = nil
			m.result = "No committable changes found."
			m.viewport.SetContent(m.result)
			return m, nil
		}

		m.commitItems = msg.Items
		m.commitCursor = 0
		m.commitOffset = 0
		m.screen = screenCommitSelect
		return m, nil

	case revertItemsLoadedMsg:
		if msg.Err != nil {
			m.screen = screenResult
			m.err = msg.Err
			m.result = "Failed to load working copy changes."
			m.viewport.SetContent(m.result + "\n\n" + msg.Err.Error())
			return m, nil
		}

		if len(msg.Items) == 0 {
			m.screen = screenResult
			m.err = nil
			m.result = "No revertable changes found."
			m.viewport.SetContent(m.result)
			return m, nil
		}

		m.commitItems = msg.Items
		m.commitCursor = 0
		m.commitOffset = 0
		m.screen = screenRevertSelect
		return m, nil

	case conflictItemsLoadedMsg:
		if msg.Err != nil {
			m.screen = screenResult
			m.err = msg.Err
			m.result = "Failed to load conflicts."
			m.viewport.SetContent(m.result + "\n\n" + msg.Err.Error())
			return m, nil
		}

		if len(msg.Items) == 0 {
			m.screen = screenResult
			m.err = nil
			m.result = "No conflicts found."
			m.viewport.SetContent(m.result)
			return m, nil
		}

		m.conflictItems = msg.Items
		m.conflictCursor = 0
		m.conflictOffset = 0
		m.screen = screenConflictSelect
		return m, nil

	case historyLoadedMsg:
		m.screen = screenHistory
		m.err = msg.Err
		m.historyTitle = msg.Title
		if strings.TrimSpace(m.historyTitle) == "" {
			m.historyTitle = "Commit history"
		}

		if msg.Err != nil {
			m.viewport.SetContent("Failed to load " + strings.ToLower(m.historyTitle) + ".\n\n" + msg.Output + "\n\n" + msg.Err.Error())
		} else {
			m.viewport.SetContent(msg.Output)
		}

		m.viewport.GotoTop()
		return m, nil

	case fileHistoryMatchesLoadedMsg:
		if msg.Err != nil {
			m.screen = screenResult
			m.err = msg.Err
			m.result = "Failed to search files."
			m.viewport.SetContent(m.result + "\n\n" + msg.Err.Error())
			return m, nil
		}

		if len(msg.Items) == 0 {
			m.screen = screenResult
			m.err = nil
			m.result = fmt.Sprintf("No files found for search: %s", msg.Query)
			m.viewport.SetContent(m.result)
			return m, nil
		}

		m.fileHistoryQuery = msg.Query
		m.fileHistoryItems = msg.Items
		m.fileHistoryCursor = 0
		m.fileHistoryOffset = 0
		m.screen = screenFileHistorySelect
		return m, nil

	case diffLoadedMsg:
		m.screen = screenDiff
		m.err = msg.Err

		if msg.Err != nil {
			m.viewport.SetContent("Failed to load side-by-side diff for:\n" + msg.Path + "\n\n" + msg.Output + "\n\n" + msg.Err.Error())
		} else {
			if strings.TrimSpace(msg.Output) == "" {
				m.viewport.SetContent("No diff found for:\n" + msg.Path)
			} else {
				m.viewport.SetContent(msg.Output)
			}
		}

		m.viewport.GotoTop()
		return m, nil

	case partialHunksLoadedMsg:
		if msg.Err != nil {
			m.screen = screenResult
			m.err = msg.Err
			m.result = "Failed to load partial commit hunks."
			m.viewport.SetContent(m.result + "\n\n" + msg.Err.Error())
			return m, nil
		}

		if len(msg.Hunks) == 0 {
			m.screen = screenResult
			m.err = nil
			m.result = "No selectable hunks found for partial commit."
			m.viewport.SetContent(m.result)
			return m, nil
		}

		m.partialItem = msg.Item
		m.partialHunks = msg.Hunks
		m.partialHunkCursor = 0
		m.partialHunkOffset = 0
		m.partialCommit = false
		m.screen = screenPartialHunkSelect
		return m, nil

	case commandResult:
		m.screen = screenResult
		m.result = msg.Output
		m.err = msg.Err

		if msg.CurrentLocation != "" {
			m.activeRepo.CurrentLocation = msg.CurrentLocation

			for i := range m.repos {
				if m.repos[i].Path == m.activeRepo.Path {
					m.repos[i].CurrentLocation = msg.CurrentLocation
					break
				}
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

	if m.screen == screenCreateBranchInput ||
		m.screen == screenCheckoutRevisionInput ||
		m.screen == screenCommitMessageInput ||
		m.screen == screenFileHistorySearch {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	if m.screen == screenResult || m.screen == screenHistory || m.screen == screenDiff {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "a":
		if m.screen == screenHistory && m.selectedAction == actionRevisionTree {
			m.screen = screenRunning
			m.runningTitle = "Building full ASCII revision tree..."
			return m, loadRevisionTreeCmd(m.activeRepo, true)
		}

	case "q":
		if m.screen == screenRepoSelect ||
			m.screen == screenActionSelect ||
			m.screen == screenResult ||
			m.screen == screenHistory {
			return m, tea.Quit
		}

		m.screen = screenActionSelect
		return m, nil

	case "esc":
		switch m.screen {
		case screenRepoSelect:
			return m, tea.Quit

		case screenActionSelect:
			m.screen = screenRepoSelect

		case screenDiff:
			if m.selectedAction == actionPull {
				m.screen = screenPullSelect
			} else if m.selectedAction == actionRevertFiles {
				m.screen = screenRevertSelect
			} else {
				m.screen = screenCommitSelect
			}

		case screenCreateBranchInput,
			screenCheckoutRevisionInput,
			screenFileHistorySearch,
			screenFileHistorySelect,
			screenBranchSelect,
			screenPullSelect,
			screenCommitSelect,
			screenCommitMessageInput,
			screenPartialHunkSelect,
			screenRevertSelect,
			screenConflictSelect,
			screenHistory,
			screenResult:
			m.screen = screenActionSelect
		}

		return m, nil
	}

	switch m.screen {
	case screenRepoSelect:
		return m.updateRepoSelect(msg)

	case screenActionSelect:
		return m.updateActionSelect(msg)

	case screenCreateBranchInput:
		return m.updateCreateBranchInput(msg)

	case screenCheckoutRevisionInput:
		return m.updateCheckoutRevisionInput(msg)

	case screenFileHistorySearch:
		return m.updateFileHistorySearch(msg)

	case screenFileHistorySelect:
		return m.updateFileHistorySelect(msg)

	case screenBranchSelect:
		return m.updateBranchSelect(msg)

	case screenPullSelect:
		return m.updatePullSelect(msg)

	case screenCommitSelect:
		return m.updateCommitSelect(msg)

	case screenCommitMessageInput:
		return m.updateCommitMessageInput(msg)

	case screenPartialHunkSelect:
		return m.updatePartialHunkSelect(msg)

	case screenRevertSelect:
		return m.updateRevertSelect(msg)

	case screenConflictSelect:
		return m.updateConflictSelect(msg)

	case screenResult:
		if msg.String() == "enter" {
			m.screen = screenActionSelect
			m.result = ""
			m.err = nil
			m.viewport.SetContent("")
		}

	case screenHistory, screenDiff:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) updateRepoSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(7)

	switch msg.String() {
	case "up", "k":
		if m.repoCursor > 0 {
			m.repoCursor--
		}

	case "down", "j":
		if m.repoCursor < len(m.repos)-1 {
			m.repoCursor++
		}

	case "pgup":
		m.repoCursor = max(0, m.repoCursor-visible)

	case "pgdown":
		m.repoCursor = min(len(m.repos)-1, m.repoCursor+visible)

	case "home":
		m.repoCursor = 0

	case "end":
		m.repoCursor = len(m.repos) - 1

	case "enter":
		m.activeRepo = m.repos[m.repoCursor]
		m.activeRepo.CurrentLocation = getCurrentLocation(m.activeRepo)
		m.repos[m.repoCursor].CurrentLocation = m.activeRepo.CurrentLocation

		m.actionCursor = 0
		m.actionOffset = 0
		m.screen = screenActionSelect
	}

	m.repoOffset = adjustOffset(m.repoOffset, m.repoCursor, visible)

	return m, nil
}

func (m model) updateActionSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(8)

	switch msg.String() {
	case "up", "k":
		if m.actionCursor > 0 {
			m.actionCursor--
		}

	case "down", "j":
		if m.actionCursor < len(m.actions)-1 {
			m.actionCursor++
		}

	case "pgup":
		m.actionCursor = max(0, m.actionCursor-visible)

	case "pgdown":
		m.actionCursor = min(len(m.actions)-1, m.actionCursor+visible)

	case "home":
		m.actionCursor = 0

	case "end":
		m.actionCursor = len(m.actions) - 1

	case "enter":
		m.selectedAction = action(m.actionCursor)

		switch m.selectedAction {
		case actionPull:
			m.screen = screenRunning
			m.runningTitle = "Loading incoming pull changes..."
			return m, loadPullItemsCmd(m.activeRepo)

		case actionStatus:
			m.screen = screenRunning
			m.runningTitle = "Loading status..."
			return m, statusCmd(m.activeRepo)

		case actionRevertFiles:
			m.screen = screenRunning
			m.runningTitle = "Loading revertable files..."
			return m, loadRevertItemsCmd(m.activeRepo)

		case actionCheckoutRevision:
			m.input.Reset()
			m.input.Placeholder = "Revision number, e.g. 12345"
			m.input.Focus()
			m.screen = screenCheckoutRevisionInput

		case actionCreateBranch:
			m.input.Reset()
			m.input.Placeholder = "e.g. ASD-123 or create-branch-test"
			m.input.Focus()
			m.screen = screenCreateBranchInput

		case actionSwitchBranch:
			m.screen = screenRunning
			m.runningTitle = "Loading branches..."
			return m, loadBranchesCmd(m.activeRepo)

		case actionSwitchTrunk:
			m.screen = screenRunning
			m.runningTitle = "Switching to trunk..."
			return m, switchTrunkCmd(m.activeRepo)

		case actionCommit:
			m.screen = screenRunning
			m.runningTitle = "Loading working copy changes..."
			return m, loadCommitItemsCmd(m.activeRepo)

		case actionResolveConflicts:
			m.screen = screenRunning
			m.runningTitle = "Loading conflicts..."
			return m, loadConflictItemsCmd(m.activeRepo)

		case actionCommitHistory:
			m.screen = screenRunning
			m.runningTitle = "Loading commit history..."
			return m, loadHistoryCmd(m.activeRepo)

		case actionFileHistory:
			m.input.Reset()
			m.input.Placeholder = "Search file path, e.g. action.php or inc/config"
			m.input.Focus()
			m.screen = screenFileHistorySearch

		case actionRevisionTree:
			m.screen = screenRunning
			m.runningTitle = "Building ASCII revision tree..."
			return m, loadRevisionTreeCmd(m.activeRepo, false)

		case actionQuit:
			return m, tea.Quit
		}
	}

	m.actionOffset = adjustOffset(m.actionOffset, m.actionCursor, visible)

	return m, nil
}

func (m model) updateCreateBranchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		param := strings.TrimSpace(m.input.Value())
		if param == "" {
			m.screen = screenResult
			m.err = fmt.Errorf("branch name parameter is required")
			m.result = "Please enter a branch parameter, e.g. ASD-123."
			m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
			return m, nil
		}

		m.screen = screenRunning
		m.runningTitle = "Creating branch..."
		return m, createBranchCmd(m.activeRepo, param)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	return m, cmd
}

func (m model) updateCheckoutRevisionInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		revision := strings.TrimSpace(m.input.Value())

		if revision == "" {
			m.screen = screenResult
			m.err = fmt.Errorf("revision number is required")
			m.result = "Please enter an SVN revision number."
			m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
			return m, nil
		}

		for _, ch := range revision {
			if ch < '0' || ch > '9' {
				m.screen = screenResult
				m.err = fmt.Errorf("invalid revision number: %s", revision)
				m.result = "Revision must be a number, e.g. 12345."
				m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
				return m, nil
			}
		}

		m.screen = screenRunning
		m.runningTitle = "Checking out revision..."
		return m, checkoutRevisionCmd(m.activeRepo, revision)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	return m, cmd
}

func (m model) updateFileHistorySearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		query := strings.TrimSpace(m.input.Value())
		if query == "" {
			m.screen = screenResult
			m.err = fmt.Errorf("file history search query is required")
			m.result = "Please enter a file path search query, e.g. action.php or inc/config."
			m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
			return m, nil
		}

		m.screen = screenRunning
		m.runningTitle = "Searching files..."
		return m, searchFileHistoryMatchesCmd(m.activeRepo, query)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	return m, cmd
}

func (m model) updateFileHistorySelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(10)

	switch msg.String() {
	case "up", "k":
		if m.fileHistoryCursor > 0 {
			m.fileHistoryCursor--
		}

	case "down", "j":
		if m.fileHistoryCursor < len(m.fileHistoryItems)-1 {
			m.fileHistoryCursor++
		}

	case "pgup":
		m.fileHistoryCursor = max(0, m.fileHistoryCursor-visible)

	case "pgdown":
		m.fileHistoryCursor = min(len(m.fileHistoryItems)-1, m.fileHistoryCursor+visible)

	case "home":
		m.fileHistoryCursor = 0

	case "end":
		m.fileHistoryCursor = len(m.fileHistoryItems) - 1

	case "enter":
		if len(m.fileHistoryItems) == 0 {
			return m, nil
		}

		path := m.fileHistoryItems[m.fileHistoryCursor]
		m.screen = screenRunning
		m.runningTitle = "Loading file history..."
		return m, loadFileHistoryCmd(m.activeRepo, path)
	}

	m.fileHistoryOffset = adjustOffset(m.fileHistoryOffset, m.fileHistoryCursor, visible)

	return m, nil
}

func (m model) updateBranchSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(8)

	switch msg.String() {
	case "up", "k":
		m.branchNumberInput = ""
		if m.branchCursor > 0 {
			m.branchCursor--
		}

	case "down", "j":
		m.branchNumberInput = ""
		if m.branchCursor < len(m.branches)-1 {
			m.branchCursor++
		}

	case "pgup":
		m.branchNumberInput = ""
		m.branchCursor = max(0, m.branchCursor-visible)

	case "pgdown":
		m.branchNumberInput = ""
		m.branchCursor = min(len(m.branches)-1, m.branchCursor+visible)

	case "home":
		m.branchNumberInput = ""
		m.branchCursor = 0

	case "end":
		m.branchNumberInput = ""
		m.branchCursor = len(m.branches) - 1

	case "backspace", "ctrl+h":
		if len(m.branchNumberInput) > 0 {
			m.branchNumberInput = m.branchNumberInput[:len(m.branchNumberInput)-1]
		}

	case "enter":
		selectedIndex := m.branchCursor
		if strings.TrimSpace(m.branchNumberInput) != "" {
			var number int
			if _, err := fmt.Sscanf(m.branchNumberInput, "%d", &number); err != nil || number < 1 || number > len(m.branches) {
				m.screen = screenResult
				m.err = fmt.Errorf("invalid branch number: %s", m.branchNumberInput)
				m.result = fmt.Sprintf("Branch number must be between 1 and %d.", len(m.branches))
				m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
				m.branchNumberInput = ""
				return m, nil
			}
			selectedIndex = number - 1
		}

		selected := m.branches[selectedIndex]

		m.screen = screenRunning
		m.runningTitle = "Switching to branch..."
		m.branchNumberInput = ""
		return m, switchBranchCmd(m.activeRepo, selected.Name)

	default:
		key := msg.String()
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

func (m model) updatePullSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(10)

	switch msg.String() {
	case "up", "k":
		if m.commitCursor > 0 {
			m.commitCursor--
		}

	case "down", "j":
		if m.commitCursor < len(m.commitItems)-1 {
			m.commitCursor++
		}

	case "pgup":
		m.commitCursor = max(0, m.commitCursor-visible)

	case "pgdown":
		m.commitCursor = min(len(m.commitItems)-1, m.commitCursor+visible)

	case "home":
		m.commitCursor = 0

	case "end":
		m.commitCursor = len(m.commitItems) - 1

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
			return m, nil
		}

		item := m.commitItems[m.commitCursor]
		m.screen = screenRunning
		m.runningTitle = "Loading incoming diff..."
		return m, remoteDiffCmd(m.activeRepo, item, m.width)

	case "enter":
		paths := selectedCommitPaths(m.commitItems)
		if len(paths) == 0 {
			m.screen = screenResult
			m.err = fmt.Errorf("no files selected")
			m.result = "Select at least one incoming file with Space before pulling. Use 'a' to select all."
			m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
			return m, nil
		}

		m.screen = screenRunning
		m.runningTitle = "Pulling selected files..."
		return m, pullCmd(m.activeRepo, paths)
	}

	m.commitOffset = adjustOffset(m.commitOffset, m.commitCursor, visible)

	return m, nil
}

func (m model) updateCommitSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(10)

	switch msg.String() {
	case "up", "k":
		if m.commitCursor > 0 {
			m.commitCursor--
		}

	case "down", "j":
		if m.commitCursor < len(m.commitItems)-1 {
			m.commitCursor++
		}

	case "pgup":
		m.commitCursor = max(0, m.commitCursor-visible)

	case "pgdown":
		m.commitCursor = min(len(m.commitItems)-1, m.commitCursor+visible)

	case "home":
		m.commitCursor = 0

	case "end":
		m.commitCursor = len(m.commitItems) - 1

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
			return m, nil
		}

		item := m.commitItems[m.commitCursor]

		m.screen = screenRunning
		m.runningTitle = "Loading side-by-side diff..."
		return m, diffCmd(m.activeRepo, item, m.viewport.Width)

	case "p":
		if len(m.commitItems) == 0 {
			return m, nil
		}

		item := m.commitItems[m.commitCursor]
		if item.Unversioned || strings.HasPrefix(item.Status, "?") {
			m.screen = screenResult
			m.err = fmt.Errorf("partial commit is not available for unversioned files")
			m.result = "Use normal commit for unversioned files. Partial commit only works for modified versioned files."
			m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
			return m, nil
		}

		if !strings.HasPrefix(item.Status, "M") {
			m.screen = screenResult
			m.err = fmt.Errorf("partial commit is only supported for modified versioned files")
			m.result = "Partial commit currently supports M files only. Added/deleted/replaced files should use normal commit."
			m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
			return m, nil
		}

		m.screen = screenRunning
		m.runningTitle = "Loading partial commit hunks..."
		return m, loadPartialHunksCmd(m.activeRepo, item)

	case "enter":
		if len(selectedCommitItems(m.commitItems)) == 0 {
			m.screen = screenResult
			m.err = fmt.Errorf("no files selected")
			m.result = "Select at least one file with Space before committing."
			m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
			return m, nil
		}

		m.input.Reset()
		m.input.Placeholder = "Commit message"
		m.input.Focus()
		m.screen = screenCommitMessageInput
	}

	m.commitOffset = adjustOffset(m.commitOffset, m.commitCursor, visible)

	return m, nil
}

func (m model) updatePartialHunkSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(10)

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
		m.partialHunkCursor = max(0, m.partialHunkCursor-visible)

	case "pgdown":
		m.partialHunkCursor = min(len(m.partialHunks)-1, m.partialHunkCursor+visible)

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
		if len(selectedPartialHunks(m.partialHunks)) == 0 {
			m.screen = screenResult
			m.err = fmt.Errorf("no hunks selected")
			m.result = "Select at least one hunk with Space before partial committing."
			m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
			return m, nil
		}

		m.partialCommit = true
		m.input.Reset()
		m.input.Placeholder = "Partial commit message"
		m.input.Focus()
		m.screen = screenCommitMessageInput
	}

	m.partialHunkOffset = adjustOffset(m.partialHunkOffset, m.partialHunkCursor, visible)

	return m, nil
}

func (m model) updateRevertSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(10)

	switch msg.String() {
	case "up", "k":
		if m.commitCursor > 0 {
			m.commitCursor--
		}

	case "down", "j":
		if m.commitCursor < len(m.commitItems)-1 {
			m.commitCursor++
		}

	case "pgup":
		m.commitCursor = max(0, m.commitCursor-visible)

	case "pgdown":
		m.commitCursor = min(len(m.commitItems)-1, m.commitCursor+visible)

	case "home":
		m.commitCursor = 0

	case "end":
		m.commitCursor = len(m.commitItems) - 1

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
			return m, nil
		}

		item := m.commitItems[m.commitCursor]

		m.screen = screenRunning
		m.runningTitle = "Loading side-by-side diff..."
		return m, diffCmd(m.activeRepo, item, m.viewport.Width)

	case "enter":
		paths := selectedCommitPaths(m.commitItems)

		if len(paths) == 0 {
			m.screen = screenResult
			m.err = fmt.Errorf("no files selected")
			m.result = "Select at least one file with Space before reverting."
			m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
			return m, nil
		}

		m.screen = screenRunning
		m.runningTitle = "Reverting selected files..."
		return m, revertCmd(m.activeRepo, paths)
	}

	m.commitOffset = adjustOffset(m.commitOffset, m.commitCursor, visible)

	return m, nil
}

func (m model) updateCommitMessageInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		message := strings.TrimSpace(m.input.Value())
		if message == "" {
			m.screen = screenResult
			m.err = fmt.Errorf("commit message is required")
			m.result = "Please enter a commit message."
			m.viewport.SetContent(m.result + "\n\n" + m.err.Error())
			return m, nil
		}

		if m.partialCommit {
			item := m.partialItem
			hunks := selectedPartialHunks(m.partialHunks)

			m.partialCommit = false
			m.partialItem = commitItem{}

			m.screen = screenRunning
			m.runningTitle = "Committing selected hunks..."
			return m, partialHunkCommitCmd(m.activeRepo, item, hunks, message)
		}

		items := selectedCommitItems(m.commitItems)

		m.screen = screenRunning
		m.runningTitle = "Committing selected files..."
		return m, commitCmd(m.activeRepo, items, message)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	return m, cmd
}

func (m model) updateConflictSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleListCount(10)

	switch msg.String() {
	case "up", "k":
		if m.conflictCursor > 0 {
			m.conflictCursor--
		}

	case "down", "j":
		if m.conflictCursor < len(m.conflictItems)-1 {
			m.conflictCursor++
		}

	case "pgup":
		m.conflictCursor = max(0, m.conflictCursor-visible)

	case "pgdown":
		m.conflictCursor = min(len(m.conflictItems)-1, m.conflictCursor+visible)

	case "home":
		m.conflictCursor = 0

	case "end":
		m.conflictCursor = len(m.conflictItems) - 1

	case "enter":
		selected := m.conflictItems[m.conflictCursor]

		m.screen = screenRunning
		m.runningTitle = "Resolving conflict with Meld..."
		return m, resolveConflictWithMeldCmd(m.activeRepo, selected.Path)
	}

	m.conflictOffset = adjustOffset(m.conflictOffset, m.conflictCursor, visible)

	return m, nil
}

func (m model) View() string {
	switch m.screen {
	case screenRepoSelect:
		return m.viewRepoSelect()

	case screenActionSelect:
		return m.viewActionSelect()

	case screenCreateBranchInput:
		return m.viewCreateBranchInput()

	case screenCheckoutRevisionInput:
		return m.viewCheckoutRevisionInput()

	case screenFileHistorySearch:
		return m.viewFileHistorySearch()

	case screenFileHistorySelect:
		return m.viewFileHistorySelect()

	case screenBranchSelect:
		return m.viewBranchSelect()

	case screenPullSelect:
		return m.viewPullSelect()

	case screenCommitSelect:
		return m.viewCommitSelect()

	case screenCommitMessageInput:
		return m.viewCommitMessageInput()

	case screenPartialHunkSelect:
		return m.viewPartialHunkSelect()

	case screenRevertSelect:
		return m.viewRevertSelect()

	case screenConflictSelect:
		return m.viewConflictSelect()

	case screenHistory:
		return m.viewHistory()

	case screenDiff:
		return m.viewDiff()

	case screenRunning:
		return m.viewRunning()

	case screenResult:
		return m.viewResult()
	}

	return ""
}

func (m model) viewRepoSelect() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Select SVN repository") + "\n")

	visible := m.visibleListCount(7)
	end := min(len(m.repos), m.repoOffset+visible)

	for i := m.repoOffset; i < end; i++ {
		r := m.repos[i]

		cursor := " "
		lineStyle := normalStyle

		if i == m.repoCursor {
			cursor = ">"
			lineStyle = successStyle
		}

		line := fmt.Sprintf("%s %s", cursor, r.Path)
		b.WriteString(lineStyle.Render(line) + "\n")
		b.WriteString(labelYellowStyle.Render("    URL: ") + valueWhiteStyle.Render(r.URL) + "\n")
		b.WriteString(labelYellowStyle.Render("    Root: ") + valueWhiteStyle.Render(r.Root) + "\n")

		if r.CurrentLocation != "" {
			b.WriteString(labelYellowStyle.Render("    Current: ") + valueWhiteStyle.Render(r.CurrentLocation) + "\n")
		}

		if r.Username != "" {
			b.WriteString(mutedStyle.Render("    User: "+r.Username) + "\n")
		}
		if r.BranchUsername != "" {
			b.WriteString(mutedStyle.Render("    Branch user: "+r.BranchUsername) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(scrollHint(m.repoOffset, end, len(m.repos))) + "\n")
	b.WriteString(mutedStyle.Render("↑/↓ or j/k: move | PgUp/PgDn: scroll | Enter: select | q: quit"))

	return b.String()
}

func (m model) viewActionSelect() string {
	var b strings.Builder

	b.WriteString(repoInfoHeader("Select SVN action", m.activeRepo))

	visible := m.visibleListCount(10)
	end := min(len(m.actions), m.actionOffset+visible)

	for i := m.actionOffset; i < end; i++ {
		a := m.actions[i]

		cursor := " "
		lineStyle := actionStyle

		if i == m.actionCursor {
			cursor = ">"
			lineStyle = actionSelectedStyle
		}

		b.WriteString(lineStyle.Render(fmt.Sprintf("%s %s", cursor, a)) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(scrollHint(m.actionOffset, end, len(m.actions))) + "\n")
	b.WriteString(mutedStyle.Render("↑/↓ or j/k: move | Enter: run | Esc: repositories | q: quit"))

	return b.String()
}

func (m model) viewCreateBranchInput() string {
	var b strings.Builder

	b.WriteString(repoInfoHeader("Create branch", m.activeRepo))
	b.WriteString(textStyle.Render("Branch parameter:") + "\n")
	b.WriteString(m.input.View() + "\n\n")

	b.WriteString(mutedStyle.Render("Final branch name format: YYYY-MM-DD_username_parameter"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Enter: create branch | Esc: back"))

	return b.String()
}

func (m model) viewCheckoutRevisionInput() string {
	var b strings.Builder

	b.WriteString(repoInfoHeader("Checkout revision", m.activeRepo))
	b.WriteString(mutedStyle.Render("This runs: svn update -r REVISION") + "\n\n")

	b.WriteString(textStyle.Render("Revision number:") + "\n")
	b.WriteString(m.input.View() + "\n\n")

	b.WriteString(warningStyle.Render("Note: Pull will update the working copy back to HEAD.") + "\n")
	b.WriteString(mutedStyle.Render("Enter: checkout revision | Esc: back"))

	return b.String()
}

func (m model) viewFileHistorySearch() string {
	var b strings.Builder

	b.WriteString(repoInfoHeader("File history search", m.activeRepo))
	b.WriteString(mutedStyle.Render("Search by local working-copy file path. This does not list the whole repository.") + "\n\n")
	b.WriteString(textStyle.Render("File path search:") + "\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(mutedStyle.Render("Enter: search | Esc: back"))

	return b.String()
}

func (m model) viewFileHistorySelect() string {
	var b strings.Builder

	b.WriteString(repoInfoHeader("File history", m.activeRepo))
	b.WriteString(mutedStyle.Render("Search: "+m.fileHistoryQuery) + "\n\n")
	b.WriteString(textStyle.Render("Matching files:") + "\n")
	b.WriteString(mutedStyle.Render("---------------") + "\n")

	visible := m.visibleListCount(12)
	end := min(len(m.fileHistoryItems), m.fileHistoryOffset+visible)

	for i := m.fileHistoryOffset; i < end; i++ {
		path := m.fileHistoryItems[i]

		cursor := " "
		lineStyle := normalStyle

		if i == m.fileHistoryCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}

		line := fmt.Sprintf("%s %s", cursor, path)
		b.WriteString(lineStyle.Render(line) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(scrollHint(m.fileHistoryOffset, end, len(m.fileHistoryItems))) + "\n")
	b.WriteString(mutedStyle.Render("↑/↓ or j/k: move | PgUp/PgDn: scroll | Enter: show history | Esc: back"))

	return b.String()
}

func (m model) viewBranchSelect() string {
	var b strings.Builder

	b.WriteString(repoInfoHeader("Switch to branch", m.activeRepo))
	b.WriteString(textStyle.Render("Available SVN branches:") + "\n")
	if strings.TrimSpace(m.branchNumberInput) != "" {
		b.WriteString(labelYellowStyle.Render("Branch number: ") + valueWhiteStyle.Render(m.branchNumberInput) + mutedStyle.Render("  Enter: switch to this number | Backspace: edit") + "\n")
	}
	b.WriteString(mutedStyle.Render("-----------------------") + "\n")

	visible := m.visibleListCount(10)
	end := min(len(m.branches), m.branchOffset+visible)

	for i := m.branchOffset; i < end; i++ {
		br := m.branches[i]

		cursor := " "
		lineStyle := normalStyle

		if i == m.branchCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}

		line := fmt.Sprintf("%s %3d) %s", cursor, i+1, br.Name)
		b.WriteString(lineStyle.Render(line) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(scrollHint(m.branchOffset, end, len(m.branches))) + "\n")
	b.WriteString(mutedStyle.Render("↑/↓ or j/k: move | type number + Enter: switch | PgUp/PgDn: scroll | Esc: back"))

	return b.String()
}

func (m model) viewPullSelect() string {
	var b strings.Builder

	selectedCount := len(selectedCommitPaths(m.commitItems))

	b.WriteString(repoInfoHeader("Pull incoming changes", m.activeRepo))
	b.WriteString(mutedStyle.Render(fmt.Sprintf("Selected files: %d of %d", selectedCount, len(m.commitItems))) + "\n\n")
	b.WriteString(textStyle.Render("Incoming repository changes:") + "\n")
	b.WriteString(mutedStyle.Render("----------------------------") + "\n")

	visible := m.visibleListCount(12)
	end := min(len(m.commitItems), m.commitOffset+visible)

	for i := m.commitOffset; i < end; i++ {
		item := m.commitItems[i]

		cursor := " "
		lineStyle := normalStyle

		if i == m.commitCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}

		check := checkboxStyle.Render("[ ]")
		if item.Selected {
			check = checkedStyle.Render("[x]")
		}

		status := colorizePullStatus(item.Status)
		line := fmt.Sprintf("%s %s ", cursor, check) + status + " " + valueWhiteStyle.Render(item.Path)

		if item.Selected && i != m.commitCursor {
			b.WriteString(checkedStyle.Render(fmt.Sprintf("%s %s ", cursor, "[x]")) + status + " " + valueWhiteStyle.Render(item.Path) + "\n")
		} else {
			b.WriteString(lineStyle.Render(line) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(scrollHint(m.commitOffset, end, len(m.commitItems))) + "\n")
	b.WriteString(mutedStyle.Render("Space: select | a: all | n: none | d: diff | Enter: pull selected | Esc: back"))

	return b.String()
}

func (m model) viewCommitSelect() string {
	var b strings.Builder

	selectedCount := len(selectedCommitItems(m.commitItems))
	unversionedCount := len(selectedUnversionedCommitPaths(m.commitItems))

	b.WriteString(repoInfoHeader("Commit", m.activeRepo))
	b.WriteString(mutedStyle.Render(fmt.Sprintf("Selected files: %d | Selected unversioned files that will be svn add-ed first: %d", selectedCount, unversionedCount)) + "\n\n")
	b.WriteString(textStyle.Render("Working copy changes:") + "\n")
	b.WriteString(mutedStyle.Render("---------------------") + "\n")

	visible := m.visibleListCount(12)
	end := min(len(m.commitItems), m.commitOffset+visible)

	for i := m.commitOffset; i < end; i++ {
		item := m.commitItems[i]

		cursor := " "
		lineStyle := normalStyle

		if i == m.commitCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}

		check := checkboxStyle.Render("[ ]")
		if item.Selected {
			check = checkedStyle.Render("[x]")
		}

		status := item.Status
		if item.Unversioned {
			status = "? add"
		}

		line := fmt.Sprintf("%s %s %-8s %s", cursor, check, status, item.Path)

		if item.Selected && i != m.commitCursor {
			b.WriteString(checkedStyle.Render(line) + "\n")
		} else {
			b.WriteString(lineStyle.Render(line) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(scrollHint(m.commitOffset, end, len(m.commitItems))) + "\n")
	b.WriteString(mutedStyle.Render("Space: select | a: all | n: none | d: diff | p: partial hunks | Enter: commit message | Esc: back"))

	return b.String()
}

func (m model) viewPartialHunkSelect() string {
	var b strings.Builder

	selectedCount := len(selectedPartialHunks(m.partialHunks))

	b.WriteString(repoInfoHeader("Partial commit hunks", m.activeRepo))
	b.WriteString(mutedStyle.Render("File: "+m.partialItem.Path) + "\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("Selected hunks: %d of %d", selectedCount, len(m.partialHunks))) + "\n\n")
	b.WriteString(textStyle.Render("Choose hunks to commit:") + "\n")
	b.WriteString(mutedStyle.Render("-----------------------") + "\n")

	visible := m.visibleListCount(12)
	end := min(len(m.partialHunks), m.partialHunkOffset+visible)

	for i := m.partialHunkOffset; i < end; i++ {
		hunk := m.partialHunks[i]

		cursor := " "
		lineStyle := normalStyle

		if i == m.partialHunkCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}

		check := checkboxStyle.Render("[ ]")
		if hunk.Selected {
			check = checkedStyle.Render("[x]")
		}

		addInfo := diffAddedStyle.Render(fmt.Sprintf("+%d", hunk.Added))
		removeInfo := diffDeletedStyle.Render(fmt.Sprintf("-%d", hunk.Removed))
		headerText := hunk.Header
		if hunk.Added > 0 && hunk.Removed > 0 {
			headerText = diffModifiedStyle.Render(headerText)
		}

		summary := fmt.Sprintf("%s %s hunk %d  %s  %s %s", cursor, check, i+1, headerText, addInfo, removeInfo)
		if hunk.Selected && i != m.partialHunkCursor {
			b.WriteString(checkedStyle.Render(summary) + "\n")
		} else {
			b.WriteString(lineStyle.Render(summary) + "\n")
		}

		for _, previewLine := range renderPartialHunkPreview(hunk, 8) {
			b.WriteString("      " + previewLine + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(scrollHint(m.partialHunkOffset, end, len(m.partialHunks))) + "\n")
	b.WriteString(mutedStyle.Render("Space: select | a: all | n: none | Enter: commit message | Esc: back"))

	return b.String()
}

func (m model) viewRevertSelect() string {
	var b strings.Builder

	selectedCount := len(selectedCommitPaths(m.commitItems))

	b.WriteString(repoInfoHeader("Revert files", m.activeRepo))
	b.WriteString(warningStyle.Render("Warning: SVN revert discards local changes for selected versioned files.") + "\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("Selected files: %d", selectedCount)) + "\n\n")
	b.WriteString(textStyle.Render("Working copy changes:") + "\n")
	b.WriteString(mutedStyle.Render("---------------------") + "\n")

	visible := m.visibleListCount(12)
	end := min(len(m.commitItems), m.commitOffset+visible)

	for i := m.commitOffset; i < end; i++ {
		item := m.commitItems[i]

		cursor := " "
		lineStyle := normalStyle

		if i == m.commitCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}

		check := checkboxStyle.Render("[ ]")
		if item.Selected {
			check = checkedStyle.Render("[x]")
		}

		line := fmt.Sprintf("%s %s %-8s %s", cursor, check, item.Status, item.Path)

		if item.Selected && i != m.commitCursor {
			b.WriteString(checkedStyle.Render(line) + "\n")
		} else {
			b.WriteString(lineStyle.Render(line) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(scrollHint(m.commitOffset, end, len(m.commitItems))) + "\n")
	b.WriteString(mutedStyle.Render("Space: select | a: all | n: none | d: side-by-side diff | Enter: revert selected | Esc: back"))

	return b.String()
}

func renderPartialHunkPreview(h partialHunk, maxChangedLines int) []string {
	type previewLine struct {
		Prefix string
		Text   string
		Kind   string
	}

	var changed []previewLine
	for _, rawLine := range h.Lines {
		if rawLine == "" {
			continue
		}

		prefix := string(rawLine[0])
		if prefix == " " {
			continue
		}

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

		changed = append(changed, previewLine{
			Prefix: prefix,
			Text:   strings.TrimSpace(text),
			Kind:   kind,
		})
	}

	for i := 0; i < len(changed)-1; i++ {
		if changed[i].Prefix == "-" && changed[i+1].Prefix == "+" {
			changed[i].Kind = "modified"
			changed[i+1].Kind = "modified"
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

		preview := line.Prefix + " " + line.Text
		switch line.Kind {
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

func (m model) viewCommitMessageInput() string {
	var b strings.Builder

	if m.partialCommit {
		selected := selectedPartialHunks(m.partialHunks)

		b.WriteString(repoInfoHeader("Partial commit message", m.activeRepo))
		b.WriteString(mutedStyle.Render("File: "+m.partialItem.Path) + "\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("Selected hunks: %d", len(selected))) + "\n\n")

		previewLimit := min(6, len(selected))
		for i := 0; i < previewLimit; i++ {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  hunk %d: %s", i+1, selected[i].Header)) + "\n")
		}

		if len(selected) > previewLimit {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  ... and %d more", len(selected)-previewLimit)) + "\n")
		}

		b.WriteString("\n")
		b.WriteString(textStyle.Render("Commit message:") + "\n")
		b.WriteString(m.input.View() + "\n\n")
		b.WriteString(mutedStyle.Render("Enter: commit selected hunks | Esc: back"))

		return b.String()
	}

	items := selectedCommitItems(m.commitItems)

	b.WriteString(repoInfoHeader("Commit message", m.activeRepo))
	b.WriteString(mutedStyle.Render(fmt.Sprintf("Files selected: %d", len(items))) + "\n\n")

	previewLimit := min(8, len(items))
	for i := 0; i < previewLimit; i++ {
		prefix := "  "
		if items[i].Unversioned {
			prefix = "  + "
		}

		b.WriteString(mutedStyle.Render(prefix+items[i].Path) + "\n")
	}

	if len(items) > previewLimit {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  ... and %d more", len(items)-previewLimit)) + "\n")
	}

	if len(selectedUnversionedCommitPaths(m.commitItems)) > 0 {
		b.WriteString("\n")
		b.WriteString(warningStyle.Render("Selected unversioned files will be added with svn add before commit.") + "\n")
	}

	b.WriteString("\n")
	b.WriteString(textStyle.Render("Commit message:") + "\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(mutedStyle.Render("Enter: commit | Esc: back"))

	return b.String()
}

func (m model) viewConflictSelect() string {
	var b strings.Builder

	b.WriteString(repoInfoHeader("Resolve conflicts", m.activeRepo))
	b.WriteString(textStyle.Render("Conflicted files:") + "\n")
	b.WriteString(mutedStyle.Render("-----------------") + "\n")

	visible := m.visibleListCount(12)
	end := min(len(m.conflictItems), m.conflictOffset+visible)

	for i := m.conflictOffset; i < end; i++ {
		item := m.conflictItems[i]

		cursor := " "
		lineStyle := normalStyle

		if i == m.conflictCursor {
			cursor = ">"
			lineStyle = selectedStyle
		}

		line := fmt.Sprintf("%s %-8s %s", cursor, item.Status, item.Path)
		b.WriteString(lineStyle.Render(line) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(warningStyle.Render("File conflicts open Meld. Tree conflicts are resolved with --accept=working.") + "\n")
	b.WriteString(mutedStyle.Render(scrollHint(m.conflictOffset, end, len(m.conflictItems))) + "\n")
	b.WriteString(mutedStyle.Render("↑/↓ or j/k: move | PgUp/PgDn: scroll | Enter: resolve with Meld | Esc: back"))

	return b.String()
}

func (m model) viewHistory() string {
	var b strings.Builder

	title := strings.TrimSpace(m.historyTitle)
	if title == "" {
		title = "Commit history"
	}

	b.WriteString(repoInfoHeader(title, m.activeRepo))
	if m.selectedAction == actionRevisionTree {
		b.WriteString(mutedStyle.Render("↑/↓: scroll | PgUp/PgDn: page | Home/End: jump | a: load full history | Esc: back | q: quit") + "\n\n")
	} else {
		b.WriteString(mutedStyle.Render("↑/↓: scroll | PgUp/PgDn: page | Home/End: jump | Esc: back | q: quit") + "\n\n")
	}
	b.WriteString(textStyle.Render(m.viewport.View()))

	return b.String()
}

func (m model) viewDiff() string {
	var b strings.Builder

	b.WriteString(repoInfoHeader("Side-by-side diff viewer", m.activeRepo))
	b.WriteString(mutedStyle.Render("Legend: = same | - removed/old | + added/new | ~ changed") + "\n")
	b.WriteString(mutedStyle.Render("↑/↓: scroll | PgUp/PgDn: page | Home/End: jump | Esc: back") + "\n\n")
	b.WriteString(m.viewport.View())

	return b.String()
}

func (m model) viewRunning() string {
	return repoInfoHeader(m.runningTitle, m.activeRepo) +
		mutedStyle.Render("SVN is working. It may grumble a little.")
}

func (m model) viewResult() string {
	var b strings.Builder

	if m.err != nil {
		b.WriteString(repoInfoHeader("Error", m.activeRepo))
	} else {
		b.WriteString(repoInfoHeader("Done", m.activeRepo))
	}

	b.WriteString(textStyle.Render(m.viewport.View()))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render("↑/↓: scroll | PgUp/PgDn: page | Enter: back to menu | q: quit"))

	return b.String()
}

func loadBranchesCmd(r repo) tea.Cmd {
	return func() tea.Msg {
		branches, err := loadBranches(r)

		return branchesLoadedMsg{
			Branches: branches,
			Err:      err,
		}
	}
}

func loadBranches(r repo) ([]branch, error) {
	branchesURL := r.Root + "/branches"

	out, err := svn(r, "list", "-v", branchesURL)
	if err != nil {
		return nil, fmt.Errorf(
			"svn list failed\n\nWorking copy: %s\nBranches URL: %s\n\nOutput:\n%s\n\nError: %w",
			r.Path,
			branchesURL,
			out,
			err,
		)
	}

	var branches []branch

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		revision := 0
		fmt.Sscanf(fields[0], "%d", &revision)

		name := fields[len(fields)-1]
		name = strings.TrimSuffix(name, "/")

		if name == "." || name == "" {
			continue
		}

		branches = append(branches, branch{
			Name:     name,
			Revision: revision,
		})
	}

	sort.Slice(branches, func(i, j int) bool {
		if branches[i].Revision == branches[j].Revision {
			return branches[i].Name > branches[j].Name
		}

		return branches[i].Revision > branches[j].Revision
	})

	return branches, nil
}

func loadPullItemsCmd(r repo) tea.Cmd {
	return func() tea.Msg {
		items, err := loadPullItems(r)
		return pullItemsLoadedMsg{
			Items: items,
			Err:   err,
		}
	}
}

func loadPullItems(r repo) ([]commitItem, error) {
	out, err := svn(r, "diff", "--summarize", "-r", "BASE:HEAD")
	if err != nil {
		return nil, fmt.Errorf(
			"svn incoming diff summary failed\n\nWorking copy: %s\n\nOutput:\n%s\n\nError: %w",
			r.Path,
			out,
			err,
		)
	}

	var items []commitItem
	for _, line := range strings.Split(out, "\n") {
		item, ok := parseSVNDiffSummaryLine(line)
		if ok {
			item.Selected = false
			items = append(items, item)
		}
	}

	if len(items) == 0 {
		statusOut, statusErr := svn(r, "status", "-u")
		if statusErr == nil {
			for _, line := range strings.Split(statusOut, "\n") {
				item, ok := parseSVNStatusUpdateLine(line)
				if ok {
					item.Selected = false
					items = append(items, item)
				}
			}
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Status == items[j].Status {
			return items[i].Path < items[j].Path
		}
		return items[i].Status < items[j].Status
	})

	return items, nil
}

func parseSVNDiffSummaryLine(line string) (commitItem, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return commitItem{}, false
	}

	fields := strings.Fields(trimmed)
	if len(fields) < 2 {
		return commitItem{}, false
	}

	status := strings.TrimSpace(fields[0])
	path := strings.Join(fields[1:], " ")
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	if path == "" {
		return commitItem{}, false
	}

	return commitItem{Status: status, Path: path}, true
}

func parseSVNStatusUpdateLine(line string) (commitItem, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "Status against revision") {
		return commitItem{}, false
	}

	prefixLen := min(len(line), 24)
	if !strings.Contains(line[:prefixLen], "*") {
		return commitItem{}, false
	}

	status, path, ok := parseSVNStatusLine(line)
	if !ok {
		return commitItem{}, false
	}

	if strings.TrimSpace(status) == "" {
		status = "U"
	}

	fields := strings.Fields(line)
	if len(fields) > 0 {
		last := strings.TrimPrefix(filepath.ToSlash(fields[len(fields)-1]), "./")
		if isSafeSVNWorkingCopyPath(last) {
			path = last
		}
	}

	return commitItem{Status: status, Path: path}, true
}

func colorizePullStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		status = "U"
	}
	return statusStyleForSVNPathAction(status).Render(fmt.Sprintf("%-8s", status))
}

func parseSVNStatusLine(line string) (string, string, bool) {
	if isSVNStatusNoiseLine(line) || len(line) < 8 {
		return "", "", false
	}

	statusArea := line[:8]
	if strings.TrimSpace(statusArea) == ">" || !isValidSVNStatusArea(statusArea) {
		return "", "", false
	}

	path := strings.TrimSpace(line[8:])
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	if !isSafeSVNWorkingCopyPath(path) {
		return "", "", false
	}

	return strings.TrimSpace(statusArea), path, true
}

func isValidSVNStatusArea(statusArea string) bool {
	if len(statusArea) == 0 {
		return false
	}

	hasStatus := false
	allowed := "ACDMR?!~IXL+SKOC"
	for _, r := range statusArea {
		if r == ' ' || r == '\t' {
			continue
		}
		if !strings.ContainsRune(allowed, r) {
			return false
		}
		hasStatus = true
	}

	return hasStatus
}

func isSVNStatusNoiseLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}

	lower := strings.ToLower(trimmed)
	noisePrefixes := []string{
		">",
		"summary of conflicts:",
		"text conflicts:",
		"property conflicts:",
		"tree conflicts:",
		"local ",
		"incoming ",
		"of conflicts:",
		"conflicts:",
	}
	for _, prefix := range noisePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	return false
}

func isSafeSVNWorkingCopyPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || path == "." || path == ".." || strings.HasPrefix(path, "-") {
		return false
	}

	lower := strings.ToLower(path)
	noiseFragments := []string{
		"local dir edit",
		"incoming dir delete",
		"incoming file delete",
		"summary of conflicts",
		"tree conflicts:",
		"text conflicts:",
		"property conflicts:",
	}
	for _, fragment := range noiseFragments {
		if strings.Contains(lower, fragment) {
			return false
		}
	}

	return true
}

func loadCommitItemsCmd(r repo) tea.Cmd {
	return func() tea.Msg {
		items, err := loadCommitItems(r, true)

		return commitItemsLoadedMsg{
			Items: items,
			Err:   err,
		}
	}
}

func loadRevertItemsCmd(r repo) tea.Cmd {
	return func() tea.Msg {
		items, err := loadCommitItems(r, false)

		return revertItemsLoadedMsg{
			Items: items,
			Err:   err,
		}
	}
}

func loadCommitItems(r repo, includeUnversioned bool) ([]commitItem, error) {
	out, err := svn(r, "status")
	if err != nil {
		return nil, fmt.Errorf(
			"svn status failed\n\nWorking copy: %s\n\nOutput:\n%s\n\nError: %w",
			r.Path,
			out,
			err,
		)
	}

	var items []commitItem

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		status, path, ok := parseSVNStatusLine(line)
		if !ok {
			continue
		}

		unversioned := strings.HasPrefix(status, "?")
		if unversioned && !includeUnversioned {
			continue
		}

		items = append(items, commitItem{
			Status:      status,
			Path:        path,
			Selected:    false,
			Unversioned: unversioned,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Unversioned != items[j].Unversioned {
			return !items[i].Unversioned && items[j].Unversioned
		}

		return items[i].Path < items[j].Path
	})

	return items, nil
}

func loadConflictItemsCmd(r repo) tea.Cmd {
	return func() tea.Msg {
		items, err := loadConflictItems(r)

		return conflictItemsLoadedMsg{
			Items: items,
			Err:   err,
		}
	}
}

func loadConflictItems(r repo) ([]conflictItem, error) {
	out, err := svn(r, "status")
	if err != nil {
		return nil, fmt.Errorf(
			"svn status failed\n\nWorking copy: %s\n\nOutput:\n%s\n\nError: %w",
			r.Path,
			out,
			err,
		)
	}

	var items []conflictItem

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		status, path, ok := parseSVNStatusLine(line)
		if !ok {
			continue
		}

		if strings.Contains(status, "C") {
			isTree := isTreeConflict(r, path)

			displayStatus := status
			if isTree {
				displayStatus = status + " TREE"
			}

			items = append(items, conflictItem{
				Status: displayStatus,
				Path:   path,
				IsTree: isTree,
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Path < items[j].Path
	})

	return items, nil
}

func isTreeConflict(r repo, path string) bool {
	infoOut, err := svn(r, "info", path)
	if err != nil {
		return false
	}

	return strings.Contains(strings.ToLower(infoOut), "tree conflict")
}

func createBranchCmd(r repo, parameter string) tea.Cmd {
	return func() tea.Msg {
		branchName, err := buildBranchName(r, parameter)
		if err != nil {
			return commandResult{
				Output:          "Failed to build branch name.",
				Err:             err,
				CurrentLocation: getCurrentLocation(r),
			}
		}

		trunkURL := r.Root + "/trunk"
		branchURL := r.Root + "/branches/" + branchName

		var output strings.Builder

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Repository root: " + r.Root + "\n")
		output.WriteString("Branch name: " + branchName + "\n\n")

		copyOut, err := svn(
			r,
			"copy",
			trunkURL,
			branchURL,
			"-m",
			"Creating branch "+branchName,
		)

		output.WriteString(copyOut)

		if err != nil {
			return commandResult{
				Output:          output.String(),
				Err:             err,
				CurrentLocation: getCurrentLocation(r),
			}
		}

		output.WriteString("\nSwitching to created branch...\n\n")

		switchOut, err := svn(
			r,
			"switch",
			"--ignore-ancestry",
			branchURL,
		)

		output.WriteString(switchOut)

		if err != nil {
			return commandResult{
				Output:          output.String(),
				Err:             err,
				CurrentLocation: getCurrentLocation(r),
			}
		}

		output.WriteString("\nBranch " + branchName + " created and switched to successfully.")

		return commandResult{
			Output:          output.String(),
			Err:             nil,
			CurrentLocation: getCurrentLocation(r),
		}
	}
}

func switchBranchCmd(r repo, branchName string) tea.Cmd {
	return func() tea.Msg {
		branchURL := r.Root + "/branches/" + branchName

		var output strings.Builder

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Switching to branch: " + branchName + "\n")
		output.WriteString("Target URL: " + branchURL + "\n\n")

		out, err := svn(
			r,
			"switch",
			"--ignore-ancestry",
			branchURL,
		)

		output.WriteString(out)

		if err == nil {
			output.WriteString("\nSwitched to branch " + branchName + " successfully.")
		}

		return commandResult{
			Output:          output.String(),
			Err:             err,
			CurrentLocation: getCurrentLocation(r),
		}
	}
}

func switchTrunkCmd(r repo) tea.Cmd {
	return func() tea.Msg {
		trunkURL := r.Root + "/trunk"

		var output strings.Builder

		currentURL, _ := svn(r, "info", "--show-item", "url")

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Current URL: " + strings.TrimSpace(currentURL) + "\n")
		output.WriteString("Switching to trunk\n")
		output.WriteString("Target URL: " + trunkURL + "\n\n")

		out, err := svn(
			r,
			"switch",
			"--ignore-ancestry",
			trunkURL,
		)

		output.WriteString(out)

		if err == nil {
			output.WriteString("\nSwitched back to trunk successfully.")
		}

		return commandResult{
			Output:          output.String(),
			Err:             err,
			CurrentLocation: getCurrentLocation(r),
		}
	}
}

func pullCmd(r repo, paths []string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		currentURL, _ := svn(r, "info", "--show-item", "url")

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Current URL: " + strings.TrimSpace(currentURL) + "\n")

		if r.Username != "" {
			output.WriteString("Auth user: " + r.Username + "\n")
		} else {
			output.WriteString("Auth user: NOT SET\n")
		}

		if r.Password != "" {
			output.WriteString("Auth password: SET\n")
		} else {
			output.WriteString("Auth password: NOT SET\n")
		}

		cleanedPaths := cleanSVNPathList(paths)
		if len(cleanedPaths) == 0 {
			return commandResult{
				Output:          output.String() + "\nNo valid SVN paths were selected for pull.",
				Err:             fmt.Errorf("no valid SVN paths selected"),
				CurrentLocation: getCurrentLocation(r),
			}
		}

		output.WriteString("\nRunning cleanup before pull...\n\n")
		cleanupOut, cleanupErr := svn(r, "cleanup")
		output.WriteString(cleanupOut)
		if cleanupErr != nil {
			return commandResult{
				Output:          output.String(),
				Err:             fmt.Errorf("svn cleanup before pull failed: %w", cleanupErr),
				CurrentLocation: getCurrentLocation(r),
			}
		}

		args := []string{"update", "--"}
		args = append(args, cleanedPaths...)

		output.WriteString("Selected files:\n")
		for _, path := range cleanedPaths {
			output.WriteString("  " + path + "\n")
		}
		output.WriteString("\nRunning: svn " + strings.Join(args, " ") + "\n\n")

		out, err := svn(r, args...)
		output.WriteString(colorizeSVNUpdateOutput(out))

		if err != nil {
			output.WriteString("\n\nUpdate failed or was interrupted. Running cleanup after failed pull...\n\n")
			cleanupOut, cleanupErr = svn(r, "cleanup")
			output.WriteString(cleanupOut)
			if cleanupErr != nil {
				err = fmt.Errorf("svn update failed: %w; cleanup after failure also failed: %v", err, cleanupErr)
			}
		}

		if err == nil {
			output.WriteString("\nPull finished successfully.")
		}

		return commandResult{
			Output:          output.String(),
			Err:             err,
			CurrentLocation: getCurrentLocation(r),
		}
	}
}

func statusCmd(r repo) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		currentURL, _ := svn(r, "info", "--show-item", "url")

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Current URL: " + strings.TrimSpace(currentURL) + "\n")
		output.WriteString("Running: svn status\n\n")

		out, err := svn(r, "status")
		output.WriteString(out)

		if err != nil {
			return commandResult{
				Output:          output.String(),
				Err:             err,
				CurrentLocation: getCurrentLocation(r),
			}
		}

		if strings.TrimSpace(out) == "" {
			output.WriteString("Working copy is clean. No local changes found.\n")
		}

		output.WriteString("\nStatus finished successfully.")

		return commandResult{
			Output:          output.String(),
			Err:             nil,
			CurrentLocation: getCurrentLocation(r),
		}
	}
}

func checkoutRevisionCmd(r repo, revision string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		currentURL, _ := svn(r, "info", "--show-item", "url")

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Current URL: " + strings.TrimSpace(currentURL) + "\n")
		output.WriteString("Target revision: " + revision + "\n")
		output.WriteString("Running: svn update -r " + revision + "\n\n")

		out, err := svn(r, "update", "-r", revision)
		output.WriteString(out)

		if err == nil {
			output.WriteString("\nWorking copy updated to revision " + revision + ".")
			output.WriteString("\nUse Pull to update back to HEAD.")
		}

		return commandResult{
			Output:          output.String(),
			Err:             err,
			CurrentLocation: getCurrentLocation(r),
		}
	}
}

func commitCmd(r repo, items []commitItem, message string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		paths := commitItemPaths(items)
		unversionedPaths := unversionedItemPaths(items)

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Commit message: " + message + "\n")
		output.WriteString("Selected files:\n")

		for _, item := range items {
			prefix := "  "
			if item.Unversioned {
				prefix = "  + "
			}
			output.WriteString(prefix + item.Path + "\n")
		}

		if len(unversionedPaths) > 0 {
			output.WriteString("\nAdding selected unversioned files before commit...\n\n")

			addArgs := []string{"add", "--parents"}
			addArgs = append(addArgs, unversionedPaths...)

			addOut, err := svn(r, addArgs...)
			output.WriteString(addOut)

			if err != nil {
				return commandResult{
					Output:          output.String(),
					Err:             err,
					CurrentLocation: getCurrentLocation(r),
				}
			}

			output.WriteString("\nUnversioned files added successfully.\n")
		}

		output.WriteString("\nRunning commit...\n\n")

		args := []string{"commit"}
		args = append(args, paths...)
		args = append(args, "-m", message)

		out, err := svn(r, args...)
		output.WriteString(out)

		if err == nil {
			output.WriteString("\nCommit finished successfully.")
		}

		return commandResult{
			Output:          output.String(),
			Err:             err,
			CurrentLocation: getCurrentLocation(r),
		}
	}
}

func diffCmd(r repo, item commitItem, width int) tea.Cmd {
	return func() tea.Msg {
		out, err := buildSideBySideDiff(r, item, width)

		if err != nil {
			fallbackOut, fallbackErr := svn(r, "diff", item.Path)
			if fallbackOut != "" {
				out += "\n\nUnified svn diff fallback:\n\n" + fallbackOut
			}

			if fallbackErr != nil {
				err = fmt.Errorf("%w\n\nfallback svn diff also failed: %v", err, fallbackErr)
			}

			return diffLoadedMsg{
				Output: out,
				Err: fmt.Errorf(
					"side-by-side diff failed\n\nWorking copy: %s\nPath: %s\n\nError: %w",
					r.Path,
					item.Path,
					err,
				),
				Path: item.Path,
			}
		}

		return diffLoadedMsg{
			Output: out,
			Err:    nil,
			Path:   item.Path,
		}
	}
}

func remoteDiffCmd(r repo, item commitItem, width int) tea.Cmd {
	return func() tea.Msg {
		out, err := svn(r, "diff", "-r", "BASE:HEAD", "--", item.Path)
		if err != nil {
			return diffLoadedMsg{
				Output: out,
				Err: fmt.Errorf(
					"incoming diff failed\n\nWorking copy: %s\nPath: %s\n\nOutput:\n%s\n\nError: %w",
					r.Path,
					item.Path,
					out,
					err,
				),
				Path: item.Path,
			}
		}

		if strings.TrimSpace(out) == "" {
			out = "No incoming diff found for:\n" + item.Path
		} else {
			out = "Incoming diff: BASE -> HEAD\nPath: " + item.Path + "\nStatus: " + item.Status + "\n\n" + colorizeUnifiedDiff(out)
		}

		return diffLoadedMsg{
			Output: out,
			Err:    nil,
			Path:   item.Path,
		}
	}
}

func revertCmd(r repo, paths []string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		paths = cleanSVNPathList(paths)

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Selected files to revert:\n")

		for _, p := range paths {
			output.WriteString("  " + p + "\n")
		}

		output.WriteString("\nRunning revert...\n\n")

		if len(paths) == 0 {
			return commandResult{
				Output:          output.String() + "\nNo valid SVN paths were selected for revert.",
				Err:             fmt.Errorf("no valid SVN paths selected"),
				CurrentLocation: getCurrentLocation(r),
			}
		}

		args := []string{"revert", "--"}
		args = append(args, paths...)

		out, err := svn(r, args...)
		output.WriteString(out)

		if err == nil {
			output.WriteString("\nRevert finished successfully.")
		}

		return commandResult{
			Output:          output.String(),
			Err:             err,
			CurrentLocation: getCurrentLocation(r),
		}
	}
}

func resolveConflictWithMeldCmd(r repo, path string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Conflicted path: " + path + "\n\n")

		infoOut, _ := svn(r, "info", path)

		if strings.Contains(strings.ToLower(infoOut), "tree conflict") {
			output.WriteString("This is a tree conflict, not a regular file merge conflict.\n")
			output.WriteString("Meld cannot resolve SVN tree conflicts automatically.\n\n")

			output.WriteString("What is a tree conflict?\n")
			output.WriteString("A tree conflict means SVN disagrees about the structure of the working copy, for example a file/folder was moved, deleted, replaced, or added differently between branches.\n\n")

			output.WriteString("Resolution strategy used by this TUI:\n")
			output.WriteString("Keeping the current working copy version.\n\n")

			output.WriteString("SVN info:\n")
			output.WriteString(infoOut)
			output.WriteString("\n\n")

			output.WriteString("Running: svn resolve --accept=working " + path + "\n\n")

			resolveOut, err := svn(
				r,
				"resolve",
				"--accept=working",
				path,
			)

			output.WriteString(resolveOut)

			if err != nil {
				return commandResult{
					Output:          output.String(),
					Err:             err,
					CurrentLocation: getCurrentLocation(r),
				}
			}

			output.WriteString("\nTree conflict marked as resolved.")
			output.WriteString("\nReview the directory before committing if needed.")

			return commandResult{
				Output:          output.String(),
				Err:             nil,
				CurrentLocation: getCurrentLocation(r),
			}
		}

		if _, err := exec.LookPath("meld"); err != nil {
			output.WriteString("Meld was not found in PATH.\n")
			output.WriteString("Install it first, e.g. on Arch:\n")
			output.WriteString("  sudo pacman -S meld\n")

			return commandResult{
				Output:          output.String(),
				Err:             err,
				CurrentLocation: getCurrentLocation(r),
			}
		}

		output.WriteString("Launching Meld through SVN resolve...\n\n")
		output.WriteString("Running: svn resolve --accept=launch --config-option=config:helpers:merge-tool-cmd=meld " + path + "\n\n")

		launchOut, err := svnInteractive(
			r,
			"resolve",
			"--accept=launch",
			"--config-option=config:helpers:merge-tool-cmd=meld",
			path,
		)

		output.WriteString(launchOut)

		if err != nil {
			infoOut, _ := svn(r, "info", path)

			output.WriteString("\nMeld/SVN launch failed.\n\n")
			output.WriteString("SVN info for conflicted path:\n")
			output.WriteString(infoOut)
			output.WriteString("\n\n")
			output.WriteString("If this is a tree conflict or directory conflict, use:\n")
			output.WriteString("  svn resolve --accept=working " + path + "\n")

			return commandResult{
				Output:          output.String(),
				Err:             err,
				CurrentLocation: getCurrentLocation(r),
			}
		}

		output.WriteString("\nMeld closed. Marking file as resolved using working copy version...\n\n")
		output.WriteString("Running: svn resolve --accept=working " + path + "\n\n")

		resolveOut, err := svn(
			r,
			"resolve",
			"--accept=working",
			path,
		)

		output.WriteString(resolveOut)

		if err != nil {
			return commandResult{
				Output:          output.String(),
				Err:             err,
				CurrentLocation: getCurrentLocation(r),
			}
		}

		output.WriteString("\nConflict resolved successfully.")
		output.WriteString("\nReview the file before committing if needed.")

		return commandResult{
			Output:          output.String(),
			Err:             nil,
			CurrentLocation: getCurrentLocation(r),
		}
	}
}

func searchFileHistoryMatchesCmd(r repo, query string) tea.Cmd {
	return func() tea.Msg {
		items, err := searchFileHistoryMatches(r, query, 300)
		return fileHistoryMatchesLoadedMsg{
			Query: query,
			Items: items,
			Err:   err,
		}
	}
}

func searchFileHistoryMatches(r repo, query string, limit int) ([]string, error) {
	query = strings.ToLower(strings.TrimSpace(filepath.ToSlash(query)))
	if query == "" {
		return nil, fmt.Errorf("empty file search query")
	}

	if limit <= 0 {
		limit = 300
	}

	var exact []string
	var contains []string

	err := filepath.WalkDir(r.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			if d.Name() == ".svn" || d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(r.Path, path)
		if err != nil {
			return nil
		}

		rel = filepath.ToSlash(rel)
		lower := strings.ToLower(rel)

		if lower == query || filepath.Base(lower) == query {
			exact = append(exact, rel)
			return nil
		}

		if strings.Contains(lower, query) {
			contains = append(contains, rel)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(exact)
	sort.Strings(contains)

	items := append(exact, contains...)
	if len(items) > limit {
		items = items[:limit]
	}

	return items, nil
}

func loadFileHistoryCmd(r repo, path string) tea.Cmd {
	return func() tea.Msg {
		out, err := svn(r, "log", "-l", "80", "--", path)

		if err != nil {
			return historyLoadedMsg{
				Output: out,
				Title:  "File history",
				Err: fmt.Errorf(
					"svn file log failed\n\nWorking copy: %s\nFile: %s\n\nOutput:\n%s\n\nError: %w",
					r.Path,
					path,
					out,
					err,
				),
			}
		}

		if strings.TrimSpace(out) == "" {
			out = "No file history found for: " + path
		} else {
			out = "File: " + path + "\n\n" + colorizeSVNLog(out)
		}

		return historyLoadedMsg{
			Output: out,
			Err:    nil,
			Title:  "File history",
		}
	}
}

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
			inEntry = false
			inChangedPaths = false
			inMessage = false
			continue
		}

		if strings.HasPrefix(line, "r") && strings.Contains(line, " | ") {
			b.WriteString(colorizeSVNLogMetaLine(line) + "\n")
			inEntry = true
			inChangedPaths = false
			inMessage = false
			continue
		}

		if inEntry && trimmed == "Changed paths:" {
			b.WriteString(mutedStyle.Render(line) + "\n")
			inChangedPaths = true
			inMessage = false
			continue
		}

		if inChangedPaths {
			if trimmed == "" {
				b.WriteString(line + "\n")
				inChangedPaths = false
				inMessage = true
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

	action := fields[0]
	rest := strings.TrimSpace(strings.TrimPrefix(trimmedLeft, action))
	return prefix + statusStyleForSVNPathAction(action).Render(action) + " " + valueWhiteStyle.Render(rest)
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

func colorizeSVNUpdateOutput(out string) string {
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			b.WriteString("\n")
			continue
		}

		if len(trimmed) >= 2 {
			status := strings.TrimSpace(trimmed[:1])
			path := strings.TrimSpace(trimmed[1:])
			if status == "A" || status == "D" || status == "U" || status == "G" || status == "C" || status == "E" {
				b.WriteString(statusStyleForSVNUpdateAction(status).Render(status) + " " + valueWhiteStyle.Render(path) + "\n")
				continue
			}
		}

		b.WriteString(mutedStyle.Render(line) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func statusStyleForSVNUpdateAction(action string) lipgloss.Style {
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

func colorizeUnifiedDiff(out string) string {
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
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

func loadHistoryCmd(r repo) tea.Cmd {
	return func() tea.Msg {
		out, err := svn(
			r,
			"log",
			"-v",
			"-l",
			"80",
		)

		if err != nil {
			return historyLoadedMsg{
				Output: out,
				Title:  "Commit history",
				Err: fmt.Errorf(
					"svn log failed\n\nWorking copy: %s\n\nOutput:\n%s\n\nError: %w",
					r.Path,
					out,
					err,
				),
			}
		}

		if strings.TrimSpace(out) == "" {
			out = "No commit history found."
		} else {
			out = colorizeSVNLog(out)
		}

		return historyLoadedMsg{
			Output: out,
			Err:    nil,
			Title:  "Commit history",
		}
	}
}

type svnLogXML struct {
	Entries []svnLogEntryXML `xml:"logentry"`
}

type svnLogEntryXML struct {
	Revision int             `xml:"revision,attr"`
	Author   string          `xml:"author"`
	Date     string          `xml:"date"`
	Msg      string          `xml:"msg"`
	Paths    []svnLogPathXML `xml:"paths>path"`
}

type svnLogPathXML struct {
	Action       string `xml:"action,attr"`
	CopyFromRev  int    `xml:"copyfrom-rev,attr"`
	CopyFromPath string `xml:"copyfrom-path,attr"`
	Path         string `xml:",chardata"`
}

func loadRevisionTreeCmd(r repo, full bool) tea.Cmd {
	return func() tea.Msg {
		target := strings.TrimSpace(r.Root)
		if target == "" {
			target = "."
		}

		args := []string{"log", "--xml", "-v"}
		if !full {
			args = append(args, "--limit", "250")
		}
		args = append(args, target)

		out, err := svn(r, args...)
		if err != nil {
			return historyLoadedMsg{
				Output: out,
				Title:  revisionTreeTitle(full),
				Err: fmt.Errorf(
					"svn revision tree log failed\n\nWorking copy: %s\nRepository root: %s\nMode: %s\n\nOutput:\n%s\n\nError: %w",
					r.Path,
					target,
					revisionTreeModeText(full),
					out,
					err,
				),
			}
		}

		tree, err := buildASCIIRevisionTree(out, r, full)
		if err != nil {
			return historyLoadedMsg{
				Output: out,
				Title:  revisionTreeTitle(full),
				Err:    err,
			}
		}

		return historyLoadedMsg{
			Output: tree,
			Err:    nil,
			Title:  revisionTreeTitle(full),
		}
	}
}

func revisionTreeTitle(full bool) string {
	if full {
		return "ASCII revision tree, full history"
	}
	return "ASCII revision tree, last 250 entries"
}

func revisionTreeModeText(full bool) string {
	if full {
		return "full history"
	}
	return "last 250 entries"
}

func buildASCIIRevisionTree(xmlOut string, r repo, full bool) (string, error) {
	var parsed svnLogXML
	if err := xml.Unmarshal([]byte(xmlOut), &parsed); err != nil {
		return "", fmt.Errorf("failed to parse svn log XML: %w", err)
	}

	if len(parsed.Entries) == 0 {
		return "No revision tree entries found.", nil
	}

	tree := buildRevisionBranchGraph(parsed.Entries)

	var b strings.Builder
	b.WriteString(labelMauveStyle.Render("Revision tree") + "\n")
	b.WriteString(labelYellowStyle.Render("Repository root: ") + valueWhiteStyle.Render(firstNonEmpty(r.Root, r.URL, r.Path)) + "\n")
	b.WriteString(labelYellowStyle.Render("Mode: ") + valueWhiteStyle.Render(revisionTreeModeText(full)) + "\n")
	if !full {
		b.WriteString(mutedStyle.Render("Showing newest 250 log entries. Press 'a' here to load full history.") + "\n")
	}
	b.WriteString(mutedStyle.Render("Branch/tag structure only. File-level changes are in Commit history.") + "\n")
	b.WriteString("\n")

	if len(tree.Nodes) == 0 {
		b.WriteString("No trunk/branch/tag activity found in the loaded log entries.")
		return strings.TrimRight(b.String(), "\n"), nil
	}

	roots := tree.Roots()
	for i, root := range roots {
		last := i == len(roots)-1
		renderRevisionTreeNode(&b, tree, root, "", last)
	}

	return strings.TrimRight(b.String(), "\n"), nil
}

type revisionBranchGraph struct {
	Nodes map[string]*revisionBranchNode
}

type revisionBranchNode struct {
	Path           string
	Name           string
	Kind           string
	CreatedRev     int
	CreatedDate    string
	CreatedFrom    string
	CreatedFromRev int
	DeletedRev     int
	LastRev        int
	CommitRevs     map[int]bool
	Commits        []revisionBranchCommit
	Children       []string
	MergeBacks     []revisionMergeBack
}

type revisionBranchCommit struct {
	Revision int
	Author   string
	Date     string
	Msg      string
}

type revisionMergeBack struct {
	Target string
	Rev    int
	Msg    string
}

func buildRevisionBranchGraph(entries []svnLogEntryXML) revisionBranchGraph {
	graph := revisionBranchGraph{Nodes: map[string]*revisionBranchNode{}}
	ensure := func(path string, kind string) *revisionBranchNode {
		path = normalizeSVNTreePath(path)
		if path == "" {
			path = "/trunk"
		}
		if node, ok := graph.Nodes[path]; ok {
			if node.Kind == "" && kind != "" {
				node.Kind = kind
			}
			return node
		}
		node := &revisionBranchNode{
			Path:       path,
			Name:       revisionTreeDisplayName(path),
			Kind:       kind,
			CommitRevs: map[int]bool{},
		}
		graph.Nodes[path] = node
		return node
	}

	ensure("/trunk", "trunk")

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Revision < entries[j].Revision
	})

	for _, entry := range entries {
		entryRoots := map[string]bool{}
		for _, changedPath := range entry.Paths {
			root, kind := revisionTreeRootPath(changedPath.Path)
			if root == "" {
				continue
			}

			node := ensure(root, kind)
			entryRoots[root] = true

			if entry.Revision > node.LastRev {
				node.LastRev = entry.Revision
			}

			action := strings.ToUpper(strings.TrimSpace(changedPath.Action))
			changedRootPath := normalizeSVNTreePath(changedPath.Path)
			if action == "A" && changedRootPath == root {
				if node.CreatedRev == 0 || entry.Revision < node.CreatedRev {
					node.CreatedRev = entry.Revision
					node.CreatedDate = formatSVNLogDate(entry.Date)
				}
				if strings.TrimSpace(changedPath.CopyFromPath) != "" {
					copyRoot, _ := revisionTreeRootPath(changedPath.CopyFromPath)
					if copyRoot == "" {
						copyRoot = normalizeSVNTreePath(changedPath.CopyFromPath)
					}
					node.CreatedFrom = copyRoot
					node.CreatedFromRev = changedPath.CopyFromRev
					ensure(copyRoot, svnTreePathKind(copyRoot))
				}
			}

			if action == "D" && changedRootPath == root {
				node.DeletedRev = entry.Revision
			}
		}

		for root := range entryRoots {
			node := ensure(root, svnTreePathKind(root))
			if !node.CommitRevs[entry.Revision] {
				node.CommitRevs[entry.Revision] = true
				node.Commits = append(node.Commits, revisionBranchCommit{
					Revision: entry.Revision,
					Author:   strings.TrimSpace(entry.Author),
					Date:     formatSVNLogDate(entry.Date),
					Msg:      compactOneLine(entry.Msg),
				})
			}
		}

		detectRevisionMergeBacks(graph, entry, entryRoots)
	}

	for path, node := range graph.Nodes {
		parent := strings.TrimSpace(node.CreatedFrom)
		if path == "/trunk" {
			continue
		}
		if parent == "" || parent == path {
			parent = "/trunk"
		}
		parentNode := ensure(parent, svnTreePathKind(parent))
		if !containsString(parentNode.Children, path) {
			parentNode.Children = append(parentNode.Children, path)
		}
	}

	for _, node := range graph.Nodes {
		sort.SliceStable(node.Children, func(i, j int) bool {
			left := graph.Nodes[node.Children[i]]
			right := graph.Nodes[node.Children[j]]
			if left == nil || right == nil {
				return node.Children[i] < node.Children[j]
			}
			if left.LastRev == right.LastRev {
				return left.Path < right.Path
			}
			return left.LastRev > right.LastRev
		})
		sort.SliceStable(node.MergeBacks, func(i, j int) bool {
			return node.MergeBacks[i].Rev > node.MergeBacks[j].Rev
		})
		sort.SliceStable(node.Commits, func(i, j int) bool {
			if node.Commits[i].Revision == node.Commits[j].Revision {
				return node.Commits[i].Msg < node.Commits[j].Msg
			}
			return node.Commits[i].Revision > node.Commits[j].Revision
		})
	}

	return graph
}

func detectRevisionMergeBacks(graph revisionBranchGraph, entry svnLogEntryXML, entryRoots map[string]bool) {
	msg := strings.ToLower(compactOneLine(entry.Msg))
	if msg == "" {
		return
	}

	mergeWords := []string{"merge", "merged", "mergel", "visszamerge", "vissza merge", "vissza lett mergelve"}
	hasMergeWord := false
	for _, word := range mergeWords {
		if strings.Contains(msg, word) {
			hasMergeWord = true
			break
		}
	}
	if !hasMergeWord {
		return
	}

	for sourcePath, sourceNode := range graph.Nodes {
		if sourcePath == "/trunk" || sourceNode == nil {
			continue
		}
		branchName := strings.ToLower(pathBaseName(sourcePath))
		if branchName == "" || !strings.Contains(msg, branchName) {
			continue
		}
		for target := range entryRoots {
			if target == sourcePath {
				continue
			}
			sourceNode.MergeBacks = append(sourceNode.MergeBacks, revisionMergeBack{
				Target: target,
				Rev:    entry.Revision,
				Msg:    compactOneLine(entry.Msg),
			})
		}
	}
}

func (g revisionBranchGraph) Roots() []string {
	if _, ok := g.Nodes["/trunk"]; ok {
		return []string{"/trunk"}
	}

	child := map[string]bool{}
	for _, node := range g.Nodes {
		for _, c := range node.Children {
			child[c] = true
		}
	}

	var roots []string
	for path := range g.Nodes {
		if !child[path] {
			roots = append(roots, path)
		}
	}
	sort.SliceStable(roots, func(i, j int) bool {
		left := g.Nodes[roots[i]]
		right := g.Nodes[roots[j]]
		if left == nil || right == nil {
			return roots[i] < roots[j]
		}
		if left.LastRev == right.LastRev {
			return left.Path < right.Path
		}
		return left.LastRev > right.LastRev
	})
	return roots
}

type revisionTreeRenderItem struct {
	Kind      string
	Revision  int
	SortLabel string
	Commit    revisionBranchCommit
	ChildPath string
	Merge     revisionMergeBack
}

func renderRevisionTreeNode(b *strings.Builder, graph revisionBranchGraph, path string, prefix string, last bool) {
	node := graph.Nodes[path]
	if node == nil {
		return
	}

	connector := "├─"
	childPrefix := prefix + labelMauveStyle.Render("│  ")
	plainChildPrefix := prefix + "│  "
	if last {
		connector = "└─"
		childPrefix = prefix + "   "
		plainChildPrefix = prefix + "   "
	}

	b.WriteString(prefix + labelMauveStyle.Render(connector) + " " + renderRevisionTreeNodeLabel(node) + "\n")

	items := revisionTreeRenderItems(graph, node)
	for i, item := range items {
		isLast := i == len(items)-1
		switch item.Kind {
		case "commit":
			renderRevisionTreeCommit(b, item.Commit, plainChildPrefix, isLast)
		case "child":
			renderRevisionTreeNode(b, graph, item.ChildPath, childPrefix, isLast)
		case "merge":
			renderRevisionTreeMergeBack(b, item.Merge, plainChildPrefix, isLast)
		}
	}
}

func revisionTreeRenderItems(graph revisionBranchGraph, node *revisionBranchNode) []revisionTreeRenderItem {
	items := make([]revisionTreeRenderItem, 0, len(node.Commits)+len(node.Children)+len(node.MergeBacks))

	for _, commit := range node.Commits {
		items = append(items, revisionTreeRenderItem{
			Kind:      "commit",
			Revision:  commit.Revision,
			SortLabel: commit.Msg,
			Commit:    commit,
		})
	}

	for _, childPath := range node.Children {
		child := graph.Nodes[childPath]
		if child == nil {
			continue
		}
		rev := child.LastRev
		if rev == 0 {
			rev = child.CreatedRev
		}
		items = append(items, revisionTreeRenderItem{
			Kind:      "child",
			Revision:  rev,
			SortLabel: child.Path,
			ChildPath: childPath,
		})
	}

	for _, merge := range node.MergeBacks {
		items = append(items, revisionTreeRenderItem{
			Kind:      "merge",
			Revision:  merge.Rev,
			SortLabel: merge.Target,
			Merge:     merge,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Revision == items[j].Revision {
			if items[i].Kind == items[j].Kind {
				return items[i].SortLabel < items[j].SortLabel
			}
			return revisionTreeItemPriority(items[i].Kind) < revisionTreeItemPriority(items[j].Kind)
		}
		return items[i].Revision > items[j].Revision
	})

	return items
}

func revisionTreeItemPriority(kind string) int {
	switch kind {
	case "merge":
		return 0
	case "child":
		return 1
	case "commit":
		return 2
	default:
		return 9
	}
}

func renderRevisionTreeMergeBack(b *strings.Builder, merge revisionMergeBack, prefix string, last bool) {
	mergeConnector := "├⤴"
	if last {
		mergeConnector = "└⤴"
	}
	line := prefix + labelMauveStyle.Render(mergeConnector) + " " + diffModifiedStyle.Render("merged back") + " " + labelMauveStyle.Render("→") + " " + valueWhiteStyle.Render(merge.Target)
	if merge.Rev > 0 {
		line += " " + successStyle.Render(fmt.Sprintf("r%d", merge.Rev))
	}
	if strings.TrimSpace(merge.Msg) != "" {
		line += " " + actionStyle.Render(compactOneLine(merge.Msg))
	}
	b.WriteString(line + "\n")
}

func renderRevisionTreeCommit(b *strings.Builder, commit revisionBranchCommit, prefix string, last bool) {
	connector := "├•"
	if last {
		connector = "└•"
	}

	line := prefix + labelMauveStyle.Render(connector) + " " + successStyle.Render(fmt.Sprintf("r%d", commit.Revision))
	if strings.TrimSpace(commit.Author) != "" {
		line += " " + labelMauveStyle.Render("|") + " " + labelYellowStyle.Render(commit.Author)
	}
	if strings.TrimSpace(commit.Date) != "" {
		line += " " + labelMauveStyle.Render("|") + " " + mutedStyle.Render(commit.Date)
	}
	if strings.TrimSpace(commit.Msg) != "" {
		line += " " + labelMauveStyle.Render("|") + " " + actionStyle.Render(commit.Msg)
	}
	b.WriteString(line + "\n")
}

func renderRevisionTreeNodeLabel(node *revisionBranchNode) string {
	label := valueWhiteStyle.Render(node.Path)
	if node.CreatedRev > 0 {
		created := fmt.Sprintf("created r%d", node.CreatedRev)
		if strings.TrimSpace(node.CreatedDate) != "" {
			created += " " + node.CreatedDate
		}
		label += " " + mutedStyle.Render(created)
	}
	if node.CreatedFrom != "" {
		label += " " + labelMauveStyle.Render("from") + " " + valueWhiteStyle.Render(node.CreatedFrom)
		if node.CreatedFromRev > 0 {
			label += " " + mutedStyle.Render(fmt.Sprintf("@r%d", node.CreatedFromRev))
		}
	}
	if node.LastRev > 0 {
		label += " " + mutedStyle.Render(fmt.Sprintf("last r%d", node.LastRev))
	}
	if node.DeletedRev > 0 {
		label += " " + errorStyle.Render(fmt.Sprintf("deleted r%d", node.DeletedRev))
	}
	return label
}

func revisionTreeDisplayName(path string) string {
	base := pathBaseName(path)
	if base == "" {
		return path
	}
	return base
}

func pathBaseName(path string) string {
	clean := strings.Trim(normalizeSVNTreePath(path), "/")
	if clean == "" {
		return ""
	}
	parts := strings.Split(clean, "/")
	return parts[len(parts)-1]
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

type revisionTreePathSummary struct {
	Action       string
	Path         string
	Kind         string
	CopyFromPath string
	CopyFromRev  int
}

func summarizeRevisionTreePaths(paths []svnLogPathXML) []revisionTreePathSummary {
	byKey := make(map[string]revisionTreePathSummary)

	for _, path := range paths {
		root, kind := revisionTreeRootPath(path.Path)
		if root == "" {
			continue
		}

		action := strings.TrimSpace(path.Action)
		if action == "" {
			action = "M"
		}

		summary := revisionTreePathSummary{
			Action: action,
			Path:   root,
			Kind:   kind,
		}

		if strings.TrimSpace(path.CopyFromPath) != "" {
			copyRoot, _ := revisionTreeRootPath(path.CopyFromPath)
			if copyRoot == "" {
				copyRoot = normalizeSVNTreePath(path.CopyFromPath)
			}
			summary.CopyFromPath = copyRoot
			summary.CopyFromRev = path.CopyFromRev
		}

		key := summary.Action + "|" + summary.Path + "|" + summary.CopyFromPath + fmt.Sprintf("|%d", summary.CopyFromRev)
		if existing, ok := byKey[key]; ok {
			if existing.Kind == "" {
				existing.Kind = summary.Kind
			}
			byKey[key] = existing
			continue
		}
		byKey[key] = summary
	}

	items := make([]revisionTreePathSummary, 0, len(byKey))
	for _, item := range byKey {
		items = append(items, item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Path == items[j].Path {
			return items[i].Action < items[j].Action
		}
		return items[i].Path < items[j].Path
	})

	return items
}

func revisionTreeRootPath(path string) (string, string) {
	clean := strings.Trim(normalizeSVNTreePath(path), "/")
	if clean == "" {
		return "", ""
	}

	parts := strings.Split(clean, "/")
	switch parts[0] {
	case "trunk":
		return "/trunk", "trunk"
	case "branches":
		if len(parts) >= 2 && parts[1] != "" {
			return "/branches/" + parts[1], "branch: " + parts[1]
		}
		return "/branches", "branches"
	case "tags":
		if len(parts) >= 2 && parts[1] != "" {
			return "/tags/" + parts[1], "tag: " + parts[1]
		}
		return "/tags", "tags"
	default:
		return "", ""
	}
}

func statusStyleForSVNPathAction(action string) lipgloss.Style {
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

func svnTreePathKind(path string) string {
	clean := strings.Trim(path, "/")
	if clean == "trunk" || strings.HasPrefix(clean, "trunk/") {
		return "trunk"
	}
	if clean == "branches" || strings.HasPrefix(clean, "branches/") {
		parts := strings.Split(clean, "/")
		if len(parts) >= 2 && parts[1] != "" {
			return "branch: " + parts[1]
		}
		return "branches"
	}
	if clean == "tags" || strings.HasPrefix(clean, "tags/") {
		parts := strings.Split(clean, "/")
		if len(parts) >= 2 && parts[1] != "" {
			return "tag: " + parts[1]
		}
		return "tags"
	}
	return ""
}

func normalizeSVNTreePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func compactOneLine(s string) string {
	fields := strings.Fields(strings.ReplaceAll(s, "\x00", " "))
	return strings.Join(fields, " ")
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "-"
}

func loadPartialHunksCmd(r repo, item commitItem) tea.Cmd {
	return func() tea.Msg {
		hunks, err := loadPartialHunks(r, item)
		return partialHunksLoadedMsg{
			Item:  item,
			Hunks: hunks,
			Err:   err,
		}
	}
}

func loadPartialHunks(r repo, item commitItem) ([]partialHunk, error) {
	if item.Unversioned || strings.HasPrefix(item.Status, "?") {
		return nil, fmt.Errorf("partial commit is not available for unversioned files")
	}

	if !strings.HasPrefix(item.Status, "M") {
		return nil, fmt.Errorf("partial commit currently supports modified versioned files only")
	}

	if isLikelyDirectory(r, item.Path) {
		return nil, fmt.Errorf("partial commit is only supported for files")
	}

	out, err := svn(r, "diff", item.Path)
	if err != nil {
		return nil, fmt.Errorf("svn diff failed for partial commit\n\nOutput:\n%s\n\nError: %w", out, err)
	}

	hunks, err := parseUnifiedDiffHunks(out)
	if err != nil {
		return nil, err
	}

	if len(hunks) == 0 {
		return nil, fmt.Errorf("no hunks found in svn diff output")
	}

	return hunks, nil
}

func partialHunkCommitCmd(r repo, item commitItem, hunks []partialHunk, message string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		fullPath := filepath.Join(r.Path, filepath.FromSlash(item.Path))

		info, err := os.Stat(fullPath)
		if err != nil {
			return commandResult{Output: output.String(), Err: err, CurrentLocation: getCurrentLocation(r)}
		}

		if info.IsDir() {
			return commandResult{Output: output.String(), Err: fmt.Errorf("partial commit is only supported for files"), CurrentLocation: getCurrentLocation(r)}
		}

		workingData, err := os.ReadFile(fullPath)
		if err != nil {
			return commandResult{Output: output.String(), Err: err, CurrentLocation: getCurrentLocation(r)}
		}

		baseText, err := readBaseFile(r, item.Path)
		if err != nil {
			return commandResult{Output: output.String(), Err: err, CurrentLocation: getCurrentLocation(r)}
		}

		baseLines := splitPatchLines(baseText)
		partialLines, err := applySelectedHunksToBase(baseLines, hunks)
		if err != nil {
			return commandResult{Output: output.String(), Err: err, CurrentLocation: getCurrentLocation(r)}
		}

		partialText := joinPatchLines(partialLines, hasFinalNewline(string(workingData), baseText))
		if partialText == baseText {
			return commandResult{Output: "Selected hunks do not change the file compared to SVN base.", Err: fmt.Errorf("partial commit produced no changes"), CurrentLocation: getCurrentLocation(r)}
		}

		backupDir, err := os.MkdirTemp("", "svn-tui-partial-hunk-backup-*")
		if err != nil {
			return commandResult{Output: output.String(), Err: err, CurrentLocation: getCurrentLocation(r)}
		}

		backupPath := filepath.Join(backupDir, filepath.Base(item.Path)+".working-backup")
		if err := os.WriteFile(backupPath, workingData, info.Mode().Perm()); err != nil {
			return commandResult{Output: output.String(), Err: err, CurrentLocation: getCurrentLocation(r)}
		}

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Partial commit file: " + item.Path + "\n")
		output.WriteString(fmt.Sprintf("Selected hunks: %d\n", len(hunks)))
		output.WriteString("Backup: " + backupPath + "\n\n")

		restored := false
		restore := func() {
			if restored {
				return
			}
			_ = os.WriteFile(fullPath, workingData, info.Mode().Perm())
			restored = true
		}
		defer restore()

		output.WriteString("Writing selected hunks into working copy temporarily...\n")
		if err := os.WriteFile(fullPath, []byte(partialText), info.Mode().Perm()); err != nil {
			return commandResult{Output: output.String(), Err: err, CurrentLocation: getCurrentLocation(r)}
		}

		output.WriteString("Running partial hunk commit...\n\n")
		out, err := svn(r, "commit", item.Path, "-m", message)
		output.WriteString(out)

		if err != nil {
			output.WriteString("\nCommit failed. Original working copy content was restored from memory. Backup kept at:\n  " + backupPath + "\n")
			return commandResult{Output: output.String(), Err: err, CurrentLocation: getCurrentLocation(r)}
		}

		restore()
		output.WriteString("\nPartial hunk commit finished successfully.")
		output.WriteString("\nOriginal full working copy content restored, so uncommitted changes remain local.")
		output.WriteString("\nBackup kept at:\n  " + backupPath + "\n")

		return commandResult{Output: output.String(), Err: nil, CurrentLocation: getCurrentLocation(r)}
	}
}

func buildSideBySideDiff(r repo, item commitItem, width int) (string, error) {
	if width <= 0 {
		width = 160
	}

	width = max(80, width)
	separatorWidth := lipgloss.Width(" │ Δ │ ")
	leftWidth := max(24, (width-separatorWidth)/2)
	rightWidth := max(24, width-separatorWidth-leftWidth)

	if isLikelyDirectory(r, item.Path) {
		out, err := svn(r, "diff", item.Path)
		if err != nil {
			return "", err
		}

		return "Directory diff uses unified SVN diff output:\n\n" + colorizeUnifiedDiff(out), nil
	}

	var oldText string
	var newText string
	var err error

	if item.Unversioned || strings.HasPrefix(item.Status, "?") || strings.HasPrefix(item.Status, "A") {
		oldText = ""
		newText, err = readWorkingFile(r, item.Path)
		if err != nil {
			return "", err
		}
	} else if strings.HasPrefix(item.Status, "D") {
		oldText, err = readBaseFile(r, item.Path)
		if err != nil {
			return "", err
		}
		newText = ""
	} else {
		oldText, err = readBaseFile(r, item.Path)
		if err != nil {
			return "", err
		}

		newText, err = readWorkingFile(r, item.Path)
		if err != nil {
			return "", err
		}
	}

	oldLines := splitLinesForDiff(oldText)
	newLines := splitLinesForDiff(newText)

	var b strings.Builder

	b.WriteString("Path: " + item.Path + "\n")
	b.WriteString("Status: " + item.Status + "\n")
	if item.Unversioned {
		b.WriteString("Note: unversioned file, left side is empty. It will be svn add-ed before commit if selected.\n")
	}
	b.WriteString("\n")
	b.WriteString(renderDiffHeader(leftWidth, rightWidth))

	rows := sideBySideRows(oldLines, newLines)

	if len(rows) == 0 {
		b.WriteString(renderDiffLine("", "", "=", leftWidth, rightWidth) + "\n")
		return b.String(), nil
	}

	for _, row := range rows {
		leftWrapped := wrapVisualLine(row.Left, leftWidth)
		rightWrapped := wrapVisualLine(row.Right, rightWidth)

		maxParts := max(len(leftWrapped), len(rightWrapped))
		for i := 0; i < maxParts; i++ {
			left := ""
			right := ""

			if i < len(leftWrapped) {
				left = leftWrapped[i]
			}
			if i < len(rightWrapped) {
				right = rightWrapped[i]
			}

			marker := row.Marker
			if i > 0 {
				marker = " "
			}

			b.WriteString(renderDiffLine(left, right, marker, leftWidth, rightWidth) + "\n")
		}
	}

	return b.String(), nil
}

type diffRow struct {
	Left   string
	Right  string
	Marker string
}

func sideBySideRows(oldLines []string, newLines []string) []diffRow {
	n := len(oldLines)
	m := len(newLines)

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

	var rows []diffRow

	i := 0
	j := 0

	for i < n && j < m {
		if oldLines[i] == newLines[j] {
			rows = append(rows, diffRow{
				Left:   oldLines[i],
				Right:  newLines[j],
				Marker: "=",
			})
			i++
			j++
			continue
		}

		if i+1 < n && oldLines[i+1] == newLines[j] {
			rows = append(rows, diffRow{
				Left:   oldLines[i],
				Right:  "",
				Marker: "-",
			})
			i++
			continue
		}

		if j+1 < m && oldLines[i] == newLines[j+1] {
			rows = append(rows, diffRow{
				Left:   "",
				Right:  newLines[j],
				Marker: "+",
			})
			j++
			continue
		}

		if dp[i+1][j] > dp[i][j+1] {
			rows = append(rows, diffRow{
				Left:   oldLines[i],
				Right:  "",
				Marker: "-",
			})
			i++
		} else if dp[i][j+1] > dp[i+1][j] {
			rows = append(rows, diffRow{
				Left:   "",
				Right:  newLines[j],
				Marker: "+",
			})
			j++
		} else {
			rows = append(rows, diffRow{
				Left:   oldLines[i],
				Right:  newLines[j],
				Marker: "~",
			})
			i++
			j++
		}
	}

	for i < n {
		rows = append(rows, diffRow{
			Left:   oldLines[i],
			Right:  "",
			Marker: "-",
		})
		i++
	}

	for j < m {
		rows = append(rows, diffRow{
			Left:   "",
			Right:  newLines[j],
			Marker: "+",
		})
		j++
	}

	return compactUnchangedRows(rows, 4)
}

func compactUnchangedRows(rows []diffRow, context int) []diffRow {
	if len(rows) == 0 {
		return rows
	}

	changed := make([]bool, len(rows))
	for i, row := range rows {
		if row.Marker != "=" {
			from := max(0, i-context)
			to := min(len(rows)-1, i+context)

			for j := from; j <= to; j++ {
				changed[j] = true
			}
		}
	}

	hasChanges := false
	for _, row := range rows {
		if row.Marker != "=" {
			hasChanges = true
			break
		}
	}

	if !hasChanges {
		return rows
	}

	var out []diffRow
	hidden := false

	for i, row := range rows {
		if changed[i] {
			if hidden {
				out = append(out, diffRow{
					Left:   "...",
					Right:  "...",
					Marker: " ",
				})
				hidden = false
			}

			out = append(out, row)
		} else {
			hidden = true
		}
	}

	if hidden {
		out = append(out, diffRow{
			Left:   "...",
			Right:  "...",
			Marker: " ",
		})
	}

	return out
}

func readBaseFile(r repo, path string) (string, error) {
	out, err := svn(r, "cat", path)
	if err != nil {
		return "", err
	}

	return out, nil
}

func readWorkingFile(r repo, path string) (string, error) {
	fullPath := filepath.Join(r.Path, filepath.FromSlash(path))

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func isLikelyDirectory(r repo, path string) bool {
	fullPath := filepath.Join(r.Path, filepath.FromSlash(path))
	info, err := os.Stat(fullPath)
	if err != nil {
		return false
	}

	return info.IsDir()
}

func splitLinesForDiff(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	if text == "" {
		return nil
	}

	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

func renderDiffHeader(leftWidth int, rightWidth int) string {
	var b strings.Builder
	b.WriteString(visiblePadRight("OLD / BASE", leftWidth) + " │ Δ │ " + visiblePadRight("NEW / WORKING COPY", rightWidth) + "\n")
	b.WriteString(strings.Repeat("─", leftWidth) + "─┼───┼─" + strings.Repeat("─", rightWidth) + "\n")
	return b.String()
}

func renderDiffLine(left string, right string, marker string, leftWidth int, rightWidth int) string {
	left = visiblePadRight(left, leftWidth)
	right = visiblePadRight(right, rightWidth)
	marker = strings.TrimSpace(marker)
	if marker == "" {
		marker = " "
	}

	style := diffStyleForMarker(marker)
	switch marker {
	case "+":
		right = style.Render(right)
	case "-":
		left = style.Render(left)
	case "~":
		left = style.Render(left)
		right = style.Render(right)
	case "=":
		left = diffSameStyle.Render(left)
		right = diffSameStyle.Render(right)
	}

	return left + " │ " + style.Render(marker) + " │ " + right
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

func wrapVisualLine(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}

	s = expandTabs(s, 4)
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

	parts = append(parts, current.String())
	return parts
}

func visiblePadRight(s string, width int) string {
	s = expandTabs(s, 4)
	for lipgloss.Width(s) > width {
		runes := []rune(s)
		if len(runes) == 0 {
			return strings.Repeat(" ", width)
		}
		s = string(runes[:len(runes)-1])
	}

	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

func expandTabs(s string, tabWidth int) string {
	if tabWidth <= 0 || !strings.Contains(s, "	") {
		return s
	}

	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '	' {
			spaces := tabWidth - (col % tabWidth)
			if spaces == 0 {
				spaces = tabWidth
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

var unifiedHunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func parseUnifiedDiffHunks(diffText string) ([]partialHunk, error) {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(diffText, "\r\n", "\n"), "\r", "\n"), "\n")

	var hunks []partialHunk
	var current *partialHunk

	flush := func() {
		if current == nil {
			return
		}

		current.PreviewText = buildHunkPreview(*current, 4)
		hunks = append(hunks, *current)
		current = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "@@ ") {
			flush()

			oldStart, oldCount, newStart, newCount, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}

			current = &partialHunk{
				Header:   line,
				OldStart: oldStart,
				OldCount: oldCount,
				NewStart: newStart,
				NewCount: newCount,
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

func parseHunkHeader(header string) (int, int, int, int, error) {
	m := unifiedHunkHeaderRe.FindStringSubmatch(header)
	if len(m) == 0 {
		return 0, 0, 0, 0, fmt.Errorf("invalid hunk header: %s", header)
	}

	oldStart := atoiDefault(m[1], 0)
	oldCount := atoiDefault(m[2], 1)
	newStart := atoiDefault(m[3], 0)
	newCount := atoiDefault(m[4], 1)

	return oldStart, oldCount, newStart, newCount, nil
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}

	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return def
	}

	return n
}

func buildHunkPreview(h partialHunk, maxLines int) string {
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

func selectedPartialHunks(hunks []partialHunk) []partialHunk {
	var selected []partialHunk
	for _, h := range hunks {
		if h.Selected {
			selected = append(selected, h)
		}
	}
	return selected
}

func splitPatchLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	if text == "" {
		return nil
	}

	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

func joinPatchLines(lines []string, finalNewline bool) string {
	out := strings.Join(lines, "\n")
	if finalNewline {
		out += "\n"
	}
	return out
}

func hasFinalNewline(values ...string) bool {
	for _, v := range values {
		if strings.HasSuffix(v, "\n") {
			return true
		}
	}
	return false
}

func applySelectedHunksToBase(baseLines []string, hunks []partialHunk) ([]string, error) {
	result := append([]string(nil), baseLines...)

	sort.Slice(hunks, func(i, j int) bool {
		return hunks[i].OldStart > hunks[j].OldStart
	})

	for _, h := range hunks {
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
			case '-':
				// Removed lines do not go into the selected target content.
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

func selectedCommitItems(items []commitItem) []commitItem {
	var selected []commitItem

	for _, item := range items {
		if item.Selected {
			selected = append(selected, item)
		}
	}

	return selected
}

func selectedCommitPaths(items []commitItem) []string {
	var paths []string
	seen := map[string]bool{}

	for _, item := range items {
		path := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(item.Path)), "./")
		if item.Selected && isSafeSVNWorkingCopyPath(path) && !seen[path] {
			paths = append(paths, path)
			seen[path] = true
		}
	}

	return paths
}

func selectedUnversionedCommitPaths(items []commitItem) []string {
	var paths []string

	for _, item := range items {
		if item.Selected && item.Unversioned {
			paths = append(paths, item.Path)
		}
	}

	return paths
}

func commitItemPaths(items []commitItem) []string {
	var paths []string

	for _, item := range items {
		paths = append(paths, item.Path)
	}

	return paths
}

func unversionedItemPaths(items []commitItem) []string {
	var paths []string

	for _, item := range items {
		if item.Unversioned {
			paths = append(paths, item.Path)
		}
	}

	return paths
}

func cleanSVNPathList(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	seen := map[string]bool{}

	for _, path := range paths {
		path = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(path)), "./")
		if !isSafeSVNWorkingCopyPath(path) || seen[path] {
			continue
		}
		cleaned = append(cleaned, path)
		seen[path] = true
	}

	return cleaned
}

func buildBranchName(r repo, parameter string) (string, error) {
	username := strings.TrimSpace(r.BranchUsername)

	if username == "" {
		username = strings.TrimSpace(r.Username)
	}

	if username == "" {
		authURL, err := authURLFromRepoRoot(r.Root)
		if err != nil {
			return "", err
		}

		out, err := svn(r, "auth", authURL)
		if err != nil {
			return "", err
		}

		username = parseSVNUsername(out)
	}

	if username == "" {
		return "", fmt.Errorf("could not determine SVN username")
	}

	if at := strings.Index(username, "@"); at >= 0 {
		username = username[:at]
	}

	date := time.Now().Format("2006-01-02")

	return fmt.Sprintf("%s_%s_%s", date, username, parameter), nil
}

func authURLFromRepoRoot(repoRoot string) (string, error) {
	parsed, err := url.Parse(repoRoot)
	if err != nil {
		return "", err
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid repository root URL: %s", repoRoot)
	}

	return parsed.Scheme + "://" + parsed.Host, nil
}

func parseSVNUsername(authOutput string) string {
	lines := strings.Split(authOutput, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)

		if strings.HasPrefix(lower, "username:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}

			username := strings.TrimSpace(parts[1])

			if at := strings.Index(username, "@"); at >= 0 {
				username = username[:at]
			}

			return username
		}
	}

	return ""
}

func svn(r repo, args ...string) (string, error) {
	finalArgs := svnBaseArgs(r, true)
	finalArgs = append(finalArgs, args...)

	cmd := exec.Command("svn", finalArgs...)
	cmd.Dir = r.Path

	out, err := cmd.CombinedOutput()

	return string(out), err
}

func svnInteractive(r repo, args ...string) (string, error) {
	finalArgs := svnBaseArgs(r, false)
	finalArgs = append(finalArgs, args...)

	cmd := exec.Command("svn", finalArgs...)
	cmd.Dir = r.Path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()

	if err != nil {
		return "", err
	}

	return "", nil
}

func svnBaseArgs(r repo, nonInteractive bool) []string {
	finalArgs := []string{}

	if r.Username != "" {
		finalArgs = append(finalArgs, "--username", r.Username)
	}

	if r.Password != "" {
		finalArgs = append(finalArgs, "--password", r.Password)

		if nonInteractive {
			finalArgs = append(finalArgs, "--non-interactive")
		}

		finalArgs = append(finalArgs, "--no-auth-cache")
	}

	return finalArgs
}

func getCurrentLocation(r repo) string {
	out, err := svn(r, "info", "--show-item", "relative-url")
	if err != nil {
		return "unknown"
	}

	location := strings.TrimSpace(out)
	location = strings.TrimPrefix(location, "^")

	if location == "" {
		return "unknown"
	}

	return location
}

func repoInfoHeader(title string, r repo) string {
	var b strings.Builder

	if r.Path != "" {
		b.WriteString(labelMauveStyle.Render("Repository: "))
		b.WriteString(valueWhiteStyle.Render(r.Path))
		b.WriteString("\n")
	}

	location := strings.TrimSpace(r.CurrentLocation)
	if location == "" {
		location = getCurrentLocation(r)
	}

	b.WriteString(labelYellowStyle.Render("Current: "))
	b.WriteString(valueWhiteStyle.Render(location))
	b.WriteString("\n")

	if r.URL != "" {
		b.WriteString(labelYellowStyle.Render("URL: "))
		b.WriteString(valueWhiteStyle.Render(r.URL))
		b.WriteString("\n")
	}

	if r.Root != "" {
		b.WriteString(labelYellowStyle.Render("Root: "))
		b.WriteString(valueWhiteStyle.Render(r.Root))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	return b.String()
}

func (m model) visibleListCount(reservedLines int) int {
	if m.height <= 0 {
		return 12
	}

	count := m.height - reservedLines
	if count < 5 {
		return 5
	}

	return count
}

func adjustOffset(offset int, cursor int, visible int) int {
	if cursor < offset {
		return cursor
	}

	if cursor >= offset+visible {
		return cursor - visible + 1
	}

	return offset
}

func scrollHint(offset int, end int, total int) string {
	if total <= 0 {
		return ""
	}

	return fmt.Sprintf("Showing %d-%d of %d", offset+1, end, total)
}

func min(a int, b int) int {
	if a < b {
		return a
	}

	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}

	return b
}
