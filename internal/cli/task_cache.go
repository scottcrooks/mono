package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var cacheEnvWhitelist = []string{"CI", "GOARCH", "GOOS", "NODE_ENV"}

type taskCache struct {
	RootDir string
}

type taskCacheEntry struct {
	Key       string    `json:"key"`
	Service   string    `json:"service"`
	Task      TaskName  `json:"task"`
	CreatedAt time.Time `json:"createdAt"`
}

func newTaskCache() taskCache {
	return taskCache{RootDir: filepath.Join(".cache", "mono", "tasks")}
}

func (c taskCache) cacheEntryPath(key string) string {
	return filepath.Join(c.RootDir, key+".json")
}

func (c taskCache) load(key string) (taskCacheEntry, bool, error) {
	path := c.cacheEntryPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return taskCacheEntry{}, false, nil
		}
		return taskCacheEntry{}, false, err
	}
	var entry taskCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return taskCacheEntry{}, false, err
	}
	return entry, true, nil
}

func (c taskCache) store(entry taskCacheEntry) error {
	if err := os.MkdirAll(c.RootDir, 0o755); err != nil {
		return err
	}
	path := c.cacheEntryPath(entry.Key)
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func cacheMissReason(noCache bool, hit bool) string {
	if noCache {
		return "disabled by --no-cache"
	}
	if !hit {
		return "cache entry not found"
	}
	return ""
}

func buildTaskCacheKey(svc Service, task TaskName) (string, error) {
	h := sha256.New()

	pieces := []string{string(task), svc.Name, normalizeServicePath(svc.Path)}
	for _, env := range cacheEnvWhitelist {
		pieces = append(pieces, env+"="+os.Getenv(env))
	}
	sort.Strings(pieces)
	for _, p := range pieces {
		if _, err := io.WriteString(h, p+"\n"); err != nil {
			return "", err
		}
	}

	if err := hashTree(h, svc.Path); err != nil {
		return "", err
	}
	for _, lock := range []string{"go.sum", "package-lock.json", "pnpm-lock.yaml"} {
		if err := hashFileIfPresent(h, filepath.Join(svc.Path, lock)); err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashTree(h io.Writer, root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".cache" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(h, filepath.ToSlash(rel)+"\n"); err != nil {
			return err
		}
		return hashFileInto(h, path)
	})
}

func hashFileIfPresent(h io.Writer, path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := io.WriteString(h, fmt.Sprintf("lock:%s\n", filepath.ToSlash(path))); err != nil {
		return err
	}
	return hashFileInto(h, path)
}

func hashFileInto(h io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(h, f)
	return err
}
