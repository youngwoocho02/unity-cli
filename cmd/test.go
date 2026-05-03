package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

type suppressWriter struct {
	w        io.Writer
	suppress string
}

func (s *suppressWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(s.suppress)) {
		return len(p), nil
	}
	return s.w.Write(p)
}

func testCmd(args []string, send sendFn, port int) (*client.CommandResponse, error) {
	flags := parseSubFlags(args)

	mode := "EditMode"
	if m, ok := flags["mode"]; ok {
		mode = m
	}

	if mode != "EditMode" && mode != "PlayMode" {
		return nil, fmt.Errorf("--mode must be EditMode or PlayMode, got: %s", mode)
	}

	params := map[string]interface{}{
		"mode": mode,
	}
	if filter, ok := flags["filter"]; ok {
		params["filter"] = filter
	}
	if _, ok := flags["allow-dirty-scenes"]; ok {
		params["allowDirtyScenes"] = true
	}
	if _, ok := flags["auto-save-scenes"]; ok {
		params["autoSaveScenes"] = true
	}

	resp, err := send("run_tests", params)
	if err != nil {
		return nil, err
	}

	if !resp.Success && strings.Contains(resp.Message, "Unknown command") {
		return nil, fmt.Errorf(
			"'run_tests' is not available.\n" +
				"Install the Unity Test Framework package:\n" +
				"  Window > Package Manager > search 'Test Framework' > Install")
	}

	// EditMode: results returned directly in response
	if mode == "EditMode" {
		return resp, nil
	}

	// PlayMode: Unity returns "running", poll results file
	if resp.Message != "running" {
		return resp, nil
	}

	fmt.Fprintln(os.Stderr, "PlayMode tests running, waiting for results...")

	// Suppress "Unsolicited response received on idle HTTP channel" during domain reload
	original := log.Writer()
	log.SetOutput(&suppressWriter{w: os.Stderr, suppress: "Unsolicited response received on idle HTTP channel"})
	defer log.SetOutput(original)

	return pollTestResults(port)
}

func pollTestResults(port int) (*client.CommandResponse, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	resultsPath := filepath.Join(home, ".unity-cli", "status", fmt.Sprintf("test-results-%d.json", port))
	deadline := time.Now().Add(10 * time.Minute)

	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)

		data, err := os.ReadFile(resultsPath)
		if err == nil {
			_ = os.Remove(resultsPath)
			var resp client.CommandResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				return nil, fmt.Errorf("failed to parse test results: %w", err)
			}
			return &resp, nil
		}

		// Check Unity process is still alive (heartbeat may be stale during domain reload)
		inst, err := readStatus(port)
		if err == nil && inst.State == "stopped" {
			return nil, fmt.Errorf("unity editor has stopped (port %d)", port)
		}
	}

	return nil, fmt.Errorf("timed out waiting for test results (10m)")
}
