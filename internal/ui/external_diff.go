package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"svn-tui/internal/model"
	"svn-tui/internal/svn"
)

type readOnlyDiffPreparedMsg struct {
	executable   string
	args         []string
	tempDir      string
	returnScreen model.Screen
	err          error
}

type readOnlyDiffExitedMsg struct {
	returnScreen model.Screen
	err          error
}

func prepareWorkingCopyMergerCmd(r model.Repo, item model.CommitItem, returnScreen model.Screen) tea.Cmd {
	return func() tea.Msg {
		left, right, err := workingCopyDiffSides(r, item)
		if err != nil {
			return readOnlyDiffPreparedMsg{returnScreen: returnScreen, err: err}
		}
		return prepareMergerFiles(left, right, "BASE", "WORKING", item.Path, returnScreen)
	}
}

func prepareIncomingMergerCmd(r model.Repo, item model.CommitItem, returnScreen model.Screen) tea.Cmd {
	return func() tea.Msg {
		left, leftErr := svn.Cat(r, "-r", "BASE", "--", item.Path)
		right, rightErr := svn.Cat(r, "-r", "HEAD", "--", item.Path)
		if leftErr != nil && !strings.HasPrefix(item.Status, "A") {
			return readOnlyDiffPreparedMsg{returnScreen: returnScreen, err: fmt.Errorf("read BASE %s: %w", item.Path, leftErr)}
		}
		if rightErr != nil && !strings.HasPrefix(item.Status, "D") {
			return readOnlyDiffPreparedMsg{returnScreen: returnScreen, err: fmt.Errorf("read HEAD %s: %w", item.Path, rightErr)}
		}
		return prepareMergerFiles(left, right, "BASE", "HEAD", item.Path, returnScreen)
	}
}

func prepareBranchMergerCmd(r model.Repo, ctx model.BranchDiffContext, item model.BranchDiffItem, returnScreen model.Screen) tea.Cmd {
	return func() tea.Msg {
		var left, right []byte
		var leftErr, rightErr error
		if !strings.HasPrefix(item.Status, "A") {
			left, leftErr = svn.Cat(r, ctx.Old.PathTarget(item.Path))
		}
		if !strings.HasPrefix(item.Status, "D") {
			right, rightErr = svn.Cat(r, ctx.New.PathTarget(item.Path))
		}
		if leftErr != nil {
			return readOnlyDiffPreparedMsg{returnScreen: returnScreen, err: fmt.Errorf("read %s: %w", ctx.Old.Label, leftErr)}
		}
		if rightErr != nil {
			return readOnlyDiffPreparedMsg{returnScreen: returnScreen, err: fmt.Errorf("read %s: %w", ctx.New.Label, rightErr)}
		}
		return prepareMergerFiles(left, right, ctx.Old.Label, ctx.New.Label, item.Path, returnScreen)
	}
}

func workingCopyDiffSides(r model.Repo, item model.CommitItem) ([]byte, []byte, error) {
	var left, right []byte
	var err error
	status := strings.TrimSpace(item.Status)
	if !item.Unversioned && !strings.HasPrefix(status, "?") && !strings.HasPrefix(status, "A") {
		left, err = svn.Cat(r, "-r", "BASE", "--", item.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("read BASE %s: %w", item.Path, err)
		}
	}
	if !strings.HasPrefix(status, "D") {
		right, err = os.ReadFile(filepath.Join(r.Path, filepath.FromSlash(item.Path)))
		if err != nil {
			return nil, nil, fmt.Errorf("read working copy %s: %w", item.Path, err)
		}
	}
	return left, right, nil
}

func prepareMergerFiles(left, right []byte, leftLabel, rightLabel, name string, returnScreen model.Screen) readOnlyDiffPreparedMsg {
	executable, err := findMergeTool(defaultConflictMergeTool)
	if err != nil {
		return readOnlyDiffPreparedMsg{returnScreen: returnScreen, err: err}
	}
	tempDir, err := os.MkdirTemp("", "svntui-diff-")
	if err != nil {
		return readOnlyDiffPreparedMsg{returnScreen: returnScreen, err: err}
	}
	baseName := filepath.Base(filepath.FromSlash(name))
	if baseName == "." || baseName == string(filepath.Separator) || baseName == "" {
		baseName = "content"
	}
	leftPath := filepath.Join(tempDir, "left-"+baseName)
	rightPath := filepath.Join(tempDir, "right-"+baseName)
	if err := os.WriteFile(leftPath, left, 0o600); err != nil {
		os.RemoveAll(tempDir)
		return readOnlyDiffPreparedMsg{returnScreen: returnScreen, err: err}
	}
	if err := os.WriteFile(rightPath, right, 0o600); err != nil {
		os.RemoveAll(tempDir)
		return readOnlyDiffPreparedMsg{returnScreen: returnScreen, err: err}
	}
	return readOnlyDiffPreparedMsg{
		executable: executable, tempDir: tempDir, returnScreen: returnScreen,
		args: []string{
			"--read-only", "--label-left", leftLabel, "--label-right", rightLabel,
			leftPath, rightPath,
		},
	}
}

func runPreparedMerger(prepared readOnlyDiffPreparedMsg) tea.Cmd {
	command := exec.Command(prepared.executable, prepared.args...)
	return tea.ExecProcess(command, func(err error) tea.Msg {
		if prepared.tempDir != "" {
			_ = os.RemoveAll(prepared.tempDir)
		}
		return readOnlyDiffExitedMsg{returnScreen: prepared.returnScreen, err: err}
	})
}
