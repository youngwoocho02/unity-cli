package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func TestTestCmd_RejectsInvalidMode(t *testing.T) {
	_, err := testCmd([]string{"--mode", "Foo"}, unusedSend(t), nil)
	if err == nil {
		t.Fatal("expected invalid mode error")
	}
	if !strings.Contains(err.Error(), "EditMode or PlayMode") {
		t.Fatalf("error = %v, want mode validation", err)
	}
}

func TestPollTestResultsStopsWhenProjectInstanceDisappears(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	_, err := pollTestResults("missing", func() (*client.Instance, error) {
		return nil, fmt.Errorf("no Unity instance found for project: /projects/current")
	})
	if err == nil {
		t.Fatal("expected stopped editor error")
	}
	if !strings.Contains(err.Error(), "unity editor has stopped") {
		t.Fatalf("expected stopped editor error, got %v", err)
	}
}

func TestPollTestResultsReadsResultFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	statusDir := filepath.Join(home, ".unity-cli", "status")
	if err := os.MkdirAll(statusDir, 0755); err != nil {
		t.Fatalf("failed to create status dir: %v", err)
	}

	runID := "run-1"
	data, err := json.Marshal(client.CommandResponse{Success: true, Message: "done"})
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}
	if err := os.WriteFile(filepath.Join(statusDir, "test-results-"+runID+".json"), data, 0644); err != nil {
		t.Fatalf("failed to write results: %v", err)
	}

	resp, err := pollTestResults(runID, nil)
	if err != nil {
		t.Fatalf("pollTestResults returned error: %v", err)
	}
	if resp.Message != "done" {
		t.Fatalf("Message = %q, want done", resp.Message)
	}
}
