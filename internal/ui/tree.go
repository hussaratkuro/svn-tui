package ui

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"svn-tui/internal/model"
	"svn-tui/internal/svn"
)

func buildASCIIRevisionTree(xmlOut string, r model.Repo, full bool) (string, error) {
	var parsed model.SVNLogXML
	if err := xml.Unmarshal([]byte(xmlOut), &parsed); err != nil {
		return "", fmt.Errorf("failed to parse svn log XML: %w", err)
	}
	if len(parsed.Entries) == 0 {
		return "No revision tree entries found.", nil
	}

	tree := buildRevisionBranchGraph(parsed.Entries)

	var b strings.Builder
	b.WriteString(labelMauveStyle.Render("Revision tree") + "\n")
	b.WriteString(labelYellowStyle.Render("Repository root: ") + valueWhiteStyle.Render(svn.FirstNonEmpty(r.Root, r.URL, r.Path)) + "\n")
	b.WriteString(labelYellowStyle.Render("Mode: ") + valueWhiteStyle.Render(revisionTreeModeText(full)) + "\n")
	if !full {
		b.WriteString(mutedStyle.Render("Showing newest 250 log entries. Press 'a' to load full history.") + "\n")
	}
	b.WriteString(mutedStyle.Render("Branch/tag structure only. File-level changes are in Commit history.") + "\n\n")

	if len(tree.Nodes) == 0 {
		b.WriteString("No trunk/branch/tag activity found in the loaded log entries.")
		return strings.TrimRight(b.String(), "\n"), nil
	}

	roots := tree.Roots()
	sort.SliceStable(roots, func(i, j int) bool {
		left := tree.Nodes[roots[i]]
		right := tree.Nodes[roots[j]]
		if left == nil || right == nil {
			return roots[i] < roots[j]
		}
		if left.LastRev == right.LastRev {
			return left.Path < right.Path
		}
		return left.LastRev > right.LastRev
	})

	for i, root := range roots {
		renderRevisionTreeNode(&b, tree, root, "", i == len(roots)-1)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func buildRevisionBranchGraph(entries []model.SVNLogEntryXML) model.RevisionBranchGraph {
	graph := model.RevisionBranchGraph{Nodes: map[string]*model.RevisionBranchNode{}}

	ensure := func(path, kind string) *model.RevisionBranchNode {
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
		node := &model.RevisionBranchNode{
			Path: path, Name: revisionTreeDisplayName(path),
			Kind: kind, CommitRevs: map[int]bool{},
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
		for _, cp := range entry.Paths {
			root, kind := revisionTreeRootPath(cp.Path)
			if root == "" {
				continue
			}
			node := ensure(root, kind)
			entryRoots[root] = true

			if entry.Revision > node.LastRev {
				node.LastRev = entry.Revision
			}

			action := strings.ToUpper(strings.TrimSpace(cp.Action))
			changedRoot := normalizeSVNTreePath(cp.Path)
			if action == "A" && changedRoot == root {
				if node.CreatedRev == 0 || entry.Revision < node.CreatedRev {
					node.CreatedRev = entry.Revision
					node.CreatedDate = formatSVNLogDate(entry.Date)
				}
				if strings.TrimSpace(cp.CopyFromPath) != "" {
					copyRoot, _ := revisionTreeRootPath(cp.CopyFromPath)
					if copyRoot == "" {
						copyRoot = normalizeSVNTreePath(cp.CopyFromPath)
					}
					node.CreatedFrom = copyRoot
					node.CreatedFromRev = cp.CopyFromRev
					ensure(copyRoot, svnTreePathKind(copyRoot))
				}
			}
			if action == "D" && changedRoot == root {
				node.DeletedRev = entry.Revision
			}
		}

		for root := range entryRoots {
			node := ensure(root, svnTreePathKind(root))
			if !node.CommitRevs[entry.Revision] {
				node.CommitRevs[entry.Revision] = true
				node.Commits = append(node.Commits, model.RevisionBranchCommit{
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
		if path == "/trunk" {
			continue
		}
		parent := strings.TrimSpace(node.CreatedFrom)
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
			l, r := graph.Nodes[node.Children[i]], graph.Nodes[node.Children[j]]
			if l == nil || r == nil {
				return node.Children[i] < node.Children[j]
			}
			if l.LastRev == r.LastRev {
				return l.Path < r.Path
			}
			return l.LastRev > r.LastRev
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

func detectRevisionMergeBacks(graph model.RevisionBranchGraph, entry model.SVNLogEntryXML, entryRoots map[string]bool) {
	msg := strings.ToLower(compactOneLine(entry.Msg))
	if msg == "" {
		return
	}
	mergeWords := []string{"merge", "merged", "mergel", "visszamerge", "vissza merge", "vissza lett mergelve"}
	hasMergeWord := false
	for _, w := range mergeWords {
		if strings.Contains(msg, w) {
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
		if branchName == "" {
			continue
		}
		if !strings.Contains(msg, branchName) && !branchNameWordsMatchMsg(branchName, msg) {
			continue
		}
		for target := range entryRoots {
			if target == sourcePath {
				continue
			}
			sourceNode.MergeBacks = append(sourceNode.MergeBacks, model.RevisionMergeBack{
				Target: target,
				Rev:    entry.Revision,
				Msg:    compactOneLine(entry.Msg),
			})
		}
	}
}

// branchNameWordsMatchMsg returns true if any significant word from the branch
// name appears in the commit message. Words shorter than 4 chars or purely
// numeric (e.g. date segments like "2026", "05") are skipped to avoid noise.
func branchNameWordsMatchMsg(branchName, msg string) bool {
	parts := strings.FieldsFunc(branchName, func(r rune) bool {
		return r == '-' || r == '_'
	})
	for _, part := range parts {
		if len(part) < 4 {
			continue
		}
		allDigits := true
		for _, c := range part {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			continue
		}
		if strings.Contains(msg, part) {
			return true
		}
	}
	return false
}

func renderRevisionTreeNode(b *strings.Builder, graph model.RevisionBranchGraph, path, prefix string, last bool) {
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

func revisionTreeRenderItems(graph model.RevisionBranchGraph, node *model.RevisionBranchNode) []model.RevisionTreeRenderItem {
	items := make([]model.RevisionTreeRenderItem, 0, len(node.Commits)+len(node.Children)+len(node.MergeBacks))

	for _, commit := range node.Commits {
		items = append(items, model.RevisionTreeRenderItem{
			Kind: "commit", Revision: commit.Revision, SortLabel: commit.Msg, Commit: commit,
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
		items = append(items, model.RevisionTreeRenderItem{
			Kind: "child", Revision: rev, SortLabel: child.Path, ChildPath: childPath,
		})
	}
	for _, merge := range node.MergeBacks {
		items = append(items, model.RevisionTreeRenderItem{
			Kind: "merge", Revision: merge.Rev, SortLabel: merge.Target, Merge: merge,
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
	default:
		return 2
	}
}

func renderRevisionTreeMergeBack(b *strings.Builder, merge model.RevisionMergeBack, prefix string, last bool) {
	con := "├⤴"
	if last {
		con = "└⤴"
	}
	line := prefix + labelMauveStyle.Render(con) + " " + labelSapphireStyle.Render("merged back") + " " + labelMauveStyle.Render("→") + " " + valueWhiteStyle.Render(merge.Target)
	if merge.Rev > 0 {
		line += " " + successStyle.Render(fmt.Sprintf("r%d", merge.Rev))
	}
	if strings.TrimSpace(merge.Msg) != "" {
		line += " " + actionStyle.Render(compactOneLine(merge.Msg))
	}
	b.WriteString(line + "\n")
}

func renderRevisionTreeCommit(b *strings.Builder, commit model.RevisionBranchCommit, prefix string, last bool) {
	con := "├•"
	if last {
		con = "└•"
	}
	line := prefix + labelMauveStyle.Render(con) + " " + successStyle.Render(fmt.Sprintf("r%d", commit.Revision))
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

func renderRevisionTreeNodeLabel(node *model.RevisionBranchNode) string {
	label := labelRedStyle.Render(node.Path)
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

func compactOneLine(s string) string {
	fields := strings.Fields(strings.ReplaceAll(s, "\x00", " "))
	return strings.Join(fields, " ")
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
