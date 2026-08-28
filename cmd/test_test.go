package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	// 유예 0이면 첫 resolve 실패를 바로 종료로 본다. 리로드 유예는 라이브/기본값에서만 쓴다.
	prevTimeout, prevGrace := testResultsPollTimeout, testResultsGoneGrace
	testResultsPollTimeout = time.Second
	testResultsGoneGrace = 0
	defer func() {
		testResultsPollTimeout = prevTimeout
		testResultsGoneGrace = prevGrace
	}()

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

func TestPollTestResultsIgnoresTransientMissingInstance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	statusDir := filepath.Join(home, ".unity-cli", "status")
	if err := os.MkdirAll(statusDir, 0755); err != nil {
		t.Fatalf("failed to create status dir: %v", err)
	}

	prevTimeout, prevGrace := testResultsPollTimeout, testResultsGoneGrace
	testResultsPollTimeout = 3 * time.Second
	testResultsGoneGrace = time.Minute
	defer func() {
		testResultsPollTimeout = prevTimeout
		testResultsGoneGrace = prevGrace
	}()

	runID := "transient-1"
	data, err := json.Marshal(client.CommandResponse{Success: true, Message: "done"})
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	// 첫 폴링은 인스턴스가 없다. 결과 파일이 생기면 리로드가 끝난 것으로 보고 성공해야 한다.
	n := 0
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(statusDir, "test-results-"+runID+".json"), data, 0644)
	}()

	resp, err := pollTestResults(runID, func() (*client.Instance, error) {
		n++
		if n < 3 {
			return nil, fmt.Errorf("no Unity instance found for project: /projects/current")
		}
		return &client.Instance{}, nil
	})
	if err != nil {
		t.Fatalf("pollTestResults returned error: %v", err)
	}
	if resp.Message != "done" {
		t.Fatalf("Message = %q, want done", resp.Message)
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
