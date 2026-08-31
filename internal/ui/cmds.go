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

func mergeBranchCmd(r model.Repo, branchName string) tea.Cmd {
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

		err := streamSVN("merge", "-r", revisionRange, branchURL+"@HEAD", ".")
		if err == nil {
			line("")
			line("Branch " + branchName + " merged successfully. Review changes, then commit.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
	})
}

func branchStartRevision(r model.Repo, branchURL string) (int, string, error) {
	out, err := svn.Run(r, "log", "--xml", "--stop-on-copy", "-v", branchURL)
	if err != nil {
		return 0, out, err
	}
	var parsed model.SVNLogXML
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		return 0, "", err
	}
	if len(parsed.Entries) == 0 {
		return 0, "", fmt.Errorf("no svn log entries found for branch")
	}
	oldest := parsed.Entries[0].Revision
	for _, entry := range parsed.Entries {
		if entry.Revision > 0 && (oldest == 0 || entry.Revision < oldest) {
			oldest = entry.Revision
		}
	}
	if oldest <= 0 {
		return 0, "", fmt.Errorf("invalid branch start revision")
	}
	return oldest, "", nil
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
		items, err := loadCommitItems(r, true)
		if err == nil {
			items = filterSelectFilesOnly(r, items)
		}
		return model.CommitItemsLoadedMsg{Items: items, Err: err}
	}
}

func loadRevertItemsCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		items, err := loadCommitItems(r, false)
		if err == nil {
			items = filterSelectFilesOnly(r, items)
		}
		return model.RevertItemsLoadedMsg{Items: items, Err: err}
	}
}

func loadShelveItemsCmd(r model.Repo) tea.Cmd {
	return func() tea.Msg {
		items, err := loadCommitItems(r, true)
		return model.RevertItemsLoadedMsg{Items: items, Err: err}
	}
}

func loadCommitItems(r model.Repo, includeUnversioned bool) ([]model.CommitItem, error) {
	out, err := svn.Run(r, "status")
	if err != nil {
		return nil, fmt.Errorf("svn status failed\n\nWorking copy: %s\n\nOutput:\n%s\n\nError: %w", r.Path, out, err)
	}
	var items []model.CommitItem
	for _, line := range strings.Split(out, "\n") {
		item, ok := parseSVNLocalChangeStatusLine(r, line, includeUnversioned)
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
	return items, nil
}

func parseSVNLocalChangeStatusLine(r model.Repo, line string, includeUnversioned bool) (model.CommitItem, bool) {
	if strings.TrimSpace(line) == "" {
		return model.CommitItem{}, false
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
	if path == "" || shouldHideFromCommitSelect(path) {
		return model.CommitItem{}, false
	}
	unversioned := textStatus == '?'
	if unversioned && !includeUnversioned {
		return model.CommitItem{}, false
	}
	textChanged := strings.ContainsRune("MADRC!~", rune(textStatus))
	propsChanged := propStatus == 'M' || propStatus == 'C'
	if !unversioned && !textChanged && !propsChanged {
		return model.CommitItem{}, false
	}
	status := strings.TrimSpace(line[:min(len(line), 8)])
	if status == "" {
		status = string(textStatus)
	}
	return model.CommitItem{
		Status:      status,
		Path:        path,
		Unversioned: unversioned,
		IsDir:       isSVNDir(r, path),
	}, true
}

// shouldHideFromCommitSelect drops paths that must never reach the commit list:
// the shelf store plus everything named in ~/.config/svn-tui/ignore.txt.
func shouldHideFromCommitSelect(path string) bool {
	clean := strings.TrimPrefix(strings.TrimSpace(filepath.ToSlash(path)), "./")
	if clean == "." || clean == model.ShelvesDir || strings.HasPrefix(clean, model.ShelvesDir+"/") {
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

func filterSelectFilesOnly(r model.Repo, items []model.CommitItem) []model.CommitItem {
	filtered := make([]model.CommitItem, 0, len(items))
	for _, item := range items {
		isDir := item.IsDir || isSVNDir(r, item.Path)
		item.IsDir = isDir
		if isDir && !item.Unversioned && !isScheduledDirChange(item) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
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
		for _, p := range paths {
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

		line("")
		line("Running commit...")
		line("")

		args := append([]string{"commit"}, paths...)
		args = append(args, "-m", message)

		err := svn.StreamLines(r, func(raw string) { output.WriteString(raw + "\n"); emit(raw) }, args...)
		if err == nil {
			line("")
			line("Commit finished successfully.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
	})
}

func revertCmd(r model.Repo, paths []string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder
		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Selected files to revert:\n")
		for _, p := range paths {
			output.WriteString("  " + p + "\n")
		}
		output.WriteString("\nRunning revert...\n\n")
		out, err := svn.Run(r, append([]string{"revert"}, paths...)...)
		output.WriteString(out)
		if err == nil {
			output.WriteString("\nRevert finished successfully.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
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

func shelveChangesCmd(r model.Repo, items []model.CommitItem) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder

		selected := selectedCommitItems(items)
		if len(selected) == 0 {
			return model.CommandResult{Output: "Select at least one file.", Err: fmt.Errorf("no files selected"), CurrentLocation: svn.GetCurrentLocation(r), URL: svn.GetCurrentURL(r)}
		}

		shelfName := "auto-" + time.Now().Format("20060102-150405")
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

		if err := os.MkdirAll(filesDir, 0700); err != nil {
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

func resolveConflictWithMeldCmd(r model.Repo, path string) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder
		output.WriteString("Working copy: " + r.Path + "\n")
		output.WriteString("Conflicted path: " + path + "\n\n")

		infoOut, _ := svn.Run(r, "info", path)

		if strings.Contains(strings.ToLower(infoOut), "tree conflict") {
			output.WriteString("This is a tree conflict. Meld cannot resolve SVN tree conflicts automatically.\n\n")
			output.WriteString("Resolution strategy: keeping the current working copy version.\n\n")
			output.WriteString("SVN info:\n" + infoOut + "\n\n")
			output.WriteString("Running: svn resolve --accept=working " + path + "\n\n")
			resolveOut, err := svn.Run(r, "resolve", "--accept=working", path)
			output.WriteString(resolveOut)
			if err == nil {
				output.WriteString("\nTree conflict marked as resolved.")
			}
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}

		if _, err := exec.LookPath("meld"); err != nil {
			output.WriteString("Meld was not found in PATH.\nInstall it: sudo pacman -S meld\n")
			return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
		}

		mineFile, oldFile, newFile := parseSVNConflictFiles(infoOut, r.Path, path)
		if mineFile == "" || oldFile == "" || newFile == "" {
			fullPath := filepath.Join(r.Path, filepath.FromSlash(path))
			mineFile, oldFile, newFile = guessSVNConflictFiles(fullPath)
		}
		if mineFile == "" || oldFile == "" || newFile == "" {
			output.WriteString("Could not locate SVN conflict files (.mine, .rOLD, .rNEW).\n\nSVN info:\n" + infoOut)
			return model.CommandResult{Output: output.String(), Err: fmt.Errorf("conflict files not found"), CurrentLocation: svn.GetCurrentLocation(r)}
		}

		fullPath := filepath.Join(r.Path, filepath.FromSlash(path))
		output.WriteString("Launching Meld with conflict files...\n\n")
		output.WriteString("  Local (mine):    " + mineFile + "\n")
		output.WriteString("  Base (old rev):  " + oldFile + "\n")
		output.WriteString("  Incoming (new):  " + newFile + "\n")
		output.WriteString("  Result:          " + fullPath + "\n\n")

		cmd := exec.Command("meld", mineFile, oldFile, newFile, "--output="+fullPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			output.WriteString("\nMeld exited with an error.\n")
			return model.CommandResult{Output: output.String(), Err: fmt.Errorf("meld exited with error: %w", err), CurrentLocation: svn.GetCurrentLocation(r)}
		}

		output.WriteString("\nMeld closed. Marking file as resolved using working copy version...\n\n")
		output.WriteString("Running: svn resolve --accept=working " + path + "\n\n")

		resolveOut, err := svn.Run(r, "resolve", "--accept=working", path)
		output.WriteString(resolveOut)
		if err == nil {
			output.WriteString("\nConflict resolved successfully.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(r)}
	}
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
		out, err := buildSideBySideDiff(r, item, width)
		if err != nil {
			fallbackOut, fallbackErr := svn.Run(r, "diff", item.Path)
			if fallbackOut != "" {
				out += "\n\nUnified svn diff fallback:\n\n" + fallbackOut
			}
			if fallbackErr != nil {
				err = fmt.Errorf("%w\n\nfallback svn diff also failed: %v", err, fallbackErr)
			}
			return model.DiffLoadedMsg{Output: out, Err: fmt.Errorf("side-by-side diff failed\n\nWorking copy: %s\nPath: %s\n\nError: %w", r.Path, item.Path, err), Path: item.Path}
		}
		return model.DiffLoadedMsg{Output: out, Path: item.Path}
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
		if item.Selected {
			out = append(out, item)
		}
	}
	return out
}

func selectedCommitPaths(items []model.CommitItem) []string {
	var out []string
	for _, item := range items {
		if item.Selected {
			out = append(out, item.Path)
		}
	}
	return out
}

func selectedUnversionedCommitPaths(items []model.CommitItem) []string {
	var out []string
	for _, item := range items {
		if item.Selected && item.Unversioned {
			out = append(out, item.Path)
		}
	}
	return out
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
