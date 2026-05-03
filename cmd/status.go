package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

type instanceResolver func() (*client.Instance, error)

var statusPollInterval = 500 * time.Millisecond

func statusCmd(inst *client.Instance) error {
	status, err := readStatus(inst.Port)
	if err != nil {
		return fmt.Errorf("no status for port %d — Unity may not be running", inst.Port)
	}

	age := time.Since(time.UnixMilli(status.Timestamp))
	if age > 3*time.Second {
		fmt.Fprintf(os.Stderr, "Unity (port %d): not responding (last heartbeat %s ago)\n", status.Port, age.Truncate(time.Second))
		return checkConnectorVersion(status, Version, flagIgnoreVersionMismatch)
	}

	fmt.Printf("Unity (port %d): %s\n", status.Port, status.State)
	fmt.Printf("  Project: %s\n", status.ProjectPath)
	fmt.Printf("  Version: %s\n", status.UnityVersion)
	fmt.Printf("  Connector: %s\n", connectorVersionLabel(status.ConnectorVersion))
	fmt.Printf("  PID:     %d\n", status.PID)
	return checkConnectorVersion(status, Version, flagIgnoreVersionMismatch)
}

func discoverStatusInstance(project string, port int) (*client.Instance, error) {
	if port > 0 {
		return client.FindByPort(port)
	}
	return client.DiscoverInstance(project, 0)
}

// readStatus finds the instance file matching the given port (any state).
func readStatus(port int) (*client.Instance, error) {
	return client.FindByPort(port)
}

func connectorVersionLabel(version string) string {
	if strings.TrimSpace(version) == "" {
		return "unknown"
	}
	return version
}

func checkConnectorVersion(inst *client.Instance, cliVersion string, ignoreMismatch bool) error {
	if normalizeVersion(cliVersion) == "dev" {
		return nil
	}
	if ignoreMismatch {
		return nil
	}
	if inst == nil {
		return nil
	}

	connectorVersion := strings.TrimSpace(inst.ConnectorVersion)
	if connectorVersion == "" {
		return fmt.Errorf("connector version is unknown; update the Unity Connector package to match unity-cli %s, or rerun with --ignore-version-mismatch", cliVersion)
	}
	if normalizeVersion(connectorVersion) != normalizeVersion(cliVersion) {
		return fmt.Errorf("connector version mismatch: unity-cli %s, connector %s. Update both to the same release, or rerun with --ignore-version-mismatch", cliVersion, connectorVersion)
	}
	return nil
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	return version
}

// waitForAlive resolves the current target instance, then polls until a newer heartbeat appears.
// This keeps following the same project even if Unity rebinds to a new port during reload.
func waitForAlive(resolve instanceResolver, timeoutMs int) (*client.Instance, error) {
	baseline := time.Now().UnixMilli()
	inst, err := resolve()
	if err == nil {
		baseline = inst.Timestamp
		// Already fresh — check if timestamp was updated within the last second
		if time.Now().UnixMilli()-baseline < 1000 {
			return inst, nil
		}
	}

	fmt.Fprintf(os.Stderr, "Waiting for Unity...\n")

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(statusPollInterval)
		inst, err = resolve()
		if err != nil {
			continue
		}
		if inst.Timestamp > baseline {
			fmt.Fprintf(os.Stderr, "Unity is ready.\n")
			return inst, nil
		}
	}

	return nil, fmt.Errorf("timed out waiting for Unity")
}

// waitForReady polls indefinitely until the heartbeat state becomes "ready".
// Returns true if compilation had errors.
func waitForReady(resolve instanceResolver) bool {
	fmt.Fprintf(os.Stderr, "Waiting for compilation...\n")

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(statusPollInterval)
		status, err := resolve()
		if err != nil {
			continue
		}
		if status.State == "ready" {
			if status.CompileErrors {
				fmt.Fprintf(os.Stderr, "Compilation finished with errors.\n")
			} else {
				fmt.Fprintf(os.Stderr, "Compilation complete.\n")
			}
			return status.CompileErrors
		}
	}

	fmt.Fprintf(os.Stderr, "Timed out waiting for compilation (5m).\n")
	return true
}
