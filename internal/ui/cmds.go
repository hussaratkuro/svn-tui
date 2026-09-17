package ui

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"svn-tui/internal/diff"
	"svn-tui/internal/model"
	"svn-tui/internal/svn"
)

// ── Streaming infrastructure ──────────────────────────────────────────────────

func startStreamingCommand(run func(emit func(string)) model.CommandResult) tea.Cmd {
	ch := make(chan model.SVNStreamItem, 512)
	go func() {
		result := run(func(line string) {
			ch <- model.SVNStreamItem{Line: line}
		})
		ch <- model.SVNStreamItem{Done: true, Result: result}
	}()
	return readNextSVNStream(ch)
}

func readNextSVNStream(ch <-chan model.SVNStreamItem) tea.Cmd {
	return func() tea.Msg {
		item := <-ch
		if item.Done {
			return item.Result
		}
		return model.StreamOutputMsg{Line: item.Line, Ch: ch}
	}
}

// ── Branch commands ───────────────────────────────────────────────────────────

func loadBranchesCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		branches, err := loadBranches(r)
		return model.BranchesLoadedMsg{Branches: branches, Err: err}
	}
}

func loadBranches(r model.Repo) ([]model.Branch, error) {
	branchesURL := r.Root + "/branches"
	out, err := svn.Run(r, "list", "-v", branchesURL)
	if err != nil {
		return nil, fmt.Errorf("svn list failed\n\nWorking copy: %s\nBranches URL: %s\n\nOutput:\n%s\n\nError: %w", r.Path, branchesURL, out, err)
	}

	var branches []model.Branch
	for _, line := range strings.Split(out, "\n") {
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
		name := strings.TrimSuffix(fields[len(fields)-1], "/")
		if name == "." || name == "" {
			continue
		}
		branches = append(branches, model.Branch{Name: name, Revision: revision})
	}

	sort.Slice(branches, func(i, j int) bool {
		if branches[i].Revision == branches[j].Revision {
			return branches[i].Name > branches[j].Name
		}
		return branches[i].Revision > branches[j].Revision
	})
	return branches, nil
}

func createBranchCmd(r model.Repo, parameter string) tea.Cmd {
	return startStreamingCommand(func(emit func(string)) model.CommandResult {
		branchName, err := svn.BuildBranchName(r, parameter)
		if err != nil {
			return model.CommandResult{Output: "Failed to build branch name.\n\n" + err.Error(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}

		trunkURL := r.Root + "/trunk"
		branchURL := r.Root + "/branches/" + branchName

		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n"); emit(s) }

		line("Working copy: " + r.Path)
		line("Repository root: " + r.Root)
		line("Branch name: " + branchName)
		line("")

		err = svn.StreamLines(r, func(raw string) { output.WriteString(raw + "\n"); emit(raw) },
			"copy", trunkURL, branchURL, "-m", "Creating branch "+branchName)
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}

		line("")
		line("Switching to created branch...")
		line("")

		err = svn.StreamLines(r, func(raw string) {
			colored := colorizeSVNUpdateLine(raw)
			output.WriteString(colored + "\n")
			emit(colored)
		}, "switch", "--ignore-ancestry", branchURL)
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}

		line("")
		line("Branch " + branchName + " created and switched to successfully.")
		return model.CommandResult{Output: output.String(), CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
	})
}

func switchBranchCmd(r model.Repo, branchName string) tea.Cmd {
	return startStreamingCommand(func(emit func(string)) model.CommandResult {
		branchURL := r.Root + "/branches/" + branchName
		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n"); emit(s) }

		line("Working copy: " + r.Path)
		line("Switching to branch: " + branchName)
		line("Target URL: " + branchURL)
		line("")

		err := svn.StreamLines(r, func(raw string) {
			colored := colorizeSVNUpdateLine(raw)
			output.WriteString(colored + "\n")
			emit(colored)
		}, "switch", branchURL)
		if err == nil {
			line("")
			line("Switched to branch " + branchName + " successfully.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
	})
}

func switchTrunkCmd(r model.Repo) tea.Cmd {
	return startStreamingCommand(func(emit func(string)) model.CommandResult {
		trunkURL := r.Root + "/trunk"
		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n"); emit(s) }

		line("Working copy: " + r.Path)
		line("Current URL: " + strings.TrimSpace(svn.GetCurrentURL(r)))
		line("Switching to trunk")
		line("Target URL: " + trunkURL)
		line("")

		err := svn.StreamLines(r, func(raw string) {
			colored := colorizeSVNUpdateLine(raw)
			output.WriteString(colored + "\n")
			emit(colored)
		}, "switch", trunkURL)
		if err == nil {
			line("")
			line("Switched back to trunk successfully.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
	})
}

// branchDeleteConfirmWord has to be typed out before a branch is deleted, so a
// stray Enter on the branch list can never remove one.
const branchDeleteConfirmWord = "delete"

// loadBranchDeleteInfoCmd collects what the confirmation screen shows about the
// branch: its URL and its last commit.
func loadBranchDeleteInfoCmd(r model.Repo, branchName string) tea.Cmd {
	return func() tea.Msg {
		branchURL := r.Root + "/branches/" + branchName
		info := model.BranchDeleteInfo{
			Name:       branchName,
			URL:        branchURL,
			IsCheckout: isCheckedOutBranch(r, branchName),
		}

		out, err := svn.Run(r, "log", "--xml", "-l", "1", branchURL)
		if err != nil {
			return model.BranchDeleteInfoLoadedMsg{Info: info, Output: out, Err: fmt.Errorf("svn log failed\n\nBranch URL: %s\n\nOutput:\n%s\n\nError: %w", branchURL, out, err)}
		}
		var parsed model.SVNLogXML
		if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
			return model.BranchDeleteInfoLoadedMsg{Info: info, Output: out, Err: fmt.Errorf("could not parse svn log output\n\nOutput:\n%s\n\nError: %w", out, err)}
		}
		if len(parsed.Entries) > 0 {
			last := parsed.Entries[0]
			info.LastRev = last.Revision
			info.Author = last.Author
			info.Date = formatSVNLogDate(last.Date)
			info.Msg = compactOneLine(last.Msg)
		}
		return model.BranchDeleteInfoLoadedMsg{Info: info}
	}
}

// isCheckedOutBranch reports whether the working copy currently sits on the
// branch, in which case deleting it would leave the checkout pointing at a
// missing URL.
func isCheckedOutBranch(r model.Repo, branchName string) bool {
	loc := strings.Trim(strings.TrimSpace(svn.GetCurrentLocation(r)), "/")
	if !strings.HasPrefix(loc, "branches/") {
		return false
	}
	name := strings.TrimPrefix(loc, "branches/")
	if slash := strings.Index(name, "/"); slash >= 0 {
		name = name[:slash]
	}
	return name == branchName
}

func deleteBranchCmd(r model.Repo, branchName string) tea.Cmd {
	return startStreamingCommand(func(emit func(string)) model.CommandResult {
		branchURL := r.Root + "/branches/" + branchName
		onBranch := isCheckedOutBranch(r, branchName)

		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n"); emit(s) }

		line("Working copy: " + r.Path)
		line("Current location: " + svn.GetCurrentLocation(r))
		line("Deleting branch: " + branchName)
		line("Target URL: " + branchURL)
		line("")

		err := svn.StreamLines(r, func(raw string) { output.WriteString(raw + "\n"); emit(raw) },
			"delete", branchURL, "-m", "Deleting branch "+branchName)
		if err == nil {
			line("")
			line("Its history stays in the repository and can be brought back with svn copy from an earlier revision.")
			line("")
			line("Branch " + branchName + " deleted from the repository.")
			if onBranch {
				line("")
				line("Warning: the working copy still points at the deleted branch. Switch to trunk before working on.")
			}
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
	})
}

func loadBranchMergeRevisionsCmd(r model.Repo, branch model.Branch) tea.Cmd {
	return func() tea.Msg {
		branchURL := r.Root + "/branches/" + branch.Name
		out, err := svn.Run(r, "log", "--xml", "--stop-on-copy", branchURL)
		if err != nil {
			return model.BranchMergeRevisionsLoadedMsg{
				Branch: branch,
				Output: out,
				Err:    fmt.Errorf("svn log failed\n\nBranch URL: %s\n\nError: %w", branchURL, err),
			}
		}

		var parsed model.SVNLogXML
		if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
			return model.BranchMergeRevisionsLoadedMsg{
				Branch: branch,
				Output: out,
				Err:    fmt.Errorf("could not parse branch log: %w", err),
			}
		}
		if len(parsed.Entries) == 0 {
			return model.BranchMergeRevisionsLoadedMsg{
				Branch: branch,
				Output: out,
				Err:    fmt.Errorf("no SVN log entries found for branch %s", branch.Name),
			}
		}

		return model.BranchMergeRevisionsLoadedMsg{
			Branch:    branch,
			Revisions: branchMergeRevisionsFromLog(parsed.Entries),
		}
	}
}

// branchMergeRevisionsFromLog turns the branch log into newest-first picker
// rows. The oldest entry is the branch creation itself; cherry-picking that
// revision would try to merge the creation of the branch rather than a commit
// made on it, so it is deliberately not offered.
func branchMergeRevisionsFromLog(entries []model.SVNLogEntryXML) []model.BranchMergeRevision {
	creationRevision := 0
	for _, entry := range entries {
		if entry.Revision > 0 && (creationRevision == 0 || entry.Revision < creationRevision) {
			creationRevision = entry.Revision
		}
	}

	revisions := make([]model.BranchMergeRevision, 0, max(0, len(entries)-1))
	for _, entry := range entries {
		if entry.Revision <= 0 || entry.Revision == creationRevision {
			continue
		}
		revisions = append(revisions, model.BranchMergeRevision{
			Revision: entry.Revision,
			Author:   strings.TrimSpace(entry.Author),
			Date:     formatSVNLogDate(entry.Date),
			Msg:      compactOneLine(entry.Msg),
		})
	}
	sort.SliceStable(revisions, func(i, j int) bool {
		return revisions[i].Revision > revisions[j].Revision
	})
	return revisions
}

func mergeBranchCmd(r model.Repo, branchName, revision string) tea.Cmd {
	return startStreamingCommand(func(emit func(string)) model.CommandResult {
		branchURL := r.Root + "/branches/" + branchName
		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n"); emit(s) }
		streamSVN := func(args ...string) error {
			return svn.StreamLines(r, func(raw string) { output.WriteString(raw + "\n"); emit(raw) }, args...)
		}

		line("Working copy: " + r.Path)
		line("Current location: " + svn.GetCurrentLocation(r))
		line("Merging branch: " + branchName)
		line("Source URL: " + branchURL)
		line("Target: current working copy")

		versionText, versionErr := svn.WorkingCopyVersion(r)
		if versionErr == nil && svn.IsMixedRevision(versionText) {
			line("Working copy is mixed-revision. Running svn update first.")
			line("")
			if updateErr := streamSVN("update"); updateErr != nil {
				return model.CommandResult{Output: output.String(), Err: updateErr, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
			}
			line("")
			line("Working copy updated. Continuing merge.")
			line("")
		}

		if revision != "" {
			line("Merge mode: single branch revision")
			line("Revision: r" + revision)
			line("")

			err := streamSVN(branchMergeArgs(branchURL, revision, 0)...)
			if err == nil {
				line("")
				line("Revision r" + revision + " from branch " + branchName + " merged successfully. Review changes, then commit.")
			}
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}

		startRev, revOut, revErr := branchStartRevision(r, branchURL)
		if strings.TrimSpace(revOut) != "" {
			for _, l := range strings.Split(strings.TrimRight(revOut, "\n"), "\n") {
				line(l)
			}
		}
		if revErr != nil {
			line("Could not detect branch start revision: " + revErr.Error())
			line("Fallback: snapshot merge with --ignore-ancestry.")
			line("")
			err := streamSVN("merge", "--ignore-ancestry", branchURL, ".")
			if err == nil {
				line("")
				line("Branch " + branchName + " merged successfully. Review changes, then commit.")
			}
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}

		revisionRange := fmt.Sprintf("%d:HEAD", startRev)
		line("Merge mode: all branch revisions")
		line("Revision range: " + revisionRange)
		line("")

		err := streamSVN(branchMergeArgs(branchURL, "", startRev)...)
		if err == nil {
			line("")
			line("Branch " + branchName + " merged successfully. Review changes, then commit.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
	})
}

func branchMergeArgs(branchURL, revision string, startRev int) []string {
	if revision != "" {
		return []string{"merge", "-c", revision, branchURL + "@HEAD", "."}
	}
	return []string{"merge", "-r", fmt.Sprintf("%d:HEAD", startRev), branchURL + "@HEAD", "."}
}

func branchStartRevision(r model.Repo, branchURL string) (int, string, error) {
	origin, out, err := loadBranchOrigin(r, branchURL)
	if err != nil {
		return 0, out, err
	}
	return origin.StartRev, "", nil
}

// branchOrigin describes where a branch came from: the revision that created
// it, plus the copy source recorded by "svn copy" when SVN still knows it.
type branchOrigin struct {
	StartRev int
	FromPath string
	FromRev  int
}

func loadBranchOrigin(r model.Repo, branchURL string) (branchOrigin, string, error) {
	out, err := svn.Run(r, "log", "--xml", "--stop-on-copy", "-v", branchURL)
	if err != nil {
		return branchOrigin{}, out, err
	}
	var parsed model.SVNLogXML
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		return branchOrigin{}, "", err
	}
	if len(parsed.Entries) == 0 {
		return branchOrigin{}, "", fmt.Errorf("no svn log entries found for branch")
	}

	oldest := parsed.Entries[0]
	for _, entry := range parsed.Entries {
		if entry.Revision > 0 && (oldest.Revision == 0 || entry.Revision < oldest.Revision) {
			oldest = entry
		}
	}
	if oldest.Revision <= 0 {
		return branchOrigin{}, "", fmt.Errorf("invalid branch start revision")
	}

	origin := branchOrigin{StartRev: oldest.Revision}
	branchPath := strings.TrimPrefix(branchURL, r.Root)
	for _, path := range oldest.Paths {
		if path.CopyFromRev <= 0 || strings.TrimSpace(path.CopyFromPath) == "" {
			continue
		}
		// Prefer the entry that created the branch itself; a single revision
		// may copy several paths.
		if origin.FromPath == "" || normalizeSVNTreePath(path.Path) == normalizeSVNTreePath(branchPath) {
			origin.FromPath = path.CopyFromPath
			origin.FromRev = path.CopyFromRev
		}
	}
	return origin, out, nil
}

// ── Branch diff ───────────────────────────────────────────────────────────────

// loadBranchDiffCmd lists every path that differs between the two sides of a
// branch comparison.
func loadBranchDiffCmd(r model.Repo, branchName string, mode model.BranchDiffMode) tea.Cmd {
	return func() tea.Msg {
		ctx, err := buildBranchDiffContext(r, branchName, mode)
		if err != nil {
			return model.BranchDiffLoadedMsg{Context: ctx, Err: err}
		}
		items, out, err := loadBranchDiffItems(r, ctx)
		return model.BranchDiffLoadedMsg{Context: ctx, Items: items, Output: out, Err: err}
	}
}

func buildBranchDiffContext(r model.Repo, branchName string, mode model.BranchDiffMode) (model.BranchDiffContext, error) {
	branchURL := r.Root + "/branches/" + branchName
	trunkURL := r.Root + "/trunk"

	ctx := model.BranchDiffContext{
		Mode:   mode,
		Branch: branchName,
		New: model.BranchDiffSide{
			URL:   branchURL,
			Rev:   "HEAD",
			Label: "BRANCH " + branchName + "@HEAD",
		},
	}

	if mode == model.BranchDiffAgainstTrunkHead {
		ctx.Old = model.BranchDiffSide{URL: trunkURL, Rev: "HEAD", Label: "TRUNK@HEAD"}
		ctx.Summary = "Today's trunk (HEAD) compared with the branch head."
		return ctx, nil
	}

	origin, out, err := loadBranchOrigin(r, branchURL)
	if err != nil {
		return ctx, fmt.Errorf("could not detect the branch start revision\n\nBranch URL: %s\n\nOutput:\n%s\n\nError: %w", branchURL, out, err)
	}

	// The branch at its creation revision *is* the source state at that moment,
	// which stays right even if the branch was not copied from trunk.
	ctx.Old = model.BranchDiffSide{
		URL:   branchURL,
		Rev:   strconv.Itoa(origin.StartRev),
		Label: fmt.Sprintf("BRANCH POINT r%d", origin.StartRev),
	}
	ctx.Summary = fmt.Sprintf("Branch created in r%d.", origin.StartRev)
	if origin.FromPath != "" {
		ctx.Old.Label = fmt.Sprintf("%s@r%d (BRANCH POINT)", strings.TrimPrefix(origin.FromPath, "/"), origin.FromRev)
		ctx.Summary = fmt.Sprintf("Branch created in r%d from ^%s@%d.", origin.StartRev, origin.FromPath, origin.FromRev)
	}
	return ctx, nil
}

func loadBranchDiffItems(r model.Repo, ctx model.BranchDiffContext) ([]model.BranchDiffItem, string, error) {
	out, err := svn.Run(r, "diff", "--summarize", "--xml", ctx.Old.Target(), ctx.New.Target())
	if err != nil {
		return nil, out, fmt.Errorf("svn diff --summarize failed\n\nOld: %s\nNew: %s\n\nOutput:\n%s\n\nError: %w", ctx.Old.Target(), ctx.New.Target(), out, err)
	}

	var parsed model.SVNDiffSummarizeXML
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		return nil, out, fmt.Errorf("could not parse svn diff --summarize output\n\nOutput:\n%s\n\nError: %w", out, err)
	}

	items := make([]model.BranchDiffItem, 0, len(parsed.Paths))
	for _, path := range parsed.Paths {
		rel := branchDiffRelativePath(ctx, path.Path)
		status := branchDiffStatusLetter(path.Item, path.Props)
		if status == "" {
			continue
		}
		items = append(items, model.BranchDiffItem{
			Status: status,
			Path:   rel,
			IsDir:  strings.EqualFold(strings.TrimSpace(path.Kind), "dir"),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, out, nil
}

// branchDiffRelativePath turns the absolute URL printed by svn into a path
// relative to the compared roots.
func branchDiffRelativePath(ctx model.BranchDiffContext, raw string) string {
	rel := strings.TrimSpace(raw)
	for _, prefix := range []string{ctx.New.URL, ctx.Old.URL} {
		if prefix != "" && strings.HasPrefix(rel, prefix) {
			rel = rel[len(prefix):]
			break
		}
	}
	rel = strings.Trim(rel, "/")
	if rel == "" {
		rel = "."
	}
	return rel
}

func branchDiffStatusLetter(item, props string) string {
	letter := ""
	switch strings.ToLower(strings.TrimSpace(item)) {
	case "modified":
		letter = "M"
	case "added":
		letter = "A"
	case "deleted":
		letter = "D"
	case "replaced":
		letter = "R"
	}
	if p := strings.ToLower(strings.TrimSpace(props)); p == "modified" || p == "added" || p == "deleted" {
		letter += "P"
	}
	return letter
}

func branchDiffStatusText(item model.BranchDiffItem) string {
	kind := "file"
	if item.IsDir {
		kind = "directory"
	}
	switch {
	case strings.HasPrefix(item.Status, "A"):
		return item.Status + " — " + kind + " added on the branch side"
	case strings.HasPrefix(item.Status, "D"):
		return item.Status + " — " + kind + " missing from the branch side"
	case strings.HasPrefix(item.Status, "R"):
		return item.Status + " — " + kind + " replaced"
	case item.Status == "P":
		return "P — properties only"
	default:
		return item.Status + " — " + kind + " modified"
	}
}

// branchFileDiffCmd opens one path of a branch comparison side by side.
func branchFileDiffCmd(r model.Repo, ctx model.BranchDiffContext, item model.BranchDiffItem, width int) tea.Cmd {
	return func() tea.Msg {
		out, source, err := buildBranchFileDiffSource(r, ctx, item, width)
		if err != nil {
			return model.DiffLoadedMsg{Output: out, Err: err, Path: item.Path}
		}
		return model.DiffLoadedMsg{Output: out, Path: item.Path, Source: source}
	}
}

// branchDiffMaxBytes caps the unified diff of a whole branch: past this the
// viewport is unusable anyway, and colorizing megabytes stalls the UI.
const branchDiffMaxBytes = 2 << 20

// branchFullDiffCmd shows the complete unified diff of a branch comparison.
func branchFullDiffCmd(r model.Repo, ctx model.BranchDiffContext) tea.Cmd {
	return func() tea.Msg {
		label := ctx.Title() + ": " + ctx.Old.Label + "  ->  " + ctx.New.Label
		out, err := svn.Run(r, "diff", ctx.Old.Target(), ctx.New.Target())
		if err != nil {
			return model.DiffLoadedMsg{Output: out, Err: fmt.Errorf("svn diff failed\n\nOld: %s\nNew: %s\n\nOutput:\n%s\n\nError: %w", ctx.Old.Target(), ctx.New.Target(), out, err), Path: label}
		}
		truncated := ""
		if len(out) > branchDiffMaxBytes {
			out = out[:branchDiffMaxBytes]
			truncated = "\n\n... diff truncated at 2 MB — open single files from the list instead."
		}
		if strings.TrimSpace(out) == "" {
			return model.DiffLoadedMsg{Output: "No differences found.\n\n" + label, Path: label}
		}
		body := label + "\n" + ctx.Summary + "\n\n" + colorizeUnifiedDiff(out) + truncated
		return model.DiffLoadedMsg{Output: body, Path: label}
	}
}

// ── Pull / status ─────────────────────────────────────────────────────────────

func loadPullItemsCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		items, err := loadPullItems(r)
		return model.PullItemsLoadedMsg{Items: items, Err: err}
	}
}

func loadPullItems(r model.Repo) ([]model.CommitItem, error) {
	out, err := svn.Run(r, "status", "-u")
	if err != nil {
		return nil, fmt.Errorf("svn status -u failed\n\nWorking copy: %s\n\nOutput:\n%s\n\nError: %w", r.Path, out, err)
	}
	var items []model.CommitItem
	for _, line := range strings.Split(out, "\n") {
		item, ok := parseSVNStatusUpdateLine(line)
		if ok {
			items = append(items, item)
		}
	}
	items = filterParentDirectoryEntries(items)
	sort.SliceStable(items, func(i, j int) bool {
		di, dj := filepath.Dir(items[i].Path), filepath.Dir(items[j].Path)
		if di == dj {
			return items[i].Path < items[j].Path
		}
		return di < dj
	})
	return groupPullItemsByDir(items), nil
}

func parseSVNStatusUpdateLine(line string) (model.CommitItem, bool) {
	if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "Status against revision") {
		return model.CommitItem{}, false
	}
	prefixLen := min(len(line), 9)
	if !strings.Contains(line[:prefixLen], "*") {
		return model.CommitItem{}, false
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return model.CommitItem{}, false
	}
	path := strings.TrimPrefix(filepath.ToSlash(fields[len(fields)-1]), "./")
	status := "U"
	if len(strings.TrimSpace(line)) > 0 {
		first := strings.TrimSpace(line)[0]
		if first == 'A' || first == 'D' || first == 'M' || first == 'R' {
			status = string(first)
		}
	}
	return model.CommitItem{Status: status, Path: path}, true
}

func filterParentDirectoryEntries(items []model.CommitItem) []model.CommitItem {
	var result []model.CommitItem
	for _, item := range items {
		prefix := item.Path + "/"
		isParent := false
		for _, other := range items {
			if strings.HasPrefix(other.Path, prefix) {
				isParent = true
				break
			}
		}
		if !isParent {
			result = append(result, item)
		}
	}
	return result
}

func groupPullItemsByDir(items []model.CommitItem) []model.CommitItem {
	var result []model.CommitItem
	currentDir := ""
	for _, item := range items {
		dir := filepath.Dir(item.Path)
		if dir == "." {
			dir = ""
		}
		if dir != currentDir {
			currentDir = dir
			if dir != "" {
				result = append(result, model.CommitItem{Path: dir + "/", IsDir: true})
			}
		}
		result = append(result, item)
	}
	return result
}

func pullCmd(r model.Repo, paths []string) tea.Cmd {
	return startStreamingCommand(func(emit func(string)) model.CommandResult {
		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n"); emit(s) }

		line("Working copy: " + r.Path)
		line("Current URL: " + strings.TrimSpace(svn.GetCurrentURL(r)))
		if r.Username != "" {
			line("Auth user: " + r.Username)
		}

		args := []string{"update"}
		if len(paths) > 0 {
			args = append(args, paths...)
			line("Selected paths:")
			for _, p := range paths {
				line("  " + p)
			}
		} else {
			line("Updating all files in working copy.")
		}
		line("")
		line("Running: svn " + strings.Join(args, " "))
		line("")

		err := svn.StreamLines(r, func(raw string) {
			colored := colorizeSVNUpdateLine(raw)
			output.WriteString(colored + "\n")
			emit(colored)
		}, args...)
		if err == nil {
			line("")
			line("Pull finished successfully.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
	})
}

func statusCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder
		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Current URL: " + strings.TrimSpace(svn.GetCurrentURL(r)) + "\n")
		output.WriteString("Running: svn status\n\n")

		out, err := svn.Run(r, "status")
		output.WriteString(out)
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}
		if strings.TrimSpace(out) == "" {
			output.WriteString("Working copy is clean. No local changes found.\n")
		}
		output.WriteString("\nStatus finished successfully.")
		return model.CommandResult{Output: output.String(), CurrentLocation: svn.GetCurrentLocation(r)}
	}
}

func cleanupCmd(r model.Repo) tea.Cmd {
	return startStreamingCommand(func(emit func(string)) model.CommandResult {
		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n"); emit(s) }

		line("Working copy: " + r.Path)
		line("Running: svn cleanup")
		line("")

		err := svn.StreamLines(r, func(raw string) {
			colored := colorizeSVNUpdateLine(raw)
			output.WriteString(colored + "\n")
			emit(colored)
		}, "cleanup")
		if err == nil {
			line("")
			line("Cleanup finished successfully.")
			line("Stale locks were released and unfinished operations rolled back.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), CurrentRevision: svn.GetCurrentRevision(r)}
	})
}

// checkoutRevisionLogLimit caps how much history the revision picker loads. The
// log is cached on disk between runs, so later searches only fetch the
// revisions committed since the last one.
const checkoutRevisionLogLimit = 500

// loadCheckoutRevisionsCmd loads the log of the checked-out URL for the
// revision picker. It asks for the verbose log so a revision can also be found
// by a path it changed.
func loadCheckoutRevisionsCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		target := strings.TrimSpace(r.URL)
		if target == "" {
			target = strings.TrimSpace(svn.GetCurrentURL(r))
		}
		entries, err := svn.FetchLogIncremental(r, "checkout_revisions_"+target, target, checkoutRevisionLogLimit, false)
		if err != nil {
			return model.CheckoutRevisionsLoadedMsg{Err: fmt.Errorf("svn log failed\n\nTarget: %s\n\nError: %w", target, err)}
		}
		items := checkoutRevisionsFromLog(entries)
		if len(items) == 0 {
			return model.CheckoutRevisionsLoadedMsg{Err: fmt.Errorf("no SVN log entries found for %s", target)}
		}
		return model.CheckoutRevisionsLoadedMsg{Items: items}
	}
}

func checkoutRevisionsFromLog(entries []model.SVNLogEntryXML) []model.CheckoutRevision {
	revisions := make([]model.CheckoutRevision, 0, len(entries))
	for _, entry := range entries {
		if entry.Revision <= 0 {
			continue
		}
		paths := make([]model.CheckoutPath, 0, len(entry.Paths))
		for _, p := range entry.Paths {
			changed := strings.TrimSpace(p.Path)
			if changed == "" {
				continue
			}
			paths = append(paths, model.CheckoutPath{
				Action: strings.TrimSpace(p.Action),
				Path:   changed,
			})
		}
		sort.SliceStable(paths, func(i, j int) bool { return paths[i].Path < paths[j].Path })
		revisions = append(revisions, model.CheckoutRevision{
			Revision: entry.Revision,
			Author:   strings.TrimSpace(entry.Author),
			Date:     formatSVNLogDate(entry.Date),
			Msg:      compactOneLine(entry.Msg),
			Paths:    paths,
		})
	}
	sort.SliceStable(revisions, func(i, j int) bool {
		return revisions[i].Revision > revisions[j].Revision
	})
	return revisions
}

func checkoutRevisionCmd(r model.Repo, revision string) tea.Cmd {
	return startStreamingCommand(func(emit func(string)) model.CommandResult {
		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n"); emit(s) }

		line("Working copy: " + r.Path)
		line("Current URL: " + strings.TrimSpace(svn.GetCurrentURL(r)))
		line("Target revision: " + revision)
		line("Running: svn update -r " + revision)
		line("")

		err := svn.StreamLines(r, func(raw string) {
			colored := colorizeSVNUpdateLine(raw)
			output.WriteString(colored + "\n")
			emit(colored)
		}, "update", "-r", revision)
		if err == nil {
			line("")
			line("Working copy updated to revision " + revision + ".")
			line("Use Pull to update back to HEAD.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
	})
}

// ── Commit / revert ───────────────────────────────────────────────────────────

func loadCommitItemsCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		items, hidden, err := loadCommitItems(r, true)
		if err != nil {
			return model.CommitItemsLoadedMsg{Err: err}
		}
		// SVN aborts a commit that names a path still in conflict (E155015), so
		// those belong on the Resolve conflicts screen, not here.
		var committable []model.CommitItem
		var conflicted []string
		for _, item := range filterSelectFilesOnly(r, items, hidden) {
			if item.Conflicted {
				conflicted = append(conflicted, item.Path)
				continue
			}
			committable = append(committable, item)
		}
		committable, err = expandUnversionedCommitDirectories(r, committable)
		if err != nil {
			return model.CommitItemsLoadedMsg{Err: err}
		}
		return model.CommitItemsLoadedMsg{Items: committable, Conflicted: conflicted}
	}
}

func loadRevertItemsCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		items, hidden, err := loadCommitItems(r, false)
		if err == nil {
			items = filterSelectFilesOnly(r, items, hidden)
		}
		return model.RevertItemsLoadedMsg{Items: items, Err: err}
	}
}

func loadShelveItemsCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		items, _, err := loadCommitItems(r, true)
		if err == nil {
			items = filterShelveFilesOnly(r, items)
		}
		return model.RevertItemsLoadedMsg{Items: items, Err: err}
	}
}

// filterShelveFilesOnly drops versioned directories. A shelf is a patch plus
// copies of unversioned paths: svn diff cannot express a directory-only change,
// and svn revert refuses a scheduled directory without its children. Unversioned
// directories stay — those are copied and removed wholesale.
func filterShelveFilesOnly(r model.Repo, items []model.CommitItem) []model.CommitItem {
	filtered := make([]model.CommitItem, 0, len(items))
	for _, item := range items {
		item.IsDir = item.IsDir || isSVNDir(r, item.Path)
		if item.IsDir && !item.Unversioned {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// loadCommitItems returns the visible local changes plus the paths ignore.txt
// hides, which the directory filter needs to judge scheduled directories.
func loadCommitItems(r model.Repo, includeUnversioned bool) (items []model.CommitItem, hiddenPaths []string, err error) {
	out, err := svn.Run(r, "status")
	if err != nil {
		return nil, nil, fmt.Errorf("svn status failed\n\nWorking copy: %s\n\nOutput:\n%s\n\nError: %w", r.Path, out, err)
	}
	for _, line := range strings.Split(out, "\n") {
		item, ok, hidden := parseSVNLocalChangeStatusLine(r, line, includeUnversioned)
		if hidden {
			hiddenPaths = append(hiddenPaths, item.Path)
			continue
		}
		if !ok {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Unversioned != items[j].Unversioned {
			return !items[i].Unversioned && items[j].Unversioned
		}
		return items[i].Path < items[j].Path
	})
	return items, hiddenPaths, nil
}

// parseSVNLocalChangeStatusLine turns one svn status line into a commit item.
// The third return value marks a real change that ignore.txt hides: callers
// need it to tell "nothing changed here" from "only ignored things changed".
func parseSVNLocalChangeStatusLine(r model.Repo, line string, includeUnversioned bool) (model.CommitItem, bool, bool) {
	if strings.TrimSpace(line) == "" {
		return model.CommitItem{}, false, false
	}
	textStatus := byte(' ')
	propStatus := byte(' ')
	if len(line) > 0 {
		textStatus = line[0]
	}
	if len(line) > 1 {
		propStatus = line[1]
	}
	path := ""
	if len(line) >= 9 {
		path = strings.TrimSpace(line[8:])
	} else {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			path = fields[len(fields)-1]
		}
	}
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	if path == "" {
		return model.CommitItem{}, false, false
	}
	unversioned := textStatus == '?'
	if unversioned && !includeUnversioned {
		return model.CommitItem{}, false, false
	}
	textChanged := strings.ContainsRune("MADRC!~", rune(textStatus))
	propsChanged := propStatus == 'M' || propStatus == 'C'
	// Column 7 carries the tree-conflict marker; columns 1 and 2 the text and
	// property conflicts.
	conflicted := textStatus == 'C' || propStatus == 'C' || (len(line) > 6 && line[6] == 'C')
	if !unversioned && !textChanged && !propsChanged {
		return model.CommitItem{}, false, false
	}
	if shouldHideFromCommitSelect(path) {
		return model.CommitItem{Path: path}, false, true
	}
	// Keep the leading columns: "M" is a content change, " M" a property-only
	// one, and a merge shows up as property changes on directories.
	status := strings.TrimRight(line[:min(len(line), 8)], " ")
	if strings.TrimSpace(status) == "" {
		status = string(textStatus)
	}
	return model.CommitItem{
		Status:       status,
		Path:         path,
		Unversioned:  unversioned,
		PropsChanged: propsChanged,
		Conflicted:   conflicted,
		IsDir:        isSVNDir(r, path),
	}, true, false
}

// shouldHideFromCommitSelect drops paths that must never reach the commit list:
// the shelf store plus everything named in ~/.config/svn-tui/ignore.txt.
func shouldHideFromCommitSelect(path string) bool {
	clean := strings.TrimPrefix(strings.TrimSpace(filepath.ToSlash(path)), "./")
	if clean == model.ShelvesDir || strings.HasPrefix(clean, model.ShelvesDir+"/") {
		return true
	}
	return svn.Ignores().HidesPath(clean)
}

// isScheduledDirChange returns true for directories that are newly scheduled
// (A = added, R = replaced) and must be included in any commit that touches
// their children. SVN reports R  + for directories replaced with copy history.
func isScheduledDirChange(item model.CommitItem) bool {
	return item.IsDir && len(item.Status) > 0 && (item.Status[0] == 'A' || item.Status[0] == 'R')
}

func filterSelectFilesOnly(r model.Repo, items []model.CommitItem, hiddenPaths []string) []model.CommitItem {
	filtered := make([]model.CommitItem, 0, len(items))
	for _, item := range items {
		item.IsDir = item.IsDir || isSVNDir(r, item.Path)
		if item.IsDir && !item.Unversioned {
			switch {
			case isScheduledDirChange(item):
				// An added or replaced directory carries its whole subtree, and
				// svn status does not list the children of a copy. Ask SVN what
				// is actually inside: with nothing but ignore.txt paths in there
				// the directory has nothing to offer.
				if dirChangesAllIgnored(r, item.Path) {
					continue
				}
			case item.PropsChanged:
				// Property-only change, such as the svn:mergeinfo a merge
				// records. Committing it is what makes the merge stick.
			default:
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// expandUnversionedCommitDirectories adds display-only rows for every visible
// descendant of an unversioned directory. SVN status only reports the top-level
// "?" directory, even though svn add will recurse into it. Keeping the parent
// as the selection unit preserves commit semantics while allowing each file to
// be highlighted and diffed from the commit screen.
func expandUnversionedCommitDirectories(r model.Repo, items []model.CommitItem) ([]model.CommitItem, error) {
	expanded := append([]model.CommitItem(nil), items...)
	known := make(map[string]bool, len(items))
	for _, item := range items {
		known[item.Path] = true
	}

	for _, parent := range items {
		if !parent.Unversioned || !parent.IsDir || parent.IncludedByParent != "" {
			continue
		}
		fullRoot := filepath.Join(r.Path, filepath.FromSlash(parent.Path))
		err := filepath.WalkDir(fullRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == fullRoot {
				return nil
			}
			rel, err := filepath.Rel(r.Path, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if shouldHideFromCommitSelect(rel) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if known[rel] {
				return nil
			}
			known[rel] = true
			expanded = append(expanded, model.CommitItem{
				Status:           "?",
				Path:             rel,
				Unversioned:      true,
				IsDir:            entry.IsDir(),
				IncludedByParent: parent.Path,
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("could not list files below unversioned directory %s: %w", parent.Path, err)
		}
	}

	sort.SliceStable(expanded, func(i, j int) bool {
		if expanded[i].Unversioned != expanded[j].Unversioned {
			return !expanded[i].Unversioned && expanded[j].Unversioned
		}
		return expanded[i].Path < expanded[j].Path
	})
	return expanded, nil
}

// dirChangesAllIgnored reports whether every change SVN sees under dir is one
// ignore.txt hides. A directory SVN reports no changes for is not "all ignored":
// it is a structural change of its own and stays in the list.
func dirChangesAllIgnored(r model.Repo, dir string) bool {
	out, err := svn.Run(r, "diff", "--summarize", dir)
	if err != nil {
		return false
	}
	ignores := svn.Ignores()
	seen := false
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		path := strings.TrimPrefix(filepath.ToSlash(fields[len(fields)-1]), "./")
		if path == dir {
			continue
		}
		seen = true
		if !ignores.HidesPath(path) {
			return false
		}
	}
	return seen
}

// withRequiredParentDirs augments selected with any A/R-status directory from
// allItems that is an ancestor of a selected path but was not itself selected.
// SVN requires newly-added/replaced parent directories to be part of the same commit.
func withRequiredParentDirs(selected []model.CommitItem, allItems []model.CommitItem) []model.CommitItem {
	addedDirs := make(map[string]model.CommitItem)
	for _, item := range allItems {
		if isScheduledDirChange(item) {
			addedDirs[item.Path] = item
		}
	}
	if len(addedDirs) == 0 {
		return selected
	}
	inSelected := make(map[string]bool)
	for _, item := range selected {
		inSelected[item.Path] = true
	}
	result := append([]model.CommitItem(nil), selected...)
	for _, item := range selected {
		dir := filepath.ToSlash(filepath.Dir(item.Path))
		for dir != "" && dir != "." {
			if d, ok := addedDirs[dir]; ok && !inSelected[d.Path] {
				result = append(result, d)
				inSelected[d.Path] = true
			}
			dir = filepath.ToSlash(filepath.Dir(dir))
		}
	}
	return result
}

func commitCmd(r model.Repo, items []model.CommitItem, message string) tea.Cmd {
	return startStreamingCommand(func(emit func(string)) model.CommandResult {
		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n"); emit(s) }

		paths := commitItemPaths(items)
		unversionedPaths := unversionedItemPaths(items)

		line("Working copy: " + r.Path)
		line("Commit message: " + message)
		line("Selected files:")
		for _, item := range items {
			prefix := "  "
			if item.Unversioned {
				prefix = "  + "
			}
			line(prefix + item.Path)
		}

		line("")
		line("Converting line endings to CRLF...")
		for _, item := range items {
			if item.IsDir {
				continue
			}
			p := item.Path
			converted, cerr := ensureCRLFFile(r, p)
			if cerr != nil {
				line("  Warning: could not convert " + p + ": " + cerr.Error())
			} else if converted {
				line("  " + p + "  LF → CRLF")
			}
		}

		if len(unversionedPaths) > 0 {
			line("")
			line("Adding selected unversioned files before commit...")
			line("")
			addArgs := append([]string{"add", "--parents"}, unversionedPaths...)
			addOut, err := svn.Run(r, addArgs...)
			for _, l := range strings.Split(strings.TrimRight(addOut, "\n"), "\n") {
				line(l)
			}
			if err != nil {
				return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
			}
			// --parents may add ancestor directories not in paths; include them so SVN
			// does not reject the commit with E200009 ("not part of the commit").
			for _, addedLine := range strings.Split(addOut, "\n") {
				fields := strings.Fields(addedLine)
				if len(fields) < 2 || fields[0] != "A" {
					continue
				}
				ap := strings.TrimPrefix(filepath.ToSlash(fields[len(fields)-1]), "./")
				alreadyIn := false
				for _, p := range paths {
					if p == ap {
						alreadyIn = true
						break
					}
				}
				if !alreadyIn {
					paths = append(paths, ap)
				}
			}
			line("")
			line("Unversioned files added successfully.")
		}

		// svn add --parents may have pulled in ignore.txt paths; they must never
		// travel up in a commit.
		kept := paths[:0]
		for _, p := range paths {
			if shouldHideFromCommitSelect(p) {
				line("  skipping ignored path: " + p)
				continue
			}
			kept = append(kept, p)
		}
		paths = kept

		line("")
		line("Running commit...")
		line("")

		// --depth empty commits every target as itself: a directory contributes
		// its own node and properties, never the local changes of children that
		// were not selected. SVN still carries a copy recursively, as it must.
		args := append([]string{"commit", "--depth", "empty"}, paths...)
		args = append(args, "-m", message)

		err := svn.StreamLines(r, func(raw string) { output.WriteString(raw + "\n"); emit(raw) }, args...)
		if err == nil {
			line("")
			line("Commit finished successfully.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
	})
}

func revertCmd(r model.Repo, items []model.CommitItem) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n") }

		line("Working copy: " + r.Path)
		line("Selected files to revert:")
		var files, dirs []string
		for _, item := range items {
			// Only a scheduled directory needs its children reverted with it.
			// A property-only change reverts on its own, so the subtree is left
			// alone — that matters most for the working copy root.
			if item.IsDir && isScheduledDirChange(item) {
				dirs = append(dirs, item.Path)
				line("  " + item.Path + "  (directory — reverts everything below it)")
				continue
			}
			files = append(files, item.Path)
			line("  " + item.Path)
		}

		// A directory revert takes the whole subtree with it, so stash the
		// ignore.txt paths inside it and put them back afterwards.
		saved, tmpRoot, err := preserveIgnoredPaths(r, dirs)
		if err != nil {
			line("")
			line("Could not stash ignored files before reverting: " + err.Error())
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}
		if tmpRoot != "" {
			defer os.RemoveAll(tmpRoot)
		}

		line("")
		line("Running revert...")
		line("")

		run := func(args ...string) error {
			out, err := svn.Run(r, args...)
			output.WriteString(out)
			return err
		}

		if len(files) > 0 {
			err = run(append([]string{"revert"}, files...)...)
		}
		// SVN refuses to revert a scheduled directory without its children
		// (E155038), so directories go in one at a time with --depth infinity.
		for _, dir := range dirs {
			if err != nil {
				break
			}
			line("svn revert --depth infinity " + dir)
			err = run("revert", "--depth", "infinity", dir)
		}

		if len(saved) > 0 {
			line("")
			line("Restoring ignored files:")
			restorePreservedPaths(r, saved, line)
		}

		if err == nil {
			line("")
			line("Revert finished successfully.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
	}
}

// preservedPath is one ignore.txt file copied aside before a directory revert.
type preservedPath struct{ rel, tmp string }

// preserveIgnoredPaths copies every ignore.txt file found under dirs into a
// temporary directory. Reverting a directory discards local edits in its whole
// subtree, and files listed in ignore.txt are exactly the ones svn-tui must
// leave alone. Ignored directories are skipped rather than copied — they hold
// unversioned output (vendor, node_modules) that revert does not touch.
func preserveIgnoredPaths(r model.Repo, dirs []string) ([]preservedPath, string, error) {
	if len(dirs) == 0 {
		return nil, "", nil
	}
	ignores := svn.Ignores()
	tmpRoot, err := os.MkdirTemp("", "svn-tui-preserve-")
	if err != nil {
		return nil, "", err
	}
	var saved []preservedPath
	for _, dir := range dirs {
		root := filepath.Join(r.Path, filepath.FromSlash(dir))
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if ignores.HidesName(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(r.Path, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if !ignores.HidesPath(rel) {
				return nil
			}
			tmp := filepath.Join(tmpRoot, filepath.FromSlash(rel))
			if err := copyPath(path, tmp); err != nil {
				return nil
			}
			saved = append(saved, preservedPath{rel: rel, tmp: tmp})
			return nil
		})
	}
	if len(saved) == 0 {
		os.RemoveAll(tmpRoot)
		return nil, "", nil
	}
	return saved, tmpRoot, nil
}

// restorePreservedPaths copies the stashed ignore.txt files back over whatever
// the revert left behind.
func restorePreservedPaths(r model.Repo, saved []preservedPath, line func(string)) {
	for _, p := range saved {
		dst := filepath.Join(r.Path, filepath.FromSlash(p.rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			line("  FAILED: " + p.rel + " — " + err.Error())
			continue
		}
		if err := copyPath(p.tmp, dst); err != nil {
			line("  FAILED: " + p.rel + " — " + err.Error())
			continue
		}
		line("  kept local version of " + p.rel)
	}
}

// ── Shelve / unshelve ─────────────────────────────────────────────────────────

func loadShelvesCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		shelvesRoot := filepath.Join(r.Path, model.ShelvesDir)
		entries, err := os.ReadDir(shelvesRoot)
		if err != nil {
			if os.IsNotExist(err) {
				return model.ShelvesLoadedMsg{}
			}
			return model.ShelvesLoadedMsg{Err: err, Output: "Failed to read custom shelves directory: " + shelvesRoot}
		}
		var shelves []string
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(shelvesRoot, entry.Name(), "manifest.json")); err == nil {
				shelves = append(shelves, entry.Name())
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(shelves)))
		return model.ShelvesLoadedMsg{Shelves: shelves}
	}
}

func shelveChangesCmd(r model.Repo, items []model.CommitItem, shelfName string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		selected := selectedCommitItems(items)
		if len(selected) == 0 {
			return model.CommandResult{Output: "Select at least one file.", Err: fmt.Errorf("no files selected"), CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}

		shelfName = strings.TrimSpace(shelfName)
		if err := validateNewShelfName(r, shelfName); err != nil {
			return model.CommandResult{Output: "Shelf was not created.", Err: err, CurrentLocation: r.CurrentLocation, URL: r.URL}
		}
		shelfDir := filepath.Join(r.Path, model.ShelvesDir, shelfName)
		filesDir := filepath.Join(shelfDir, "files")
		patchPath := filepath.Join(shelfDir, "changes.patch")
		manifestPath := filepath.Join(shelfDir, "manifest.json")

		currentURL, _ := svn.Run(r, "info", "--show-item", "url")
		currentLocation := svn.GetCurrentLocation(r)

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Shelf name: " + shelfName + "\nShelf path: " + shelfDir + "\n\nSelected files:\n")
		for _, item := range selected {
			output.WriteString("  " + item.Status + " " + item.Path + "\n")
		}

		if err := os.MkdirAll(filepath.Join(r.Path, model.ShelvesDir), 0700); err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}
		if err := os.Mkdir(shelfDir, 0700); err != nil {
			if os.IsExist(err) {
				err = fmt.Errorf("a shelf named %q already exists", shelfName)
			}
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}
		shelfSaved := false
		defer func() {
			if !shelfSaved {
				_ = os.RemoveAll(shelfDir)
				_, _ = removeShelvesRootIfEmpty(r)
			}
		}()
		if err := os.Mkdir(filesDir, 0700); err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}

		var versionedPaths, unversionedPaths []string
		for _, item := range selected {
			if item.Unversioned || strings.HasPrefix(strings.TrimSpace(item.Status), "?") {
				unversionedPaths = append(unversionedPaths, item.Path)
			} else {
				versionedPaths = append(versionedPaths, item.Path)
			}
		}

		patchContent := ""
		if len(versionedPaths) > 0 {
			out, err := svn.Run(r, append([]string{"diff"}, versionedPaths...)...)
			patchContent = out
			if err != nil {
				return model.CommandResult{Output: output.String() + "\n\nsvn diff failed:\n" + out, Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
			}
		}

		if err := os.WriteFile(patchPath, []byte(patchContent), 0600); err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}

		manifest := model.ShelfManifest{
			Name: shelfName, CreatedAt: time.Now().Format(time.RFC3339),
			WorkingCopy: r.Path, URL: strings.TrimSpace(currentURL), CurrentLocation: currentLocation,
			VersionedPaths: versionedPaths, UnversionedPaths: unversionedPaths,
		}

		for _, relPath := range unversionedPaths {
			src := filepath.Join(r.Path, filepath.FromSlash(relPath))
			dst := filepath.Join(filesDir, filepath.FromSlash(relPath))
			if err := copyPath(src, dst); err != nil {
				return model.CommandResult{Output: output.String(), Err: fmt.Errorf("copy unversioned file %s failed: %w", relPath, err), CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
			}
		}

		manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}
		if err := os.WriteFile(manifestPath, manifestBytes, 0600); err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}
		shelfSaved = true

		if len(versionedPaths) > 0 {
			output.WriteString("\nReverting selected versioned files after saving patch...\n\n")
			out, err := svn.Run(r, append([]string{"revert"}, versionedPaths...)...)
			output.WriteString(out)
			if err != nil {
				return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
			}
		}

		for _, relPath := range unversionedPaths {
			if err := os.RemoveAll(filepath.Join(r.Path, filepath.FromSlash(relPath))); err != nil {
				return model.CommandResult{Output: output.String(), Err: fmt.Errorf("remove unversioned file %s failed: %w", relPath, err), CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
			}
		}

		output.WriteString("\nShelve finished successfully.\nUse 'Unshelve changes' to restore shelf: " + shelfName)
		output.WriteString("\n\nNote: this is a custom SVN TUI shelf stored in .svn-tui-shelves.")
		return model.CommandResult{Output: output.String(), CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
	}
}

func validateNewShelfName(r model.Repo, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("shelf name is required")
	}
	if len(name) > 200 {
		return fmt.Errorf("shelf name must be at most 200 bytes")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("shelf name cannot contain / or \\ and cannot be . or ..")
	}
	for _, r := range name {
		if r < ' ' || r == 0x7f {
			return fmt.Errorf("shelf name cannot contain control characters")
		}
	}

	_, err := os.Lstat(filepath.Join(r.Path, model.ShelvesDir, name))
	if err == nil {
		return fmt.Errorf("a shelf named %q already exists", name)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("cannot check shelf name %q: %w", name, err)
	}
	return nil
}

func unshelveChangesCmd(r model.Repo, shelfName string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		shelfDir := filepath.Join(r.Path, model.ShelvesDir, shelfName)
		filesDir := filepath.Join(shelfDir, "files")
		patchPath := filepath.Join(shelfDir, "changes.patch")
		manifestPath := filepath.Join(shelfDir, "manifest.json")

		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Shelf name: " + shelfName + "\n\n")

		manifestBytes, err := os.ReadFile(manifestPath)
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}
		var manifest model.ShelfManifest
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}

		patchBytes, err := os.ReadFile(patchPath)
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}

		if strings.TrimSpace(string(patchBytes)) != "" {
			output.WriteString("Applying saved patch...\n\n")
			out, err := svnPatchFile(r, patchPath)
			output.WriteString(out)
			if err != nil {
				return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
			}
		}

		for _, relPath := range manifest.UnversionedPaths {
			src := filepath.Join(filesDir, filepath.FromSlash(relPath))
			dst := filepath.Join(r.Path, filepath.FromSlash(relPath))
			if err := copyPath(src, dst); err != nil {
				return model.CommandResult{Output: output.String(), Err: fmt.Errorf("restore unversioned file %s failed: %w", relPath, err), CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
			}
		}

		if err := os.RemoveAll(shelfDir); err != nil {
			output.WriteString("\nWarning: shelf was restored, but removing shelf directory failed: " + err.Error())
		} else {
			output.WriteString("\nShelf restored and removed from custom shelves.")
		}

		if removed, err := removeShelvesRootIfEmpty(r); err != nil {
			output.WriteString("\nWarning: checking custom shelves directory failed: " + err.Error())
		} else if removed {
			output.WriteString("\nAll shelves are restored, .svn-tui-shelves was removed.")
		}

		output.WriteString("\nRun Status/Diff to review the working copy.")
		return model.CommandResult{Output: output.String(), CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
	}
}

func svnPatchFile(r model.Repo, patchPath string) (string, error) {
	out, err := svn.Run(r, "patch", patchPath)
	if err == nil {
		return out, nil
	}
	cmd := exec.Command("patch", "-p0", "-i", patchPath)
	cmd.Dir = r.Path
	fallbackOut, fallbackErr := cmd.CombinedOutput()
	if fallbackErr == nil {
		return out + string(fallbackOut), nil
	}
	return out + string(fallbackOut), fmt.Errorf("svn patch failed: %w; fallback patch failed: %v", err, fallbackErr)
}

// deleteShelf throws away one stored shelf. A shelf is just a directory under
// .svn-tui-shelves holding a patch and copies of unversioned files, so deleting
// it discards those saved changes and leaves the working copy untouched.
func deleteShelf(r model.Repo, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return fmt.Errorf("invalid shelf name: %q", name)
	}
	if err := os.RemoveAll(filepath.Join(r.Path, model.ShelvesDir, name)); err != nil {
		return err
	}
	_, err := removeShelvesRootIfEmpty(r)
	return err
}

func removeShelvesRootIfEmpty(r model.Repo) (bool, error) {
	shelvesRoot := filepath.Join(r.Path, model.ShelvesDir)
	entries, err := os.ReadDir(shelvesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if len(entries) > 0 {
		return false, nil
	}
	return true, os.Remove(shelvesRoot)
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, info.Mode())
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

// ── Conflicts ─────────────────────────────────────────────────────────────────

// resolveAllTreeConflictsCmd resolves every tree conflict in turn. SVN cannot
// take them in one shot, so each path gets its own resolve and its own result.
func resolveAllTreeConflictsCmd(r model.Repo, items []model.ConflictItem) tea.Cmd {
	return startStreamingCommand(func(emit func(string)) model.CommandResult {
		var output strings.Builder
		line := func(s string) { output.WriteString(s + "\n"); emit(s) }

		line(fmt.Sprintf("Resolving %d tree conflict(s) with --accept=working...", len(items)))
		line("")

		var failed []string
		for _, item := range items {
			out, err := svn.Run(r, "resolve", "--accept=working", item.Path)
			if err != nil {
				line("FAILED: " + item.Path + " — " + err.Error())
				if strings.TrimSpace(out) != "" {
					line(out)
				}
				failed = append(failed, item.Path)
			} else {
				line("Resolved: " + item.Path)
			}
		}

		line("")
		if len(failed) == 0 {
			line(fmt.Sprintf("All %d tree conflict(s) resolved successfully.", len(items)))
			return model.CommandResult{Output: output.String(), CurrentLocation: svn.GetCurrentLocation(r)}
		}
		line(fmt.Sprintf("%d of %d resolved. %d failed.", len(items)-len(failed), len(items), len(failed)))
		return model.CommandResult{
			Output:          output.String(),
			Err:             fmt.Errorf("%d tree conflict(s) could not be resolved", len(failed)),
			CurrentLocation: svn.GetCurrentLocation(r),
		}
	})
}

// resolveConflictAcceptCmd resolves one conflicted path with a fixed --accept
// value: mine-full keeps the working file, theirs-full takes the incoming one.
func resolveConflictAcceptCmd(r model.Repo, item model.ConflictItem, accept, label string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder
		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Conflicted path: " + item.Path + "\n")
		output.WriteString("Keeping the " + label + "\n")
		output.WriteString("Running: svn resolve --accept=" + accept + "\n\n")

		out, err := svn.Run(r, "resolve", "--accept="+accept, item.Path)
		output.WriteString(out)
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}
		output.WriteString("\nConflict resolved: " + item.Path + " now holds the " + label + ".")
		return model.CommandResult{Output: output.String(), CurrentLocation: svn.GetCurrentLocation(r)}
	}
}

func loadConflictItemsCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		items, err := loadConflictItems(r)
		return model.ConflictItemsLoadedMsg{Items: items, Err: err}
	}
}

func loadConflictItems(r model.Repo) ([]model.ConflictItem, error) {
	out, err := svn.Run(r, "status")
	if err != nil {
		return nil, fmt.Errorf("svn status failed\n\nWorking copy: %s\n\nOutput:\n%s\n\nError: %w", r.Path, out, err)
	}
	var items []model.ConflictItem
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var status, path string
		if len(line) >= 8 {
			status = strings.TrimSpace(line[:8])
			path = strings.TrimSpace(line[8:])
		} else {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				status = fields[0]
				path = fields[len(fields)-1]
			}
		}
		if status == "" || path == "" || !strings.Contains(status, "C") {
			continue
		}
		isTree := isTreeConflict(r, path)
		displayStatus := status
		if isTree {
			displayStatus = status + " TREE"
		}
		items = append(items, model.ConflictItem{Status: displayStatus, Path: path, IsTree: isTree})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

func isTreeConflict(r model.Repo, path string) bool {
	infoOut, err := svn.Run(r, "info", path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(infoOut), "tree conflict")
}

func parseSVNConflictFiles(infoOut, repoPath, relPath string) (mineFile, oldFile, newFile string) {
	for _, line := range strings.Split(infoOut, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		val := func() string {
			idx := strings.Index(line, ":")
			if idx < 0 {
				return ""
			}
			v := strings.TrimSpace(line[idx+1:])
			if v == "" {
				return ""
			}
			if filepath.IsAbs(v) {
				return v
			}
			return filepath.Join(repoPath, filepath.FromSlash(v))
		}
		switch {
		case strings.HasPrefix(lower, "conflict previous working file"):
			mineFile = val()
		case strings.HasPrefix(lower, "conflict previous base file"):
			oldFile = val()
		case strings.HasPrefix(lower, "conflict current base file"):
			newFile = val()
		}
	}
	return
}

func guessSVNConflictFiles(fullPath string) (mineFile, oldFile, newFile string) {
	dir := filepath.Dir(fullPath)
	base := filepath.Base(fullPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var rFiles []string
	for _, e := range entries {
		name := e.Name()
		if name == base+".mine" {
			mineFile = filepath.Join(dir, name)
			continue
		}
		if strings.HasPrefix(name, base+".r") && !strings.Contains(name[len(base+".r"):], ".") {
			rFiles = append(rFiles, filepath.Join(dir, name))
		}
	}
	sort.Strings(rFiles)
	if len(rFiles) >= 2 {
		oldFile, newFile = rFiles[0], rFiles[len(rFiles)-1]
	} else if len(rFiles) == 1 {
		newFile = rFiles[0]
	}
	return
}

// ── History / file history ────────────────────────────────────────────────────

func loadHistoryCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		out, err := svn.Run(r, "log", "-v", "-l", "80")
		if err != nil {
			return model.HistoryLoadedMsg{Output: out, Title: "Commit history", Err: fmt.Errorf("svn log failed\n\nWorking copy: %s\n\nOutput:\n%s\n\nError: %w", r.Path, out, err)}
		}
		if strings.TrimSpace(out) == "" {
			out = "No commit history found."
		} else {
			out = colorizeSVNLog(out)
		}
		return model.HistoryLoadedMsg{Output: out, Title: "Commit history"}
	}
}

func searchFileHistoryMatchesCmd(r model.Repo, query string) tea.Cmd {
	return func() tea.Msg {
		items, err := searchFileHistoryMatches(r, query, 300)
		return model.FileHistoryMatchesLoadedMsg{Query: query, Items: items, Err: err}
	}
}

func searchFileHistoryMatches(r model.Repo, query string, limit int) ([]string, error) {
	query = strings.ToLower(strings.TrimSpace(filepath.ToSlash(query)))
	if query == "" {
		return nil, fmt.Errorf("empty file search query")
	}
	if limit <= 0 {
		limit = 300
	}
	ignores := svn.Ignores()
	var exact, contains []string
	err := filepath.WalkDir(r.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if ignores.HidesName(d.Name()) {
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
		} else if strings.Contains(lower, query) {
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

func loadFileHistoryCmd(r model.Repo, path string) tea.Cmd {
	return func() tea.Msg {
		out, err := svn.Run(r, "log", "-l", "80", "--", path)
		if err != nil {
			return model.HistoryLoadedMsg{Output: out, Title: "File history", Err: fmt.Errorf("svn file log failed\n\nWorking copy: %s\nFile: %s\n\nOutput:\n%s\n\nError: %w", r.Path, path, out, err)}
		}
		if strings.TrimSpace(out) == "" {
			out = "No file history found for: " + path
		} else {
			out = "File: " + path + "\n\n" + colorizeSVNLog(out)
		}
		return model.HistoryLoadedMsg{Output: out, Title: "File history"}
	}
}

// ── Revision tree ─────────────────────────────────────────────────────────────

func loadRevisionTreeCmd(r model.Repo, full bool) tea.Cmd {
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

		out, err := svn.Run(r, args...)
		if err != nil {
			return model.HistoryLoadedMsg{Output: out, Title: revisionTreeTitle(full), Err: fmt.Errorf("svn revision tree log failed\n\nWorking copy: %s\n\nOutput:\n%s\n\nError: %w", r.Path, out, err)}
		}
		tree, err := buildASCIIRevisionTree(out, r, full)
		if err != nil {
			return model.HistoryLoadedMsg{Output: out, Title: revisionTreeTitle(full), Err: err}
		}
		return model.HistoryLoadedMsg{Output: tree, Title: revisionTreeTitle(full)}
	}
}

// ── Diff commands ─────────────────────────────────────────────────────────────

func diffCmd(r model.Repo, item model.CommitItem, width int) tea.Cmd {
	return func() tea.Msg {
		out, source, err := buildSideBySideDiffSource(r, item, width)
		if err != nil {
			fallbackArgs := []string{"diff"}
			if item.IsDir {
				fallbackArgs = append(fallbackArgs, "--depth", "empty")
			}
			fallbackOut, fallbackErr := svn.Run(r, append(fallbackArgs, item.Path)...)
			if fallbackOut != "" {
				out += "\n\nUnified svn diff fallback:\n\n" + fallbackOut
			}
			if fallbackErr != nil {
				err = fmt.Errorf("%w\n\nfallback svn diff also failed: %v", err, fallbackErr)
			}
			return model.DiffLoadedMsg{Output: out, Err: fmt.Errorf("side-by-side diff failed\n\nWorking copy: %s\nPath: %s\n\nError: %w", r.Path, item.Path, err), Path: item.Path}
		}
		return model.DiffLoadedMsg{Output: out, Path: item.Path, Source: source}
	}
}

// commitDiffCmd loads the unified diff introduced by a single revision, i.e.
// what changed in that commit (svn diff -c REV compares REV-1 to REV).
func commitDiffCmd(r model.Repo, revision int) tea.Cmd {
	return func() tea.Msg {
		revStr := strconv.Itoa(revision)
		label := "r" + revStr
		out, err := svn.Run(r, "diff", "-c", revStr)
		if err != nil {
			return model.DiffLoadedMsg{Output: out, Err: fmt.Errorf("svn diff -c %s failed\n\nOutput:\n%s\n\nError: %w", revStr, out, err), Path: label}
		}
		if strings.TrimSpace(out) == "" {
			out = "No diff found for revision " + label
		} else {
			out = "Commit diff: " + label + "\n\n" + colorizeUnifiedDiff(out)
		}
		return model.DiffLoadedMsg{Output: out, Path: label}
	}
}

func remoteDiffCmd(r model.Repo, item model.CommitItem, width int) tea.Cmd {
	return func() tea.Msg {
		out, err := svn.Run(r, "diff", "-r", "BASE:HEAD", "--", item.Path)
		if err != nil {
			return model.DiffLoadedMsg{Output: out, Err: fmt.Errorf("incoming diff failed\n\nWorking copy: %s\nPath: %s\n\nOutput:\n%s\n\nError: %w", r.Path, item.Path, out, err), Path: item.Path}
		}
		if strings.TrimSpace(out) == "" {
			out = "No incoming diff found for:\n" + item.Path
		} else {
			out = "Incoming diff: BASE -> HEAD\nPath: " + item.Path + "\nStatus: " + item.Status + "\n\n" + colorizeUnifiedDiff(out)
		}
		return model.DiffLoadedMsg{Output: out, Path: item.Path}
	}
}

// ── Partial hunks ─────────────────────────────────────────────────────────────

func loadPartialHunksCmd(r model.Repo, item model.CommitItem) tea.Cmd {
	return func() tea.Msg {
		hunks, err := loadPartialHunks(r, item)
		return model.PartialHunksLoadedMsg{Item: item, Hunks: hunks, Err: err}
	}
}

func loadPartialHunks(r model.Repo, item model.CommitItem) ([]model.PartialHunk, error) {
	if item.Unversioned || strings.HasPrefix(item.Status, "?") {
		return nil, fmt.Errorf("partial commit is not available for unversioned files")
	}
	if !strings.HasPrefix(item.Status, "M") {
		return nil, fmt.Errorf("partial commit currently supports modified versioned files only")
	}
	if isLikelyDir(r, item.Path) {
		return nil, fmt.Errorf("partial commit is only supported for files")
	}
	out, err := svn.Run(r, "diff", item.Path)
	if err != nil {
		return nil, fmt.Errorf("svn diff failed for partial commit\n\nOutput:\n%s\n\nError: %w", out, err)
	}
	hunks, err := diff.ParseHunks(out)
	if err != nil {
		return nil, err
	}
	if len(hunks) == 0 {
		return nil, fmt.Errorf("no hunks found in svn diff output")
	}
	return hunks, nil
}

func partialHunkCommitCmd(r model.Repo, item model.CommitItem, hunks []model.PartialHunk, message string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		fullPath := filepath.Join(r.Path, filepath.FromSlash(item.Path))
		info, err := os.Stat(fullPath)
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}
		if info.IsDir() {
			return model.CommandResult{Output: output.String(), Err: fmt.Errorf("partial commit is only supported for files"), CurrentLocation: svn.GetCurrentLocation(r)}
		}

		workingData, err := os.ReadFile(fullPath)
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}

		baseText, err := readBaseFile(r, item.Path)
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}

		baseLines := diff.SplitLines(baseText)
		partialLines, err := diff.ApplyToBase(baseLines, hunks)
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}

		partialText := toCRLF(diff.JoinLines(partialLines, diff.HasFinalNewline(string(workingData), baseText)))
		if partialText == baseText {
			return model.CommandResult{Output: "Selected hunks do not change the file compared to SVN base.", Err: fmt.Errorf("partial commit produced no changes"), CurrentLocation: svn.GetCurrentLocation(r)}
		}

		backupDir, err := os.MkdirTemp("", "svn-tui-partial-hunk-backup-*")
		if err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}
		backupPath := filepath.Join(backupDir, filepath.Base(item.Path)+".working-backup")
		if err := os.WriteFile(backupPath, workingData, info.Mode().Perm()); err != nil {
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
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
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}

		output.WriteString("Running partial hunk commit...\n\n")
		out, err := svn.Run(r, "commit", item.Path, "-m", message)
		output.WriteString(out)
		if err != nil {
			output.WriteString("\nCommit failed. Original working copy content was restored. Backup kept at:\n  " + backupPath + "\n")
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}

		restore()
		output.WriteString("\nPartial hunk commit finished successfully.")
		output.WriteString("\nOriginal full working copy content restored.")
		output.WriteString("\nBackup kept at:\n  " + backupPath + "\n")
		return model.CommandResult{Output: output.String(), CurrentLocation: svn.GetCurrentLocation(r)}
	}
}

// ── Selection helpers ─────────────────────────────────────────────────────────

func selectedCommitItems(items []model.CommitItem) []model.CommitItem {
	var out []model.CommitItem
	for _, item := range items {
		if item.Selected && item.IncludedByParent == "" {
			out = append(out, item)
		}
	}
	return out
}

func selectedCommitPaths(items []model.CommitItem) []string {
	var out []string
	for _, item := range items {
		if item.Selected && item.IncludedByParent == "" {
			out = append(out, item.Path)
		}
	}
	return out
}

func selectedUnversionedCommitPaths(items []model.CommitItem) []string {
	var out []string
	for _, item := range items {
		if item.Selected && item.Unversioned && item.IncludedByParent == "" {
			out = append(out, item.Path)
		}
	}
	return out
}

// commitSelectionRootIndex maps a nested preview row back to the unversioned
// directory which will actually be added and committed.
func commitSelectionRootIndex(items []model.CommitItem, index int) int {
	if index < 0 || index >= len(items) || items[index].IncludedByParent == "" {
		return index
	}
	for i := range items {
		if items[i].Path == items[index].IncludedByParent && items[i].IncludedByParent == "" {
			return i
		}
	}
	return index
}

func setCommitSelection(items []model.CommitItem, index int, selected bool) {
	root := commitSelectionRootIndex(items, index)
	if root < 0 || root >= len(items) {
		return
	}
	items[root].Selected = selected
	rootPath := items[root].Path
	for i := range items {
		if items[i].IncludedByParent == rootPath {
			items[i].Selected = selected
		}
	}
}

func commitItemPaths(items []model.CommitItem) []string {
	var out []string
	for _, item := range items {
		out = append(out, item.Path)
	}
	return out
}

func unversionedItemPaths(items []model.CommitItem) []string {
	var out []string
	for _, item := range items {
		if item.Unversioned {
			out = append(out, item.Path)
		}
	}
	return out
}

func pullUpdatePaths(items []model.CommitItem) []string {
	totalFiles, selectedFiles := 0, 0
	for _, item := range items {
		if !item.IsDir {
			totalFiles++
			if item.Selected {
				selectedFiles++
			}
		}
	}
	if selectedFiles == 0 {
		return nil
	}
	if selectedFiles == totalFiles {
		return []string{}
	}
	var paths []string
	i := 0
	for i < len(items) {
		item := items[i]
		if item.IsDir {
			dirPath := strings.TrimSuffix(item.Path, "/")
			j := i + 1
			allSel := true
			anySel := false
			var childPaths []string
			for j < len(items) && !items[j].IsDir {
				if items[j].Selected {
					anySel = true
					childPaths = append(childPaths, items[j].Path)
				} else {
					allSel = false
				}
				j++
			}
			if allSel && len(childPaths) > 0 {
				paths = append(paths, dirPath)
			} else if anySel {
				paths = append(paths, childPaths...)
			}
			i = j
		} else {
			if item.Selected {
				paths = append(paths, item.Path)
			}
			i++
		}
	}
	return paths
}

// ── CRLF helpers ──────────────────────────────────────────────────────────────

// ensureCRLFFile converts a working-copy file's line endings to CRLF in place.
// Returns (true, nil) if the file was converted, (false, nil) if already CRLF or binary.
func ensureCRLFFile(r model.Repo, relPath string) (bool, error) {
	fullPath := filepath.Join(r.Path, filepath.FromSlash(relPath))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return false, err
	}
	if isBinaryContent(data) {
		return false, nil
	}
	converted := toCRLF(string(data))
	if converted == string(data) {
		return false, nil
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(fullPath, []byte(converted), info.Mode().Perm())
}

// isBinaryContent returns true if data contains a null byte (heuristic for binary files).
func isBinaryContent(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// ── XML (needed for cmds.go) ─────────────────────────────────────────────────
// xml imported at top of file

// ── Properties ────────────────────────────────────────────────────────────────

func searchPropertyTargetsCmd(r model.Repo, query string) tea.Cmd {
	return func() tea.Msg {
		items, err := searchPropertyTargets(r, query, 300)
		return model.PropertyTargetsLoadedMsg{Query: query, Items: items, Err: err}
	}
}

// searchPropertyTargets finds versioned directories and files whose path
// contains the query. Directories come first: properties live on them far more
// often than on files.
func searchPropertyTargets(r model.Repo, query string, limit int) ([]string, error) {
	query = strings.ToLower(strings.TrimSpace(filepath.ToSlash(query)))
	if limit <= 0 {
		limit = 300
	}
	ignores := svn.Ignores()
	var dirs, files []string
	err := filepath.WalkDir(r.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && ignores.HidesName(d.Name()) {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(r.Path, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || ignores.HidesPath(rel) {
			return nil
		}
		if !strings.Contains(strings.ToLower(rel), query) {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, rel)
		} else {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	sort.Strings(files)
	// "." is the merge target and the most common property holder, so it leads.
	items := append([]string{"."}, append(dirs, files...)...)
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func loadPropertiesCmd(r model.Repo, target string, notice string) tea.Cmd {
	return func() tea.Msg {
		items, err := loadProperties(r, target)
		return model.PropertiesLoadedMsg{Target: target, Items: items, Err: err, Notice: notice}
	}
}

func loadProperties(r model.Repo, target string) ([]model.PropertyItem, error) {
	out, err := svn.Run(r, "proplist", "-v", "--xml", target)
	if err != nil {
		return nil, fmt.Errorf("svn proplist failed\n\nWorking copy: %s\nTarget: %s\n\nOutput:\n%s\n\nError: %w", r.Path, target, out, err)
	}
	var parsed model.SVNPropListXML
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		return nil, fmt.Errorf("could not parse svn proplist output for %s: %w\n\nOutput:\n%s", target, err, out)
	}
	var items []model.PropertyItem
	for _, t := range parsed.Targets {
		for _, prop := range t.Properties {
			items = append(items, model.PropertyItem{Name: prop.Name, Value: prop.Value})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

// setPropertyCmd writes one property and reloads the list so the result is
// visible straight away.
func setPropertyCmd(r model.Repo, target, name, value string) tea.Cmd {
	return func() tea.Msg {
		out, err := svn.Run(r, "propset", name, value, target)
		if err != nil {
			return model.PropertiesLoadedMsg{
				Target: target,
				Err:    fmt.Errorf("svn propset %s failed\n\nTarget: %s\n\nOutput:\n%s\n\nError: %w", name, target, out, err),
			}
		}
		items, err := loadProperties(r, target)
		return model.PropertiesLoadedMsg{Target: target, Items: items, Err: err, Notice: "Set " + name + " on " + target}
	}
}

func deletePropertyCmd(r model.Repo, target, name string) tea.Cmd {
	return func() tea.Msg {
		out, err := svn.Run(r, "propdel", name, target)
		if err != nil {
			return model.PropertiesLoadedMsg{
				Target: target,
				Err:    fmt.Errorf("svn propdel %s failed\n\nTarget: %s\n\nOutput:\n%s\n\nError: %w", name, target, out, err),
			}
		}
		items, err := loadProperties(r, target)
		return model.PropertiesLoadedMsg{Target: target, Items: items, Err: err, Notice: "Deleted " + name + " from " + target}
	}
}

// expandPropertyValue turns a typed "\n" into a real newline: svn:ignore and
// svn:mergeinfo are line-based, and the input field is single-line.
func expandPropertyValue(value string) string {
	return strings.ReplaceAll(value, `\n`, "\n")
}

// collapsePropertyValue is the inverse, for prefilling the input when editing.
// SVN appends a trailing newline to line-based values, so it is trimmed first:
// without that, every edit round trip would grow an extra blank line.
func collapsePropertyValue(value string) string {
	return strings.ReplaceAll(strings.TrimRight(value, "\n"), "\n", `\n`)
}
