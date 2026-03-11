# unity-cli

[English](README.md) | [한국어](README.ko.md)

> Control Unity Editor from the command line. Built for AI agents, works with anything.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**No MCP protocol. No Python relay. No runtime dependencies. Just a single binary.**

## Why not MCP?

MCP-based Unity integrations ship tens of thousands of lines of code across Python relays, WebSocket bridges, JSON-RPC protocol layers, and runtime dependencies. The result is a system that's hard to install, hard to debug, and impossible to understand without reading the source.

This project takes the opposite approach. The entire CLI is ~500 lines of Go. The Unity-side connector is ~1,500 lines of C#. There is no protocol layer, no relay process, no virtual environment — just an HTTP POST from a binary to Unity's built-in HttpListener.

If you can run a shell command, you can control Unity. That's it.

## Install

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/youngwoocho02/unity-cli/master/install.sh | sh
```

### Windows (PowerShell)

```powershell
Invoke-WebRequest -Uri "https://github.com/youngwoocho02/unity-cli/releases/latest/download/unity-cli-windows-amd64.exe" -OutFile "$env:LOCALAPPDATA\unity-cli.exe"; if($env:Path -notlike "*$env:LOCALAPPDATA*"){[Environment]::SetEnvironmentVariable("Path","$([Environment]::GetEnvironmentVariable('Path','User'));$env:LOCALAPPDATA","User");$env:Path+= ";$env:LOCALAPPDATA"}; unity-cli version
```

### Other options

```bash
# Go install (any platform with Go)
go install github.com/youngwoocho02/unity-cli@latest

# Manual download (pick your platform)
# Linux amd64 / Linux arm64 / macOS amd64 / macOS arm64 / Windows amd64
curl -fsSL https://github.com/youngwoocho02/unity-cli/releases/latest/download/unity-cli-linux-amd64 -o unity-cli
chmod +x unity-cli && sudo mv unity-cli /usr/local/bin/
```

Supported platforms: Linux (amd64, arm64), macOS (Intel, Apple Silicon), Windows (amd64).

### Update

```bash
# Update to the latest version
unity-cli update

# Check for updates without installing
unity-cli update --check
```

## Unity Setup

Add the Unity Connector package via **Package Manager → Add package from git URL**:

```
https://github.com/youngwoocho02/unity-cli.git?path=unity-connector
```

Or add directly to `Packages/manifest.json`:
```json
"com.youngwoocho02.unity-cli-connector": "https://github.com/youngwoocho02/unity-cli.git?path=unity-connector"
```

To pin a specific version, append `#v0.1.0` to the URL.

Once added, the Connector starts automatically when Unity opens. No configuration needed.

### Recommended: Disable Editor Throttling

By default, Unity throttles editor updates when the window is unfocused. This means CLI commands may not execute until you click back into Unity.

To fix this, go to **Edit → Preferences → General → Interaction Mode** and set it to **No Throttling**.

This ensures CLI commands are processed immediately, even when Unity is in the background.

## How It Works

```
Terminal                              Unity Editor
────────                              ────────────
$ unity-cli editor play --wait
    │
    ├─ reads ~/.unity-cli/instances.json
    │  → finds Unity on port 8090
    │
    ├─ POST http://127.0.0.1:8090/command
    │  { "command": "manage_editor",
    │    "params": { "action": "play",
    │                "wait_for_completion": true }}
    │                                      │
    │                                  HttpServer receives
    │                                      │
    │                                  CommandRouter dispatches
    │                                      │
    │                                  ManageEditor.HandleCommand()
    │                                  → EditorApplication.isPlaying = true
    │                                  → waits for PlayModeStateChange
    │                                      │
    ├─ receives JSON response  ←───────────┘
    │  { "success": true,
    │    "message": "Entered play mode (confirmed)." }
    │
    └─ prints: Entered play mode (confirmed).
```

The Unity Connector:
1. Opens an HTTP server on `localhost:8090` when the Editor starts
2. Registers itself in `~/.unity-cli/instances.json` so the CLI knows where to connect
3. Writes a heartbeat to `~/.unity-cli/status/{port}.json` every 0.5s with the current state
4. Discovers all `[UnityCliTool]` classes via reflection on each request
5. Routes incoming commands to the matching handler on the main thread
6. Survives domain reloads (script recompilation)

Before compiling or reloading, the Connector records the state (`compiling`, `reloading`) to the status file. When the main thread freezes, the timestamp stops updating. The CLI detects this and waits for a fresh timestamp before sending commands.

## Built-in Commands

### Editor Control

```bash
# Enter play mode
unity-cli editor play

# Enter play mode and wait until fully loaded
unity-cli editor play --wait

# Stop play mode
unity-cli editor stop

# Toggle pause (only works during play mode)
unity-cli editor pause

# Refresh assets
unity-cli editor refresh

# Refresh and recompile scripts (waits for compilation to finish)
unity-cli editor refresh --compile
```

### Console Logs

```bash
# Read error and warning logs (default)
unity-cli console

# Read last 20 log entries of all types
unity-cli console --lines 20 --filter all

# Read only errors
unity-cli console --filter error

# Clear console
# (use exec for this)
unity-cli exec "typeof(UnityEditor.LogEntries).GetMethod(\"Clear\", System.Reflection.BindingFlags.Static | System.Reflection.BindingFlags.Public).Invoke(null, null); return \"cleared\";"
```

### Execute C# Code

Run arbitrary C# code inside the Unity Editor at runtime. This is the most powerful command — it gives you full access to UnityEngine, UnityEditor, ECS, and every loaded assembly. No need to write a custom tool for one-off queries or mutations.

Single expressions auto-return their result. Multi-statement code needs an explicit `return`.

```bash
# Simple expressions
unity-cli exec "Time.time"
unity-cli exec "Application.dataPath"
unity-cli exec "EditorSceneManager.GetActiveScene().name"

# Query game objects
unity-cli exec "GameObject.FindObjectsOfType<Camera>().Length"
unity-cli exec "Selection.activeGameObject?.name ?? \"nothing selected\""

# Multi-statement (explicit return)
unity-cli exec "var go = new GameObject(\"Marker\"); go.tag = \"EditorOnly\"; return go.name;"

# ECS world inspection with extra usings
unity-cli exec "World.All.Count" --usings Unity.Entities
unity-cli exec "var sb = new System.Text.StringBuilder(); foreach(var w in World.All) sb.AppendLine(w.Name); return sb.ToString();" --usings Unity.Entities

# Modify project settings at runtime
unity-cli exec "PlayerSettings.bundleVersion = \"1.2.3\"; return PlayerSettings.bundleVersion;"

# Access ScriptableObjects
unity-cli exec "Resources.Load<GameSettings>(\"GameSettings\").maxPlayers" --usings YourNamespace
```

Because `exec` compiles and runs real C#, it can do anything a custom tool can — inspect ECS entities, modify assets, call internal APIs, run editor utilities. For AI agents, this means **zero-friction access to Unity's entire runtime** without writing a single line of tool code.

### Menu Items

```bash
# Execute any Unity menu item by path
unity-cli menu "File/Save Project"
unity-cli menu "Assets/Refresh"
unity-cli menu "Window/General/Console"
```

Note: `File/Quit` is blocked for safety.

### Asset Reserialize

AI agents (and humans) can edit Unity asset files — `.prefab`, `.unity`, `.asset`, `.mat` — as plain text YAML. But Unity's YAML serializer is strict: a missing field, wrong indent, or stale `fileID` will corrupt the asset silently.

`reserialize` fixes this. After a text edit, it tells Unity to load the asset into memory and write it back out through its own serializer. The result is a clean, valid YAML file — as if you had edited it through the Inspector.

```bash
# After editing a prefab's transform values in a text editor
unity-cli reserialize Assets/Prefabs/Player.prefab

# After batch-editing multiple scenes
unity-cli reserialize Assets/Scenes/Main.unity Assets/Scenes/Lobby.unity

# After modifying material properties
unity-cli reserialize Assets/Materials/Character.mat
```

This is what makes text-based asset editing safe. Without it, a single misplaced YAML field can break a prefab with no visible error until runtime. With it, **AI agents can confidently modify any Unity asset through plain text** — add components to prefabs, adjust scene hierarchies, change material properties — and know the result will load correctly.

### Profiler

```bash
# Read profiler hierarchy (last frame)
unity-cli profiler hierarchy

# With depth limit
unity-cli profiler hierarchy --depth 3
```

### Custom Tools

```bash
# List all registered tools (built-in + project custom)
unity-cli tool list

# Call a custom tool
unity-cli tool call my_custom_tool --params '{"key": "value"}'

# Get tool help
unity-cli tool help my_custom_tool
```

### Status

```bash
# Show Unity Editor state
unity-cli status
# Output: Unity (port 8090): ready
#   Project: /path/to/project
#   Version: 6000.1.0f1
#   PID:     12345
```

The CLI also checks Unity's state automatically before sending any command. If Unity is busy (compiling, reloading), it waits for Unity to become responsive.

## Global Options

| Flag | Description | Default |
|------|-------------|---------|
| `--port <N>` | Override Unity instance port (skip auto-discovery) | auto |
| `--project <path>` | Select Unity instance by project path | latest |
| `--json` | Output raw JSON response | off |
| `--timeout <ms>` | HTTP request timeout | 120000 |

```bash
# Connect to a specific Unity instance
unity-cli --port 8091 editor play

# Select by project path when multiple Unity instances are open
unity-cli --project MyGame editor stop

# Get raw JSON output (useful for AI parsing)
unity-cli --json console --lines 10
```

## Writing Custom Tools

Create a static class with `[UnityCliTool]` attribute in any Editor assembly. The Connector discovers it automatically on domain reload.

```csharp
using UnityCliConnector;
using Newtonsoft.Json.Linq;

[UnityCliTool(Description = "Spawn an enemy at a position")]
public static class SpawnEnemy
{
    // Command name auto-derived: "spawn_enemy"
    // Call with: unity-cli tool call spawn_enemy --params '{"x":1,"y":0,"z":5}'

    public class Parameters
    {
        [ToolParameter("X world position", Required = true)]
        public float X { get; set; }

        [ToolParameter("Y world position", Required = true)]
        public float Y { get; set; }

        [ToolParameter("Z world position", Required = true)]
        public float Z { get; set; }

        [ToolParameter("Prefab name in Resources folder")]
        public string Prefab { get; set; }
    }

    public static object HandleCommand(JObject parameters)
    {
        float x = parameters["x"]?.Value<float>() ?? 0;
        float y = parameters["y"]?.Value<float>() ?? 0;
        float z = parameters["z"]?.Value<float>() ?? 0;
        string prefabName = parameters["prefab"]?.Value<string>() ?? "Enemy";

        var prefab = Resources.Load<GameObject>(prefabName);
        var instance = Object.Instantiate(prefab, new Vector3(x, y, z), Quaternion.identity);

        return new SuccessResponse("Enemy spawned", new
        {
            name = instance.name,
            position = new { x, y, z }
        });
    }
}
```

The `Parameters` class is optional but recommended. When present, `tool list` and `tool help` expose parameter names, types, descriptions, and required flags — so AI assistants can discover how to call your tool without reading the source code.

```bash
$ unity-cli tool help spawn_enemy
spawn_enemy — Spawn an enemy at a position
  x        float    (required) X world position
  y        float    (required) Y world position
  z        float    (required) Z world position
  prefab   string              Prefab name in Resources folder
```

### Rules

- Class must be `static`
- Must have `public static object HandleCommand(JObject parameters)` or `async Task<object>` variant
- Return `SuccessResponse(message, data)` or `ErrorResponse(message)`
- Add a `Parameters` nested class with `[ToolParameter]` attributes for discoverability
- Class name is auto-converted to snake_case for the command name
- Override with `[UnityCliTool(Name = "my_name")]` if needed
- Runs on Unity main thread, so all Unity APIs are safe to call
- Discovered automatically on Editor start and after every script recompilation

## Multiple Unity Instances

When multiple Unity Editors are open, each registers on a different port (8090, 8091, ...):

```bash
# See all running instances
cat ~/.unity-cli/instances.json

# Select by project path
unity-cli --project MyGame editor play

# Select by port
unity-cli --port 8091 editor play

# Default: uses the most recently registered instance
unity-cli editor play
```

## Compared to MCP

| | MCP | unity-cli |
|---|-----|-----------|
| **Install** | Python + uv + FastMCP + config JSON | Single binary |
| **Dependencies** | Python runtime, WebSocket relay | None |
| **Protocol** | JSON-RPC 2.0 over stdio + WebSocket | Direct HTTP POST |
| **Setup** | Generate MCP config, restart AI tool | Add Unity package, done |
| **Reconnection** | Complex reconnect logic for domain reloads | Stateless per request |
| **Compatibility** | MCP-compatible clients only | Anything with a shell |
| **Custom tools** | Same `[Attribute]` + `HandleCommand` pattern | Same |

## Author

Created by **DevBookOfArray**

[![YouTube](https://img.shields.io/badge/YouTube-DevBookOfArray-red?logo=youtube&logoColor=white)](https://www.youtube.com/@DevBookOfArray)
[![GitHub](https://img.shields.io/badge/GitHub-youngwoocho02-181717?logo=github)](https://github.com/youngwoocho02)

## License

MIT
