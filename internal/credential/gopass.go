package credential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"svn-tui/internal/model"
)

type resolvedCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func Required(configs []model.RepoConfig) bool {
	for _, config := range configs {
		if strings.TrimSpace(config.CredentialRef) != "" {
			return true
		}
	}
	return false
}

func PromptVaultPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("gopass credential references require an interactive terminal")
	}
	if _, err := fmt.Fprint(os.Stderr, "gopass vault password: "); err != nil {
		return "", err
	}
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read gopass vault password: %w", err)
	}
	if len(password) == 0 {
		return "", fmt.Errorf("gopass vault password cannot be empty")
	}
	return string(password), nil
}

func Resolve(configs []model.RepoConfig, masterPassword string) ([]model.RepoConfig, error) {
	executable, err := findGopass()
	if err != nil {
		return nil, err
	}
	resolved := append([]model.RepoConfig(nil), configs...)
	for index := range resolved {
		ref := strings.TrimSpace(resolved[index].CredentialRef)
		if ref == "" {
			continue
		}
		credential, err := resolveOne(executable, ref, masterPassword)
		if err != nil {
			return nil, fmt.Errorf("repository %s: %w", resolved[index].Path, err)
		}
		if resolved[index].Username == "" {
			resolved[index].Username = credential.Username
		}
		resolved[index].Password = credential.Password
	}
	return resolved, nil
}

func resolveOne(executable, ref, masterPassword string) (resolvedCredential, error) {
	command := exec.Command(executable, "credential", "get", "--ref", ref, "--password-stdin")
	command.Stdin = strings.NewReader(masterPassword + "\n")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return resolvedCredential{}, fmt.Errorf("resolve gopass credential %q: %s", ref, message)
	}
	var credential resolvedCredential
	if err := json.Unmarshal(stdout.Bytes(), &credential); err != nil {
		return resolvedCredential{}, fmt.Errorf("decode gopass response: %w", err)
	}
	return credential, nil
}

func findGopass() (string, error) {
	if executable, err := exec.LookPath("gopass"); err == nil {
		return executable, nil
	}
	if current, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(current), "gopass")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("gopass was not found in PATH or beside svntui")
}
