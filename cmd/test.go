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

	mode, ok := flags["mode"]
	if !ok {
		mode = "EditMode"
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

	// No timeout — connection stays open for EditMode, file polling for PlayMode
	flagTimeout = 0

	resp, err := send("run_tests", params)
	if err != nil {
		return nil, err
	}

	// If Unity doesn't recognise the command, the Test Framework package is likely missing
	if !resp.Success && strings.Contains(resp.Message, "Unknown command") {
		return nil, fmt.Errorf(
			"'run_tests' is not available.\n" +
				"Make sure the Unity Test Framework package is installed:\n" +
				"  Window → Package Manager → search 'Test Framework' → Install\n" +
				"The UnityCliConnector.TestRunner assembly requires it to compile.")
	}

	// EditMode: results are in the response directly
	if mode == "EditMode" {
		return resp, nil
	}

	// PlayMode: Unity returns immediately with "running", we poll the results file
	if resp.Message != "running" {
		return resp, nil
	}

	fmt.Println("PlayMode tests running, waiting for results...")

	// Domain reload is imminent — suppress the "Unsolicited response on idle HTTP channel"
	// log line that fires when Unity kills the connection during reload.
	original := log.Writer()
	log.SetOutput(&suppressWriter{w: os.Stderr, suppress: "Unsolicited response received on idle HTTP channel"})
	result, err := pollTestResults(port)
	log.SetOutput(original)
	return result, err
}

func pollTestResults(port int) (*client.CommandResponse, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	resultsPath := filepath.Join(home, ".unity-cli", "status", fmt.Sprintf("test-results-%d.json", port))

	for {
		data, err := os.ReadFile(resultsPath)
		if err == nil {
			// File exists — parse and return
			os.Remove(resultsPath) // clean up
			var resp client.CommandResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				return nil, fmt.Errorf("failed to parse test results: %w", err)
			}
			return &resp, nil
		}

		// File not there yet — check if Unity is still alive via its heartbeat
		heartbeatPath := filepath.Join(home, ".unity-cli", "status", fmt.Sprintf("%d.json", port))
		if _, err := os.Stat(heartbeatPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("Unity editor appears to have crashed — no heartbeat at port %d", port)
		}

		time.Sleep(500 * time.Millisecond)
	}
}


