package cmd

import (
	"strings"
	"testing"
	"time"
)

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

func TestCommandDeadlineDefaultWhenZero(t *testing.T) {
	remaining := time.Until(commandDeadline(0))
	if remaining < 119*time.Second || remaining > 121*time.Second {
		t.Fatalf("commandDeadline(0) remaining %v, want ~120s", remaining)
	}
}

func TestCommandDeadlineUsesGivenTimeout(t *testing.T) {
	remaining := time.Until(commandDeadline(50))
	if remaining < 20*time.Millisecond || remaining > 80*time.Millisecond {
		t.Fatalf("commandDeadline(50) remaining %v, want ~50ms", remaining)
	}
}

func TestProbeTimeoutMsCapsAtOneSecond(t *testing.T) {
	if got := probeTimeoutMs(time.Now().Add(30 * time.Second)); got != 1000 {
		t.Fatalf("probeTimeoutMs: got %d, want 1000", got)
	}
}

func TestProbeTimeoutMsUsesRemainingWhenUnderOneSecond(t *testing.T) {
	got := probeTimeoutMs(time.Now().Add(40 * time.Millisecond))
	if got < 1 || got > 40 {
		t.Fatalf("probeTimeoutMs: got %d, want 1..40", got)
	}
}

func TestProbeTimeoutMsExpiredIsOne(t *testing.T) {
	if got := probeTimeoutMs(time.Now().Add(-time.Second)); got != 1 {
		t.Fatalf("probeTimeoutMs: got %d, want 1", got)
	}
}

func TestCommandTimeoutMsUsesRemaining(t *testing.T) {
	got := commandTimeoutMs(time.Now().Add(250 * time.Millisecond))
	if got < 1 || got > 250 {
		t.Fatalf("commandTimeoutMs: got %d, want 1..250", got)
	}
}

func TestCommandTimeoutMsExpiredIsOne(t *testing.T) {
	if got := commandTimeoutMs(time.Now().Add(-time.Second)); got != 1 {
		t.Fatalf("commandTimeoutMs: got %d, want 1", got)
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

func TestValidateExecAsyncPolicy_BlocksPositionalAsyncCode(t *testing.T) {
	params := map[string]interface{}{
		"args": []string{"await Task.Delay(1); return null;"},
	}

	err := validateExecAsyncPolicy(params)
	if err == nil {
		t.Fatal("expected async code to be blocked")
	}
	if !strings.Contains(err.Error(), "--allow-async") {
		t.Fatalf("expected allow flag hint, got %v", err)
	}
}

func TestValidateExecAsyncPolicy_BlocksCodeParamAsyncCode(t *testing.T) {
	params := map[string]interface{}{
		"code": "EditorApplication.delayCall += () => Debug.Log(\"later\"); return null;",
	}

	if err := validateExecAsyncPolicy(params); err == nil {
		t.Fatal("expected deferred Unity callback to be blocked")
	}
}

func TestValidateExecAsyncPolicy_AllowsAsyncWithFlagAndRemovesParam(t *testing.T) {
	params := map[string]interface{}{
		"args":        []string{"await Task.Delay(1); return null;"},
		"allow-async": true,
	}

	if err := validateExecAsyncPolicy(params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := params["allow-async"]; exists {
		t.Fatal("allow-async should not be sent to Unity")
	}
}

func TestValidateExecAsyncPolicy_AllowsSynchronousCode(t *testing.T) {
	params := map[string]interface{}{
		"args": []string{"return UnityEngine.Time.time;"},
	}

	if err := validateExecAsyncPolicy(params); err != nil {
		t.Fatalf("unexpected error: %v", err)
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
