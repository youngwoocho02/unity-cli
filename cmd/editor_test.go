package cmd

import (
	"testing"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func TestEditorCmd_Play(t *testing.T) {
	send, params := mockSend("manage_editor", t)
	resolve := func() (*client.Instance, error) { return nil, nil }
	if _, err := editorCmd([]string{"play"}, send, resolve); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if (*params)["action"] != "play" {
		t.Errorf("expected action=play, got %v", (*params)["action"])
	}
	if (*params)["wait_for_completion"] != false {
		t.Errorf("expected wait_for_completion=false, got %v", (*params)["wait_for_completion"])
	}
}

func TestEditorCmd_PlayWait(t *testing.T) {
	send, params := mockSend("manage_editor", t)
	resolve := func() (*client.Instance, error) { return nil, nil }
	if _, err := editorCmd([]string{"play", "--wait"}, send, resolve); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if (*params)["wait_for_completion"] != true {
		t.Errorf("expected wait_for_completion=true, got %v", (*params)["wait_for_completion"])
	}
}

func TestEditorCmd_Stop(t *testing.T) {
	send, params := mockSend("manage_editor", t)
	resolve := func() (*client.Instance, error) { return nil, nil }
	if _, err := editorCmd([]string{"stop"}, send, resolve); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if (*params)["action"] != "stop" {
		t.Errorf("expected action=stop, got %v", (*params)["action"])
	}
}

func TestEditorCmd_Pause(t *testing.T) {
	send, params := mockSend("manage_editor", t)
	resolve := func() (*client.Instance, error) { return nil, nil }
	if _, err := editorCmd([]string{"pause"}, send, resolve); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if (*params)["action"] != "pause" {
		t.Errorf("expected action=pause, got %v", (*params)["action"])
	}
}

func TestEditorCmd_Refresh(t *testing.T) {
	send, _ := mockSend("refresh_unity", t)
	resolve := func() (*client.Instance, error) { return nil, nil }
	if _, err := editorCmd([]string{"refresh"}, send, resolve); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEditorCmd_RefreshForce(t *testing.T) {
	send, params := mockSend("refresh_unity", t)
	resolve := func() (*client.Instance, error) { return nil, nil }
	if _, err := editorCmd([]string{"refresh", "--force"}, send, resolve); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if (*params)["force"] != true {
		t.Errorf("expected force=true, got %v", (*params)["force"])
	}
	if (*params)["mode"] != "force" {
		t.Errorf("expected mode=force, got %v", (*params)["mode"])
	}
}

func TestEditorCmd_RefreshCompileForce(t *testing.T) {
	send, params := mockSend("refresh_unity", t)
	resolve := func() (*client.Instance, error) {
		return &client.Instance{State: "ready"}, nil
	}
	if _, err := editorCmd([]string{"refresh", "--compile", "--force"}, send, resolve); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if (*params)["compile"] != "request" {
		t.Errorf("expected compile=request, got %v", (*params)["compile"])
	}
	if (*params)["force"] != true {
		t.Errorf("expected force=true, got %v", (*params)["force"])
	}
	if (*params)["mode"] != "force" {
		t.Errorf("expected mode=force, got %v", (*params)["mode"])
	}
}

func TestEditorCmd_RefreshCompileFailureDoesNotWait(t *testing.T) {
	resolveCalled := false
	send := func(cmd string, params interface{}) (*client.CommandResponse, error) {
		if cmd != "refresh_unity" {
			t.Errorf("send called with command %q, want refresh_unity", cmd)
		}
		return &client.CommandResponse{Success: false, Message: "blocked"}, nil
	}
	resolve := func() (*client.Instance, error) {
		resolveCalled = true
		return &client.Instance{State: "ready"}, nil
	}

	resp, err := editorCmd([]string{"refresh", "--compile"}, send, resolve)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Success {
		t.Fatalf("expected failed response, got %+v", resp)
	}
	if resolveCalled {
		t.Error("expected refresh failure to skip compilation wait")
	}
}

func TestEditorCmd_EmptyArgs(t *testing.T) {
	send, _ := mockSend("manage_editor", t)
	resolve := func() (*client.Instance, error) { return nil, nil }
	_, err := editorCmd(nil, send, resolve)
	if err == nil {
		t.Error("expected error for empty args")
	}
}

func TestEditorCmd_UnknownAction(t *testing.T) {
	send, _ := mockSend("manage_editor", t)
	resolve := func() (*client.Instance, error) { return nil, nil }
	_, err := editorCmd([]string{"fly"}, send, resolve)
	if err == nil {
		t.Error("expected error for unknown action")
	}
}
