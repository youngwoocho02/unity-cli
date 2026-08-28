package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

type instanceResolver func() (*client.Instance, error)

var statusPollInterval = 500 * time.Millisecond

// statusCmd는 heartbeat 파일만 읽어 현재 Unity 상태를 출력한다. /command는 보내지 않는다.
func statusCmd(project string, ignoreVersionMismatch bool) error {
	instances, err := statusInstances(project)
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		return fmt.Errorf("no Unity instances running")
	}
	for i := range instances {
		if i > 0 {
			fmt.Println()
		}
		printStatus(&instances[i])
		if err := checkConnectorVersion(&instances[i], Version, ignoreVersionMismatch); err != nil {
			return err
		}
	}
	return nil
}

// statusInstances는 스캔한 인스턴스 중 timestamp가 있는 것만 남긴다. project가 있으면 그 경로만.
func statusInstances(project string) ([]client.Instance, error) {
	instances, err := client.ScanInstances()
	if err != nil {
		return nil, fmt.Errorf("no Unity instances found")
	}

	var result []client.Instance
	projectNorm := normalizeStatusProjectPath(project)
	for _, inst := range instances {
		if inst.Timestamp <= 0 {
			continue
		}
		if projectNorm != "" {
			instNorm := normalizeStatusProjectPath(inst.ProjectPath)
			if instNorm != projectNorm && !strings.Contains(instNorm, projectNorm) {
				continue
			}
		}
		result = append(result, inst)
	}
	if project != "" && len(result) == 0 {
		return nil, fmt.Errorf("no Unity instance found for project: %s", project)
	}
	return result, nil
}

// normalizeStatusProjectPath는 status용 경로 비교다. DiscoverInstance와 같이 슬래시/대소문자를 맞춘다.
func normalizeStatusProjectPath(path string) string {
	normalized := strings.TrimRight(strings.ReplaceAll(path, "\\", "/"), "/")
	if runtime.GOOS == "windows" {
		normalized = strings.ToLower(normalized)
	}
	return normalized
}

// printStatus는 heartbeat가 3초 이상 멈추면 "not responding", 아니면 state/경로/버전을 찍는다.
func printStatus(status *client.Instance) {
	age := time.Since(time.UnixMilli(status.Timestamp))
	if age > 3*time.Second {
		fmt.Fprintf(os.Stderr, "Unity: not responding (last heartbeat %s ago)\n", age.Truncate(time.Second))
		return
	}

	fmt.Printf("Unity: %s\n", status.State)
	fmt.Printf("  Project: %s\n", status.ProjectPath)
	fmt.Printf("  Version: %s\n", status.UnityVersion)
	fmt.Printf("  Connector: %s\n", connectorVersionLabel(status.ConnectorVersion))
	fmt.Printf("  PID:     %d\n", status.PID)
}

// connectorVersionLabel은 빈 버전을 unknown으로 보여 준다.
func connectorVersionLabel(version string) string {
	if strings.TrimSpace(version) == "" {
		return "unknown"
	}
	return version
}

// checkConnectorVersion은 CLI와 커넥터 패키지 버전이 같은지 본다.
// 개발 빌드(dev)와 --ignore-version-mismatch는 통과시킨다.
func checkConnectorVersion(inst *client.Instance, cliVersion string, ignoreMismatch bool) error {
	if normalizeVersion(cliVersion) == "dev" {
		return nil
	}
	if ignoreMismatch {
		return nil
	}
	if inst == nil {
		return nil
	}

	connectorVersion := strings.TrimSpace(inst.ConnectorVersion)
	if connectorVersion == "" {
		return fmt.Errorf("connector version is unknown; update the Unity Connector package to match unity-cli %s, or rerun with --ignore-version-mismatch", cliVersion)
	}
	if normalizeVersion(connectorVersion) != normalizeVersion(cliVersion) {
		return fmt.Errorf("connector version mismatch: unity-cli %s, connector %s. Update both to the same release, or rerun with --ignore-version-mismatch", cliVersion, connectorVersion)
	}
	return nil
}

// normalizeVersion은 앞의 v/V와 공백을 빼 비교한다. v0.3.22와 0.3.22를 같게 본다.
func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	return version
}

// waitForReady는 heartbeat state가 ready가 될 때까지 폴링한다.
// 컴파일 에러가 있으면 true. 5분이 지나면 타임아웃으로 true.
func waitForReady(resolve instanceResolver) bool {
	fmt.Fprintf(os.Stderr, "Waiting for compilation...\n")

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(statusPollInterval)
		status, err := resolve()
		if err != nil {
			continue
		}
		if status.State == "ready" {
			if status.CompileErrors {
				fmt.Fprintf(os.Stderr, "Compilation finished with errors.\n")
			} else {
				fmt.Fprintf(os.Stderr, "Compilation complete.\n")
			}
			return status.CompileErrors
		}
	}

	fmt.Fprintf(os.Stderr, "Timed out waiting for compilation (5m).\n")
	return true
}
