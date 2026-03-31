package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Instance represents a running Unity Editor discovered from ~/.unity-cli/instances/.
type Instance struct {
	State         string `json:"state"`
	ProjectPath   string `json:"projectPath"`
	Port          int    `json:"port"`
	PID           int    `json:"pid"`
	UnityVersion  string `json:"unityVersion,omitempty"`
	Timestamp     int64  `json:"timestamp,omitempty"`
	CompileErrors bool   `json:"compileErrors,omitempty"`
}

// CommandRequest is the JSON body sent to Unity's HTTP server.
type CommandRequest struct {
	Command string      `json:"command"`
	Params  interface{} `json:"params"`
}

// CommandResponse is the JSON body returned by Unity.
// Data is raw JSON so callers can unmarshal into any shape.
type CommandResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func instancesDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".unity-cli", "instances")
}

// ScanInstances reads all instance files from ~/.unity-cli/instances/.
func ScanInstances() ([]Instance, error) {
	dir := instancesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var instances []Instance
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var inst Instance
		if err := json.Unmarshal(data, &inst); err != nil {
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// FindByPort scans instance files and returns the one matching the given port.
func FindByPort(port int) (*Instance, error) {
	instances, err := ScanInstances()
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if inst.Port == port {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("no instance on port %d", port)
}

// DiscoverInstance finds a running Unity instance from ~/.unity-cli/instances/.
// If port > 0, skips discovery and connects directly.
// If project is set, matches by project path substring.
// Otherwise returns the most recently active instance.
func DiscoverInstance(project string, port int) (*Instance, error) {
	if port > 0 {
		return &Instance{ProjectPath: "override", Port: port}, nil
	}

	instances, err := ScanInstances()
	if err != nil {
		return nil, fmt.Errorf("no Unity instances found.\nIs Unity running with the Connector package?\nExpected: %s", instancesDir())
	}

	// Filter out stopped instances
	var alive []Instance
	for _, inst := range instances {
		if inst.State == "stopped" {
			continue
		}
		alive = append(alive, inst)
	}

	if len(alive) == 0 {
		return nil, fmt.Errorf("no Unity instances running")
	}

	if project != "" {
		for _, inst := range alive {
			if strings.Contains(inst.ProjectPath, project) {
				return &inst, nil
			}
		}
		return nil, fmt.Errorf("no Unity instance found for project: %s", project)
	}

	// Try to match by current working directory before falling back to timestamp
	if cwd, err := os.Getwd(); err == nil {
		cwdNorm := filepath.ToSlash(cwd)
		for _, inst := range alive {
			projNorm := filepath.ToSlash(inst.ProjectPath)
			if cwdNorm == projNorm || strings.HasPrefix(cwdNorm, projNorm+"/") {
				return &inst, nil
			}
		}
	}

	// Return the most recently updated
	best := alive[0]
	for _, inst := range alive[1:] {
		if inst.Timestamp > best.Timestamp {
			best = inst
		}
	}
	return &best, nil
}

func Send(inst *Instance, command string, params interface{}, timeoutMs int) (*CommandResponse, error) {
	if params == nil {
		params = map[string]interface{}{}
	}

	body, err := json.Marshal(CommandRequest{Command: command, Params: params})
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/command", inst.Port)
	httpClient := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}

	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		// 연결이 끊어진 경우: Unity가 도메인 리로드(에셋 변경 등)로 HTTP 서버를
		// 재시작했을 가능성. 커맨드 자체는 실행됐을 수 있으므로 성공으로 간주.
		if isConnectionReset(err) {
			return &CommandResponse{
				Success: true,
				Message: fmt.Sprintf("%s sent (Unity reloaded before response)", command),
			}, nil
		}
		return nil, fmt.Errorf("cannot connect to Unity at port %d: %v", inst.Port, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body []byte
		body, _ = io.ReadAll(resp.Body)
		if len(body) > 0 {
			return nil, fmt.Errorf("HTTP %d from Unity: %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("HTTP %d from Unity (command: %s)", resp.StatusCode, command)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil || len(respBody) == 0 {
		// Some commands (e.g. play mode entry) close the connection before responding.
		return &CommandResponse{
			Success: true,
			Message: fmt.Sprintf("%s sent (connection closed before response)", command),
		}, nil
	}

	var result CommandResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Unity sent a non-JSON body — treat as plain message.
		return &CommandResponse{
			Success: true,
			Message: string(respBody),
		}, nil
	}

	return &result, nil
}

// isConnectionReset checks if the error is a connection reset/EOF,
// which happens when Unity's HTTP server restarts during domain reload.
func isConnectionReset(err error) bool {
	if err == nil {
		return false
	}

	// EOF (connection closed cleanly)
	if errors.Is(err, io.EOF) {
		return true
	}

	// "connection reset by peer" (TCP RST)
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}

	// url.Error wraps transport errors
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isConnectionReset(urlErr.Err)
	}

	// Fallback: check error message
	msg := err.Error()
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "forcibly closed") ||
		strings.Contains(msg, "broken pipe")
}
