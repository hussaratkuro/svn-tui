package svn

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// IgnoreRules describes what must never show up in the commit list, and which
// directories the file-history walker skips.
type IgnoreRules struct {
	// Names are path components hidden anywhere in the working copy, so
	// "vendor" hides vendor/ at any depth.
	Names []string
	// Paths are exact working-copy-relative paths. Unlike Names these hide
	// only that one file rather than every file sharing its basename.
	Paths []string
}

var (
	ignoreOnce  sync.Once
	ignoreRules IgnoreRules
)

// Ignores returns the ignore rules, loading ignore.txt on first use.
func Ignores() IgnoreRules {
	ignoreOnce.Do(func() { ignoreRules = LoadIgnoreRules() })
	return ignoreRules
}

// LoadIgnoreRules reads ignore.txt from the config directories. Nothing is
// hidden without it — every name and path lives in the config file.
func LoadIgnoreRules() IgnoreRules {
	var rules IgnoreRules
	for _, path := range configFilePaths("ignore.txt") {
		rules.merge(loadIgnoreFile(path))
	}
	return rules
}

// HidesName reports whether a single path component is ignored.
func (ir IgnoreRules) HidesName(name string) bool {
	for _, n := range ir.Names {
		if n == name {
			return true
		}
	}
	return false
}

// HidesPath reports whether a working-copy-relative path is ignored, either by
// an exact path rule or because any of its components is an ignored name.
func (ir IgnoreRules) HidesPath(path string) bool {
	clean := strings.TrimPrefix(strings.TrimSpace(filepath.ToSlash(path)), "./")
	for _, p := range ir.Paths {
		if clean == p {
			return true
		}
	}
	for _, part := range strings.Split(clean, "/") {
		if ir.HidesName(part) {
			return true
		}
	}
	return false
}

func (ir *IgnoreRules) merge(other IgnoreRules) {
	for _, n := range other.Names {
		if !ir.HidesName(n) {
			ir.Names = append(ir.Names, n)
		}
	}
	ir.Paths = append(ir.Paths, other.Paths...)
}

// loadIgnoreFile parses one ignore.txt. Entries are either "name=vendor" /
// "path=some/dir/file.php", or a bare line: one containing "/" is treated as a
// path, anything else as a name.
func loadIgnoreFile(path string) IgnoreRules {
	f, err := os.Open(path)
	if err != nil {
		return IgnoreRules{}
	}
	defer f.Close()

	var rules IgnoreRules
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, val := "", line
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			key = strings.ToLower(strings.TrimSpace(parts[0]))
			val = strings.TrimSpace(parts[1])
		}
		val = strings.TrimPrefix(strings.TrimSuffix(filepath.ToSlash(val), "/"), "./")
		if val == "" {
			continue
		}
		switch key {
		case "name", "names", "dir", "file":
			rules.Names = append(rules.Names, val)
		case "path", "paths":
			rules.Paths = append(rules.Paths, val)
		case "":
			if strings.Contains(val, "/") {
				rules.Paths = append(rules.Paths, val)
			} else {
				rules.Names = append(rules.Names, val)
			}
		}
	}
	return rules
}
