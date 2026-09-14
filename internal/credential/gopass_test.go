package credential

import (
	"os"
	"path/filepath"
	"testing"

	"svn-tui/internal/model"
)

func TestResolveCredentialFillsMissingUsernameAndPassword(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "gopass")
	script := "#!/bin/sh\nread master\n[ \"$master\" = correct ] || exit 1\nprintf '%s\\n' '{\"username\":\"alice\",\"password\":\"secret\"}'\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	credential, err := resolveOne(executable, "SVN login", "correct")
	if err != nil {
		t.Fatal(err)
	}
	if credential.Username != "alice" || credential.Password != "secret" {
		t.Fatalf("resolved credential = %#v", credential)
	}

	configs := []model.RepoConfig{{Path: "/repo", CredentialRef: "SVN login"}}
	if !Required(configs) || Required([]model.RepoConfig{{Path: "/repo"}}) {
		t.Fatal("credential requirement detection is incorrect")
	}
}
