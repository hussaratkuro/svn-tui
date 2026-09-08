package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"svn-tui/internal/model"
	"svn-tui/internal/svn"
)

const defaultConflictMergeTool = "merger"

type conflictToolPreparedMsg struct {
	repo       model.Repo
	path       string
	tool       string
	executable string
	args       []string
	mine       string
	base       string
	theirs     string
	result     string
	err        error
}

type conflictToolExitedMsg struct {
	prepared conflictToolPreparedMsg
	err      error
}

func prepareConflictToolCmd(r model.Repo, path, tool string) tea.Cmd {
	return func() tea.Msg {
		prepared := conflictToolPreparedMsg{repo: r, path: path, tool: tool}
		infoOut, _ := svn.Run(r, "info", path)
		if strings.Contains(strings.ToLower(infoOut), "tree conflict") {
			prepared.err = fmt.Errorf("%s handles text conflicts; use r for this tree conflict", tool)
			return prepared
		}

		prepared.executable, prepared.err = findMergeTool(tool)
		if prepared.err != nil {
			return prepared
		}
		prepared.mine, prepared.base, prepared.theirs = parseSVNConflictFiles(infoOut, r.Path, path)
		prepared.result = filepath.Join(r.Path, filepath.FromSlash(path))
		if prepared.mine == "" || prepared.base == "" || prepared.theirs == "" {
			prepared.mine, prepared.base, prepared.theirs = guessSVNConflictFiles(prepared.result)
		}
		if prepared.mine == "" || prepared.base == "" || prepared.theirs == "" {
			prepared.err = fmt.Errorf("could not locate SVN conflict files (.mine, .rOLD, .rNEW)")
			return prepared
		}
		prepared.args = []string{prepared.mine, prepared.base, prepared.theirs, "--output=" + prepared.result}
		return prepared
	}
}

func prepareDefaultConflictToolCmd(r model.Repo, path string) tea.Cmd {
	return prepareConflictToolCmd(r, path, defaultConflictMergeTool)
}

func findMergeTool(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	// Installing both binaries into the same directory is the normal local
	// setup. Looking beside svntui makes that work even if the directory is not
	// globally present in PATH.
	if name == "merger" {
		if executable, err := os.Executable(); err == nil {
			candidate := filepath.Join(filepath.Dir(executable), name)
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("%s was not found in PATH or beside the svntui executable", name)
}

func conflictToolCancelled(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 2
}

func resolveConflictAfterToolCmd(prepared conflictToolPreparedMsg) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder
		output.WriteString("Working copy: " + prepared.repo.Path + "\n")
		output.WriteString("Conflicted path: " + prepared.path + "\n")
		output.WriteString("Merge tool: " + prepared.tool + "\n")
		output.WriteString("Result: " + prepared.result + "\n\n")
		output.WriteString("Running: svn resolve --accept=working " + prepared.path + "\n\n")
		resolveOut, err := svn.Run(prepared.repo, "resolve", "--accept=working", prepared.path)
		output.WriteString(resolveOut)
		if err == nil {
			output.WriteString("\nConflict resolved successfully.")
		}
		return model.CommandResult{Output: output.String(), Err: err, CurrentLocation: svn.GetCurrentLocation(prepared.repo)}
	}
}
