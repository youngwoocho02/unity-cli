package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Instance represents a running Unity Editor discovered from ~/.unity-cli/instances/.
type Instance struct {
	State            string `json:"state"`
	ProjectPath      string `json:"projectPath"`
	Port             int    `json:"port"`
	PID              int    `json:"pid"`
	UnityVersion     string `json:"unityVersion,omitempty"`
	ConnectorVersion string `json:"connectorVersion,omitempty"`
	Timestamp        int64  `json:"timestamp,omitempty"`
	CompileErrors    bool   `json:"compileErrors,omitempty"`
	Ready            bool   `json:"ready,omitempty"`
	ListenerRunning  bool   `json:"listenerRunning,omitempty"`
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

// HealthResponse is returned by the connector listener without Unity main-thread dispatch.
type HealthResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Data    Instance `json:"data"`
}

var ErrHealthEndpointUnavailable = errors.New("unity health endpoint unavailable")

// isProcessDead returns true only when the process is confirmed to not exist.
// Permission errors or transient failures return false (not confirmed dead),
// so the instance file is preserved.
// Defaults to the OS-specific implementation; overridden in tests.
var isProcessDead = checkProcessDead

func instancesDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".unity-cli", "instances")
}

// ScanInstances reads all instance files from ~/.unity-cli/instances/.
// Stale files whose PID is no longer running are automatically removed.
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
		fp := filepath.Join(dir, e.Name())
		inst, err := readInstanceFile(fp)
		if err != nil {
			continue
		}
		if inst.PID <= 0 || isProcessDead(inst.PID) {
			_ = os.Remove(fp)
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

func isActiveInstance(inst Instance) bool {
	return inst.State != "stopped" && inst.Timestamp > 0
}

// ActiveInstances returns heartbeat-backed Unity instances that are not marked stopped.
func ActiveInstances() ([]Instance, error) {
	instances, err := ScanInstances()
	if err != nil {
		return nil, err
	}
	var alive []Instance
	for _, inst := range instances {
		if isActiveInstance(inst) {
			alive = append(alive, inst)
		}
	}
	return alive, nil
}

// DiscoverInstance finds a running Unity instance from ~/.unity-cli/instances/.
// If project is set, matches by project path substring.
// Otherwise selects by the current working directory or the sole active instance.
func DiscoverInstance(project string) (*Instance, error) {
	alive, err := ActiveInstances()
	if err != nil {
		return nil, fmt.Errorf("no Unity instances found.\nIs Unity running with the Connector package?\nExpected: %s", instancesDir())
	}

	if len(alive) == 0 {
		return nil, fmt.Errorf("no Unity instances running")
	}

	if project != "" {
		projectNorm := normalizeProjectPath(project)
		var exact []int
		var matches []int
		for i, inst := range alive {
			instNorm := normalizeProjectPath(inst.ProjectPath)
			if instNorm == projectNorm {
				exact = append(exact, i)
				continue
			}
			if strings.Contains(instNorm, projectNorm) {
				matches = append(matches, i)
			}
		}
		if len(exact) == 1 {
			return &alive[exact[0]], nil
		}
		if len(exact) > 1 {
			return nil, fmt.Errorf("multiple Unity instances found for exact project path: %s", project)
		}
		if len(matches) == 1 {
			return &alive[matches[0]], nil
		}
		if len(matches) > 1 {
			var projects []string
			for _, idx := range matches {
				projects = append(projects, fmt.Sprintf("  %s", alive[idx].ProjectPath))
			}
			return nil, fmt.Errorf("multiple Unity instances match project: %s\n%s", project, strings.Join(projects, "\n"))
		}
		return nil, fmt.Errorf("no Unity instance found for project: %s", project)
	}

	// Try to match by current working directory before accepting a sole active instance.
	if cwd, err := os.Getwd(); err == nil {
		cwdNorm := normalizeProjectPath(cwd)
		for i, inst := range alive {
			projNorm := normalizeProjectPath(inst.ProjectPath)
			if cwdNorm == projNorm || strings.HasPrefix(cwdNorm, projNorm+"/") {
				return &alive[i], nil
			}
		}
	}

	if len(alive) == 1 {
		return &alive[0], nil
	}

	var projects []string
	for _, inst := range alive {
		projects = append(projects, fmt.Sprintf("  %s", inst.ProjectPath))
	}
	return nil, fmt.Errorf("multiple Unity instances running; use --project:\n%s", strings.Join(projects, "\n"))
}

func normalizeProjectPath(path string) string {
	normalized := filepath.Clean(path)
	if filepath.IsAbs(normalized) {
		if resolved, err := filepath.EvalSymlinks(normalized); err == nil {
			normalized = resolved
		}
	}
	normalized = strings.TrimRight(filepath.ToSlash(normalized), "/")
	if runtime.GOOS == "windows" {
		normalized = strings.ToLower(normalized)
	}
	return normalized
}

func readInstanceFile(path string) (Instance, error) {
	var inst Instance
	data, err := os.ReadFile(path)
	if err != nil {
		return inst, err
	}
	if err := json.Unmarshal(data, &inst); err == nil {
		return inst, nil
	}

	time.Sleep(25 * time.Millisecond)
	data, err = os.ReadFile(path)
	if err != nil {
		return inst, err
	}
	if err := json.Unmarshal(data, &inst); err != nil {
		return inst, err
	}
	return inst, nil
}

func Health(inst *Instance, timeoutMs int) (*Instance, error) {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeoutMs <= 0 {
		timeout = 2 * time.Second
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/health", inst.Port)
	httpClient := &http.Client{Timeout: timeout}

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, errors.New("cannot reach Unity health endpoint")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed || isLegacyMissingHealthResponse(resp.StatusCode, string(body)) {
			return nil, fmt.Errorf("%w: HTTP %d from Unity health endpoint", ErrHealthEndpointUnavailable, resp.StatusCode)
		}
		if len(body) > 0 {
			return nil, fmt.Errorf("HTTP %d from Unity health endpoint: %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("HTTP %d from Unity health endpoint", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result HealthResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("invalid Unity health response: %w", err)
	}
	if !result.Success {
		if result.Message == "" {
			result.Message = "unknown health error"
		}
		return nil, errors.New(result.Message)
	}
	if !result.Data.Ready || result.Data.ProjectPath == "" || result.Data.PID == 0 || result.Data.Timestamp == 0 {
		return nil, errors.New("unity health endpoint is not ready")
	}
	if inst.ProjectPath != "" && normalizeProjectPath(result.Data.ProjectPath) != normalizeProjectPath(inst.ProjectPath) {
		return nil, fmt.Errorf("unity health project mismatch: expected %s, got %s", inst.ProjectPath, result.Data.ProjectPath)
	}
	return &result.Data, nil
}

func isLegacyMissingHealthResponse(status int, body string) bool {
	return status == http.StatusBadRequest &&
		strings.Contains(body, "Expected POST /command") &&
		strings.Contains(body, "GET /health")
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
	if timeoutMs <= 0 {
		timeoutMs = 120000
	}
	httpClient := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}

	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("cannot connect to Unity")
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
