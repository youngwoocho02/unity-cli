package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

var Version = "dev"

var (
	flagPort    int
	flagProject string
	flagTimeout int
	flagJSON    bool
)

func Execute() error {
	flag.IntVar(&flagPort, "port", 0, "Override Unity instance port")
	flag.StringVar(&flagProject, "project", "", "Select Unity instance by project path")
	flag.IntVar(&flagTimeout, "timeout", 120000, "Request timeout in milliseconds")
	flag.BoolVar(&flagJSON, "json", false, "Output raw JSON")

	flag.Usage = func() { printHelp() }

	// Find first non-flag arg position
	args := os.Args[1:]
	cmdArgs := extractCommandArgs(args)

	if len(cmdArgs) == 0 {
		printHelp()
		return nil
	}

	category := cmdArgs[0]
	subArgs := cmdArgs[1:]

	// Handle help/version before instance discovery
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
	}

	// Parse remaining flags
	flag.CommandLine.Parse(extractFlags(args))

	inst, err := client.DiscoverInstance(flagProject, flagPort)
	if err != nil {
		return err
	}

	send := func(command string, params interface{}) (*client.CommandResponse, error) {
		return client.Send(inst, command, params, flagTimeout)
	}

	var resp *client.CommandResponse

	switch category {
	case "editor":
		resp, err = editorCmd(subArgs, send)
	case "console":
		resp, err = consoleCmd(subArgs, send)
	case "exec":
		resp, err = execCmd(subArgs, send)
	case "query":
		resp, err = queryCmd(subArgs, send)
	case "game":
		resp, err = gameCmd(subArgs, send)
	case "tool":
		resp, err = toolCmd(subArgs, send)
	case "diag":
		resp, err = diagCmd(subArgs, send)
	case "profiler":
		resp, err = profilerCmd(subArgs, send)
	case "menu":
		resp, err = menuCmd(subArgs, send)
	case "reserialize":
		resp, err = reserializeCmd(subArgs, send)
	default:
		// Try as direct custom tool call
		resp, err = send(category, map[string]interface{}{})
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
	if flagJSON {
		b, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(b))
		return
	}

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
			b, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Println(string(resp.Data))
		}
	} else if resp.Message != "" {
		fmt.Println(resp.Message)
	}
}

func extractCommandArgs(args []string) []string {
	var result []string
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if a == "--port" || a == "--project" || a == "--timeout" {
			skip = true
			continue
		}
		if a == "--json" {
			continue
		}
		result = append(result, a)
	}
	return result
}

func extractFlags(args []string) []string {
	var result []string
	for i, a := range args {
		if a == "--port" || a == "--project" || a == "--timeout" {
			result = append(result, a)
			if i+1 < len(args) {
				result = append(result, args[i+1])
			}
		}
		if a == "--json" {
			result = append(result, a)
		}
	}
	return result
}

func printHelp() {
	fmt.Print(`unity-cli ` + Version + ` — Control Unity Editor from the command line

Usage: unity-cli <command> [subcommand] [options]

Editor Control:
  editor play [--wait]          Enter play mode (--wait blocks until fully entered)
  editor stop                   Exit play mode
  editor pause                  Toggle pause/resume (play mode only)
  editor refresh                Refresh asset database
  editor refresh --compile      Request script compilation and wait

Console:
  console                       Read error & warning logs (default)
  console --lines 20            Limit to N entries
  console --filter all          Filter: error, warn, log, all

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
  reserialize <path> [paths...] Force YAML reserialize after text edits

  Examples:
    reserialize Assets/Scenes/Main.unity
    reserialize Assets/Prefabs/A.prefab Assets/Prefabs/B.prefab

Profiler:
  profiler hierarchy             Top-level profiler samples (last frame)
  profiler hierarchy --parent 5  Drill into item by ID
  profiler hierarchy --min 0.5   Filter items below 0.5ms
  profiler hierarchy --sort self Sort by self time
  profiler enable                Start profiler recording
  profiler disable               Stop profiler recording
  profiler status                Show profiler state
  profiler clear                 Clear all captured frames

Custom Tools:
  tool list                     List all registered tools (built-in + custom)
  tool call <name>              Call a tool with no parameters
  tool call <name> --params '{"key":"val"}'
                                Call a tool with JSON parameters
  tool help <name>              Show tool description

Update:
  update                        Update to the latest version
  update --check                Check for updates without installing

Global Options:
  --port <N>          Connect to specific Unity port (skip auto-discovery)
  --project <path>    Select Unity instance by project path
  --json              Output full JSON response (default: data only)
  --timeout <ms>      Request timeout in ms (default: 120000)

Help Topics:
  help editor              Editor control details
  help exec                C# execution guide with examples
  help custom-tools        How to write custom tools
  help setup               Installation and Unity setup

Notes:
  - Unity must be open with the Connector package installed
  - Multiple Unity instances: use --port or --project to select
  - Custom tools: any [UnityCliTool] class is auto-discovered
  - Run 'tool list' to see all available tools
`)
}

func printTopicHelp(topic string) {
	switch topic {
	case "editor":
		fmt.Print(`unity-cli editor — Control Unity Editor state

Subcommands:
  play [--wait]       Enter play mode
                      --wait blocks until Unity fully enters play mode.
                      Without --wait, returns immediately after requesting.

  stop                Exit play mode. No effect if not playing.

  pause               Toggle pause. Only works during play mode.
                      First call pauses, second call resumes.

  refresh             Refresh AssetDatabase (reimport changed assets).
    --compile         Also request script compilation and wait for it.

Examples:
  unity-cli editor play --wait    # Start and wait for play mode
  unity-cli editor stop           # Stop play mode
  unity-cli editor refresh --compile  # Recompile scripts
`)
	case "exec":
		fmt.Print(`unity-cli exec — Execute C# code inside Unity Editor

The code runs with full access to UnityEngine, UnityEditor, and all
loaded assemblies. Single expressions auto-return their result.
Multi-statement code needs an explicit 'return' statement.

Usage:
  unity-cli exec "<code>"
  unity-cli exec "<code>" --usings <namespace1,namespace2>

Examples:
  # Simple expression (auto-returns)
  unity-cli exec "Time.time"
  unity-cli exec "Application.dataPath"
  unity-cli exec "GameObject.FindObjectsOfType<Camera>().Length"

  # Get active scene
  unity-cli exec "EditorSceneManager.GetActiveScene().name"

  # Multi-statement (explicit return)
  unity-cli exec "var go = new GameObject(\"Test\"); return go.name;"

  # With extra using directives
  unity-cli exec "World.All.Count" --usings Unity.Entities

  # Complex query
  unity-cli exec "Selection.activeGameObject?.name ?? \"nothing selected\""

Notes:
  - Strings inside code need escaped quotes: \"text\"
  - The code is compiled at runtime using CSharpCodeProvider
  - Compilation errors are returned in the response message
`)
	case "custom-tools", "custom", "tools":
		fmt.Print(`unity-cli custom tools — How to write and use custom tools

Custom tools are C# classes that run inside Unity Editor. The CLI
discovers them automatically via reflection.

## Writing a Tool

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

            [ToolParameter("Y world position", Required = true)]
            public float Y { get; set; }

            [ToolParameter("Z world position", Required = true)]
            public float Z { get; set; }
        }

        public static object HandleCommand(JObject parameters)
        {
            float x = parameters["x"]?.Value<float>() ?? 0;
            float y = parameters["y"]?.Value<float>() ?? 0;
            float z = parameters["z"]?.Value<float>() ?? 0;

            var prefab = Resources.Load<GameObject>("Enemy");
            var instance = Object.Instantiate(prefab,
                new Vector3(x, y, z), Quaternion.identity);

            return new SuccessResponse("Enemy spawned", new {
                name = instance.name,
                position = new { x, y, z }
            });
        }
    }

## Rules

  - Class must be static
  - Must have: public static object HandleCommand(JObject parameters)
  - Return SuccessResponse(message, data) or ErrorResponse(message)
  - Add Parameters class with [ToolParameter] for discoverability
  - Class name auto-converts to snake_case (SpawnEnemy → spawn_enemy)
  - Override name: [UnityCliTool(Name = "my_name")]
  - Runs on Unity main thread — all Unity APIs are safe
  - Discovered on Editor start and after every script recompilation

## Using Tools

  unity-cli tool list                           # List all tools with schemas
  unity-cli tool call spawn_enemy --params '{"x":1,"y":0,"z":5}'
  unity-cli tool help spawn_enemy               # Show parameter details
`)
	case "setup", "install":
		fmt.Print(`unity-cli setup — Installation and Unity configuration

## CLI Installation

  # Linux / macOS (one-liner)
  curl -fsSL https://raw.githubusercontent.com/youngwoocho02/unity-cli/master/install.sh | sh

  # Windows (PowerShell)
  Invoke-WebRequest -Uri "https://github.com/youngwoocho02/unity-cli/releases/latest/download/unity-cli-windows-amd64.exe" -OutFile "$env:LOCALAPPDATA\unity-cli.exe"

  # Go install (any platform)
  go install github.com/youngwoocho02/unity-cli@latest

## Unity Setup

  Add the Connector package via Package Manager:
    1. Window → Package Manager → + → Add package from git URL
    2. Paste: https://github.com/youngwoocho02/unity-cli.git?path=unity-connector

  Or add to Packages/manifest.json:
    "com.youngwoocho02.unity-cli-connector": "https://github.com/youngwoocho02/unity-cli.git?path=unity-connector"

  The Connector starts automatically when Unity opens.

## Multiple Unity Instances

  When multiple editors are open, each registers on a different port
  (8090, 8091, ...). Select by project path or port:

    unity-cli --project MyGame editor play
    unity-cli --port 8091 editor play

  Default: uses the most recently registered instance.

## Verification

  1. Open Unity with the Connector package installed
  2. Run: unity-cli tool list
  3. You should see a list of available tools
`)
	default:
		fmt.Printf("Unknown help topic: %s\n\nAvailable topics: editor, exec, custom-tools, setup\n", topic)
	}
}
