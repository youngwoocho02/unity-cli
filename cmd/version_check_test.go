package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"v1.1.0", "v1.0.0", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.0.1", "v1.0.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.1", false},
		{"v0.9.0", "v1.0.0", false},
		{"v1.10.0", "v1.9.0", true},
	}
	for _, tt := range tests {
		got := isNewer(tt.latest, tt.current)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input string
		want  []int
	}{
		{"v1.2.3", []int{1, 2, 3}},
		{"1.2.3", []int{1, 2, 3}},
		{"v0.0.0", []int{0, 0, 0}},
		{"v10.20.30", []int{10, 20, 30}},
		{"dev", nil},
		{"v1.2", nil},
		{"v1.2.3-beta", nil},
	}
	for _, tt := range tests {
		got := parseSemver(tt.input)
		if tt.want == nil {
			if got != nil {
				t.Errorf("parseSemver(%q) = %v, want nil", tt.input, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("parseSemver(%q) = nil, want %v", tt.input, tt.want)
			continue
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Errorf("parseSemver(%q)[%d] = %d, want %d", tt.input, i, got[i], tt.want[i])
			}
		}
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
