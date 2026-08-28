package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestStatusCmd_ReturnsConnectorVersionMismatch(t *testing.T) {
	origVersion := Version
	Version = "v0.3.14"
	t.Cleanup(func() { Version = origVersion })

	writeInstanceFile(t, client.Instance{
		State:            "ready",
		ProjectPath:      "/home/user/MyProject",
		Port:             8090,
		PID:              os.Getpid(),
		UnityVersion:     "6000.3.10f1",
		ConnectorVersion: "0.3.13",
		Timestamp:        time.Now().UnixMilli(),
	})

	err := statusCmd("/home/user/MyProject", false)
	if err == nil {
		t.Fatal("expected connector mismatch error")
	}
	if !strings.Contains(err.Error(), "connector version mismatch") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestStatusInstances_IncludesStoppedHeartbeat(t *testing.T) {
	writeInstanceFile(t, client.Instance{
		State:       "stopped",
		ProjectPath: "/home/user/MyProject",
		Port:        8090,
		PID:         os.Getpid(),
		Timestamp:   time.Now().UnixMilli(),
	})

	instances, err := statusInstances("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 status instance, got %d", len(instances))
	}
	if instances[0].State != "stopped" {
		t.Errorf("State: got %q, want stopped", instances[0].State)
	}
}

func TestStatusInstances_ProjectFilterMatchesCaseVariants(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive path matching is Windows-only")
	}

	writeInstanceFile(t, client.Instance{
		State:       "ready",
		ProjectPath: "C:/WorkSpace/ProjectMaid",
		Port:        8090,
		PID:         os.Getpid(),
		Timestamp:   time.Now().UnixMilli(),
	})

	instances, err := statusInstances("c:/workspace/projectmaid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 status instance, got %d", len(instances))
	}
}

func TestStatusCmd_ReturnsMissingConnectorVersion(t *testing.T) {
	origVersion := Version
	Version = "v0.3.14"
	t.Cleanup(func() { Version = origVersion })

	writeInstanceFile(t, client.Instance{
		State:        "ready",
		ProjectPath:  "/home/user/MyProject",
		Port:         8090,
		PID:          os.Getpid(),
		UnityVersion: "6000.3.10f1",
		Timestamp:    time.Now().UnixMilli(),
	})

	err := statusCmd("/home/user/MyProject", false)
	if err == nil {
		t.Fatal("expected missing connector version error")
	}
	if !strings.Contains(err.Error(), "connector version is unknown") {
		t.Fatalf("expected missing version error, got %v", err)
	}
}

func TestStatusCmd_AllowsMatchingConnectorVersion(t *testing.T) {
	origVersion := Version
	Version = "v0.3.14"
	t.Cleanup(func() { Version = origVersion })

	writeInstanceFile(t, client.Instance{
		State:            "ready",
		ProjectPath:      "/home/user/MyProject",
		Port:             8090,
		PID:              os.Getpid(),
		UnityVersion:     "6000.3.10f1",
		ConnectorVersion: "0.3.14",
		Timestamp:        time.Now().UnixMilli(),
	})

	if err := statusCmd("/home/user/MyProject", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatusCmd_ReturnsConnectorVersionMismatchWhenStale(t *testing.T) {
	origVersion := Version
	Version = "v0.3.14"
	t.Cleanup(func() { Version = origVersion })

	writeInstanceFile(t, client.Instance{
		State:            "ready",
		ProjectPath:      "/home/user/MyProject",
		Port:             8090,
		PID:              os.Getpid(),
		UnityVersion:     "6000.3.10f1",
		ConnectorVersion: "0.3.13",
		Timestamp:        time.Now().Add(-10 * time.Second).UnixMilli(),
	})

	err := statusCmd("/home/user/MyProject", false)
	if err == nil {
		t.Fatal("expected stale connector mismatch error")
	}
	if !strings.Contains(err.Error(), "connector version mismatch") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestCheckConnectorVersion_AllowsMatchingLeadingV(t *testing.T) {
	inst := &client.Instance{ConnectorVersion: "0.3.10"}
	if err := checkConnectorVersion(inst, "v0.3.10", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckConnectorVersion_RejectsMismatch(t *testing.T) {
	inst := &client.Instance{ConnectorVersion: "0.3.9"}
	err := checkConnectorVersion(inst, "v0.3.10", false)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if !strings.Contains(err.Error(), "ignore-version-mismatch") {
		t.Fatalf("expected ignore option hint, got %v", err)
	}
}

func TestCheckConnectorVersion_RejectsMissingConnectorVersion(t *testing.T) {
	inst := &client.Instance{}
	err := checkConnectorVersion(inst, "v0.3.10", false)
	if err == nil {
		t.Fatal("expected missing connector version error")
	}
	if !strings.Contains(err.Error(), "ignore-version-mismatch") {
		t.Fatalf("expected ignore option hint, got %v", err)
	}
}

func TestCheckConnectorVersion_SkipsDevCliVersion(t *testing.T) {
	inst := &client.Instance{ConnectorVersion: "0.3.10"}
	if err := checkConnectorVersion(inst, "dev", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckConnectorVersion_IgnoreMismatchAllowsMismatch(t *testing.T) {
	inst := &client.Instance{ConnectorVersion: "0.3.9"}
	if err := checkConnectorVersion(inst, "v0.3.10", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckConnectorVersion_IgnoreMismatchAllowsMissingConnectorVersion(t *testing.T) {
	inst := &client.Instance{}
	if err := checkConnectorVersion(inst, "v0.3.10", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForReadyReturnsFalseWhenReady(t *testing.T) {
	orig := statusPollInterval
	statusPollInterval = time.Millisecond
	t.Cleanup(func() { statusPollInterval = orig })

	if waitForReady(func() (*client.Instance, error) {
		return &client.Instance{State: "ready"}, nil
	}) {
		t.Fatal("expected no compile errors")
	}
}

func TestWaitForReadyReturnsCompileErrors(t *testing.T) {
	orig := statusPollInterval
	statusPollInterval = time.Millisecond
	t.Cleanup(func() { statusPollInterval = orig })

	if !waitForReady(func() (*client.Instance, error) {
		return &client.Instance{State: "ready", CompileErrors: true}, nil
	}) {
		t.Fatal("expected compile errors")
	}
}
