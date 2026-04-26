package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// stubIsProcessDead replaces isProcessDead for testing.
// deadPIDs maps PID → true if the process is confirmed dead.
func stubIsProcessDead(t *testing.T, deadPIDs map[int]bool) {
	t.Helper()
	orig := isProcessDead
	isProcessDead = func(pid int) bool {
		return deadPIDs[pid]
	}
	t.Cleanup(func() { isProcessDead = orig })
}

// writeInstanceFiles creates isolated instance files and points both HOME and
// USERPROFILE to the temp directory so tests never read real local instances.
func writeInstanceFiles(t *testing.T, files map[string]Instance) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".unity-cli", "instances")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create instances dir: %v", err)
	}
	for name, inst := range files {
		data, err := json.Marshal(inst)
		if err != nil {
			t.Fatalf("failed to marshal instance: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatalf("failed to write instance file: %v", err)
		}
	}
	return home
}

// --- FindActiveByPort tests ---

// TestFindActiveByPort_SkipsStoppedPicksLatest verifies the core bug fix:
// when a stopped instance and a ready instance share the same port,
// FindActiveByPort must return the ready instance with the latest timestamp.
func TestFindActiveByPort_SkipsStoppedPicksLatest(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		// Alphabetically first — the old bug would pick this one
		"aaa_stopped.json": {
			State:       "stopped",
			ProjectPath: "/projects/old",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
		"bbb_ready.json": {
			State:       "ready",
			ProjectPath: "/projects/current",
			Port:        8090,
			PID:         200,
			Timestamp:   2000,
		},
	})
	t.Setenv("HOME", home)

	got, err := FindActiveByPort(8090)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.State != "ready" {
		t.Errorf("State: got %q, want %q", got.State, "ready")
	}
	if got.ProjectPath != "/projects/current" {
		t.Errorf("ProjectPath: got %q, want %q", got.ProjectPath, "/projects/current")
	}
	if got.Timestamp != 2000 {
		t.Errorf("Timestamp: got %d, want %d", got.Timestamp, 2000)
	}
}

// TestFindActiveByPort_PicksLatestTimestamp verifies that among multiple active
// instances on the same port, the one with the newest timestamp wins.
func TestFindActiveByPort_PicksLatestTimestamp(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"aaa_old.json": {
			State:       "ready",
			ProjectPath: "/projects/old",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
		"bbb_new.json": {
			State:       "ready",
			ProjectPath: "/projects/new",
			Port:        8090,
			PID:         200,
			Timestamp:   2000,
		},
	})
	t.Setenv("HOME", home)

	got, err := FindActiveByPort(8090)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Timestamp != 2000 {
		t.Errorf("Timestamp: got %d, want %d", got.Timestamp, 2000)
	}
}

func TestFindActiveByPort_SkipsZeroTimestamp(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"zero_timestamp.json": {
			State:       "ready",
			ProjectPath: "/projects/incomplete",
			Port:        8090,
			PID:         100,
			Timestamp:   0,
		},
	})
	t.Setenv("HOME", home)

	if _, err := FindActiveByPort(8090); err == nil {
		t.Fatal("expected error for instance without heartbeat timestamp")
	}
}

// --- FindByPort tests (exact lookup, includes stopped) ---

// TestFindByPort_ReturnsStoppedInstance verifies that FindByPort returns
// stopped instances, so `unity-cli status` can display them.
func TestFindByPort_ReturnsStoppedInstance(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"stopped.json": {
			State:       "stopped",
			ProjectPath: "/projects/old",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
	})
	t.Setenv("HOME", home)

	got, err := FindByPort(8090)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.State != "stopped" {
		t.Errorf("State: got %q, want %q", got.State, "stopped")
	}
}

// TestFindByPort_PicksLatestWhenMixed verifies that FindByPort picks
// the latest timestamp even among mixed states.
func TestFindByPort_PicksLatestWhenMixed(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"aaa_stopped.json": {
			State:       "stopped",
			ProjectPath: "/projects/old",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
		"bbb_ready.json": {
			State:       "ready",
			ProjectPath: "/projects/current",
			Port:        8090,
			PID:         200,
			Timestamp:   2000,
		},
	})
	t.Setenv("HOME", home)

	got, err := FindByPort(8090)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Latest timestamp wins regardless of state
	if got.Timestamp != 2000 {
		t.Errorf("Timestamp: got %d, want %d", got.Timestamp, 2000)
	}
}

// --- ScanInstances tests ---

// TestScanInstances_RemovesDeadPID verifies that instance files with
// a confirmed-dead PID are removed from disk and excluded from results.
func TestScanInstances_RemovesDeadPID(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{
		100: true,  // confirmed dead
		200: false, // alive
	})

	home := writeInstanceFiles(t, map[string]Instance{
		"dead.json": {
			State:       "ready",
			ProjectPath: "/projects/dead",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
		"alive.json": {
			State:       "ready",
			ProjectPath: "/projects/alive",
			Port:        8091,
			PID:         200,
			Timestamp:   2000,
		},
	})
	t.Setenv("HOME", home)

	instances, err := ScanInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].ProjectPath != "/projects/alive" {
		t.Errorf("expected alive instance, got %q", instances[0].ProjectPath)
	}

	// Verify the dead file was actually deleted
	deadPath := filepath.Join(home, ".unity-cli", "instances", "dead.json")
	if _, err := os.Stat(deadPath); !os.IsNotExist(err) {
		t.Error("dead.json should have been deleted")
	}
}

// TestScanInstances_KeepsOnPermissionError verifies that when isProcessDead
// returns false (e.g. permission error), the instance file is preserved.
func TestScanInstances_KeepsOnPermissionError(t *testing.T) {
	// isProcessDead returns false for PID 100 — simulates EPERM / ACCESS_DENIED
	stubIsProcessDead(t, map[int]bool{100: false})

	home := writeInstanceFiles(t, map[string]Instance{
		"eperm.json": {
			State:       "ready",
			ProjectPath: "/projects/eperm",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
	})
	t.Setenv("HOME", home)

	instances, err := ScanInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}

	// Verify the file was NOT deleted
	fp := filepath.Join(home, ".unity-cli", "instances", "eperm.json")
	if _, err := os.Stat(fp); err != nil {
		t.Error("eperm.json should have been preserved")
	}
}

// TestScanInstances_KeepsZeroPID verifies that instances with PID 0
// (e.g. legacy files) are kept without process checking.
func TestScanInstances_KeepsZeroPID(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"legacy.json": {
			State:       "ready",
			ProjectPath: "/projects/legacy",
			Port:        8090,
			PID:         0,
			Timestamp:   1000,
		},
	})
	t.Setenv("HOME", home)

	instances, err := ScanInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
}

// TestDiscoverInstance_ProjectPathMatchesSlashVariants verifies --project can
// match Windows-style backslashes against Unity's forward-slash projectPath.
func TestDiscoverInstance_ProjectPathMatchesSlashVariants(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("slash normalization of backslashes is Windows-only")
	}
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"project.json": {
			State:       "ready",
			ProjectPath: "E:/GamerAworlD",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
	})
	t.Setenv("HOME", home)

	got, err := DiscoverInstance(`E:\GamerAworlD`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProjectPath != "E:/GamerAworlD" {
		t.Errorf("ProjectPath: got %q, want %q", got.ProjectPath, "E:/GamerAworlD")
	}
}

// TestDiscoverInstance_PortFlagPopulatesTimestamp verifies that --port lookups
// return the actual instance file's timestamp when one exists. Without this,
// waitForAlive sees Timestamp=0 and polls indefinitely for a "newer" heartbeat
// that never arrives, hanging on "Waiting for Unity..." until --timeout expires.
func TestDiscoverInstance_PortFlagPopulatesTimestamp(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"current.json": {
			State:       "ready",
			ProjectPath: "/projects/current",
			Port:        8091,
			PID:         200,
			Timestamp:   12345,
		},
	})
	t.Setenv("HOME", home)

	got, err := DiscoverInstance("", 8091)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Port != 8091 {
		t.Errorf("Port: got %d, want %d", got.Port, 8091)
	}
	if got.Timestamp != 12345 {
		t.Errorf("Timestamp: got %d, want %d", got.Timestamp, 12345)
	}
	if got.ProjectPath != "/projects/current" {
		t.Errorf("ProjectPath: got %q, want %q", got.ProjectPath, "/projects/current")
	}
}

// TestDiscoverInstance_PortFlagRequiresActiveInstance verifies that --port
// still resolves through heartbeat state instead of manufacturing a partial
// Instance that polling code can mistake for real status.
func TestDiscoverInstance_PortFlagRequiresActiveInstance(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{})
	t.Setenv("HOME", home)

	if _, err := DiscoverInstance("", 8091); err == nil {
		t.Fatal("expected error for port without an active instance file")
	}
}
