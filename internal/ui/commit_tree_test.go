package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"svn-tui/internal/model"
)

func TestExpandUnversionedCommitDirectories(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Workspace", "eHAZ", "Web", "eHAZ", "_cron", "szamla-backfill")
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib", "job.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	parentPath := "Workspace/eHAZ/Web/eHAZ/_cron/szamla-backfill"
	items, err := expandUnversionedCommitDirectories(model.Repo{Path: root}, []model.CommitItem{{
		Status: "?", Path: parentPath, Unversioned: true, IsDir: true,
	}})
	if err != nil {
		t.Fatal(err)
	}

	var paths []string
	for _, item := range items {
		paths = append(paths, item.Path)
		if item.Path != parentPath && item.IncludedByParent != parentPath {
			t.Fatalf("child %q has IncludedByParent %q, want %q", item.Path, item.IncludedByParent, parentPath)
		}
	}
	want := []string{
		parentPath,
		parentPath + "/lib",
		parentPath + "/lib/job.php",
		parentPath + "/run.php",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("expanded paths = %#v, want %#v", paths, want)
	}
}

func TestCommitDirectoryChildrenUseParentSelection(t *testing.T) {
	items := []model.CommitItem{
		{Path: "new-dir", Unversioned: true, IsDir: true},
		{Path: "new-dir/file.txt", Unversioned: true, IncludedByParent: "new-dir"},
	}

	setCommitSelection(items, 1, true)
	if !items[0].Selected || !items[1].Selected {
		t.Fatalf("selecting child did not select its directory: %#v", items)
	}
	selected := selectedCommitItems(items)
	if len(selected) != 1 || selected[0].Path != "new-dir" {
		t.Fatalf("selected commit items = %#v, want parent directory only", selected)
	}

	setCommitSelection(items, 1, false)
	if items[0].Selected || items[1].Selected {
		t.Fatalf("unselecting child did not unselect its directory: %#v", items)
	}
}

func TestCommitTreeItemLabel(t *testing.T) {
	parent := "Workspace/eHAZ/Web/eHAZ/_cron/szamla-backfill"
	tests := []struct {
		item model.CommitItem
		want string
	}{
		{item: model.CommitItem{Path: parent, IsDir: true}, want: parent + "/"},
		{item: model.CommitItem{Path: parent + "/run.php", IncludedByParent: parent}, want: "  • run.php"},
		{item: model.CommitItem{Path: parent + "/lib", IsDir: true, IncludedByParent: parent}, want: "  • lib/"},
		{item: model.CommitItem{Path: parent + "/lib/job.php", IncludedByParent: parent}, want: "    • job.php"},
	}
	for _, tt := range tests {
		if got := commitTreeItemLabel(tt.item); got != tt.want {
			t.Errorf("commitTreeItemLabel(%q) = %q, want %q", tt.item.Path, got, tt.want)
		}
	}
}

func TestExpandedUnversionedFileCanBeDiffed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "new-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new-dir", "file.txt"), []byte("first line\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	item := model.CommitItem{
		Status: "?", Path: "new-dir/file.txt", Unversioned: true, IncludedByParent: "new-dir",
	}
	output, err := buildSideBySideDiff(model.Repo{Path: root}, item, 80)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stripANSI(output), "first line") {
		t.Fatalf("expanded file diff does not contain file content:\n%s", output)
	}
}

func TestCommitViewShowsExpandedDirectoryAsNestedList(t *testing.T) {
	parent := "Workspace/eHAZ/Web/eHAZ/_cron/szamla-backfill"
	m := Model{
		width: 100, height: 30,
		commitItems: []model.CommitItem{
			{Status: "?", Path: parent, Unversioned: true, IsDir: true},
			{Status: "?", Path: parent + "/run.php", Unversioned: true, IncludedByParent: parent},
		},
	}
	view := stripANSI(m.viewCommitSelect())
	if !strings.Contains(view, parent+"/") || !strings.Contains(view, "• run.php") {
		t.Fatalf("commit view does not show the expanded directory tree:\n%s", view)
	}
}
