package ui

import (
	"reflect"
	"strings"
	"testing"

	"svn-tui/internal/model"
)

func TestBranchMergeRevisionsFromLog(t *testing.T) {
	entries := []model.SVNLogEntryXML{
		{Revision: 21, Author: "alice", Date: "2026-09-07T10:30:00Z", Msg: "Fix the thing\nwith detail"},
		{Revision: 10, Author: "creator", Msg: "Create branch"},
		{Revision: 18, Author: "bob", Msg: "Add tests"},
	}

	got := branchMergeRevisionsFromLog(entries)
	if len(got) != 2 {
		t.Fatalf("branchMergeRevisionsFromLog() returned %d rows, want 2", len(got))
	}
	if got[0].Revision != 21 || got[0].Msg != "Fix the thing with detail" {
		t.Fatalf("first revision = %#v, want compacted r21", got[0])
	}
	if got[1].Revision != 18 || got[1].Msg != "Add tests" {
		t.Fatalf("second revision = %#v, want r18", got[1])
	}
}

func TestViewBranchMergeSelectShowsLatestActionAndCommitMessages(t *testing.T) {
	m := Model{
		screen:      model.ScreenBranchMergeSelect,
		width:       100,
		height:      30,
		mergeBranch: model.Branch{Name: "feature-one"},
		mergeRevisions: []model.BranchMergeRevision{
			{Revision: 42, Author: "alice", Date: "2026-09-07 12:30", Msg: "Useful commit message"},
		},
	}

	view := stripANSI(m.viewBranchMergeSelect())
	for _, want := range []string{
		"Merge latest branch state through HEAD",
		"r42  alice  2026-09-07 12:30",
		"Useful commit message",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("merge picker does not contain %q:\n%s", want, view)
		}
	}
}

func TestBranchMergeArgs(t *testing.T) {
	const branchURL = "https://svn.example.test/repo/branches/feature"

	t.Run("single revision", func(t *testing.T) {
		want := []string{"merge", "-c", "42", branchURL + "@HEAD", "."}
		if got := branchMergeArgs(branchURL, "42", 0); !reflect.DeepEqual(got, want) {
			t.Fatalf("branchMergeArgs() = %#v, want %#v", got, want)
		}
	})

	t.Run("whole branch", func(t *testing.T) {
		want := []string{"merge", "-r", "17:HEAD", branchURL + "@HEAD", "."}
		if got := branchMergeArgs(branchURL, "", 17); !reflect.DeepEqual(got, want) {
			t.Fatalf("branchMergeArgs() = %#v, want %#v", got, want)
		}
	})
}
