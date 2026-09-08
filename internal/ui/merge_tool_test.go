package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"svn-tui/internal/model"
)

func TestFindMergeToolUsesPATH(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "merger")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := findMergeTool("merger")
	if err != nil {
		t.Fatal(err)
	}
	if got != tool {
		t.Fatalf("findMergeTool() = %q, want %q", got, tool)
	}
}

func TestPrepareAndResolveConflictWithRealSVNWorkingCopy(t *testing.T) {
	if _, err := exec.LookPath("svn"); err != nil {
		t.Skip("svn is not installed")
	}
	if _, err := exec.LookPath("svnadmin"); err != nil {
		t.Skip("svnadmin is not installed")
	}

	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	mineWC := filepath.Join(root, "mine-wc")
	theirsWC := filepath.Join(root, "theirs-wc")
	run := func(dir, name string, args ...string) string {
		t.Helper()
		command := exec.Command(name, args...)
		command.Dir = dir
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
		}
		return string(output)
	}

	run(root, "svnadmin", "create", repository)
	repositoryURL := "file://" + filepath.ToSlash(repository)
	run(root, "svn", "checkout", repositoryURL, mineWC)
	conflicted := filepath.Join(mineWC, "conflicted.txt")
	if err := os.WriteFile(conflicted, []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(mineWC, "svn", "add", "conflicted.txt")
	run(mineWC, "svn", "commit", "-m", "base")
	run(root, "svn", "checkout", repositoryURL, theirsWC)
	if err := os.WriteFile(conflicted, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theirsWC, "conflicted.txt"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(theirsWC, "svn", "commit", "-m", "theirs")
	run(mineWC, "svn", "update", "--accept=postpone")

	toolDir := filepath.Join(root, "bin")
	if err := os.Mkdir(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(toolDir, "merger")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	command := prepareDefaultConflictToolCmd(model.Repo{Path: mineWC}, "conflicted.txt")
	prepared, ok := command().(conflictToolPreparedMsg)
	if !ok {
		t.Fatal("prepare command returned an unexpected message")
	}
	if prepared.err != nil {
		t.Fatal(prepared.err)
	}
	if prepared.tool != defaultConflictMergeTool || prepared.executable != tool || prepared.result != conflicted {
		t.Fatalf("prepared default tool/executable/result = %q / %q / %q", prepared.tool, prepared.executable, prepared.result)
	}
	for _, path := range []string{prepared.mine, prepared.base, prepared.theirs} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("prepared conflict input %q is unavailable: %v", path, err)
		}
	}
	if got, want := prepared.args, []string{prepared.mine, prepared.base, prepared.theirs, "--output=" + conflicted}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("prepared args = %#v, want %#v", got, want)
	}

	result, ok := resolveConflictAfterToolCmd(prepared)().(model.CommandResult)
	if !ok || result.Err != nil {
		t.Fatalf("resolve result = %#v", result)
	}
	status := run(mineWC, "svn", "status", "conflicted.txt")
	if strings.Contains(status, "C") {
		t.Fatalf("conflict remained after resolve:\n%s", status)
	}
}

func TestConflictToolCancellationUsesMergerExitCode(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 2").Run()
	if !conflictToolCancelled(err) {
		t.Fatalf("exit 2 was not recognized as cancellation: %v", err)
	}
	err = exec.Command("sh", "-c", "exit 1").Run()
	if conflictToolCancelled(err) {
		t.Fatalf("exit 1 was incorrectly recognized as cancellation: %v", err)
	}
	if conflictToolCancelled(errors.New("not an exit error")) {
		t.Fatal("ordinary error was incorrectly recognized as cancellation")
	}
}

func TestCancelledMergerLeavesConflictScreenOpen(t *testing.T) {
	m := Model{screen: model.ScreenRunning}
	err := exec.Command("sh", "-c", "exit 2").Run()
	updated, cmd := m.Update(conflictToolExitedMsg{
		prepared: conflictToolPreparedMsg{tool: "merger", path: "conflicted.txt"},
		err:      err,
	})
	got := updated.(Model)
	if cmd != nil || got.screen != model.ScreenConflictSelect {
		t.Fatalf("cancel result: screen=%v cmd=%v", got.screen, cmd)
	}
	if !strings.Contains(got.conflictNotice, "still unresolved") {
		t.Fatalf("cancel notice = %q", got.conflictNotice)
	}
}

func TestConflictViewOffersMergerAsDefaultAndMeldAsFallback(t *testing.T) {
	m := Model{
		screen:        model.ScreenConflictSelect,
		width:         120,
		height:        30,
		conflictItems: []model.ConflictItem{{Status: "C", Path: "file.txt"}},
	}
	view := stripANSI(m.viewConflictSelect())
	for _, want := range []string{"Merger (default, Enter)", "Meld (M)", "Enter:Merger (default)", "M:Meld"} {
		if !strings.Contains(view, want) {
			t.Fatalf("conflict view does not contain %q:\n%s", want, view)
		}
	}
}
