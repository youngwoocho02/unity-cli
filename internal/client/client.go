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

var (
	// ErrUnityUnreachable: HTTP 연결 자체가 안 됨 (에디터 종료, 포트 닫힘).
	ErrUnityUnreachable = errors.New("cannot connect to Unity")
	// ErrUnityNotAccepting: 연결은 됐지만 컴파일/리로드 등으로 명령을 받을 수 없음 (보통 503).
	ErrUnityNotAccepting = errors.New("unity is not accepting commands")
	// ErrUnityDisconnected: 요청은 나갔는데 응답 본문이 비어 연결이 끊긴 경우.
	ErrUnityDisconnected = errors.New("unity closed the connection before responding")
)

// IsTransientUnityError는 같은 명령을 다시 보내도 되는 일시 오류인지 본다.
// 버전 불일치나 잘못된 JSON 같은 영구 오류는 false다.
func IsTransientUnityError(err error) bool {
	return errors.Is(err, ErrUnityUnreachable) ||
		errors.Is(err, ErrUnityNotAccepting) ||
		errors.Is(err, ErrUnityDisconnected)
}

// isProcessDead returns true only when the process is confirmed to not exist.
// Permission errors or transient failures return false (not confirmed dead),
// so the instance file is preserved.
// Defaults to the OS-specific implementation; overridden in tests.
var isProcessDead = checkProcessDead

// instancesDir은 커넥터가 heartbeat JSON을 쓰는 디렉터리다.
func instancesDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".unity-cli", "instances")
}

// ScanInstances는 ~/.unity-cli/instances/*.json을 읽는다.
// PID가 없거나 프로세스가 죽은 파일은 지운다. 깨진 JSON은 건너뛴다.
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
		// PID 0이거나 프로세스가 없으면 오래된 heartbeat로 보고 삭제한다.
		if inst.PID <= 0 || isProcessDead(inst.PID) {
			_ = os.Remove(fp)
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// isActiveInstance는 정상 종료(stopped)가 아니고 heartbeat가 한 번이라도 쓰인 인스턴스인지 본다.
func isActiveInstance(inst Instance) bool {
	return inst.State != "stopped" && inst.Timestamp > 0
}

// ActiveInstances는 살아 있고 stopped가 아닌 Unity 목록만 돌려준다.
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

// DiscoverInstance는 실행 중인 Unity 하나를 고른다.
// project가 있으면 경로 일치(정확 일치 우선, 그다음 부분 문자열)로 고른다.
// 없으면 현재 작업 디렉터리와 맞는 프로젝트를 찾고, 인스턴스가 하나뿐이면 그걸 쓴다.
// 여러 개인데 어디인지 모르면 --project를 요구한다.
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
		// 정확 일치가 하나면 그걸 쓰고, 여럿이면 모호하니 에러.
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

	// --project가 없으면 cwd가 그 Unity 프로젝트 안인지 먼저 본다.
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

// normalizeProjectPath는 경로 비교용으로 슬래시/대소문자/심볼릭 링크를 맞춘다.
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

// readInstanceFile은 heartbeat JSON을 읽는다.
// 커넥터가 파일을 교체하는 순간에 깨진 JSON을 읽을 수 있어, 한 번 짧게 재시도한다.
func readInstanceFile(path string) (Instance, error) {
	var inst Instance
	data, err := os.ReadFile(path)
	if err != nil {
		return inst, err
	}
	if err := json.Unmarshal(data, &inst); err == nil {
		return inst, nil
	}

	// 원자적 replace 중일 수 있으니 짧게 기다렸다가 다시 읽는다.
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

// Health는 GET /health로 리스너가 살아 있고 명령을 받을 수 있는지 확인한다.
// 메인 스레드에 명령을 넣지 않으므로 컴파일 중에도 응답이 온다.
// state가 ready/playing/paused가 아니면 ErrUnityNotAccepting을 돌려 호출측이 기다리게 한다.
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
	// snapshot이 비어 있으면 리스너는 떠 있어도 아직 heartbeat가 안 쓰인 것이다.
	if !result.Data.Ready || result.Data.ProjectPath == "" || result.Data.PID == 0 || result.Data.Timestamp == 0 {
		return nil, errors.New("unity health endpoint is not ready")
	}
	// 발견한 인스턴스와 health가 다른 프로젝트를 가리키면 잘못된 포트다.
	if inst.ProjectPath != "" && normalizeProjectPath(result.Data.ProjectPath) != normalizeProjectPath(inst.ProjectPath) {
		return nil, fmt.Errorf("unity health project mismatch: expected %s, got %s", inst.ProjectPath, result.Data.ProjectPath)
	}
	// compiling/reloading 등에서는 명령을 보내지 않는다. CLI가 기다렸다가 다시 Health를 친다.
	if !canAcceptCommands(result.Data.State) {
		state := result.Data.State
		if state == "" {
			state = "unknown"
		}
		return nil, fmt.Errorf("%w (%s)", ErrUnityNotAccepting, state)
	}
	return &result.Data, nil
}

// canAcceptCommands는 Unity가 지금 도구를 실행할 수 있는 상태인지 본다.
// ready(편집), playing, paused만 허용한다. compiling/reloading/refreshing은 거부한다.
func canAcceptCommands(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "ready", "playing", "paused":
		return true
	default:
		return false
	}
}

// Send는 POST /command로 명령을 한 번 보낸다. 재시도하지 않는다.
// 재시도는 cmd.sendWithRetry가 Health로 받을 수 있을 때까지 기다린 뒤 여기서 한 번 호출한다.
// 503이면 아직 못 받는다. 빈 본문은 도메인 리로드 등으로 연결이 끊긴 것이다.
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
		return nil, ErrUnityUnreachable
	}
	defer resp.Body.Close()

	// 커넥터는 컴파일/리로드 중이면 큐에 넣지 않고 503을 준다.
	if resp.StatusCode == http.StatusServiceUnavailable {
		var body []byte
		body, _ = io.ReadAll(resp.Body)
		message := parseUnityErrorMessage(body)
		if message == "" {
			message = "busy"
		}
		return nil, fmt.Errorf("%w: %s", ErrUnityNotAccepting, message)
	}

	if resp.StatusCode != http.StatusOK {
		var body []byte
		body, _ = io.ReadAll(resp.Body)
		if len(body) > 0 {
			return nil, fmt.Errorf("HTTP %d from Unity: %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("HTTP %d from Unity (command: %s)", resp.StatusCode, command)
	}

	// 200인데 본문이 없으면 처리 중 리스너가 죽은 것이다. 성공으로 치우지 않는다.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil || len(respBody) == 0 {
		return nil, ErrUnityDisconnected
	}

	var result CommandResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("invalid Unity response: %w", err)
	}

	return &result, nil
}

// parseUnityErrorMessage는 503 본문에서 사람이 읽을 메시지를 꺼낸다.
// JSON이면 message 필드, 아니면 본문 그대로.
func parseUnityErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var result CommandResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return strings.TrimSpace(string(body))
	}
	return strings.TrimSpace(result.Message)
}
