package model

import "strings"

const ShelvesDir = ".svn-tui-shelves"

// ── Screens ──────────────────────────────────────────────────────────────────

type Screen int

const (
	ScreenRepoSelect Screen = iota
	ScreenActionSelect
	ScreenCreateBranchInput
	ScreenCheckoutRevisionInput
	ScreenBranchSelect
	ScreenBranchDiffSelect
	ScreenDeleteBranchConfirm
	ScreenShelfSelect
	ScreenShelveSelect
	ScreenPullSelect
	ScreenCommitSelect
	ScreenCommitMessageInput
	ScreenPartialHunkSelect
	ScreenRevertSelect
	ScreenConflictSelect
	ScreenFileHistorySearch
	ScreenFileHistorySelect
	ScreenPropertyTargetInput
	ScreenPropertyTargetSelect
	ScreenPropertyList
	ScreenPropertyNameInput
	ScreenPropertyValueInput
	ScreenHistory
	ScreenHistorySearch
	ScreenDiff
	ScreenRunning
	ScreenResult
)

// ── Actions ───────────────────────────────────────────────────────────────────

type Action int

const (
	ActionPull Action = iota
	ActionStatus
	ActionRevertFiles
	ActionCommit
	ActionCreateBranch
	ActionSwitchBranch
	ActionMergeBranch
	ActionBranchDiffFromStart
	ActionBranchDiffVsTrunk
	ActionDeleteBranch
	ActionShelveChanges
	ActionUnshelveChanges
	ActionSwitchTrunk
	ActionCheckoutRevision
	ActionResolveConflicts
	ActionCleanup
	ActionProperties
	ActionCommitHistory
	ActionFileHistory
	ActionRevisionTree
	ActionQuit
)

// ── Repo ──────────────────────────────────────────────────────────────────────

type RepoConfig struct {
	Path           string
	Username       string
	Password       string
	BranchUsername string
}

type Repo struct {
	Path            string
	URL             string
	Root            string
	Username        string
	Password        string
	BranchUsername  string
	CurrentLocation string
	CurrentRevision string
}

type Branch struct {
	Name     string
	Revision int
}

// BranchDeleteInfo is what the delete confirmation shows about a branch, so the
// deletion can be verified before it is typed out.
type BranchDeleteInfo struct {
	Name       string
	URL        string
	LastRev    int
	Author     string
	Date       string
	Msg        string
	IsCheckout bool
}

type BranchDeleteInfoLoadedMsg struct {
	Info   BranchDeleteInfo
	Output string
	Err    error
}

// ── Branch diff ───────────────────────────────────────────────────────────────

// BranchDiffMode selects which two repository states a branch diff compares.
type BranchDiffMode int

const (
	// BranchDiffSinceBranchPoint compares the branch as it was created (the
	// trunk state at that moment) with everything committed on it since.
	BranchDiffSinceBranchPoint BranchDiffMode = iota
	// BranchDiffAgainstTrunkHead compares today's trunk with the branch head.
	BranchDiffAgainstTrunkHead
)

// BranchDiffSide is one side of a branch comparison: a URL pinned to a
// revision, plus the label shown above its column.
type BranchDiffSide struct {
	URL   string
	Rev   string
	Label string
}

// Target renders the side as an SVN peg-revision target, e.g. URL@1234.
func (s BranchDiffSide) Target() string {
	if strings.TrimSpace(s.Rev) == "" {
		return s.URL
	}
	return s.URL + "@" + s.Rev
}

// PathTarget renders a path below the side as a peg-revision target.
func (s BranchDiffSide) PathTarget(relPath string) string {
	url := s.URL
	if relPath = strings.Trim(relPath, "/"); relPath != "" {
		url += "/" + relPath
	}
	if strings.TrimSpace(s.Rev) == "" {
		return url
	}
	return url + "@" + s.Rev
}

type BranchDiffContext struct {
	Mode    BranchDiffMode
	Branch  string
	Old     BranchDiffSide
	New     BranchDiffSide
	Summary string
}

func (c BranchDiffContext) Title() string {
	if c.Mode == BranchDiffAgainstTrunkHead {
		return "Branch diff vs trunk HEAD"
	}
	return "Branch diff since branch point"
}

type BranchDiffItem struct {
	// Status is the summarize letter: M, A, D or R, with a trailing P when
	// only properties changed alongside.
	Status string
	// Path is relative to both compared roots.
	Path  string
	IsDir bool
}

type BranchDiffLoadedMsg struct {
	Context BranchDiffContext
	Items   []BranchDiffItem
	Output  string
	Err     error
}

// ── Commit items ──────────────────────────────────────────────────────────────

type CommitItem struct {
	Status       string
	Path         string
	Selected     bool
	Unversioned  bool
	PropsChanged bool
	Conflicted   bool
	IsDir        bool
}

type ConflictItem struct {
	Status string
	Path   string
	IsTree bool
}

// ── Properties ────────────────────────────────────────────────────────────────

type PropertyItem struct {
	Name  string
	Value string
}

// SVNDiffSummarizeXML mirrors "svn diff --summarize --xml".
type SVNDiffSummarizeXML struct {
	Paths []SVNDiffPathXML `xml:"paths>path"`
}

type SVNDiffPathXML struct {
	Item  string `xml:"item,attr"`
	Props string `xml:"props,attr"`
	Kind  string `xml:"kind,attr"`
	Path  string `xml:",chardata"`
}

// SVNPropListXML mirrors "svn proplist -v --xml".
type SVNPropListXML struct {
	Targets []SVNPropTargetXML `xml:"target"`
}

type SVNPropTargetXML struct {
	Path       string           `xml:"path,attr"`
	Properties []SVNPropertyXML `xml:"property"`
}

type SVNPropertyXML struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

// ── Command results ───────────────────────────────────────────────────────────

type CommandResult struct {
	Output          string
	Err             error
	CurrentLocation string
	URL             string
	CurrentRevision string
}

type SVNStreamItem struct {
	Line   string
	Done   bool
	Result CommandResult
}

// ── BubbleTea messages ────────────────────────────────────────────────────────

type StreamOutputMsg struct {
	Line string
	Ch   <-chan SVNStreamItem
}

type BranchesLoadedMsg struct {
	Branches []Branch
	Err      error
}

type ShelvesLoadedMsg struct {
	Shelves []string
	Err     error
	Output  string
}

type PullItemsLoadedMsg struct {
	Items []CommitItem
	Err   error
}

type CommitItemsLoadedMsg struct {
	Items []CommitItem
	// Conflicted lists paths left out because SVN refuses to commit a path that
	// is still in conflict (E155015).
	Conflicted []string
	Err        error
}

type ConflictItemsLoadedMsg struct {
	Items []ConflictItem
	Err   error
}

type HistoryLoadedMsg struct {
	Output string
	Err    error
	Title  string
}

type FileHistoryMatchesLoadedMsg struct {
	Query string
	Items []string
	Err   error
}

type RevertItemsLoadedMsg struct {
	Items []CommitItem
	Err   error
}

type PropertyTargetsLoadedMsg struct {
	Query string
	Items []string
	Err   error
}

type PropertiesLoadedMsg struct {
	Target string
	Items  []PropertyItem
	Err    error
	Notice string
}

type DiffLoadedMsg struct {
	Output string
	Err    error
	Path   string
}

type PartialHunksLoadedMsg struct {
	Item  CommitItem
	Hunks []PartialHunk
	Err   error
}

// ── Partial hunks ─────────────────────────────────────────────────────────────

type PartialHunk struct {
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

// ── Diff rows ─────────────────────────────────────────────────────────────────

type DiffRow struct {
	Left      string
	Right     string
	Marker    string
	LeftNum   int
	RightNum  int
	LeftCRLF  bool // original line was \r\n terminated
	RightCRLF bool
}

// ── Shelves ───────────────────────────────────────────────────────────────────

type ShelfManifest struct {
	Name             string   `json:"name"`
	CreatedAt        string   `json:"created_at"`
	WorkingCopy      string   `json:"working_copy"`
	URL              string   `json:"url"`
	CurrentLocation  string   `json:"current_location"`
	VersionedPaths   []string `json:"versioned_paths"`
	UnversionedPaths []string `json:"unversioned_paths"`
}

// ── SVN log XML ───────────────────────────────────────────────────────────────

type SVNLogXML struct {
	Entries []SVNLogEntryXML `xml:"logentry"`
}

type SVNLogEntryXML struct {
	Revision int             `xml:"revision,attr"`
	Author   string          `xml:"author"`
	Date     string          `xml:"date"`
	Msg      string          `xml:"msg"`
	Paths    []SVNLogPathXML `xml:"paths>path"`
}

type SVNLogPathXML struct {
	Action       string `xml:"action,attr"`
	CopyFromRev  int    `xml:"copyfrom-rev,attr"`
	CopyFromPath string `xml:"copyfrom-path,attr"`
	Path         string `xml:",chardata"`
}

// ── Revision tree ─────────────────────────────────────────────────────────────

type RevisionBranchGraph struct {
	Nodes map[string]*RevisionBranchNode
}

type RevisionBranchNode struct {
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
	Commits        []RevisionBranchCommit
	Children       []string
	MergeBacks     []RevisionMergeBack
}

type RevisionBranchCommit struct {
	Revision int
	Author   string
	Date     string
	Msg      string
}

type RevisionMergeBack struct {
	Target string
	Rev    int
	Msg    string
}

type RevisionTreeRenderItem struct {
	Kind      string
	Revision  int
	SortLabel string
	Commit    RevisionBranchCommit
	ChildPath string
	Merge     RevisionMergeBack
}

func (g RevisionBranchGraph) Roots() []string {
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
	return roots
}
