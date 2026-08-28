package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestActiveInstances_SkipsStoppedAndZeroTimestamp(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"stopped.json": {
			State:       "stopped",
			ProjectPath: "/projects/old",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
		"zero_timestamp.json": {
			State:       "ready",
			ProjectPath: "/projects/incomplete",
			Port:        8091,
			PID:         150,
			Timestamp:   0,
		},
		"ready.json": {
			State:       "ready",
			ProjectPath: "/projects/current",
			Port:        8092,
			PID:         200,
			Timestamp:   2000,
		},
	})
	t.Setenv("HOME", home)

	got, err := ActiveInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 active instance, got %d", len(got))
	}
	if got[0].ProjectPath != "/projects/current" {
		t.Errorf("ProjectPath: got %q, want /projects/current", got[0].ProjectPath)
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

func TestScanInstances_RemovesZeroPID(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"zero_pid.json": {
			State:       "ready",
			ProjectPath: "/projects/zero-pid",
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
	if len(instances) != 0 {
		t.Fatalf("expected 0 instances, got %d", len(instances))
	}
	fp := filepath.Join(home, ".unity-cli", "instances", "zero_pid.json")
	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Error("zero_pid.json should have been deleted")
	}
}

func TestScanInstances_RetriesTransientInvalidJSON(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".unity-cli", "instances")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create instances dir: %v", err)
	}
	path := filepath.Join(dir, "instance.json")
	if err := os.WriteFile(path, []byte("{"), 0644); err != nil {
		t.Fatalf("failed to write partial instance: %v", err)
	}

	done := make(chan struct{})
	go func() {
		time.Sleep(5 * time.Millisecond)
		data, _ := json.Marshal(Instance{
			State:       "ready",
			ProjectPath: "/projects/current",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		})
		_ = os.WriteFile(path, data, 0644)
		close(done)
	}()
	t.Cleanup(func() { <-done })

	instances, err := ScanInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].ProjectPath != "/projects/current" {
		t.Errorf("ProjectPath: got %q, want /projects/current", instances[0].ProjectPath)
	}
}

func TestScanInstances_SkipsPersistentInvalidJSON(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".unity-cli", "instances")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create instances dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), []byte("{"), 0644); err != nil {
		t.Fatalf("failed to write partial instance: %v", err)
	}

	instances, err := ScanInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 0 {
		t.Fatalf("expected 0 instances, got %d", len(instances))
	}
}

func TestHealth_ReturnsListenerSnapshot(t *testing.T) {
	server, port := healthTestServer(t, http.StatusOK, `{"success":true,"message":"ok","data":{"state":"ready","projectPath":"/projects/current","port":8090,"pid":123,"unityVersion":"6000.3.10f1","connectorVersion":"0.3.19","timestamp":1000,"ready":true,"listenerRunning":true}}`)
	_ = server

	got, err := Health(&Instance{Port: port}, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProjectPath != "/projects/current" {
		t.Errorf("ProjectPath: got %q, want /projects/current", got.ProjectPath)
	}
	if got.ConnectorVersion != "0.3.19" {
		t.Errorf("ConnectorVersion: got %q, want 0.3.19", got.ConnectorVersion)
	}
}

func TestHealth_RejectsNotReadySnapshot(t *testing.T) {
	server, port := healthTestServer(t, http.StatusOK, `{"success":true,"message":"ok","data":{"state":"starting","projectPath":"","port":8090,"pid":0,"timestamp":0,"ready":false}}`)
	_ = server

	if _, err := Health(&Instance{Port: port}, 1000); err == nil {
		t.Fatal("expected not-ready health snapshot error")
	}
}

func TestHealth_ReturnsNonOKError(t *testing.T) {
	server, port := healthTestServer(t, http.StatusServiceUnavailable, `busy`)
	_ = server

	if _, err := Health(&Instance{Port: port}, 1000); err == nil {
		t.Fatal("expected non-OK health error")
	}
}

func TestHealth_ReturnsMissingHealthEndpointSentinel(t *testing.T) {
	server, port := healthTestServer(t, http.StatusNotFound, `missing`)
	_ = server

	_, err := Health(&Instance{Port: port}, 1000)
	if !errors.Is(err, ErrHealthEndpointUnavailable) {
		t.Fatalf("expected ErrHealthEndpointUnavailable, got %v", err)
	}
}

func TestHealth_ReturnsMissingHealthEndpointSentinelForLegacyCommandOnlyEndpoint(t *testing.T) {
	server, port := healthTestServer(t, http.StatusBadRequest, `{"success":false,"message":"Expected POST /command, got GET /health","data":null}`)
	_ = server

	_, err := Health(&Instance{Port: port}, 1000)
	if !errors.Is(err, ErrHealthEndpointUnavailable) {
		t.Fatalf("expected ErrHealthEndpointUnavailable, got %v", err)
	}
}

func TestHealth_ReturnsInvalidJSONError(t *testing.T) {
	server, port := healthTestServer(t, http.StatusOK, `{`)
	_ = server

	if _, err := Health(&Instance{Port: port}, 1000); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestHealth_ReturnsUnsuccessfulHealthError(t *testing.T) {
	server, port := healthTestServer(t, http.StatusOK, `{"success":false,"message":"not ready"}`)
	_ = server

	if _, err := Health(&Instance{Port: port}, 1000); err == nil {
		t.Fatal("expected unsuccessful health error")
	}
}

func TestHealth_RejectsProjectMismatch(t *testing.T) {
	server, port := healthTestServer(t, http.StatusOK, `{"success":true,"message":"ok","data":{"state":"ready","projectPath":"/projects/other","port":8090,"pid":123,"timestamp":1000,"ready":true}}`)
	_ = server

	if _, err := Health(&Instance{ProjectPath: "/projects/current", Port: port}, 1000); err == nil {
		t.Fatal("expected project mismatch error")
	}
}

func TestHealth_RejectsNotDispatchableSnapshot(t *testing.T) {
	server, port := healthTestServer(t, http.StatusOK, `{"success":true,"message":"ok","data":{"state":"compiling","projectPath":"/projects/current","port":8090,"pid":123,"timestamp":1000,"ready":true,"dispatchable":false}}`)
	_ = server

	_, err := Health(&Instance{Port: port}, 1000)
	if !errors.Is(err, ErrUnityNotAccepting) {
		t.Fatalf("expected ErrUnityNotAccepting, got %v", err)
	}
}

func TestHealth_AllowsLegacySnapshotWithoutDispatchable(t *testing.T) {
	server, port := healthTestServer(t, http.StatusOK, `{"success":true,"message":"ok","data":{"state":"ready","projectPath":"/projects/current","port":8090,"pid":123,"timestamp":1000,"ready":true}}`)
	_ = server

	if _, err := Health(&Instance{Port: port}, 1000); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHealth_ConnectionErrorDoesNotExposePort(t *testing.T) {
	port := closedLocalPort(t)

	_, err := Health(&Instance{Port: port}, 1000)
	if err == nil {
		t.Fatal("expected connection error")
	}
	assertNoPortLeak(t, err.Error(), port)
}

func TestSend_ConnectionErrorDoesNotExposePort(t *testing.T) {
	port := closedLocalPort(t)

	_, err := Send(&Instance{Port: port}, "exec", nil, 1000)
	if err == nil {
		t.Fatal("expected connection error")
	}
	if !errors.Is(err, ErrUnityUnreachable) {
		t.Fatalf("expected ErrUnityUnreachable, got %v", err)
	}
	assertNoPortLeak(t, err.Error(), port)
}

func TestSend_ServiceUnavailableIsNotAccepting(t *testing.T) {
	server, port := commandTestServer(t, http.StatusServiceUnavailable, `{"success":false,"message":"unity is busy with another command"}`)
	_ = server

	_, err := Send(&Instance{Port: port}, "exec", nil, 1000)
	if !errors.Is(err, ErrUnityNotAccepting) {
		t.Fatalf("expected ErrUnityNotAccepting, got %v", err)
	}
}

func TestSend_EmptyBodyIsDisconnected(t *testing.T) {
	server, port := commandTestServer(t, http.StatusOK, "")
	_ = server

	_, err := Send(&Instance{Port: port}, "exec", nil, 1000)
	if !errors.Is(err, ErrUnityDisconnected) {
		t.Fatalf("expected ErrUnityDisconnected, got %v", err)
	}
}

func healthTestServer(t *testing.T, status int, body string) (*http.Server, int) {
	t.Helper()
	return localTestServer(t, "/health", status, body)
}

func commandTestServer(t *testing.T, status int, body string) (*http.Server, int) {
	t.Helper()
	return localTestServer(t, "/command", status, body)
}

func localTestServer(t *testing.T, path string, status int, body string) (*http.Server, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	var port int
	if _, err := fmt.Sscanf(listener.Addr().String(), "127.0.0.1:%d", &port); err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}
	return server, port
}

func closedLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(listener.Addr().String(), "127.0.0.1:%d", &port); err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("failed to close listener: %v", err)
	}
	return port
}

func assertNoPortLeak(t *testing.T, message string, port int) {
	t.Helper()
	if strings.Contains(message, "127.0.0.1") {
		t.Fatalf("error should not expose host, got %q", message)
	}
	if strings.Contains(message, fmt.Sprintf("%d", port)) {
		t.Fatalf("error should not expose port, got %q", message)
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

	got, err := DiscoverInstance(`E:\GamerAworlD`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProjectPath != "E:/GamerAworlD" {
		t.Errorf("ProjectPath: got %q, want %q", got.ProjectPath, "E:/GamerAworlD")
	}
}

func TestDiscoverInstance_ProjectPathMatchesCaseVariants(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive path matching is Windows-only")
	}
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"project.json": {
			State:       "ready",
			ProjectPath: "C:/WorkSpace/ProjectMaid",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
	})
	t.Setenv("HOME", home)

	got, err := DiscoverInstance("c:/workspace/projectmaid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProjectPath != "C:/WorkSpace/ProjectMaid" {
		t.Errorf("ProjectPath: got %q, want C:/WorkSpace/ProjectMaid", got.ProjectPath)
	}
}

func TestDiscoverInstance_UsesCwdProjectMatch(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"project.json": {
			State:       "ready",
			ProjectPath: "/projects/current",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
		"other.json": {
			State:       "ready",
			ProjectPath: "/projects/other",
			Port:        8091,
			PID:         200,
			Timestamp:   2000,
		},
	})
	t.Setenv("HOME", home)

	cwd := filepath.Join(t.TempDir(), "current", "Assets")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatalf("failed to create cwd: %v", err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// Rewrite the project path to the temp root because tests should not depend on fixed /projects paths.
	home = writeInstanceFiles(t, map[string]Instance{
		"project.json": {
			State:       "ready",
			ProjectPath: filepath.Dir(cwd),
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
		"other.json": {
			State:       "ready",
			ProjectPath: "/projects/other",
			Port:        8091,
			PID:         200,
			Timestamp:   2000,
		},
	})
	t.Setenv("HOME", home)

	got, err := DiscoverInstance("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalizeProjectPath(got.ProjectPath) != normalizeProjectPath(filepath.Dir(cwd)) {
		t.Errorf("ProjectPath: got %q, want %q", got.ProjectPath, filepath.Dir(cwd))
	}
}

func TestDiscoverInstance_UsesCwdProjectMatchCaseInsensitiveOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows project paths are case-insensitive")
	}
	stubIsProcessDead(t, map[int]bool{})

	cwd := filepath.Join(t.TempDir(), "ProjectMaid", "Assets")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatalf("failed to create cwd: %v", err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	projectPath := strings.ToUpper(filepath.Dir(cwd))
	home := writeInstanceFiles(t, map[string]Instance{
		"project.json": {
			State:       "ready",
			ProjectPath: projectPath,
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
		"other.json": {
			State:       "ready",
			ProjectPath: filepath.Join(t.TempDir(), "Other"),
			Port:        8091,
			PID:         200,
			Timestamp:   2000,
		},
	})
	t.Setenv("HOME", home)

	got, err := DiscoverInstance("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProjectPath != projectPath {
		t.Errorf("ProjectPath: got %q, want %q", got.ProjectPath, projectPath)
	}
}

func TestDiscoverInstance_UsesSingleActiveInstance(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"project.json": {
			State:       "ready",
			ProjectPath: "/projects/current",
			Port:        8090,
			PID:         100,
			Timestamp:   1000,
		},
	})
	t.Setenv("HOME", home)

	got, err := DiscoverInstance("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProjectPath != "/projects/current" {
		t.Errorf("ProjectPath: got %q, want /projects/current", got.ProjectPath)
	}
}

func TestDiscoverInstance_RejectsAmbiguousProjectSelection(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"one.json": {
			State:       "ready",
			ProjectPath: "/projects/one",
			Port:        8091,
			PID:         200,
			Timestamp:   12345,
		},
		"two.json": {
			State:       "ready",
			ProjectPath: "/projects/two",
			Port:        8092,
			PID:         300,
			Timestamp:   12346,
		},
	})
	t.Setenv("HOME", home)

	if _, err := DiscoverInstance(""); err == nil {
		t.Fatal("expected error for multiple instances without project")
	}
}

func TestDiscoverInstance_RejectsAmbiguousProjectSubstring(t *testing.T) {
	stubIsProcessDead(t, map[int]bool{})

	home := writeInstanceFiles(t, map[string]Instance{
		"one.json": {
			State:       "ready",
			ProjectPath: "/projects/MyGame",
			Port:        8091,
			PID:         200,
			Timestamp:   12345,
		},
		"two.json": {
			State:       "ready",
			ProjectPath: "/archive/MyGame",
			Port:        8092,
			PID:         300,
			Timestamp:   12346,
		},
	})
	t.Setenv("HOME", home)

	if _, err := DiscoverInstance("MyGame"); err == nil {
		t.Fatal("expected error for ambiguous project substring")
	}
}
