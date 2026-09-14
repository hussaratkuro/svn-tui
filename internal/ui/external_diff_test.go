package ui

import (
	"os"
	"path/filepath"
	"testing"

	"svn-tui/internal/model"
)

func TestPrepareMergerFilesUsesReadOnlyLabelsAndPrivateTemps(t *testing.T) {
	binDir := t.TempDir()
	merger := filepath.Join(binDir, "merger")
	if err := os.WriteFile(merger, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	prepared := prepareMergerFiles(
		[]byte("base\n"), []byte("working\n"), "BASE", "WORKING", "src/file.txt", model.ScreenCommitSelect,
	)
	if prepared.err != nil {
		t.Fatal(prepared.err)
	}
	defer os.RemoveAll(prepared.tempDir)
	if prepared.executable != merger || len(prepared.args) != 7 || prepared.args[0] != "--read-only" || prepared.args[2] != "BASE" || prepared.args[4] != "WORKING" {
		t.Fatalf("prepared merger = %#v", prepared)
	}
	for _, path := range prepared.args[5:] {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("temporary diff file mode = %o", info.Mode().Perm())
		}
	}
}
