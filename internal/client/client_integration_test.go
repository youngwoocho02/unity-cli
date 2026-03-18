//go:build integration

package client_test

import (
	"strings"
	"testing"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func TestDiscoverInstance(t *testing.T) {
	inst, err := client.DiscoverInstance("", 0)
	if err != nil {
		t.Fatalf("failed to discover Unity instance: %v", err)
	}
	if inst.Port == 0 {
		t.Error("expected non-zero port")
	}
	t.Logf("found Unity at port %d, project: %s", inst.Port, inst.ProjectPath)
}

func TestSendExec(t *testing.T) {
	inst, err := client.DiscoverInstance("", 0)
	if err != nil {
		t.Fatalf("failed to discover Unity instance: %v", err)
	}

	resp, err := client.Send(inst, "execute_csharp", map[string]interface{}{
		"code": "return (1 + 1).ToString();",
	}, 5000)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
	t.Logf("response: %s", resp.Data)
}

func TestSendConsole(t *testing.T) {
	inst, err := client.DiscoverInstance("", 0)
	if err != nil {
		t.Fatalf("failed to discover Unity instance: %v", err)
	}

	resp, err := client.Send(inst, "read_console", map[string]interface{}{
		"count": 5,
	}, 5000)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
	t.Logf("console response: %s", resp.Data)
}

func TestDiscoverInstance_HasToken(t *testing.T) {
	inst, err := client.DiscoverInstance("", 0)
	if err != nil {
		t.Fatalf("failed to discover: %v", err)
	}
	if inst.Token == "" {
		t.Error("expected non-empty token from discovered instance")
	}
	if len(inst.Token) != 32 {
		t.Errorf("expected 32-char token, got %d chars: %q", len(inst.Token), inst.Token)
	}
	t.Logf("token: %s...%s", inst.Token[:4], inst.Token[len(inst.Token)-4:])
}

func TestSend_AuthRequired(t *testing.T) {
	// Discover real instance to get its port
	inst, err := client.DiscoverInstance("", 0)
	if err != nil {
		t.Fatalf("failed to discover: %v", err)
	}

	// Send with correct token should succeed
	resp, err := client.Send(inst, "list_tools", nil, 5000)
	if err != nil {
		t.Fatalf("send with correct token failed: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success with correct token, got: %s", resp.Message)
	}

	// Send with wrong token should fail with 401
	badInst := &client.Instance{Port: inst.Port, Token: "wrong-token"}
	_, err = client.Send(badInst, "list_tools", nil, 5000)
	if err == nil {
		t.Fatal("expected error with wrong token, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got: %v", err)
	}

	// Send with no token should also fail
	noTokenInst := &client.Instance{Port: inst.Port, Token: ""}
	_, err = client.Send(noTokenInst, "list_tools", nil, 5000)
	if err == nil {
		t.Fatal("expected error with no token, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got: %v", err)
	}
}

func TestSendUnknownCommand(t *testing.T) {
	inst, err := client.DiscoverInstance("", 0)
	if err != nil {
		t.Fatalf("failed to discover Unity instance: %v", err)
	}

	resp, err := client.Send(inst, "nonexistent_command", map[string]interface{}{}, 5000)
	if err != nil {
		t.Fatalf("unexpected connection error: %v", err)
	}
	if resp.Success {
		t.Error("expected failure for unknown command")
	}
}
