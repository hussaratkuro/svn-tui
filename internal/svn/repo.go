package svn

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"svn-tui/internal/model"
)

// LoadRepos discovers all configured SVN repositories.
func LoadRepos() []model.Repo {
	var configs []model.RepoConfig

	for _, path := range configFilePaths("repo.txt") {
		configs = append(configs, loadConfigFile(path)...)
	}

	if len(configs) == 0 {
		for _, p := range os.Args[1:] {
			if p = strings.TrimSpace(p); p != "" {
				configs = append(configs, model.RepoConfig{Path: p})
			}
		}
		if env := strings.TrimSpace(os.Getenv("SVN_TUI_REPOS")); env != "" {
			for _, p := range filepath.SplitList(env) {
				if p = strings.TrimSpace(p); p != "" {
					configs = append(configs, model.RepoConfig{Path: p})
				}
			}
		}
		if wd, err := os.Getwd(); err == nil {
			configs = append(configs, model.RepoConfig{Path: wd})
		}
	}

	seen := map[string]bool{}
	var repos []model.Repo
	for _, cfg := range configs {
		if strings.TrimSpace(cfg.Path) == "" {
			continue
		}
		abs, err := filepath.Abs(cfg.Path)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		cfg.Path = abs
		if r, err := BuildRepo(cfg); err == nil {
			repos = append(repos, r)
		}
	}
	return repos
}

// configFilePaths returns the candidate locations of a config file, in the
// order they are read.
func configFilePaths(name string) []string {
	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		add(filepath.Join(configDir, "svn-tui", name))
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".config", "svn-tui", name))
	}
	return paths
}

func loadConfigFile(path string) []model.RepoConfig {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var configs []model.RepoConfig
	current := model.RepoConfig{}

	flush := func() {
		if strings.TrimSpace(current.Path) != "" {
			configs = append(configs, current)
		}
		current = model.RepoConfig{}
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "path", "repo", "working_copy":
			current.Path = val
		case "username", "user":
			current.Username = val
		case "password", "pass":
			current.Password = val
		case "branch_username", "branch_user", "branchname_user":
			current.BranchUsername = val
		}
	}
	flush()
	return configs
}

// BuildRepo constructs a Repo from a config by querying svn info.
func BuildRepo(cfg model.RepoConfig) (model.Repo, error) {
	r := model.Repo{
		Path:           cfg.Path,
		Username:       cfg.Username,
		Password:       cfg.Password,
		BranchUsername: cfg.BranchUsername,
	}
	root, err := Run(r, "info", "--show-item", "repos-root-url")
	if err != nil {
		return model.Repo{}, err
	}
	currentURL, err := Run(r, "info", "--show-item", "url")
	if err != nil {
		return model.Repo{}, err
	}
	r.Root = strings.TrimSpace(root)
	r.URL = strings.TrimSpace(currentURL)
	r.CurrentLocation = GetCurrentLocation(r)
	r.CurrentRevision = GetCurrentRevision(r)
	return r, nil
}

func GetCurrentURL(r model.Repo) string {
	out, err := Run(r, "info", "--show-item", "url")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func GetCurrentLocation(r model.Repo) string {
	out, err := Run(r, "info", "--show-item", "relative-url")
	if err != nil {
		return "unknown"
	}
	loc := strings.TrimSpace(out)
	loc = strings.TrimPrefix(loc, "^")
	if loc == "" {
		return "unknown"
	}
	return loc
}

func GetCurrentRevision(r model.Repo) string {
	out, err := WorkingCopyVersion(r)
	if err != nil {
		return "unknown"
	}
	rev := strings.TrimSpace(out)
	if rev == "" {
		return "unknown"
	}
	return rev
}

func IsMixedRevision(versionText string) bool {
	v := strings.TrimSpace(versionText)
	if v == "" {
		return false
	}
	lower := strings.ToLower(v)
	if strings.Contains(lower, "exported") || strings.Contains(lower, "unversioned") {
		return false
	}
	return strings.Contains(v, ":")
}

// BuildBranchName constructs the branch name from a user-supplied parameter.
func BuildBranchName(r model.Repo, parameter string) (string, error) {
	username := strings.TrimSpace(r.BranchUsername)
	if username == "" {
		username = strings.TrimSpace(r.Username)
	}
	if username == "" {
		authURL, err := authURLFromRoot(r.Root)
		if err != nil {
			return "", err
		}
		out, err := Run(r, "auth", authURL)
		if err != nil {
			return "", err
		}
		username = parseSVNUsername(out)
	}
	if username == "" {
		return "", fmt.Errorf("could not determine SVN username")
	}
	if at := strings.Index(username, "@"); at >= 0 {
		username = username[:at]
	}
	date := time.Now().Format("2006-01-02")
	return fmt.Sprintf("%s_%s_%s", date, username, parameter), nil
}

func authURLFromRoot(repoRoot string) (string, error) {
	parsed, err := url.Parse(repoRoot)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid repository root URL: %s", repoRoot)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func parseSVNUsername(authOutput string) string {
	for _, line := range strings.Split(authOutput, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "username:") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		u := strings.TrimSpace(parts[1])
		if at := strings.Index(u, "@"); at >= 0 {
			u = u[:at]
		}
		return u
	}
	return ""
}

func HelpText() string {
	return `Usage:

Config file:

  ~/.config/svn-tui/repo.txt

Example:

  path=/home/user/dev/
  username=user
  password=YOUR_PASSWORD_HERE
  branch_username=user

Important:

  chmod 600 ~/.config/svn-tui/repo.txt

Hidden files (never listed for commit):

  ~/.config/svn-tui/ignore.txt

  name=vendor
  name=node_modules
  path=some/dir/generated.php

Meld conflict resolve requires:

  sudo pacman -S meld
`
}

func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
