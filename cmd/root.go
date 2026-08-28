package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

// Version은 릴리스 빌드에서 -X main.Version로 주입된다. 소스 기본값 dev는 버전 검사를 건너뛴다.
var Version = "dev"

var (
	flagProject               string
	flagTimeout               int
	flagIgnoreVersionMismatch bool
)

// Execute는 CLI 진입점이다.
// 전역 플래그를 파싱한 뒤 명령을 고르고, Unity가 받을 수 있을 때까지 기다린 다음 한 번 보내 응답 하나를 출력한다.
func Execute() error {
	flag.StringVar(&flagProject, "project", "", "Select Unity instance by project path")
	flag.IntVar(&flagTimeout, "timeout", 120000, "Request timeout in milliseconds")
	flag.BoolVar(&flagIgnoreVersionMismatch, "ignore-version-mismatch", false, "Skip CLI/connector version check")

	flag.Usage = func() { printHelp() }

	args := os.Args[1:]
	if err := rejectRemovedFlags(args); err != nil {
		return err
	}

	flagArgs, cmdArgs := splitArgs(args)
	if err := flag.CommandLine.Parse(flagArgs); err != nil {
		fmt.Fprintf(os.Stderr, "flag parse error: %v\n", err)
		os.Exit(1)
	}

	if len(cmdArgs) == 0 {
		printHelp()
		return nil
	}

	category := cmdArgs[0]
	subArgs := cmdArgs[1:]

	// --help / -h on any command
	for _, a := range subArgs {
		if a == "--help" || a == "-h" {
			printTopicHelp(category)
			return nil
		}
	}

	switch category {
	case "help", "--help", "-h":
		if len(subArgs) > 0 {
			printTopicHelp(subArgs[0])
		} else {
			printHelp()
		}
		return nil
	case "version", "--version", "-v":
		fmt.Println("unity-cli " + Version)
		return nil
	case "update":
		return updateCmd(subArgs)
	case "status":
		statusErr := statusCmd(flagProject, flagIgnoreVersionMismatch)
		printUpdateNotice()
		return statusErr
	}

	// 어느 Unity에 보낼지 정한다. 이후 폴링도 같은 프로젝트를 다시 찾는다.
	inst, err := client.DiscoverInstance(flagProject)
	if err != nil {
		return err
	}

	targetProject := flagProject
	if targetProject == "" {
		targetProject = inst.ProjectPath
	}

	resolve := func() (*client.Instance, error) {
		return client.DiscoverInstance(targetProject)
	}

	// 모든 명령은 sendWithRetry를 탄다. Health가 받을 수 있을 때까지 기다린 뒤 POST 한 번.
	timeout := flagTimeout
	send := func(command string, params interface{}) (*client.CommandResponse, error) {
		return sendWithRetry(resolve, command, params, timeout)
	}

	var resp *client.CommandResponse

	switch category {
	case "editor":
		resp, err = editorCmd(subArgs, send, resolve)
	case "test":
		resp, err = testCmd(subArgs, send, resolve)
	case "exec":
		subArgs = readStdinIfPiped(subArgs)
		var params map[string]interface{}
		params, err = buildParams(subArgs, nil)
		if err == nil {
			err = validateExecAsyncPolicy(params)
		}
		if err == nil {
			resp, err = send("exec", params)
		}
	default:
		var params map[string]interface{}
		params, err = buildParams(subArgs, nil)
		if err == nil {
			resp, err = send(category, params)
		}
	}

	if err != nil {
		return err
	}

	printResponse(resp)

	printUpdateNotice()

	if !resp.Success {
		os.Exit(1)
	}

	return nil
}

// sendFn is the function signature for sending a command to Unity.
// Injected into each command function so they can be tested without a real Unity connection.
type sendFn func(command string, params interface{}) (*client.CommandResponse, error)

var (
	sendCommand = client.Send
	healthCheck = client.Health
)

// sendWithRetry는 한 명령을 성공할 때까지 반복한다.
// 1) 인스턴스를 다시 찾고 2) 버전을 확인하고 3) /health로 받을 수 있는지 보고
// 4) 받을 수 있으면 /command를 한 번 보낸다.
// 컴파일 중·연결 끊김 같은 일시 오류만 다시 시도한다. 잘못된 명령은 바로 실패한다.
func sendWithRetry(resolve instanceResolver, command string, params interface{}, timeoutMs int) (*client.CommandResponse, error) {
	deadline := commandDeadline(timeoutMs)
	var lastErr error
	for {
		inst, err := resolve()
		if err != nil {
			lastErr = err
			if !sleepUntilNextPoll(deadline) {
				break
			}
			continue
		}
		if err := checkConnectorVersion(inst, Version, flagIgnoreVersionMismatch); err != nil {
			return nil, err
		}
		health, err := healthCheck(inst, probeTimeoutMs(deadline))
		if err != nil {
			// 아직 compiling 등이면 보내지 않고 다음 폴링까지 기다린다.
			lastErr = err
			if !sleepUntilNextPoll(deadline) {
				break
			}
			continue
		}
		if err := checkConnectorVersion(health, Version, flagIgnoreVersionMismatch); err != nil {
			return nil, err
		}
		resp, err := sendCommand(health, command, params, commandTimeoutMs(deadline))
		if err == nil {
			return resp, nil
		}
		if client.IsTransientUnityError(err) {
			lastErr = err
			if !sleepUntilNextPoll(deadline) {
				break
			}
			continue
		}
		return nil, fmt.Errorf("failed sending command to Unity: %v", err)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("timed out sending command to Unity: %v", lastErr)
	}
	return nil, fmt.Errorf("timed out sending command to Unity")
}

// commandDeadline은 --timeout(ms)을 절대 시각으로 바꾼다. 0이하면 기본 120초.
func commandDeadline(timeoutMs int) time.Time {
	if timeoutMs <= 0 {
		timeoutMs = 120000
	}
	return time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
}

// probeTimeoutMs는 Health 한 번의 HTTP 타임아웃이다. 남은 전체 대기보다 길면 안 되고, 보통 최대 1초.
func probeTimeoutMs(deadline time.Time) int {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 1
	}
	if remaining > time.Second {
		return 1000
	}
	ms := int(remaining / time.Millisecond)
	if ms < 1 {
		return 1
	}
	return ms
}

// commandTimeoutMs는 실제 POST /command에 주는 남은 시간이다.
func commandTimeoutMs(deadline time.Time) int {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 1
	}
	ms := int(remaining / time.Millisecond)
	if ms < 1 {
		return 1
	}
	return ms
}

// sleepUntilNextPoll은 다음 heartbeat 간격만큼 잔다. deadline이 지났으면 false.
func sleepUntilNextPoll(deadline time.Time) bool {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	sleep := statusPollInterval
	if remaining < sleep {
		sleep = remaining
	}
	time.Sleep(sleep)
	return time.Now().Before(deadline)
}

// printResponse는 성공이면 data(또는 message)를 stdout에, 실패면 stderr에 찍는다.
func printResponse(resp *client.CommandResponse) {
	if !resp.Success {
		msg := resp.Message
		if msg == "" {
			msg = "unknown error"
		}
		if len(resp.Data) > 0 && string(resp.Data) != "null" {
			fmt.Fprintf(os.Stderr, "Error: %s\nDetails: %s\n", msg, string(resp.Data))
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
		}
		return
	}

	if len(resp.Data) > 0 && string(resp.Data) != "null" {
		var pretty interface{}
		if json.Unmarshal(resp.Data, &pretty) == nil {
			// If data is a plain string, print it raw (preserves newlines for tree output etc.)
			if s, ok := pretty.(string); ok {
				fmt.Println(s)
			} else {
				b, _ := json.MarshalIndent(pretty, "", "  ")
				fmt.Println(string(b))
			}
		} else {
			fmt.Println(string(resp.Data))
		}
	} else if resp.Message != "" {
		fmt.Println(resp.Message)
	}
}

// parseSubFlags parses --key value and --flag (boolean) pairs from subcommand args.
// Non-flag args (no "--" prefix) are silently ignored.
func parseSubFlags(args []string) map[string]string {
	flags := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			key := a[2:]
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				flags[key] = args[i+1]
				i++
			} else {
				flags[key] = "true"
			}
		}
	}
	return flags
}

// buildParams parses --flag value pairs and positional args from args and merges with base params.
func buildParams(args []string, base map[string]interface{}) (map[string]interface{}, error) {
	params := map[string]interface{}{}
	for k, v := range base {
		params[k] = v
	}

	var positional []string
	flags := map[string][]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			key := a[2:]
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				flags[key] = append(flags[key], args[i+1])
				i++
			} else {
				flags[key] = append(flags[key], "true")
			}
		} else {
			positional = append(positional, a)
		}
	}

	// --params JSON이 있으면 그걸 기본으로 깔고, 나머지 플래그가 덮어쓴다.
	if values, ok := flags["params"]; ok && len(values) > 0 {
		raw := values[len(values)-1]
		if jsonErr := json.Unmarshal([]byte(raw), &params); jsonErr != nil {
			return nil, fmt.Errorf("invalid JSON in --params: %w", jsonErr)
		}
	}
	for k, values := range flags {
		if k == "params" {
			continue
		}
		if _, exists := params[k]; exists {
			continue
		}
		if k == "usings" {
			params[k] = normalizeUsings(values)
			continue
		}

		v := values[len(values)-1]
		if n, err := strconv.Atoi(v); err == nil {
			params[k] = n
		} else if v == "true" {
			params[k] = true
		} else if v == "false" {
			params[k] = false
		} else {
			params[k] = v
		}
	}

	if len(positional) > 0 {
		params["args"] = positional
	}

	return params, nil
}

// normalizeUsings는 --usings를 쉼표/반복 입력을 합치고 중복을 뺀다.
func normalizeUsings(values []string) []string {
	var result []string
	seen := map[string]bool{}
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			using := strings.TrimSpace(item)
			if using == "" || seen[using] {
				continue
			}
			seen[using] = true
			result = append(result, using)
		}
	}
	return result
}

var execAsyncPatterns = []struct {
	label string
	re    *regexp.Regexp
}{
	{"async", regexp.MustCompile(`\basync\b`)},
	{"await", regexp.MustCompile(`\bawait\b`)},
	{"Task", regexp.MustCompile(`\b(?:Task|ValueTask|TaskCompletionSource)\b`)},
	{"UniTask", regexp.MustCompile(`\bUniTask\b`)},
	{"Awaitable", regexp.MustCompile(`\bAwaitable\b`)},
	{"IAsync", regexp.MustCompile(`\bIAsync(?:Enumerable|Enumerator)\b`)},
	{"Coroutine", regexp.MustCompile(`\b(?:Coroutine|StartCoroutine|EditorCoroutineUtility|WaitForSeconds|WaitUntil|WaitWhile)\b`)},
	{"AsyncOperation", regexp.MustCompile(`\b(?:AsyncOperation|UnityWebRequestAsyncOperation)\b`)},
	{"Unity async API", regexp.MustCompile(`\b(?:LoadSceneAsync|UnloadSceneAsync|LoadAsync|InstantiateAsync|AsyncGPUReadback)\b`)},
	{"EditorApplication async callback", regexp.MustCompile(`\bEditorApplication\s*\.\s*(?:update|delayCall)\b`)},
}

// validateExecAsyncPolicy는 exec 코드에 async/코루틴이 있으면 기본 차단한다.
// --allow-async가 있을 때만 통과시킨다.
func validateExecAsyncPolicy(params map[string]interface{}) error {
	if allowExecAsync(params) {
		delete(params, "allow-async")
		return nil
	}
	delete(params, "allow-async")

	code := execCodeFromParams(params)
	for _, pattern := range execAsyncPatterns {
		if pattern.re.MatchString(code) {
			return fmt.Errorf("exec blocks async or deferred Unity code by default: found %s; rerun with --allow-async to allow it", pattern.label)
		}
	}

	return nil
}

// allowExecAsync는 --allow-async 플래그가 true인지 본다.
func allowExecAsync(params map[string]interface{}) bool {
	v, ok := params["allow-async"]
	if !ok {
		return false
	}
	allowed, ok := v.(bool)
	return ok && allowed
}

// execCodeFromParams는 --code 또는 위치 인자에서 실행할 C# 문자열을 꺼낸다.
func execCodeFromParams(params map[string]interface{}) string {
	if code, ok := params["code"].(string); ok {
		return code
	}
	if args, ok := params["args"].([]string); ok && len(args) > 0 {
		return args[0]
	}
	if args, ok := params["args"].([]interface{}); ok && len(args) > 0 {
		if code, ok := args[0].(string); ok {
			return code
		}
	}
	return ""
}

// rejectRemovedFlags는 예전에 쓰던 --port를 막아 --project로 유도한다.
func rejectRemovedFlags(args []string) error {
	for _, arg := range args {
		if arg == "--port" || strings.HasPrefix(arg, "--port=") {
			return fmt.Errorf("--port was removed; select Unity by project path with --project")
		}
	}

	return nil
}

// readStdinIfPiped reads stdin when piped and prepends it as the first positional arg.
func readStdinIfPiped(args []string) []string {
	info, err := os.Stdin.Stat()
	if err != nil {
		return args
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return args // interactive terminal, not piped
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil || len(data) == 0 {
		return args
	}
	code := strings.TrimRight(string(data), "\n\r")
	return append([]string{code}, args...)
}

// splitArgs separates global flags from subcommand args.
// Global flags must be parsed by flag.CommandLine before the subcommand runs.
func splitArgs(args []string) (flags, commands []string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ignore-version-mismatch":
			flags = append(flags, args[i])
		case "--ignore-version-mismatch=true", "--ignore-version-mismatch=false":
			flags = append(flags, args[i])
		case "--project", "--timeout":
			flags = append(flags, args[i])
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			commands = append(commands, args[i])
		}
	}
	return
}

// printHelp는 전체 사용법을 stdout에 출력한다.
func printHelp() {
	fmt.Print(`unity-cli ` + Version + ` — Control Unity Editor from the command line

Usage: unity-cli <command> [subcommand] [options]

Editor Control:
  editor play [--wait]          Enter play mode (--wait blocks until fully entered)
  editor stop                   Exit play mode
  editor pause                  Toggle pause/resume (play mode only)
  editor refresh [--force]      Refresh asset database (blocked in play mode unless forced)
  editor refresh --compile      Recompile scripts and wait until done

Console:
  console                       Read error & warning logs (default)
  console --lines 20            Limit to N entries
  console --type error,warning,log   Filter by log types (comma-separated)
  console --stacktrace full     Stack trace: none, user (default), full
  console --clear               Clear console

Execute C#:
  exec "<code>"                 Run C# code in Unity (return required for output)
  echo '<code>' | exec          Pipe code via stdin (avoids shell escaping)
  exec "<code>" --usings x,y    Add extra using directives (repeatable)

  Examples:
    exec "Time.time"
    exec "GameObject.FindObjectsOfType<Camera>().Length"
    exec "var go = new GameObject(\"Test\"); return go.name;"

Menu:
  menu "<path>"                 Execute Unity menu item by path

  Examples:
    menu "File/Save Project"
    menu "Assets/Refresh"

Screenshot:
  screenshot                          Capture scene view (default)
  screenshot --view game              Capture game view
  screenshot --output_path <path>     Custom output path

Reserialize:
  reserialize [path...]          Force reserialize (no args = entire project)

  Examples:
    reserialize                                                    Reserialize entire project
    reserialize Assets/Scenes/Main.unity
    reserialize Assets/Prefabs/A.prefab Assets/Prefabs/B.prefab

Tests:
  test                            Run EditMode tests (default)
  test --mode PlayMode            Run PlayMode tests
  test --filter <name>            Filter by namespace, class, or full test name

Profiler:
  profiler hierarchy              Top-level profiler samples (last frame)
  profiler hierarchy --depth 5    Recursive drill-down (0=unlimited)
  profiler hierarchy --root Name  Set root by name (substring match)
  profiler hierarchy --frames 30  Average over last 30 frames
  profiler hierarchy --parent 5   Drill into item by ID
  profiler hierarchy --min 0.5    Filter items below 0.5ms
  profiler hierarchy --sort self  Sort by self time
  profiler enable                Start profiler recording
  profiler disable               Stop profiler recording
  profiler status                Show profiler state
  profiler clear                 Clear all captured frames

Custom Tools:
  list                          List all registered tools with parameter schemas
  <name>                        Call a custom tool directly
  <name> --params '{"k":"v"}'   Call with JSON parameters

Status:
  status                        Show Unity Editor state (ready, compiling, etc.)

Update:
  update                        Update to the latest version
  update --check                Check for updates without installing

Global Options:
  --project <path>    Select Unity instance by project path
  --timeout <ms>      Wait for Unity to accept, then for the response (default: 120000)
  --ignore-version-mismatch
                      Skip CLI/connector version check
Use "unity-cli <command> --help" for more information about a command.

Notes:
  - Unity must be open with the Connector package installed
  - Multiple Unity instances: use --project to select
  - Custom tools: any [UnityCliTool] class is auto-discovered
  - Run 'list' to see all available tools
`)
}

// printTopicHelp는 한 명령의 자세한 사용법을 출력한다.
func printTopicHelp(topic string) {
	switch topic {
	case "editor":
		fmt.Print(`Usage: unity-cli editor <play|stop|pause|refresh> [options]

Subcommands:
  play [--wait]       Enter play mode
                      --wait waits until heartbeat reports playing.
                      Without --wait, returns immediately after requesting.
  stop                Exit play mode. No effect if not playing.
  pause               Toggle pause. Only works during play mode.
  refresh             Refresh AssetDatabase (reimport changed assets).
                      Blocked in play mode unless --force is set.
    --compile         Recompile scripts and wait until compilation finishes.
    --force           Allow refresh during play mode and force asset update.

Examples:
  unity-cli editor play --wait
  unity-cli editor stop
  unity-cli editor refresh --compile
  unity-cli editor refresh --force
`)
	case "console":
		fmt.Print(`Usage: unity-cli console [options]

Read Unity console log entries.

Options:
  --lines <N>          Limit to N entries
  --type <types>       Comma-separated log types: error, warning, log (default: error,warning,log)
  --stacktrace <mode>  none: first line only
                        user: with stack trace, internal frames filtered (default)
                        full: raw message including all frames
  --clear              Clear console

Examples:
  unity-cli console
  unity-cli console --lines 20 --type error,warning,log
  unity-cli console --stacktrace user
  unity-cli console --type error --stacktrace full
  unity-cli console --clear
`)
	case "exec":
		fmt.Print(`Usage: unity-cli exec "<code>" [options]

Execute C# code inside Unity Editor. Full access to UnityEngine,
UnityEditor, and all loaded assemblies.

Use 'return' to get output. Add --usings for types outside default namespaces.

Options:
  --usings <ns1,ns2>   Add extra using directives (comma-separated or repeatable)
  --csc <path>         Path to csc compiler (csc.dll or csc.exe). Auto-detected if omitted.
  --dotnet <path>      Path to dotnet runtime. Auto-detected if omitted.
  --allow-async        Allow async/deferred Unity code. Blocked by default.

Default usings: System, System.Collections.Generic, System.IO, System.Linq,
  System.Reflection, System.Threading.Tasks, UnityEngine,
  UnityEngine.SceneManagement, UnityEditor, UnityEditor.SceneManagement,
  UnityEditorInternal

Examples:
  unity-cli exec "return 1+1;"
  unity-cli exec "return Application.dataPath;"
  echo 'return EditorSceneManager.GetActiveScene().name;' | unity-cli exec
  echo 'Debug.Log("hello"); return null;' | unity-cli exec
  unity-cli exec "return World.All.Count;" --usings Unity.Entities
  unity-cli exec "return World.All.Count;" --usings Unity.Entities --usings Unity.Mathematics

Stdin:
  Pipe code via stdin to avoid shell escaping issues.
  echo '<code>' | unity-cli exec [--usings ns1,ns2] [--usings ns3]

Notes:
  - Use 'return' for output, 'return null;' for void operations
`)
	case "menu":
		fmt.Print(`Usage: unity-cli menu "<path>"

Execute a Unity menu item by its path.

Examples:
  unity-cli menu "File/Save Project"
  unity-cli menu "Assets/Refresh"
  unity-cli menu "Window/General/Console"

Note: File/Quit is blocked for safety.
`)
	case "screenshot":
		fmt.Print(`Usage: unity-cli screenshot [options]

Capture a screenshot of the Unity editor.

Options:
  --view <mode>      scene (default), game
  --width <N>        Scene view width (default: 1920). Ignored for game view.
  --height <N>       Scene view height (default: 1080). Ignored for game view.
  --output_path <path>  Output path, absolute or relative to project root
                        (default: Screenshots/screenshot.png)

Game view captures the composited Game view, including Screen Space - Overlay UI.
Width/height do not apply; the file is the Game view's native resolution.

Examples:
  unity-cli screenshot
  unity-cli screenshot --view game
  unity-cli screenshot --view scene --width 3840 --height 2160
  unity-cli screenshot --output_path captures/my_scene.png
`)
	case "reserialize":
		fmt.Print(`Usage: unity-cli reserialize [path...]

Force Unity to reserialize assets through its own YAML serializer.
Run after editing .prefab, .unity, .asset, or .mat files as text.
No arguments = reserialize the entire project.

Examples:
  unity-cli reserialize
  unity-cli reserialize Assets/Prefabs/Player.prefab
  unity-cli reserialize Assets/Scenes/Main.unity Assets/Scenes/Lobby.unity
`)
	case "profiler":
		fmt.Print(`Usage: unity-cli profiler <subcommand> [options]

Subcommands:
  hierarchy             Top-level profiler samples (last frame)
    --depth <N>         Recursive depth (0=unlimited, default: 1)
    --root <name>       Set root by name (substring match, searches full tree)
    --frames <N>        Average over last N frames (flat output, sorted by time)
    --from <N>          Start frame index for range average
    --to <N>            End frame index for range average
    --parent <ID>       Drill into item by ID
    --min <ms>          Filter items below threshold
    --sort <col>        Sort by: total (default), self, calls
    --max <N>           Max children per level (default: 30)
    --frame <N>         Specific frame index
    --thread <N>        Thread index (0=main)
  enable                Start profiler recording
  disable               Stop profiler recording
  status                Show profiler state
  clear                 Clear all captured frames

Examples:
  unity-cli profiler hierarchy --depth 3
  unity-cli profiler hierarchy --root SimulationSystem --depth 3
  unity-cli profiler hierarchy --frames 30 --min 0.5 --sort self
  unity-cli profiler enable
`)
	case "test":
		fmt.Print(`Usage: unity-cli test [options]

Run Unity tests via the Test Runner API.

Options:
  --mode <EditMode|PlayMode>    Test mode (default: EditMode)
  --filter <name>               Filter by namespace, class, or full test name
                                Must be the full path (e.g. MyNamespace.MyClass)

EditMode tests hold the connection open and return results directly.
PlayMode tests return immediately and poll a results file (domain reload safe).

Requires the Unity Test Framework package (com.unity.test-framework).

Examples:
  unity-cli test
  unity-cli test --mode PlayMode
  unity-cli test --filter MyNamespace.MyTests
  unity-cli test --mode EditMode --filter MyNamespace.MyTests.SpecificTest
`)
	case "list":
		fmt.Print(`Usage: unity-cli list

List all registered tools (built-in + custom) with parameter schemas.

Example:
  unity-cli list
`)
	case "status":
		fmt.Print(`Usage: unity-cli status

Show the current Unity Editor state: project path, version, PID.
Reports "not responding" if heartbeat is older than 3 seconds.

Example:
  unity-cli status
`)
	case "update":
		fmt.Print(`Usage: unity-cli update [options]

Update the CLI binary to the latest release from GitHub.

Options:
  --check              Check for updates without installing

Examples:
  unity-cli update
  unity-cli update --check
`)
	case "custom-tools", "custom", "tools":
		fmt.Print(`How to write custom tools for unity-cli

Custom tools are C# classes that run inside Unity Editor. The CLI
discovers them automatically via reflection.

Create a static class with [UnityCliTool] in any Editor assembly:

    using UnityCliConnector;
    using Newtonsoft.Json.Linq;

    [UnityCliTool(Description = "Spawn an enemy at a position")]
    public static class SpawnEnemy
    {
        public class Parameters
        {
            [ToolParameter("X world position", Required = true)]
            public float X { get; set; }
        }

        public static object HandleCommand(JObject parameters)
        {
            float x = parameters["x"]?.Value<float>() ?? 0;
            var go = Object.Instantiate(prefab, new Vector3(x, 0, 0), Quaternion.identity);
            return new SuccessResponse("Spawned", new { name = go.name });
        }
    }

Rules:
  - Class must be static
  - Must have: public static object HandleCommand(JObject parameters)
  - Return SuccessResponse(message, data) or ErrorResponse(message)
  - Add Parameters class with [ToolParameter] for discoverability
  - Class name auto-converts to snake_case (SpawnEnemy → spawn_enemy)
  - Override name: [UnityCliTool(Name = "my_name")]
  - Runs on Unity main thread — all Unity APIs are safe
  - Discovered on Editor start and after every script recompilation
  - Duplicate tool names are detected and logged as errors (first wins)
`)
	case "setup", "install":
		fmt.Print(`Installation and Unity setup

CLI Installation:
  # Linux / macOS
  curl -fsSL https://raw.githubusercontent.com/youngwoocho02/unity-cli/master/install.sh | sh

  # Windows (PowerShell)
  irm https://raw.githubusercontent.com/youngwoocho02/unity-cli/master/install.ps1 | iex

  # Go install (any platform)
  go install github.com/youngwoocho02/unity-cli@latest

Unity Setup:
  1. Window → Package Manager → + → Add package from git URL
  2. Paste: https://github.com/youngwoocho02/unity-cli.git?path=unity-connector
  The Connector starts automatically when Unity opens.

Verify:
  unity-cli list
`)
	default:
		fmt.Printf("Unknown help topic: %s\n\nUse \"unity-cli --help\" for available commands.\n", topic)
	}
}
