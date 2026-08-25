package svn

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"svn-tui/internal/model"
)

// logCacheFile is the on-disk shape of a cached svn log for one target.
type logCacheFile struct {
	MaxRevision int                    `json:"max_revision"`
	Entries     []model.SVNLogEntryXML `json:"entries"`
}

// FetchLogIncremental returns svn log entries for target (newest first), backed by a
// local on-disk cache keyed by cacheKey. Only revisions newer than the cached
// max revision are fetched from svn; the result is merged with the cache and
// persisted. If keep > 0, the returned (and stored) set is trimmed to the
// newest `keep` entries. If stopOnCopy is true, history does not cross branch/tag
// copy points (i.e. only the target's own line of history is returned).
func FetchLogIncremental(r model.Repo, cacheKey, target string, keep int, stopOnCopy bool) ([]model.SVNLogEntryXML, error) {
	cached := loadLogCache(r, cacheKey)

	head, err := headRevision(r, target)
	if err != nil {
		if cached != nil {
			return trimLogEntries(cached.Entries, keep), nil
		}
		return nil, err
	}

	if cached != nil && cached.MaxRevision >= head {
		return trimLogEntries(cached.Entries, keep), nil
	}

	args := []string{"log", "--xml", "-v"}
	if stopOnCopy {
		args = append(args, "--stop-on-copy")
	}
	if cached == nil || cached.MaxRevision <= 0 {
		args = append(args, "-r", fmt.Sprintf("%d:1", head))
		if keep > 0 {
			args = append(args, "--limit", strconv.Itoa(keep))
		}
	} else {
		args = append(args, "-r", fmt.Sprintf("%d:%d", head, cached.MaxRevision+1))
	}
	if strings.TrimSpace(target) != "" {
		args = append(args, target)
	}

	out, runErr := Run(r, args...)
	if runErr != nil {
		if cached != nil {
			return trimLogEntries(cached.Entries, keep), nil
		}
		return nil, fmt.Errorf("svn log failed: %w\n%s", runErr, out)
	}

	var parsed model.SVNLogXML
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		if cached != nil {
			return trimLogEntries(cached.Entries, keep), nil
		}
		return nil, fmt.Errorf("failed to parse svn log XML: %w", err)
	}

	merged := mergeLogEntries(cached, parsed.Entries)
	merged.Entries = trimLogEntries(merged.Entries, keep)
	saveLogCache(r, cacheKey, merged)
	return merged.Entries, nil
}

func headRevision(r model.Repo, target string) (int, error) {
	args := []string{"info", "--show-item", "revision", "-r", "HEAD"}
	if strings.TrimSpace(target) != "" {
		args = append(args, target)
	}
	out, err := Run(r, args...)
	if err != nil {
		return 0, fmt.Errorf("svn info failed: %w\n%s", err, out)
	}
	rev, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("unexpected svn info output: %q", out)
	}
	return rev, nil
}

func mergeLogEntries(cached *logCacheFile, fresh []model.SVNLogEntryXML) *logCacheFile {
	byRev := map[int]model.SVNLogEntryXML{}
	if cached != nil {
		for _, e := range cached.Entries {
			byRev[e.Revision] = e
		}
	}
	for _, e := range fresh {
		byRev[e.Revision] = e
	}
	entries := make([]model.SVNLogEntryXML, 0, len(byRev))
	for _, e := range byRev {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Revision > entries[j].Revision })
	max := 0
	if len(entries) > 0 {
		max = entries[0].Revision
	}
	return &logCacheFile{MaxRevision: max, Entries: entries}
}

func trimLogEntries(entries []model.SVNLogEntryXML, keep int) []model.SVNLogEntryXML {
	if keep > 0 && len(entries) > keep {
		return entries[:keep]
	}
	return entries
}

func logCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "svn-tui", "logcache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func logCacheFilePath(r model.Repo, key string) (string, error) {
	dir, err := logCacheDir()
	if err != nil {
		return "", err
	}
	repoID := sanitizeCacheKey(FirstNonEmpty(r.Root, r.Path))
	name := repoID + "__" + sanitizeCacheKey(key) + ".json"
	return filepath.Join(dir, name), nil
}

func sanitizeCacheKey(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "root"
	}
	if len(out) > 100 {
		sum := sha1.Sum([]byte(lower))
		out = out[:80] + "_" + hex.EncodeToString(sum[:6])
	}
	return out
}

func loadLogCache(r model.Repo, key string) *logCacheFile {
	path, err := logCacheFilePath(r, key)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c logCacheFile
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	return &c
}

func saveLogCache(r model.Repo, key string, c *logCacheFile) {
	path, err := logCacheFilePath(r, key)
	if err != nil {
		return
	}
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
