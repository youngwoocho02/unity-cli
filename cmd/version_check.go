package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const checkInterval = 1 * time.Hour

var fetchLatestReleaseFn = fetchLatestRelease

type versionCache struct {
	CheckedAt int64  `json:"checked_at"`
	Latest    string `json:"latest,omitempty"`
	Outdated  bool   `json:"outdated,omitempty"`
}

func cacheFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".unity-cli", "version-check.json")
}

func loadCache(path string) (*versionCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c versionCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveCache(path string, c *versionCache) {
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0755)
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}

// printUpdateNotice checks for a newer version and prints a notice if available.
// Silently does nothing on any error (no network, bad cache, etc.).
func printUpdateNotice() {
	if Version == "dev" {
		return
	}

	path := cacheFilePath()
	if path == "" {
		return
	}

	now := time.Now().Unix()
	cache, _ := loadCache(path)
	latestNotice := ""

	if cache != nil && cache.Outdated && cache.Latest != "" && cache.Latest != Version {
		latestNotice = cache.Latest
	}

	if cache != nil && now-cache.CheckedAt < int64(checkInterval.Seconds()) {
		if latestNotice != "" {
			printNotice(Version, latestNotice)
		}
		return
	}

	// Fetch from GitHub
	release, err := fetchLatestReleaseFn()
	if err != nil {
		// Network error — save timestamp so we don't retry immediately
		if cache != nil {
			cache.CheckedAt = now
			saveCache(path, cache)
		} else {
			saveCache(path, &versionCache{CheckedAt: now})
		}
		if latestNotice != "" {
			printNotice(Version, latestNotice)
		}
		return
	}

	nextCache := &versionCache{
		CheckedAt: now,
		Latest:    release.TagName,
		Outdated:  release.TagName != "" && release.TagName != Version,
	}
	saveCache(path, nextCache)

	if nextCache.Outdated {
		latestNotice = release.TagName
	} else {
		latestNotice = ""
	}

	if latestNotice != "" {
		printNotice(Version, latestNotice)
	}
}

func printNotice(current, latest string) {
	fmt.Fprintf(os.Stderr, "\nUpdate available: %s → %s\nRun \"unity-cli update\" to upgrade.\n", current, latest)
}
