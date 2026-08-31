package model

const ShelvesDir = ".svn-tui-shelves"

// ── Screens ──────────────────────────────────────────────────────────────────

type Screen int

const (
	ScreenRepoSelect Screen = iota
	ScreenActionSelect
	ScreenCreateBranchInput
	ScreenCheckoutRevisionInput
	ScreenBranchSelect
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
	ActionShelveChanges
	ActionUnshelveChanges
	ActionSwitchTrunk
	ActionCheckoutRevision
	ActionResolveConflicts
	ActionCleanup
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

// ── Commit items ──────────────────────────────────────────────────────────────

type CommitItem struct {
	Status      string
	Path        string
	Selected    bool
	Unversioned bool
	IsDir       bool
}

type ConflictItem struct {
	Status string
	Path   string
	IsTree bool
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
	Err   error
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
