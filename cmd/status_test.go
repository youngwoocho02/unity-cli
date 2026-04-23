package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func writeInstanceFile(t *testing.T, inst client.Instance) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".unity-cli", "instances")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create instances dir: %v", err)
	}
	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("failed to marshal instance: %v", err)
	}
	// Use a fixed filename for testing
	path := filepath.Join(dir, "test.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write instance file: %v", err)
	}
	return home
}

func TestReadStatus_ValidFile(t *testing.T) {
	want := client.Instance{
		State:        "ready",
		ProjectPath:  "/home/user/MyProject",
		Port:         8090,
		PID:          os.Getpid(),
		UnityVersion: "6000.3.10f1",
		Timestamp:    1000000,
	}

	writeInstanceFile(t, want)

	got, err := readStatus(8090)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.State != want.State {
		t.Errorf("State: got %q, want %q", got.State, want.State)
	}
	if got.Port != want.Port {
		t.Errorf("Port: got %d, want %d", got.Port, want.Port)
	}
	if got.ProjectPath != want.ProjectPath {
		t.Errorf("ProjectPath: got %q, want %q", got.ProjectPath, want.ProjectPath)
	}
}

func TestReadStatus_MissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	_, err := readStatus(9999)
	if err == nil {
		t.Error("expected error for missing status file")
	}
}

func TestReadStatus_InvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".unity-cli", "instances")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.json"), []byte("not json"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := readStatus(8090)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWaitForAlive_FollowsResolverPortChange(t *testing.T) {
	origPollInterval := statusPollInterval
	statusPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { statusPollInterval = origPollInterval })

	project := "C:/WorkSpace/ProjectMaid"
	callCount := 0
	resolve := func() (*client.Instance, error) {
		callCount++
		if callCount == 1 {
			return &client.Instance{
				State:       "reloading",
				ProjectPath: project,
				Port:        8090,
				Timestamp:   time.Now().Add(-5 * time.Second).UnixMilli(),
			}, nil
		}
		return &client.Instance{
			State:       "ready",
			ProjectPath: project,
			Port:        8091,
			Timestamp:   time.Now().UnixMilli(),
		}, nil
	}

	inst, err := waitForAlive(resolve, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.Port != 8091 {
		t.Fatalf("expected updated port 8091, got %d", inst.Port)
	}
	if callCount < 2 {
		t.Fatalf("expected resolver to be called multiple times, got %d", callCount)
	}
}
