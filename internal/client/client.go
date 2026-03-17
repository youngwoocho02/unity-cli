package client

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Instance struct {
	ProjectPath  string `json:"projectPath"`
	Port         int    `json:"port"`
	PID          int    `json:"pid"`
	State        string `json:"state"`
	UnityVersion string `json:"unityVersion,omitempty"`
	Timestamp    int64  `json:"timestamp"`
}

type CommandRequest struct {
	Command string      `json:"command"`
	Params  interface{} `json:"params"`
}

type CommandResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func instancesDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".unity-cli", "instances")
}

func DiscoverInstance(project string, port int) (*Instance, error) {
	if port > 0 {
		return &Instance{ProjectPath: "override", Port: port}, nil
	}

	dir := instancesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("no Unity instances found.\nIs Unity running with the Connector package?\nExpected: %s", dir)
	}

	var alive []*Instance
	now := time.Now().UnixMilli()

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var inst Instance
		if json.Unmarshal(data, &inst) != nil {
			continue
		}
		// Skip stopped or stale instances
		if inst.State == "stopped" {
			continue
		}
		if now-inst.Timestamp > 3000 {
			continue
		}
		alive = append(alive, &inst)
	}

	if len(alive) == 0 {
		return nil, fmt.Errorf("no Unity instances running")
	}

	if project != "" {
		// Prefer exact suffix match
		for _, inst := range alive {
			if inst.ProjectPath == project || strings.HasSuffix(inst.ProjectPath, "/"+project) || strings.HasSuffix(inst.ProjectPath, "\\"+project) {
				return inst, nil
			}
		}
		// Fall back to substring match
		for _, inst := range alive {
			if strings.Contains(inst.ProjectPath, project) {
				return inst, nil
			}
		}
		return nil, fmt.Errorf("no Unity instance found for project: %s", project)
	}

	// Return the most recently updated instance
	best := alive[0]
	for _, inst := range alive[1:] {
		if inst.Timestamp > best.Timestamp {
			best = inst
		}
	}
	return best, nil
}

// ReadInstance reads the instance file for the given project path hash.
func ReadInstance(projectPath string) (*Instance, error) {
	hash := ProjectHash(projectPath)
	path := filepath.Join(instancesDir(), hash+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inst Instance
	if err := json.Unmarshal(data, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// ProjectHash returns the first 16 hex chars of MD5(projectPath).
func ProjectHash(projectPath string) string {
	h := md5.Sum([]byte(projectPath))
	return fmt.Sprintf("%x", h)[:16]
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
		return nil, fmt.Errorf("cannot connect to Unity at port %d: %v", inst.Port, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body []byte
		body, _ = io.ReadAll(resp.Body)
		if len(body) > 0 {
			var errResp CommandResponse
			if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
				return nil, fmt.Errorf("Unity error: %s", errResp.Message)
			}
			return nil, fmt.Errorf("HTTP %d from Unity: %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("HTTP %d from Unity (command: %s)", resp.StatusCode, command)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil || len(respBody) == 0 {
		return &CommandResponse{
			Success: true,
			Message: fmt.Sprintf("%s sent (connection closed before response)", command),
		}, nil
	}

	var result CommandResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return &CommandResponse{
			Success: true,
			Message: string(respBody),
		}, nil
	}

	return &result, nil
}
