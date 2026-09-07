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

An unversioned directory (for example `? add path/to/new-directory`) expands
into an indented tree on the commit screen. Its files remain part of the
directory's single selection, but each file can be highlighted and opened with
`d` to preview its added-file diff.

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

A directory scheduled as added or replaced (`A`, `R`) is listed only when it has
a visible change under it, since SVN needs it in the same commit as its children.
If everything under it is hidden by `ignore.txt`, the directory is hidden too.

Directories are reverted with `--depth infinity`, because SVN refuses to revert a
scheduled directory without its children. That means reverting a directory
discards every local change below it — except the paths listed in `ignore.txt`,
which are copied aside before the revert and put back afterwards.

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

### Merge branch revisions

After choosing a branch, the merge action shows its commits with revision,
author, date, and commit message. The first row is a separate action for the
latest branch state:

- choose a commit to merge only that revision (`svn merge -c REV`)
- choose **Merge latest branch state through HEAD** to merge the whole branch

The merge changes the working copy only. Review the result and commit it in a
separate step.

### Branch diff

Two read-only comparisons of a whole branch, both driven from the branch list —
the cursor starts on the branch the working copy is on:

- **Branch diff (since branch point)** compares the branch as it was created —
  the trunk state at that moment — with the branch head, so it shows every
  change committed on the branch.
- **Branch diff (vs trunk HEAD)** compares today's trunk with the branch head,
  so it also shows what trunk gained while the branch was open.

Both open a list of the changed paths (`M` modified, `A` added, `D` deleted,
`R` replaced, trailing `P` for a property change). From there:

- `Enter` or `d` opens one path side by side, read straight from the repository
- `u` shows the complete unified diff of the comparison (truncated at 2 MB)
- `Esc` goes back to the branch list

Neither action touches the working copy, and neither needs the branch to be
checked out.

### Delete branch

Removes a branch from the repository — pick it from the same branch list, but
`Enter` there never deletes anything. It opens a confirmation screen first,
which shows what is about to go:

- the branch name and its full URL
- its last commit: revision, author, date and message
- a warning when the working copy is on that very branch

The delete only runs once you type `delete` and press `Enter`; anything else is
refused with a "branch was NOT deleted" message, and `Esc` goes back to the
branch list. It runs `svn delete <branch URL> -m "Deleting branch ..."`, so the
branch history stays in the repository and can be restored with `svn copy` from
an earlier revision.

Deleting the branch the working copy is checked out on is allowed, but the
result reminds you to switch to trunk afterwards.

### Unshelve or delete shelves

The Unshelve action lists the shelves stored in `.svn-tui-shelves`.

- `Enter` restores a shelf and removes it from the list
- `Del` deletes a shelf without restoring it — press `Del` once to arm, again to
  confirm, or move the cursor to cancel

Deleting a shelf discards its saved patch and file copies for good. The working
copy is not touched. When the last shelf is gone, `.svn-tui-shelves` is removed.

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

- file conflicts are opened with Meld (`Enter`)
- file conflicts can also be resolved whole-file, without Meld:
  - `m` keeps the current file — `svn resolve --accept=mine-full`
  - `t` takes the incoming file — `svn resolve --accept=theirs-full`
- tree conflicts are resolved with `--accept=working` (`r`)

`m` and `t` act on the file under the cursor and are offered for file conflicts
only, since tree conflicts accept `working`. Both arm on the first press and run
on the second, the same as `r`.

Tree conflicts cannot be resolved in a single SVN call, so there is no per-file
selection for them. `r` resolves every tree conflict in the list, one path at a
time, and reports each path separately. Press `r` once to arm it, `r` again to
run it; moving the cursor cancels.

Meld support expects `meld` to be installed and available in `PATH`.

### Cleanup

Runs:

```bash
svn cleanup
```

Releases stale working-copy locks and rolls back unfinished operations, for when
SVN insists the working copy is locked.

### Properties

Views and edits SVN properties on any path in the working copy.

- the search box picks the path; an empty search opens the working copy root
  (`.`), where a merge records `svn:mergeinfo`
- directories are listed before files, since properties usually live on them
- the list shows each property with a 3-line preview of its value, so a 38-line
  `svn:mergeinfo` does not fill the screen
- `a` adds a property (name, then value), `Enter`/`e` edits the value of the
  selected one, `Del` removes it (press twice to confirm)

Values are entered on one line; type `\n` for a line break, which `svn:ignore`
and `svn:mergeinfo` need. Editing prefills the current value in the same form.

Behind the scenes this is `svn proplist -v --xml`, `svn propset`, and
`svn propdel`. Properties are local changes until committed — the path shows up
in the commit list as a property-only change.

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

## Hidden files

Files that should never show up in the commit list are configured at:

```text
~/.config/svn-tui/ignore.txt
```

```ini
# hides every matching path component, at any depth
name=.svn
name=.git
name=node_modules
name=vendor
name=.claude
name=CLAUDE.md
name=graphify-out

# hides one working-copy-relative path; name a directory to hide it and
# everything under it
path=Workspace/Web/project/modul/_html.php
path=Workspace/Web/project/generated

# bare lines work too: a line with "/" is a path, anything else is a name
build/generated/version.php
```

Supported keys:

| Key | Description |
| --- | --- |
| `name`, `names`, `dir`, `file` | Path component hidden anywhere in the working copy |
| `path`, `paths` | Working-copy-relative path; a directory hides its whole subtree |

Nothing is hidden without this file, so `ignore.txt` in the repository root is a
ready-made starting point — copy it to `~/.config/svn-tui/ignore.txt`. The same
names also tell the file history search which directories to skip, so keeping
`.svn` and `.git` in the list is worth it.

The shelf store (`.svn-tui-shelves`) is hidden by the tool itself and needs no
config entry.

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
| `/` | Search the action list (Enter runs, Esc cancels) |

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
| `Space` | Select/unselect file or the parent of an expanded directory file |
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

### Branch merge revision screen

| Key | Action |
| --- | --- |
| `↑` / `↓` or `j` / `k` | Choose the latest-state action or a commit |
| `Enter` on a commit | Merge only that branch revision |
| `Enter` on the latest-state action | Merge all branch revisions through `HEAD` |
| `Esc` | Go back to the branch list |

### Delete branch confirmation screen

| Key | Action |
| --- | --- |
| `delete` + `Enter` | Delete the branch from the repository |
| `Esc` | Cancel and go back to the branch list |

### Branch diff screen

| Key | Action |
| --- | --- |
| `Enter` or `d` | Side-by-side diff of the highlighted path |
| `u` | Full unified diff of the comparison |
| `Esc` | Back to the branch list |

### Revision tree screen

| Key | Action |
| --- | --- |
| `a` | Load full history instead of the newest 250 entries |

## Notes

- The Revision tree intentionally does not show file-level modifications. Use Commit history or File history for that.
- Full revision tree loading can be slow on large repositories because it asks SVN for the complete XML log with changed paths.
- Passwords in the config file are convenient but sensitive. Keep the config file private with `chmod 600`.
- This tool shells out to the local `svn` command, so your normal SVN auth/cache behavior still matters.
