package svn

import (
	"bufio"
	"os"
	"os/exec"

	"svn-tui/internal/model"
)

// Run executes an svn command and returns combined output.
func Run(r model.Repo, args ...string) (string, error) {
	finalArgs := baseArgs(r, true)
	finalArgs = append(finalArgs, args...)

	cmd := exec.Command("svn", finalArgs...)
	cmd.Dir = r.Path

	out, err := cmd.CombinedOutput()
	return string(out), err
}

// StreamLines runs an svn command and calls emit once per output line.
func StreamLines(r model.Repo, emit func(string), args ...string) error {
	finalArgs := baseArgs(r, true)
	finalArgs = append(finalArgs, args...)

	cmd := exec.Command("svn", finalArgs...)
	cmd.Dir = r.Path

	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return err
	}

	pw.Close()

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		emit(scanner.Text())
	}
	pr.Close()

	return cmd.Wait()
}

// Interactive runs an svn command with full terminal I/O attached.
func Interactive(r model.Repo, args ...string) error {
	finalArgs := baseArgs(r, false)
	finalArgs = append(finalArgs, args...)

	cmd := exec.Command("svn", finalArgs...)
	cmd.Dir = r.Path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// WorkingCopyVersion runs svnversion and returns its output.
func WorkingCopyVersion(r model.Repo) (string, error) {
	cmd := exec.Command("svnversion", ".")
	cmd.Dir = r.Path
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func baseArgs(r model.Repo, nonInteractive bool) []string {
	var args []string
	if r.Username != "" {
		args = append(args, "--username", r.Username)
	}
	if r.Password != "" {
		args = append(args, "--password", r.Password)
		if nonInteractive {
			args = append(args, "--non-interactive")
		}
		args = append(args, "--no-auth-cache")
	}
	return args
}
