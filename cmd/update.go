package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const repoAPI = "https://api.github.com/repos/youngwoocho02/unity-cli/releases/latest"

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func updateCmd(args []string) error {
	flags := parseSubFlags(args)
	_, checkOnly := flags["check"]

	fmt.Println("Checking for updates...")

	release, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latest := release.TagName
	current := Version

	if current == latest {
		fmt.Printf("Already up to date (%s)\n", current)
		return nil
	}

	fmt.Printf("Update available: %s → %s\n", current, latest)

	if checkOnly {
		return nil
	}

	asset := findAsset(release.Assets)
	if asset == nil {
		return fmt.Errorf("no binary found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate current binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("cannot resolve binary path: %w", err)
	}

	fmt.Printf("Downloading %s...\n", asset.Name)

	tmpFile, err := download(asset.BrowserDownloadURL, filepath.Dir(exe))
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	if err := os.Chmod(tmpFile, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	backup := exe + ".bak"
	if err := os.Rename(exe, backup); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	if err := os.Rename(tmpFile, exe); err != nil {
		if restoreErr := os.Rename(backup, exe); restoreErr != nil {
			return fmt.Errorf("replace failed: %w (restore also failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace failed: %w", err)
	}

	_ = os.Remove(backup)

	fmt.Printf("Updated to %s\n", latest)
	return nil
}

func fetchLatestRelease() (*ghRelease, error) {
	resp, err := http.Get(repoAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func findAsset(assets []ghAsset) *ghAsset {
	suffix := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	for i, a := range assets {
		if strings.Contains(a.Name, suffix) {
			return &assets[i]
		}
	}
	return nil
}

func download(url string, targetDir string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(targetDir, "unity-cli-update-*")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}

	return tmp.Name(), nil
}
