package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func unusedSend(t *testing.T) sendFn {
	t.Helper()
	return func(cmd string, params interface{}) (*client.CommandResponse, error) {
		t.Fatalf("send should not be called, got %q", cmd)
		return nil, nil
	}
}

func TestIsPlayModeState(t *testing.T) {
	cases := map[string]bool{
		"playing":   true,
		"paused":    true,
		"Playing":   true,
		" PAUSED ":  true,
		"ready":     false,
		"compiling": false,
		"":          false,
	}
	for state, want := range cases {
		if got := isPlayModeState(state); got != want {
			t.Errorf("isPlayModeState(%q)=%v, want %v", state, got, want)
		}
	}
}

func TestWaitForPlayModeSucceedsWhenPlaying(t *testing.T) {
	orig := statusPollInterval
	statusPollInterval = time.Millisecond
	t.Cleanup(func() { statusPollInterval = orig })

	if err := waitForPlayMode(func() (*client.Instance, error) {
		return &client.Instance{State: "playing"}, nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForPlayModeTimesOut(t *testing.T) {
	origInterval := statusPollInterval
	origTimeout := flagTimeout
	statusPollInterval = time.Millisecond
	flagTimeout = 20
	t.Cleanup(func() {
		statusPollInterval = origInterval
		flagTimeout = origTimeout
	})

	err := waitForPlayMode(func() (*client.Instance, error) {
		return &client.Instance{State: "ready"}, nil
	})
	if err == nil {
		t.Fatal("expected play wait timeout")
	}
	if !strings.Contains(err.Error(), "timed out waiting for play mode") {
		t.Fatalf("error = %v, want play mode timeout", err)
	}
}

func TestEditorCmd_RefreshCompileFailureDoesNotWait(t *testing.T) {
	resolveCalled := false
	send := func(cmd string, params interface{}) (*client.CommandResponse, error) {
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
	_, err := editorCmd(nil, unusedSend(t), func() (*client.Instance, error) { return nil, nil })
	if err == nil {
		t.Error("expected error for empty args")
	}
}

func TestEditorCmd_UnknownAction(t *testing.T) {
	_, err := editorCmd([]string{"fly"}, unusedSend(t), func() (*client.Instance, error) { return nil, nil })
	if err == nil {
		t.Error("expected error for unknown action")
	}
}
