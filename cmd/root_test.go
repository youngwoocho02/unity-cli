package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func mockSend(wantCmd string, t *testing.T) (sendFn, *map[string]interface{}) {
	t.Helper()
	captured := map[string]interface{}{}
	fn := func(cmd string, params interface{}) (*client.CommandResponse, error) {
		if cmd != wantCmd {
			t.Errorf("send called with command %q, want %q", cmd, wantCmd)
		}
		if p, ok := params.(map[string]interface{}); ok {
			for k, v := range p {
				captured[k] = v
			}
		}
		return &client.CommandResponse{Success: true}, nil
	}
	return fn, &captured
}

func TestParseSubFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{"empty", nil, map[string]string{}},
		{"key value pair", []string{"--filter", "error"}, map[string]string{"filter": "error"}},
		{"boolean flag", []string{"--wait"}, map[string]string{"wait": "true"}},
		{"mixed", []string{"--filter", "error", "--wait", "--clear"}, map[string]string{"filter": "error", "wait": "true", "clear": "true"}},
		{"consecutive boolean flags", []string{"--wait", "--clear"}, map[string]string{"wait": "true", "clear": "true"}},
		{"non-flag args ignored", []string{"play", "--wait"}, map[string]string{"wait": "true"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSubFlags(tt.args)
			if len(got) != len(tt.want) {
				t.Errorf("parseSubFlags(%v) = %v, want %v", tt.args, got, tt.want)
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseSubFlags(%v)[%q] = %q, want %q", tt.args, k, got[k], v)
				}
			}
		})
	}
}

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantFlags    []string
		wantCommands []string
	}{
		{"empty", nil, nil, nil},
		{"commands only", []string{"editor", "play"}, nil, []string{"editor", "play"}},
		{"project flag", []string{"--project", "myproj", "status"}, []string{"--project", "myproj"}, []string{"status"}},
		{"timeout flag", []string{"exec", "--timeout", "5000", "Time.time"}, []string{"--timeout", "5000"}, []string{"exec", "Time.time"}},
		{"ignore version mismatch flag", []string{"exec", "--ignore-version-mismatch", "Time.time"}, []string{"--ignore-version-mismatch"}, []string{"exec", "Time.time"}},
		{"ignore version mismatch value", []string{"status", "--ignore-version-mismatch=true"}, []string{"--ignore-version-mismatch=true"}, []string{"status"}},
		{"multiple global flags", []string{"--project", "myproj", "--timeout", "3000", "exec", "code"}, []string{"--project", "myproj", "--timeout", "3000"}, []string{"exec", "code"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags, commands := splitArgs(tt.args)
			if !sliceEqual(flags, tt.wantFlags) {
				t.Errorf("splitArgs(%v) flags = %v, want %v", tt.args, flags, tt.wantFlags)
			}
			if !sliceEqual(commands, tt.wantCommands) {
				t.Errorf("splitArgs(%v) commands = %v, want %v", tt.args, commands, tt.wantCommands)
			}
		})
	}
}

func TestRejectRemovedFlagsRejectsPort(t *testing.T) {
	if err := rejectRemovedFlags([]string{"--port", "8090", "status"}); err == nil {
		t.Fatal("expected --port to be rejected")
	}
	if err := rejectRemovedFlags([]string{"--port=8090", "status"}); err == nil {
		t.Fatal("expected --port= to be rejected")
	}
	if err := rejectRemovedFlags([]string{"status", "--port", "8090"}); err == nil {
		t.Fatal("expected status --port to be rejected")
	}
	if err := rejectRemovedFlags([]string{"--timeout", "1000", "--port", "8090", "status"}); err == nil {
		t.Fatal("expected global --port after another global flag to be rejected")
	}
	if err := rejectRemovedFlags([]string{"editor", "play", "--port", "8090"}); err == nil {
		t.Fatal("expected built-in command --port to be rejected")
	}
	if err := rejectRemovedFlags([]string{"test", "--port=8090"}); err == nil {
		t.Fatal("expected built-in command --port= to be rejected")
	}
	if err := rejectRemovedFlags([]string{"custom_tool", "--port", "1234"}); err == nil {
		t.Fatal("expected custom command --port to be rejected")
	}
}

func TestRejectRemovedFlagsAllowsProject(t *testing.T) {
	if err := rejectRemovedFlags([]string{"--project", "MyGame", "status"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRejectRemovedFlagsAllowsIgnoreVersionMismatch(t *testing.T) {
	if err := rejectRemovedFlags([]string{"--ignore-version-mismatch", "status"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := rejectRemovedFlags([]string{"custom_tool", "--ignore-version-mismatch"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveReadyUsesHealthEndpoint(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	Version = "dev"
	healthCalls := 0
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		healthCalls++
		if inst.Port != 8090 {
			t.Fatalf("health port: got %d, want 8090", inst.Port)
		}
		return &client.Instance{ProjectPath: inst.ProjectPath, Port: inst.Port, Timestamp: 1000, PID: 1, Ready: true}, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
	})

	got, err := resolveReady(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090}, nil
	}, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProjectPath != "/projects/current" {
		t.Errorf("ProjectPath: got %q, want /projects/current", got.ProjectPath)
	}
	if healthCalls != 1 {
		t.Fatalf("health calls: got %d, want 1", healthCalls)
	}
}

func TestResolveReadyZeroTimeoutUsesDefault(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	Version = "dev"
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		return &client.Instance{ProjectPath: inst.ProjectPath, Port: inst.Port, Timestamp: 1000, PID: 1, Ready: true}, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
	})

	if _, err := resolveReady(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090}, nil
	}, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveReadySmallTimeoutBoundsHealthProbe(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origPollInterval := statusPollInterval
	Version = "dev"
	statusPollInterval = time.Millisecond
	seenTimeout := 0
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		seenTimeout = timeoutMs
		return nil, errors.New("not ready")
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		statusPollInterval = origPollInterval
	})

	_, err := resolveReady(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090}, nil
	}, 5)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if seenTimeout <= 0 || seenTimeout > 5 {
		t.Fatalf("health timeout: got %d, want 1..5", seenTimeout)
	}
}

func TestSendWithRetryReResolvesAfterHealthFailure(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origSend := sendCommand
	Version = "dev"
	healthCalls := 0
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		healthCalls++
		if healthCalls == 1 {
			return nil, errors.New("listener down")
		}
		return &client.Instance{ProjectPath: inst.ProjectPath, Port: inst.Port, Timestamp: 1000, PID: 1, ConnectorVersion: Version, Ready: true}, nil
	}
	sendCommand = func(inst *client.Instance, command string, params interface{}, timeoutMs int) (*client.CommandResponse, error) {
		if inst.Port != 8091 {
			t.Fatalf("send port: got %d, want 8091", inst.Port)
		}
		return &client.CommandResponse{Success: true, Message: command}, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		sendCommand = origSend
	})

	resolveCalls := 0
	resp, err := sendWithRetry(func() (*client.Instance, error) {
		resolveCalls++
		if resolveCalls == 1 {
			return &client.Instance{ProjectPath: "/projects/current", Port: 8090}, nil
		}
		return &client.Instance{ProjectPath: "/projects/current", Port: 8091}, nil
	}, "exec", map[string]interface{}{}, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message != "exec" {
		t.Errorf("Message: got %q, want exec", resp.Message)
	}
	if resolveCalls < 2 {
		t.Fatalf("expected retry to resolve again, got %d calls", resolveCalls)
	}
}

func TestSendWithRetryZeroTimeoutSendsBoundedDefaultTimeout(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origSend := sendCommand
	Version = "dev"
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		return &client.Instance{ProjectPath: inst.ProjectPath, Port: inst.Port, Timestamp: 1000, PID: 1, ConnectorVersion: Version, Ready: true}, nil
	}
	seenTimeout := 0
	sendCommand = func(inst *client.Instance, command string, params interface{}, timeoutMs int) (*client.CommandResponse, error) {
		seenTimeout = timeoutMs
		return &client.CommandResponse{Success: true}, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		sendCommand = origSend
	})

	if _, err := sendWithRetry(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090}, nil
	}, "exec", nil, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seenTimeout <= 0 {
		t.Fatalf("send timeout should be bounded, got %d", seenTimeout)
	}
	if seenTimeout <= 1000 {
		t.Fatalf("send timeout should use command deadline, got %d", seenTimeout)
	}
}

func TestSendWithRetrySmallTimeoutBoundsSendTimeout(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origSend := sendCommand
	Version = "dev"
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		return &client.Instance{ProjectPath: inst.ProjectPath, Port: inst.Port, Timestamp: 1000, PID: 1, ConnectorVersion: Version, Ready: true}, nil
	}
	seenTimeout := 0
	sendCommand = func(inst *client.Instance, command string, params interface{}, timeoutMs int) (*client.CommandResponse, error) {
		seenTimeout = timeoutMs
		return &client.CommandResponse{Success: true}, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		sendCommand = origSend
	})

	if _, err := sendWithRetry(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090}, nil
	}, "exec", nil, 5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seenTimeout <= 0 || seenTimeout > 5 {
		t.Fatalf("send timeout: got %d, want 1..5", seenTimeout)
	}
}

func TestSendWithRetryDoesNotRetryAfterCommandSendFailure(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origSend := sendCommand
	Version = "dev"
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		return &client.Instance{ProjectPath: inst.ProjectPath, Port: inst.Port, Timestamp: 1000, PID: 1, ConnectorVersion: Version, Ready: true}, nil
	}
	sendCalls := 0
	sendCommand = func(inst *client.Instance, command string, params interface{}, timeoutMs int) (*client.CommandResponse, error) {
		sendCalls++
		return nil, errors.New("post failed")
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		sendCommand = origSend
	})

	_, err := sendWithRetry(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090}, nil
	}, "exec", nil, 1000)
	if err == nil {
		t.Fatal("expected send error")
	}
	if sendCalls != 1 {
		t.Fatalf("send should not be retried after command send failure, got %d calls", sendCalls)
	}
}

func TestSendWithRetryStopsOnVersionMismatch(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origSend := sendCommand
	origIgnore := flagIgnoreVersionMismatch
	Version = "v0.3.19"
	flagIgnoreVersionMismatch = false
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		return &client.Instance{ProjectPath: inst.ProjectPath, Port: inst.Port, Timestamp: 1000, PID: 1, ConnectorVersion: "0.3.18", Ready: true}, nil
	}
	sendCommand = func(inst *client.Instance, command string, params interface{}, timeoutMs int) (*client.CommandResponse, error) {
		t.Fatal("send should not be called on version mismatch")
		return nil, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		sendCommand = origSend
		flagIgnoreVersionMismatch = origIgnore
	})

	_, err := sendWithRetry(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090}, nil
	}, "exec", nil, 100)
	if err == nil {
		t.Fatal("expected version mismatch error")
	}
}

func TestSendWithRetryStopsOnVersionMismatchBeforeHealth(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origSend := sendCommand
	origIgnore := flagIgnoreVersionMismatch
	Version = "v0.3.21"
	flagIgnoreVersionMismatch = false
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		t.Fatal("health should not be called on version mismatch")
		return nil, nil
	}
	sendCommand = func(inst *client.Instance, command string, params interface{}, timeoutMs int) (*client.CommandResponse, error) {
		t.Fatal("send should not be called on version mismatch")
		return nil, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		sendCommand = origSend
		flagIgnoreVersionMismatch = origIgnore
	})

	_, err := sendWithRetry(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090, ConnectorVersion: "0.3.19"}, nil
	}, "exec", nil, 100)
	if err == nil {
		t.Fatal("expected version mismatch error")
	}
}

func TestResolveReadyStopsOnVersionMismatchBeforeHealth(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origIgnore := flagIgnoreVersionMismatch
	Version = "v0.3.21"
	flagIgnoreVersionMismatch = false
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		t.Fatal("health should not be called on version mismatch")
		return nil, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		flagIgnoreVersionMismatch = origIgnore
	})

	_, err := resolveReady(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090, ConnectorVersion: "0.3.19"}, nil
	}, 100)
	if err == nil {
		t.Fatal("expected version mismatch error")
	}
}

func TestResolveReadyIgnoreVersionMismatchAllowsMissingHealthEndpoint(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origIgnore := flagIgnoreVersionMismatch
	Version = "v0.3.21"
	flagIgnoreVersionMismatch = true
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		return nil, client.ErrHealthEndpointUnavailable
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		flagIgnoreVersionMismatch = origIgnore
	})

	got, err := resolveReady(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090, ConnectorVersion: "0.3.19"}, nil
	}, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProjectPath != "/projects/current" {
		t.Errorf("ProjectPath: got %q, want /projects/current", got.ProjectPath)
	}
}

func TestResolveReadyIgnoreVersionMismatchDoesNotHideHealthConnectionFailure(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origIgnore := flagIgnoreVersionMismatch
	Version = "v0.3.21"
	flagIgnoreVersionMismatch = true
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		return nil, errors.New("cannot reach Unity health endpoint")
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		flagIgnoreVersionMismatch = origIgnore
	})

	_, err := resolveReady(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090, ConnectorVersion: "0.3.19"}, nil
	}, 100)
	if err == nil {
		t.Fatal("expected health connection failure to remain visible")
	}
	if !strings.Contains(err.Error(), "cannot reach Unity health endpoint") {
		t.Fatalf("expected health connection failure, got %v", err)
	}
}

func TestSendWithRetryIgnoreVersionMismatchAllowsMismatch(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origSend := sendCommand
	origIgnore := flagIgnoreVersionMismatch
	Version = "v0.3.21"
	flagIgnoreVersionMismatch = true
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		return &client.Instance{ProjectPath: inst.ProjectPath, Port: inst.Port, Timestamp: 1000, PID: 1, ConnectorVersion: "0.3.19", Ready: true}, nil
	}
	sendCalled := false
	sendCommand = func(inst *client.Instance, command string, params interface{}, timeoutMs int) (*client.CommandResponse, error) {
		sendCalled = true
		return &client.CommandResponse{Success: true}, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		sendCommand = origSend
		flagIgnoreVersionMismatch = origIgnore
	})

	if _, err := sendWithRetry(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090, ConnectorVersion: "0.3.19"}, nil
	}, "exec", nil, 100); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sendCalled {
		t.Fatal("send should be called when version mismatch is ignored")
	}
}

func TestSendWithRetryIgnoreVersionMismatchSendsWhenHealthEndpointMissing(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origSend := sendCommand
	origIgnore := flagIgnoreVersionMismatch
	Version = "v0.3.21"
	flagIgnoreVersionMismatch = true
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		return nil, client.ErrHealthEndpointUnavailable
	}
	sendCalled := false
	sendCommand = func(inst *client.Instance, command string, params interface{}, timeoutMs int) (*client.CommandResponse, error) {
		sendCalled = true
		if inst.ConnectorVersion != "0.3.19" {
			t.Fatalf("send should use discovered old connector instance, got %q", inst.ConnectorVersion)
		}
		return &client.CommandResponse{Success: true}, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		sendCommand = origSend
		flagIgnoreVersionMismatch = origIgnore
	})

	if _, err := sendWithRetry(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090, ConnectorVersion: "0.3.19"}, nil
	}, "exec", nil, 100); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sendCalled {
		t.Fatal("send should be called when health endpoint is missing and mismatch is ignored")
	}
}

func TestSendWithRetryIgnoreVersionMismatchDoesNotSendWhenHealthConnectionFails(t *testing.T) {
	origVersion := Version
	origHealth := healthCheck
	origSend := sendCommand
	origIgnore := flagIgnoreVersionMismatch
	Version = "v0.3.21"
	flagIgnoreVersionMismatch = true
	healthCheck = func(inst *client.Instance, timeoutMs int) (*client.Instance, error) {
		return nil, errors.New("cannot reach Unity health endpoint")
	}
	sendCalled := false
	sendCommand = func(inst *client.Instance, command string, params interface{}, timeoutMs int) (*client.CommandResponse, error) {
		sendCalled = true
		return &client.CommandResponse{Success: true}, nil
	}
	t.Cleanup(func() {
		Version = origVersion
		healthCheck = origHealth
		sendCommand = origSend
		flagIgnoreVersionMismatch = origIgnore
	})

	_, err := sendWithRetry(func() (*client.Instance, error) {
		return &client.Instance{ProjectPath: "/projects/current", Port: 8090, ConnectorVersion: "0.3.19"}, nil
	}, "exec", nil, 100)
	if err == nil {
		t.Fatal("expected health connection failure to remain visible")
	}
	if !strings.Contains(err.Error(), "cannot reach Unity health endpoint") {
		t.Fatalf("expected health connection failure, got %v", err)
	}
	if sendCalled {
		t.Fatal("send should not be called when health connection fails")
	}
}

func TestBuildParams_IntParsing(t *testing.T) {
	p, err := buildParams([]string{"--lines", "50"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p["lines"] != 50 {
		t.Errorf("expected lines=50, got %v", p["lines"])
	}
}

func TestBuildParams_BoolParsing(t *testing.T) {
	p, err := buildParams([]string{"--clear"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p["clear"] != true {
		t.Errorf("expected clear=true, got %v", p["clear"])
	}
}

func TestBuildParams_StringParsing(t *testing.T) {
	p, err := buildParams([]string{"--filter", "error"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p["filter"] != "error" {
		t.Errorf("expected filter=error, got %v", p["filter"])
	}
}

func TestBuildParams_UsingsParsing(t *testing.T) {
	p, err := buildParams([]string{
		"--usings", "UnityEditor.Build.Profile, Accelix.Editor.Tools.BuildManager",
		"--usings", "Unity.Entities",
		"--usings", "Unity.Entities",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := p["usings"].([]string)
	if !ok {
		t.Fatalf("expected usings []string, got %T: %v", p["usings"], p["usings"])
	}

	want := []string{
		"UnityEditor.Build.Profile",
		"Accelix.Editor.Tools.BuildManager",
		"Unity.Entities",
	}
	if !sliceEqual(got, want) {
		t.Errorf("expected usings=%v, got %v", want, got)
	}
}

func TestBuildParams_BaseParams(t *testing.T) {
	p, err := buildParams([]string{"--depth", "5"}, map[string]interface{}{"action": "hierarchy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p["action"] != "hierarchy" {
		t.Errorf("expected action=hierarchy, got %v", p["action"])
	}
	if p["depth"] != 5 {
		t.Errorf("expected depth=5, got %v", p["depth"])
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
