package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })

	fn()

	_ = w.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(data)
}

func prepareVersionCheckEnv(t *testing.T, version string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	origVersion := Version
	Version = version
	t.Cleanup(func() { Version = origVersion })

	origFetch := fetchLatestReleaseFn
	t.Cleanup(func() { fetchLatestReleaseFn = origFetch })

	return filepath.Join(home, ".unity-cli", "version-check.json")
}

func TestPrintUpdateNotice_UsesCachedOutdatedNoticeWithinInterval(t *testing.T) {
	path := prepareVersionCheckEnv(t, "v0.3.10")
	saveCache(path, &versionCache{
		CheckedAt: time.Now().Unix(),
		Latest:    "v0.3.11",
		Outdated:  true,
	})

	fetchCalled := false
	fetchLatestReleaseFn = func() (*ghRelease, error) {
		fetchCalled = true
		return &ghRelease{TagName: "v9.9.9"}, nil
	}

	output := captureStderr(t, func() {
		printUpdateNotice()
	})

	if fetchCalled {
		t.Fatal("expected no remote fetch while cache interval is still valid")
	}
	if count := strings.Count(output, "Update available:"); count != 1 {
		t.Fatalf("expected 1 notice, got %d: %q", count, output)
	}
	if !strings.Contains(output, "v0.3.10 → v0.3.11") {
		t.Fatalf("expected cached latest in notice, got %q", output)
	}
}

func TestPrintUpdateNotice_RefreshesCacheAndPrintsOnceWhenStillOutdated(t *testing.T) {
	path := prepareVersionCheckEnv(t, "v0.3.10")
	saveCache(path, &versionCache{
		CheckedAt: time.Now().Add(-2 * checkInterval).Unix(),
		Latest:    "v0.3.11",
		Outdated:  true,
	})

	fetchLatestReleaseFn = func() (*ghRelease, error) {
		return &ghRelease{TagName: "v0.3.12"}, nil
	}

	output := captureStderr(t, func() {
		printUpdateNotice()
	})

	if count := strings.Count(output, "Update available:"); count != 1 {
		t.Fatalf("expected 1 notice, got %d: %q", count, output)
	}
	if !strings.Contains(output, "v0.3.10 → v0.3.12") {
		t.Fatalf("expected refreshed latest in notice, got %q", output)
	}

	loaded, err := loadCache(path)
	if err != nil {
		t.Fatalf("loadCache: %v", err)
	}
	if loaded.Latest != "v0.3.12" || !loaded.Outdated {
		t.Fatalf("unexpected cache after refresh: %+v", loaded)
	}
}

func TestPrintUpdateNotice_PreservesCachedNoticeOnFetchFailure(t *testing.T) {
	path := prepareVersionCheckEnv(t, "v0.3.10")
	before := time.Now().Add(-2 * checkInterval).Unix()
	saveCache(path, &versionCache{
		CheckedAt: before,
		Latest:    "v0.3.11",
		Outdated:  true,
	})

	fetchLatestReleaseFn = func() (*ghRelease, error) {
		return nil, errors.New("network down")
	}

	output := captureStderr(t, func() {
		printUpdateNotice()
	})

	if count := strings.Count(output, "Update available:"); count != 1 {
		t.Fatalf("expected 1 notice, got %d: %q", count, output)
	}
	if !strings.Contains(output, "v0.3.10 → v0.3.11") {
		t.Fatalf("expected cached latest in notice, got %q", output)
	}

	loaded, err := loadCache(path)
	if err != nil {
		t.Fatalf("loadCache: %v", err)
	}
	if loaded.Latest != "v0.3.11" || !loaded.Outdated {
		t.Fatalf("unexpected cache after failed refresh: %+v", loaded)
	}
	if loaded.CheckedAt <= before {
		t.Fatalf("expected CheckedAt to be refreshed, got %d <= %d", loaded.CheckedAt, before)
	}
}

func TestPrintUpdateNotice_SkipsDevVersion(t *testing.T) {
	path := prepareVersionCheckEnv(t, "dev")
	fetchCalled := false
	fetchLatestReleaseFn = func() (*ghRelease, error) {
		fetchCalled = true
		return &ghRelease{TagName: "v0.3.11"}, nil
	}

	output := captureStderr(t, func() {
		printUpdateNotice()
	})

	if fetchCalled {
		t.Fatal("expected dev version to skip remote fetch")
	}
	if output != "" {
		t.Fatalf("expected no notice for dev version, got %q", output)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no cache file for dev version")
	}
}

func TestLoadSaveCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	c := &versionCache{
		CheckedAt: time.Now().Unix(),
		Latest:    "v1.2.3",
		Outdated:  true,
	}
	saveCache(path, c)

	loaded, err := loadCache(path)
	if err != nil {
		t.Fatalf("loadCache: %v", err)
	}
	if loaded.CheckedAt != c.CheckedAt {
		t.Errorf("CheckedAt = %d, want %d", loaded.CheckedAt, c.CheckedAt)
	}
	if loaded.Latest != c.Latest {
		t.Errorf("Latest = %q, want %q", loaded.Latest, c.Latest)
	}
	if loaded.Outdated != c.Outdated {
		t.Errorf("Outdated = %v, want %v", loaded.Outdated, c.Outdated)
	}
}

func TestLoadCacheMissing(t *testing.T) {
	_, err := loadCache("/nonexistent/path/cache.json")
	if err == nil {
		t.Error("expected error for missing cache file")
	}
}

func TestLoadCacheCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	_ = os.WriteFile(path, []byte("not json"), 0644)

	_, err := loadCache(path)
	if err == nil {
		t.Error("expected error for corrupt cache file")
	}
}

func TestSaveCacheCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "deep", "cache.json")

	c := &versionCache{CheckedAt: 123, Latest: "v2.0.0", Outdated: true}
	saveCache(path, c)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	var loaded versionCache
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.CheckedAt != 123 {
		t.Errorf("CheckedAt = %d, want 123", loaded.CheckedAt)
	}
	if loaded.Latest != "v2.0.0" {
		t.Errorf("Latest = %q, want %q", loaded.Latest, "v2.0.0")
	}
	if !loaded.Outdated {
		t.Error("Outdated = false, want true")
	}
}
