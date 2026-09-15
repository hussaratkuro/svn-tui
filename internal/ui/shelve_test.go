package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"svn-tui/internal/model"
)

func TestShelveSelectionOpensNameInput(t *testing.T) {
	m := NewModel([]model.Repo{{Path: t.TempDir()}})
	m.activeRepo = m.repos[0]
	m.screen = model.ScreenShelveSelect
	m.commitItems = []model.CommitItem{{Path: "changed.txt", Status: "M", Selected: true}}

	updated, cmd := m.updateShelveSelect(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if cmd != nil {
		t.Fatal("opening the shelf name input unexpectedly started a command")
	}
	if got.screen != model.ScreenShelveNameInput {
		t.Fatalf("screen = %v, want ScreenShelveNameInput", got.screen)
	}
	if !got.input.Focused() {
		t.Fatal("shelf name input is not focused")
	}
	if !strings.Contains(stripANSI(got.View()), "Shelf name:") {
		t.Fatalf("shelf name view does not show its prompt:\n%s", got.View())
	}

	updated, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got = updated.(Model); got.screen != model.ScreenShelveSelect {
		t.Fatalf("Esc returned to screen %v, want ScreenShelveSelect", got.screen)
	}
}

func TestShelveNameInputValidatesAndStartsShelving(t *testing.T) {
	root := t.TempDir()
	m := NewModel([]model.Repo{{Path: root}})
	m.activeRepo = m.repos[0]
	m.screen = model.ScreenShelveNameInput
	m.commitItems = []model.CommitItem{{Path: "changed.txt", Status: "M", Selected: true}}

	m.input.SetValue("../outside")
	updated, cmd := m.updateShelveNameInput(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if cmd != nil || got.screen != model.ScreenShelveNameInput {
		t.Fatalf("invalid name advanced to screen %v with cmd %v", got.screen, cmd != nil)
	}
	if !strings.Contains(got.shelfNameError, "cannot contain") {
		t.Fatalf("invalid name error = %q", got.shelfNameError)
	}

	got.input.SetValue("ticket-123 login fix")
	updated, cmd = got.updateShelveNameInput(tea.KeyMsg{Type: tea.KeyEnter})
	got = updated.(Model)
	if cmd == nil {
		t.Fatal("valid shelf name did not start shelving")
	}
	if got.screen != model.ScreenRunning {
		t.Fatalf("screen = %v, want ScreenRunning", got.screen)
	}
}

func TestValidateNewShelfName(t *testing.T) {
	root := t.TempDir()
	r := model.Repo{Path: root}
	if err := os.MkdirAll(filepath.Join(root, model.ShelvesDir, "existing"), 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		wantErr bool
	}{
		{name: "release-1"},
		{name: "hibajavítás 123"},
		{name: "", wantErr: true},
		{name: ".", wantErr: true},
		{name: "..", wantErr: true},
		{name: "feature/login", wantErr: true},
		{name: `feature\login`, wantErr: true},
		{name: "line\nbreak", wantErr: true},
		{name: strings.Repeat("x", 201), wantErr: true},
		{name: "existing", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNewShelfName(r, tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateNewShelfName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestShelveCommandRejectsUnsafeNameBeforeRunningSVN(t *testing.T) {
	r := model.Repo{Path: t.TempDir()}
	cmd := shelveChangesCmd(r, []model.CommitItem{{Path: "changed.txt", Status: "M", Selected: true}}, "../outside")
	msg := cmd()
	result, ok := msg.(model.CommandResult)
	if !ok {
		t.Fatalf("command returned %T, want model.CommandResult", msg)
	}
	if result.Err == nil {
		t.Fatal("unsafe shelf name was accepted")
	}
	if _, err := os.Stat(filepath.Join(r.Path, "outside")); !os.IsNotExist(err) {
		t.Fatalf("unsafe shelf created a path outside the shelf store: %v", err)
	}
}

func TestFailedShelfDoesNotReserveItsName(t *testing.T) {
	r := model.Repo{Path: t.TempDir()}
	const name = "retry-me"
	msg := shelveChangesCmd(r, []model.CommitItem{{
		Path: "not-a-working-copy.txt", Status: "M", Selected: true,
	}}, name)()
	result, ok := msg.(model.CommandResult)
	if !ok || result.Err == nil {
		t.Fatalf("shelve result = %#v, want an SVN diff error", msg)
	}
	if _, err := os.Stat(filepath.Join(r.Path, model.ShelvesDir, name)); !os.IsNotExist(err) {
		t.Fatalf("failed shelf still reserves its name: %v", err)
	}
	if err := validateNewShelfName(r, name); err != nil {
		t.Fatalf("failed shelf name cannot be retried: %v", err)
	}
}

func TestNamedUnversionedShelfRoundTrip(t *testing.T) {
	root := t.TempDir()
	r := model.Repo{Path: root}
	const (
		name = "ticket 123"
		path = "notes.txt"
	)
	if err := os.WriteFile(filepath.Join(root, path), []byte("saved locally\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	msg := shelveChangesCmd(r, []model.CommitItem{{
		Path: path, Status: "?", Unversioned: true, Selected: true,
	}}, name)()
	result, ok := msg.(model.CommandResult)
	if !ok || result.Err != nil {
		t.Fatalf("shelve result = %#v", msg)
	}
	if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
		t.Fatalf("shelved working file still exists: %v", err)
	}

	manifestPath := filepath.Join(root, model.ShelvesDir, name, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest model.ShelfManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != name {
		t.Fatalf("manifest name = %q, want %q", manifest.Name, name)
	}

	loaded, ok := loadShelvesCmd(r)().(model.ShelvesLoadedMsg)
	if !ok || loaded.Err != nil || len(loaded.Shelves) != 1 || loaded.Shelves[0] != name {
		t.Fatalf("loaded shelves = %#v", loaded)
	}

	msg = unshelveChangesCmd(r, name)()
	result, ok = msg.(model.CommandResult)
	if !ok || result.Err != nil {
		t.Fatalf("unshelve result = %#v", msg)
	}
	restored, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "saved locally\n" {
		t.Fatalf("restored content = %q", restored)
	}
	if _, err := os.Stat(filepath.Join(root, model.ShelvesDir)); !os.IsNotExist(err) {
		t.Fatalf("empty shelf store still exists: %v", err)
	}
}
