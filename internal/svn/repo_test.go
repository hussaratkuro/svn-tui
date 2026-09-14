package svn

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRepoConfigsReadsCredentialReference(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", configHome)
	directory := filepath.Join(configHome, "svn-tui")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "path=/work/project\nusername=explicit-user\ncredential_ref=Work SVN\nbranch_username=branch-user\n"
	if err := os.WriteFile(filepath.Join(directory, "repo.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	configs := LoadRepoConfigs()
	if len(configs) != 1 {
		t.Fatalf("configs = %#v", configs)
	}
	config := configs[0]
	if config.Path != "/work/project" || config.Username != "explicit-user" || config.CredentialRef != "Work SVN" || config.BranchUsername != "branch-user" {
		t.Fatalf("config = %#v", config)
	}
}
