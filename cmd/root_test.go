package cmd

import (
	"testing"
)

func TestParseSubFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{"empty", nil, map[string]string{}},
		{"key value pair", []string{"--port", "8080"}, map[string]string{"port": "8080"}},
		{"boolean flag", []string{"--wait"}, map[string]string{"wait": "true"}},
		{"mixed", []string{"--port", "8080", "--wait", "--filter", "error"}, map[string]string{"port": "8080", "wait": "true", "filter": "error"}},
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
		{"port flag", []string{"--port", "8080", "editor", "play"}, []string{"--port", "8080"}, []string{"editor", "play"}},
		{"project flag", []string{"--project", "myproj", "status"}, []string{"--project", "myproj"}, []string{"status"}},
		{"timeout flag", []string{"exec", "--timeout", "5000", "Time.time"}, []string{"--timeout", "5000"}, []string{"exec", "Time.time"}},
		{"multiple global flags", []string{"--port", "8080", "--timeout", "3000", "exec", "code"}, []string{"--port", "8080", "--timeout", "3000"}, []string{"exec", "code"}},
		{"token flag", []string{"--token", "abc123", "exec", "code"}, []string{"--token", "abc123"}, []string{"exec", "code"}},
		{"token with other flags", []string{"--port", "8080", "--token", "secret", "status"}, []string{"--port", "8080", "--token", "secret"}, []string{"status"}},
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

func TestSetInt(t *testing.T) {
	flags := map[string]string{"lines": "50", "bad": "abc"}
	params := map[string]interface{}{}

	setInt(flags, params, "lines", "lineCount")
	if params["lineCount"] != 50 {
		t.Errorf("setInt: got %v, want 50", params["lineCount"])
	}

	setInt(flags, params, "bad", "badVal")
	if _, ok := params["badVal"]; ok {
		t.Error("setInt: should skip non-numeric value")
	}

	setInt(flags, params, "missing", "noop")
	if _, ok := params["noop"]; ok {
		t.Error("setInt: should skip missing flag")
	}
}

func TestSetFloat(t *testing.T) {
	flags := map[string]string{"min": "0.5"}
	params := map[string]interface{}{}

	setFloat(flags, params, "min", "minVal")
	if params["minVal"] != 0.5 {
		t.Errorf("setFloat: got %v, want 0.5", params["minVal"])
	}
}

func TestSetStr(t *testing.T) {
	flags := map[string]string{"filter": "error"}
	params := map[string]interface{}{}

	setStr(flags, params, "filter", "filterType")
	if params["filterType"] != "error" {
		t.Errorf("setStr: got %v, want 'error'", params["filterType"])
	}

	setStr(flags, params, "missing", "noop")
	if _, ok := params["noop"]; ok {
		t.Error("setStr: should skip missing flag")
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
