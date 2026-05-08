# svn-tui

A keyboard-driven terminal UI for working with Subversion working copies without turning every daily SVN task into a tiny command-line archaeology dig.

`svn-tui` wraps common `svn` operations in a Bubble Tea based TUI: pull/update, status, commits, branch switching, conflict resolving, file history, commit history, and an ASCII revision tree for branch/revision overview.

## Features

### Repository picker

- Auto-detects repositories from a config file.
- Falls back to command-line paths, `SVN_TUI_REPOS`, or the current working directory.
- Shows the current repository URL, repository root, and current branch/trunk location.

### Pull incoming changes

The Pull action does not blindly run `svn update` immediately. It first shows incoming repository changes using:

```bash
svn diff --summarize -r BASE:HEAD
```

From the pull screen you can:

- select individual files to update
- select all or none
- open a diff for the highlighted file with `d`
- update only the selected files

The update output is colorized so added, deleted, updated, merged, conflicted, and external changes are easier to scan.

### Status

Runs `svn status` and displays the working copy state in a scrollable, colorized result view.

### Commit selected files

The Commit action shows local working copy changes and lets you select exactly what should be committed.

Supported behavior:

- select files with `Space`
- select all with `a`
- clear selection with `n`
- inspect file diff with `d`
- commit selected files with a message
- automatically runs `svn add` for selected unversioned files

### Partial hunk commit

For modified versioned files, the commit screen supports partial commits:

- press `p` on a modified file
- select individual hunks
- enter a commit message
- commit only the selected hunks

This is useful when one file contains several unrelated edits and you do not want one giant “oops soup” commit.

### Revert selected files

The Revert action lists local changes and lets you choose which files to revert.

- select files with `Space`
- view side-by-side diff with `d`
- revert only selected files

> Warning: `svn revert` discards local changes for selected versioned files.

### Create branch

Creates a branch from the current working copy URL.

Branch names use this format:

```text
YYYY-MM-DD_username_parameter
```

The username can come from `branch_username` in the config file.

### Switch to branch

Lists available SVN branches and lets you switch to one.

For large branch lists, you do not need to scroll through the entire branch swamp:

- use arrows or `j/k` to move normally
- type the branch number and press `Enter`
- use `Backspace` to edit the typed number

### Switch to trunk

Switches the working copy back to trunk.

### Checkout revision

Runs:

```bash
svn update -r REVISION
```

The UI warns that a later Pull action updates the working copy back to `HEAD`.

### Resolve conflicts

Lists conflicts from `svn status`.

- file conflicts are opened with Meld
- tree conflicts are resolved with `--accept=working`

Meld support expects `meld` to be installed and available in `PATH`.

### Commit history

Shows recent commit history with changed paths.

The commit history includes:

- revision number
- author
- date/time
- commit message
- changed files/paths

This is the right place to inspect which files changed in a commit.

### File history

Searches files by local working-copy path and shows SVN log history for the selected file.

Useful when you know part of a filename or directory and want to avoid listing the whole repository like a medieval scroll.

### ASCII revision tree

Shows a branch-oriented ASCII revision tree.

The revision tree focuses on branch structure, not file-level changes. It shows:

- trunk, branches, and tags
- which branch/tag was created from which path and revision
- branch creation revision and creation time
- latest relevant revision
- commits under the related branch node
- each commit's revision, user, date/time, and message
- merge-back markers when they can be detected from log messages

By default, the Revision tree loads only the newest 250 log entries:

```bash
svn log --xml -v --limit 250 <repo-root>
```

Inside the Revision tree view, press `a` to load the full history:

```bash
svn log --xml -v <repo-root>
```

This keeps the normal view fast while still allowing the full-history dragon to be summoned when needed.

## Installation

Clone the repository and build the binary:

```bash
git clone <your-repo-url>
cd svn-tui
go build -o svn-tui main.go
```

Optional install step:

```bash
sudo install -m 755 svn-tui /usr/local/bin/svn-tui
```

## Requirements

- Go
- Subversion CLI: `svn`
- A valid SVN working copy
- Optional: `meld` for file conflict resolution

Go dependencies used by the app:

- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`
- `github.com/charmbracelet/lipgloss`

If the project does not already have a `go.mod`, initialize one:

```bash
go mod init svn-tui
go get github.com/charmbracelet/bubbletea github.com/charmbracelet/bubbles github.com/charmbracelet/lipgloss
go mod tidy
```

## Configuration

Create a config file at:

```text
~/.config/svn-tui/repo.txt
```

Example:

```ini
path=/home/user/dev/
username=user
password=YOUR_PASSWORD_HERE
branch_username=user
```

Multiple repositories can be configured by separating blocks with an empty line:

```ini
path=/home/user/dev/
username=user
password=YOUR_PASSWORD_HERE
branch_username=user

path=/home/user/dev/another-project
username=user
password=YOUR_PASSWORD_HERE
branch_username=user
```

Recommended permissions:

```bash
chmod 600 ~/.config/svn-tui/repo.txt
```

Supported config keys:

| Key | Description |
| --- | --- |
| `path`, `repo`, `working_copy` | Local SVN working copy path |
| `username`, `user` | SVN username |
| `password`, `pass` | SVN password |
| `branch_username`, `branch_user`, `branchname_user` | Username part used when creating branches |

## Alternative repository discovery

If no config file exists, `svn-tui` tries these sources:

1. paths passed as command-line arguments
2. paths from the `SVN_TUI_REPOS` environment variable
3. the current working directory

Examples:

```bash
svn-tui /home/user/dev/
```

```bash
SVN_TUI_REPOS="/home/user/dev/:/home/user/dev/another-project" svn-tui
```

## Keyboard shortcuts

### Global navigation

| Key | Action |
| --- | --- |
| `↑` / `↓` or `j` / `k` | Move selection |
| `PgUp` / `PgDn` | Page scroll |
| `Home` / `End` | Jump to top/bottom in scrollable views |
| `Enter` | Confirm / run selected action |
| `Esc` | Back |
| `q` | Quit |

### Pull screen

| Key | Action |
| --- | --- |
| `Space` | Select/unselect file |
| `a` | Select all files |
| `n` | Select none |
| `d` | View incoming diff for highlighted file |
| `Enter` | Pull/update selected files |

### Commit screen

| Key | Action |
| --- | --- |
| `Space` | Select/unselect file |
| `a` | Select all files |
| `n` | Select none |
| `d` | View side-by-side diff |
| `p` | Select partial hunks for modified files |
| `Enter` | Enter commit message |

### Revert screen

| Key | Action |
| --- | --- |
| `Space` | Select/unselect file |
| `a` | Select all files |
| `n` | Select none |
| `d` | View side-by-side diff |
| `Enter` | Revert selected files |

### Branch switch screen

| Key | Action |
| --- | --- |
| number keys | Type branch number |
| `Backspace` | Edit typed branch number |
| `Enter` | Switch to highlighted branch or typed branch number |

### Revision tree screen

| Key | Action |
| --- | --- |
| `a` | Load full history instead of the newest 250 entries |

## Notes

- The Revision tree intentionally does not show file-level modifications. Use Commit history or File history for that.
- Full revision tree loading can be slow on large repositories because it asks SVN for the complete XML log with changed paths.
- Passwords in the config file are convenient but sensitive. Keep the config file private with `chmod 600`.
- This tool shells out to the local `svn` command, so your normal SVN auth/cache behavior still matters.
