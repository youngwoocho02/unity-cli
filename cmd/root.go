package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

var Version = "dev"

var (
	flagPort    int
	flagProject string
	flagTimeout int
)

func Execute() error {
	flag.IntVar(&flagPort, "port", 0, "Override Unity instance port")
	flag.StringVar(&flagProject, "project", "", "Select Unity instance by project path")
	flag.IntVar(&flagTimeout, "timeout", 120000, "Request timeout in milliseconds")

	flag.Usage = func() { printHelp() }

	args := os.Args[1:]
	flagArgs, cmdArgs := splitArgs(args)
	flag.CommandLine.Parse(flagArgs)

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
		inst, err := client.DiscoverInstance(flagProject, flagPort)
		if err != nil {
			return err
		}
		return statusCmd(inst)
	}

	inst, err := client.DiscoverInstance(flagProject, flagPort)
	if err != nil {
		return err
	}

	if err := waitForAlive(inst.ProjectPath, flagTimeout); err != nil {
		return err
	}

	send := func(command string, params interface{}) (*client.CommandResponse, error) {
		return client.Send(inst, command, params, flagTimeout)
	}

	var resp *client.CommandResponse

	switch category {
	case "editor":
		resp, err = editorCmd(subArgs, send, inst.ProjectPath)
	case "console":
		resp, err = consoleCmd(subArgs, send)
	case "exec":
		resp, err = execCmd(subArgs, send)
	case "list":
		resp, err = send("list_tools", map[string]interface{}{})
	case "profiler":
		resp, err = profilerCmd(subArgs, send)
	case "menu":
		resp, err = menuCmd(subArgs, send)
	case "reserialize":
		resp, err = reserializeCmd(subArgs, send)
	default:
		// Direct custom tool call — flags become params directly
		// e.g. `unity-cli system_tree --depth 1 --scope project` → {"depth":1,"scope":"project"}
		params := map[string]interface{}{}
		flags := parseSubFlags(subArgs)
		if raw, ok := flags["params"]; ok {
			if jsonErr := json.Unmarshal([]byte(raw), &params); jsonErr != nil {
				return fmt.Errorf("invalid JSON in --params: %w", jsonErr)
			}
		}
		// Merge remaining flags into params (--params takes precedence for conflicts)
		for k, v := range flags {
			if k == "params" {
				continue
			}
			if _, exists := params[k]; exists {
				continue
			}
			// Try to parse as int, then bool, then keep as string
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
		resp, err = send(category, params)
	}

	if err != nil {
		return err
	}

	printResponse(resp)

	if !resp.Success {
		os.Exit(1)
	}

	return nil
}

type sendFn func(command string, params interface{}) (*client.CommandResponse, error)

func printResponse(resp *client.CommandResponse) {
	if !resp.Success {
		msg := resp.Message
		if msg == "" {
			msg = "unknown error"
		}
		if resp.Data != nil && len(resp.Data) > 0 && string(resp.Data) != "null" {
			fmt.Fprintf(os.Stderr, "Error: %s\nDetails: %s\n", msg, string(resp.Data))
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
		}
		return
	}

	if resp.Data != nil && len(resp.Data) > 0 && string(resp.Data) != "null" {
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

func setInt(flags map[string]string, params map[string]interface{}, flag, param string) {
	if v, ok := flags[flag]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			params[param] = n
		}
	}
}

func setFloat(flags map[string]string, params map[string]interface{}, flag, param string) {
	if v, ok := flags[flag]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			params[param] = f
		}
	}
}

func setStr(flags map[string]string, params map[string]interface{}, flag, param string) {
	if v, ok := flags[flag]; ok {
		params[param] = v
	}
}

func splitArgs(args []string) (flags, commands []string) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--port" || args[i] == "--project" || args[i] == "--timeout" {
			flags = append(flags, args[i])
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			commands = append(commands, args[i])
		}
	}
	return
}

func printHelp() {
	fmt.Print(`unity-cli ` + Version + ` — Control Unity Editor from the command line

Usage: unity-cli <command> [subcommand] [options]

Editor Control:
  editor play [--wait]          Enter play mode (--wait blocks until fully entered)
  editor stop                   Exit play mode
  editor pause                  Toggle pause/resume (play mode only)
  editor refresh                Refresh asset database
  editor refresh --compile      Recompile scripts and wait until done

Console:
  console                       Read error & warning logs (default)
  console --lines 20            Limit to N entries
  console --filter all          Filter: error, warn, log, all
  console --stacktrace short    Stack trace: none (default), short, full
  console --clear               Clear console

Execute C#:
  exec "<code>"                 Run C# code in Unity (single expression auto-returns)
  exec "<code>" --usings x,y    Add extra using directives

  Examples:
    exec "Time.time"
    exec "GameObject.FindObjectsOfType<Camera>().Length"
    exec "var go = new GameObject(\"Test\"); return go.name;"

Menu:
  menu "<path>"                 Execute Unity menu item by path

  Examples:
    menu "File/Save Project"
    menu "Assets/Refresh"

Reserialize:
  reserialize [path...]          Force reserialize (no args = entire project)

  Examples:
    reserialize                                                    Reserialize entire project
    reserialize Assets/Scenes/Main.unity
    reserialize Assets/Prefabs/A.prefab Assets/Prefabs/B.prefab

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
  --port <N>          Connect to specific Unity port (skip auto-discovery)
  --project <path>    Select Unity instance by project path
  --timeout <ms>      Request timeout in ms (default: 120000)

Use "unity-cli <command> --help" for more information about a command.

Notes:
  - Unity must be open with the Connector package installed
  - Multiple Unity instances: use --port or --project to select
  - Custom tools: any [UnityCliTool] class is auto-discovered
  - Run 'list' to see all available tools
`)
}

func printTopicHelp(topic string) {
	switch topic {
	case "editor":
		fmt.Print(`Usage: unity-cli editor <play|stop|pause|refresh> [options]

Subcommands:
  play [--wait]       Enter play mode
                      --wait blocks until Unity fully enters play mode.
                      Without --wait, returns immediately after requesting.
  stop                Exit play mode. No effect if not playing.
  pause               Toggle pause. Only works during play mode.
  refresh             Refresh AssetDatabase (reimport changed assets).
    --compile         Recompile scripts and wait until compilation finishes.

Examples:
  unity-cli editor play --wait
  unity-cli editor stop
  unity-cli editor refresh --compile
`)
	case "console":
		fmt.Print(`Usage: unity-cli console [options]

Read Unity console log entries.

Options:
  --lines <N>          Limit to N entries
  --filter <mode>      Filter: error, warn, log, all (default: error+warn)
  --stacktrace <mode>  none: first line only (default)
                        short: with stack trace, internal frames filtered
                        full: raw message including all frames
  --clear              Clear console

Examples:
  unity-cli console
  unity-cli console --lines 20 --filter all
  unity-cli console --stacktrace short
  unity-cli console --filter error --stacktrace full
  unity-cli console --clear
`)
	case "exec":
		fmt.Print(`Usage: unity-cli exec "<code>" [options]

Execute C# code inside Unity Editor. Full access to UnityEngine,
UnityEditor, and all loaded assemblies.

Single expressions auto-return their result.
Multi-statement code needs an explicit 'return' statement.

Options:
  --usings <ns1,ns2>   Add extra using directives

Examples:
  unity-cli exec "Time.time"
  unity-cli exec "Application.dataPath"
  unity-cli exec "EditorSceneManager.GetActiveScene().name" --usings UnityEditor.SceneManagement
  unity-cli exec "var go = new GameObject(\"Test\"); return go.name;"
  unity-cli exec "World.All.Count" --usings Unity.Entities

Notes:
  - Strings inside code need escaped quotes: \"text\"
  - Compilation errors are returned in the response message
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
	case "list":
		fmt.Print(`Usage: unity-cli list

List all registered tools (built-in + custom) with parameter schemas.

Example:
  unity-cli list
`)
	case "status":
		fmt.Print(`Usage: unity-cli status

Show the current Unity Editor state: port, project path, version, PID.
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
