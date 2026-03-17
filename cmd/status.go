package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func statusCmd(inst *client.Instance) error {
	// Instance already has fresh data from DiscoverInstance scan
	age := time.Since(time.UnixMilli(inst.Timestamp))
	if age > 3*time.Second {
		fmt.Fprintf(os.Stderr, "Unity (port %d): not responding (last heartbeat %s ago)\n", inst.Port, age.Truncate(time.Second))
		return nil
	}

	fmt.Printf("Unity (port %d): %s\n", inst.Port, inst.State)
	fmt.Printf("  Project: %s\n", inst.ProjectPath)
	fmt.Printf("  Version: %s\n", inst.UnityVersion)
	fmt.Printf("  PID:     %d\n", inst.PID)
	return nil
}

// waitForAlive polls the instance file until the timestamp updates.
func waitForAlive(projectPath string, timeoutMs int) error {
	baseline := time.Now().UnixMilli()
	if inst, err := client.ReadInstance(projectPath); err == nil {
		baseline = inst.Timestamp
	}

	// Already fresh
	if time.Now().UnixMilli()-baseline < 1000 {
		return nil
	}

	fmt.Fprintf(os.Stderr, "Waiting for Unity...\n")

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		inst, err := client.ReadInstance(projectPath)
		if err != nil {
			continue
		}
		if inst.Timestamp > baseline {
			fmt.Fprintf(os.Stderr, "Unity is ready.\n")
			return nil
		}
	}

	return fmt.Errorf("timed out waiting for Unity")
}

// waitForReady polls until the instance state becomes "ready" or the timeout expires.
func waitForReady(projectPath string, timeoutMs int) error {
	fmt.Fprintf(os.Stderr, "Waiting for compilation...\n")

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		inst, err := client.ReadInstance(projectPath)
		if err != nil {
			continue
		}
		if inst.State == "ready" {
			fmt.Fprintf(os.Stderr, "Compilation complete.\n")
			return nil
		}
	}

	return fmt.Errorf("timed out waiting for compilation")
}
