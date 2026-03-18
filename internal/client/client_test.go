package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDiscoverInstance_ReadsToken(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".unity-cli")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	instances := []Instance{
		{ProjectPath: "/projects/MyGame", Port: 9100, PID: 1234, Token: "deadbeef01234567890abcdef0123456"},
	}
	data, err := json.Marshal(instances)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instances.json"), data, 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows: os.UserHomeDir reads USERPROFILE

	inst, err := DiscoverInstance("", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.Token != "deadbeef01234567890abcdef0123456" {
		t.Errorf("Token = %q, want %q", inst.Token, "deadbeef01234567890abcdef0123456")
	}
}

func TestDiscoverInstance_PortOverrideHasNoToken(t *testing.T) {
	inst, err := DiscoverInstance("", 9999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.Token != "" {
		t.Errorf("port-override instance should have empty token, got %q", inst.Token)
	}
}

func TestSend_AuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"message":"ok"}`))
	}))
	defer srv.Close()

	port, _ := strconv.Atoi(strings.TrimPrefix(srv.URL, "http://127.0.0.1:"))
	inst := &Instance{Port: port, Token: "secret-token-value"}

	_, err := Send(inst, "list_tools", nil, 5000)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if gotAuth != "Bearer secret-token-value" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer secret-token-value")
	}
}

func TestSend_NoTokenNoHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"message":"ok"}`))
	}))
	defer srv.Close()

	port, _ := strconv.Atoi(strings.TrimPrefix(srv.URL, "http://127.0.0.1:"))
	inst := &Instance{Port: port, Token: ""}

	_, err := Send(inst, "list_tools", nil, 5000)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}
