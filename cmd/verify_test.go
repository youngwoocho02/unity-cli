package cmd

import (
	"encoding/json"
	"testing"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func TestVerifyCmd_DefaultFlowPassesAndStops(t *testing.T) {
	var calls []string
	send := func(cmd string, params interface{}) (*client.CommandResponse, error) {
		calls = append(calls, cmd)
		switch cmd {
		case "refresh_unity":
			return &client.CommandResponse{Success: true, Message: "Refresh requested."}, nil
		case "manage_editor":
			p := params.(map[string]interface{})
			if p["action"] == "play" {
				return &client.CommandResponse{Success: true, Message: "Entered play mode (confirmed)."}, nil
			}
			return &client.CommandResponse{Success: true, Message: "Exited play mode (confirmed)."}, nil
		case "console":
			data, _ := json.Marshal([]string{})
			return &client.CommandResponse{Success: true, Message: "Retrieved 0 entries.", Data: data}, nil
		default:
			t.Fatalf("unexpected command %q", cmd)
			return nil, nil
		}
	}
	resolve := func() (*client.Instance, error) {
		return &client.Instance{State: "ready", ProjectPath: "/projects/current"}, nil
	}

	resp, err := verifyCmd(nil, send, resolve)
	if err != nil {
		t.Fatalf("verifyCmd returned error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("response = %#v, want success", resp)
	}
	want := []string{"refresh_unity", "manage_editor", "console", "manage_editor"}
	if !sliceEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	var summary verifySummary
	if err := json.Unmarshal(resp.Data, &summary); err != nil {
		t.Fatalf("failed to parse summary: %v", err)
	}
	if !summary.CompileOK || !summary.RuntimeOK || !summary.Stopped {
		t.Fatalf("summary = %+v, want compile/runtime ok and stopped", summary)
	}
}

func TestVerifyCmd_FailsOnConsoleErrors(t *testing.T) {
	send := func(cmd string, params interface{}) (*client.CommandResponse, error) {
		switch cmd {
		case "refresh_unity", "manage_editor":
			return &client.CommandResponse{Success: true, Message: cmd}, nil
		case "console":
			data, _ := json.Marshal([]string{"boom"})
			return &client.CommandResponse{Success: true, Message: "Retrieved 1 entries.", Data: data}, nil
		default:
			t.Fatalf("unexpected command %q", cmd)
			return nil, nil
		}
	}
	resolve := func() (*client.Instance, error) {
		return &client.Instance{State: "ready", ProjectPath: "/projects/current"}, nil
	}

	resp, err := verifyCmd([]string{"--skip-compile"}, send, resolve)
	if err != nil {
		t.Fatalf("verifyCmd returned error: %v", err)
	}
	if resp.Success {
		t.Fatal("expected verify failure")
	}
	var summary verifySummary
	if err := json.Unmarshal(resp.Data, &summary); err != nil {
		t.Fatalf("failed to parse summary: %v", err)
	}
	if summary.ErrorCount != 1 || summary.RuntimeOK {
		t.Fatalf("summary = %+v, want one runtime error", summary)
	}
}

func TestVerifyCmd_DoesNotStopPreexistingPlayMode(t *testing.T) {
	var stopCalled bool
	send := func(cmd string, params interface{}) (*client.CommandResponse, error) {
		if cmd == "manage_editor" {
			p := params.(map[string]interface{})
			if p["action"] == "stop" {
				stopCalled = true
			}
		}
		if cmd == "console" {
			data, _ := json.Marshal([]string{})
			return &client.CommandResponse{Success: true, Data: data}, nil
		}
		return &client.CommandResponse{Success: true, Message: cmd}, nil
	}
	resolve := func() (*client.Instance, error) {
		return &client.Instance{State: "playing", ProjectPath: "/projects/current"}, nil
	}

	resp, err := verifyCmd([]string{"--skip-compile"}, send, resolve)
	if err != nil {
		t.Fatalf("verifyCmd returned error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got %#v", resp)
	}
	if stopCalled {
		t.Fatal("verify should not stop play mode it did not start")
	}
}
