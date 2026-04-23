package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const checkInterval = 1 * time.Hour

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

	if cache != nil && cache.Outdated && cache.Latest != "" && cache.Latest != Version {
		printNotice(Version, cache.Latest)
	}

	if cache != nil && now-cache.CheckedAt < int64(checkInterval.Seconds()) {
		return
	}

	// Fetch from GitHub
	release, err := fetchLatestRelease()
	if err != nil {
		// Network error — save timestamp so we don't retry immediately
		if cache != nil {
			cache.CheckedAt = now
			saveCache(path, cache)
		} else {
			saveCache(path, &versionCache{CheckedAt: now})
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
		printNotice(Version, release.TagName)
	}
}

func printNotice(current, latest string) {
	fmt.Fprintf(os.Stderr, "\nUpdate available: %s → %s\nRun \"unity-cli update\" to upgrade.\n", current, latest)
}

// isNewer returns true if latest is a higher semver than current.
// Both are expected as "vX.Y.Z". Falls back to string comparison if parsing fails.
func isNewer(latest, current string) bool {
	lParts := parseSemver(latest)
	cParts := parseSemver(current)
	if lParts == nil || cParts == nil {
		return latest > current
	}
	for i := 0; i < 3; i++ {
		if lParts[i] > cParts[i] {
			return true
		}
		if lParts[i] < cParts[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) []int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n := 0
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				return nil
			}
			n = n*10 + int(ch-'0')
		}
		nums[i] = n
	}
	return nums
}
