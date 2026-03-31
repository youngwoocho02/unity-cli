# unity-cli — Unity Editor CLI Tool

> Control Unity Editor from the terminal. No server, no config, no MCP — just shell commands.
> Stateless HTTP — safe for concurrent access from multiple AI agents.

## Prerequisites

1. Install the CLI binary ([Releases](https://github.com/youngwoocho02/unity-cli/releases) or `go install`)
2. Add the Unity Connector package via Package Manager:
   ```
   https://github.com/youngwoocho02/unity-cli.git?path=unity-connector
   ```
3. **Recommended**: Edit → Preferences → General → Interaction Mode → **No Throttling**

**Security note**: Always install the CLI from official GitHub Releases or build from source. Do not use pre-built binaries from unverified sources.

## Quick Reference

```bash
unity-cli status                    # Connection check
unity-cli editor play|stop|pause    # Editor control
unity-cli editor refresh --compile  # Compile + wait
unity-cli exec "return Application.dataPath;"  # Execute C# code
unity-cli console --type error      # Console logs
unity-cli list                      # Show all available tools
```

## Built-in Commands (14 tools, ~45 actions)

### Editor Control
```bash
unity-cli editor play               # Enter play mode
unity-cli editor play --wait        # Enter + wait until loaded
unity-cli editor stop               # Stop play mode
unity-cli editor pause              # Toggle pause
unity-cli editor refresh            # Refresh assets
unity-cli editor refresh --compile  # Refresh + recompile (waits for completion)
```

### GameObject (13 actions)
```bash
# Find (supports name, tag, layer, component_type filters + pagination)
unity-cli gameobject --action find --name "Player"
unity-cli gameobject --action find --tag "Enemy" --page_size 20
unity-cli gameobject --action find --component_type Rigidbody

# Create
unity-cli gameobject --action create --name "Spawn" --position "1,2,3"
unity-cli gameobject --action create_primitive --primitive_type Cube --name "Wall"

# Transform & State
unity-cli gameobject --action set_transform --name "Wall" --position "5,0,0" --rotation "0,45,0" --scale "2,4,1"
unity-cli gameobject --action set_active --name "Wall" --active false

# Hierarchy
unity-cli gameobject --action get_hierarchy --max_depth 3
unity-cli gameobject --action set_parent --name "Child" --parent "Parent"

# Components
unity-cli gameobject --action get_components --name "Player"
unity-cli gameobject --action add_component --name "Player" --component_type Rigidbody
unity-cli gameobject --action remove_component --name "Player" --component_type Rigidbody

# Other
unity-cli gameobject --action rename --name "Old" --new_name "New"
unity-cli gameobject --action duplicate --name "Template"
unity-cli gameobject --action destroy --name "Temp"
unity-cli gameobject --action destroy --instance_id -1234  # Works on inactive GOs
```

### Scene (7 actions)
```bash
unity-cli scene --action list
unity-cli scene --action get_active
unity-cli scene --action open --path "Assets/Scenes/Level1.unity"
unity-cli scene --action open --path "Assets/Scenes/UI.unity" --additive true
unity-cli scene --action save                  # Save all
unity-cli scene --action save --path "Assets/Scenes/Level1.unity"
unity-cli scene --action new --name "Test" --template empty
unity-cli scene --action close --path "Assets/Scenes/UI.unity"
unity-cli scene --action set_active --path "Assets/Scenes/Level1.unity"
```

### Component (2 actions — SerializedProperty based)
```bash
# Read all properties
unity-cli component --action get_properties --name "Main Camera" --component_type Camera

# Set a property
unity-cli component --action set_property --name "Main Camera" --component_type Camera --property "field of view" --value 90
unity-cli component --action set_property --name "Obj" --component_type Transform --property m_LocalPosition --value "0,1,0"
```

### Asset (7 actions)
```bash
unity-cli asset --action search --filter "t:Material"
unity-cli asset --action search --filter "t:Prefab" --folder "Assets/Prefabs"
unity-cli asset --action get_info --path "Assets/Scenes/Main.unity"
unity-cli asset --action create_folder --path "Assets/Generated"
unity-cli asset --action move --path "Assets/Old.mat" --destination "Assets/New.mat"
unity-cli asset --action duplicate --path "Assets/Template.prefab"
unity-cli asset --action delete --path "Assets/Temp"
unity-cli asset --action import --path "Assets/Textures/New.png"
```

### Batch (sequential execution, single Undo group)
```bash
unity-cli batch --params '{"commands":[
  {"command":"gameobject","params":{"action":"create_primitive","primitive_type":"Cube","name":"A"}},
  {"command":"gameobject","params":{"action":"add_component","name":"A","component_type":"Rigidbody"}},
  {"command":"component","params":{"action":"set_property","name":"A","component_type":"Rigidbody","property":"m_Mass","value":"5"}}
]}'

# Stop on first error
unity-cli batch --params '{"commands":[...],"stop_on_error":true}'
```

### Execute C# Code (most powerful)
```bash
# Simple query
unity-cli exec "return Application.dataPath;"

# Pipe via stdin for complex code (avoids shell escaping)
echo 'return EditorSceneManager.GetActiveScene().name;' | unity-cli exec

# Multi-line
cat <<'EOF' | unity-cli exec
var go = GameObject.Find("Player");
var rb = go.GetComponent<Rigidbody2D>();
rb.mass = 5f;
return $"mass: {rb.mass}";
EOF

# SerializedProperty access (arrays, references)
cat <<'EOF' | unity-cli exec
var comp = GameObject.Find("Mover").GetComponent<MyComponent>();
var so = new UnityEditor.SerializedObject(comp);
var prop = so.FindProperty("targets");
prop.arraySize = 3;
for (int i = 0; i < 3; i++)
    prop.GetArrayElementAtIndex(i).objectReferenceValue = GameObject.Find($"T{i}").transform;
so.ApplyModifiedProperties();
return "done";
EOF

# Additional usings
unity-cli exec "return World.All.Count;" --usings Unity.Entities
```

### Other Commands
```bash
unity-cli test --mode EditMode                    # Run tests
unity-cli test --mode PlayMode --filter MyTest
unity-cli menu "File/Save Project"                # Execute menu item
unity-cli screenshot --view scene                 # Capture screenshot
unity-cli screenshot --view game --output_path ./shot.png
unity-cli profiler --action hierarchy --depth 3   # Profiler data
unity-cli profiler --action hierarchy --frames 30 --min 0.5 --sort self
unity-cli profiler enable|disable|status|clear
unity-cli reserialize Assets/Prefabs/Player.prefab  # Fix YAML after text edit
unity-cli console --lines 20 --type error,warning,log
unity-cli console --stacktrace short
unity-cli console --clear
```

## Target Selection

GameObjects can be targeted by (priority order):
1. `--instance_id 12345` — fastest, unambiguous, works on inactive GOs
2. `--path "Parent/Child/Target"` — hierarchy path
3. `--name "Target"` — searches by name (includes inactive GOs via fallback)

## Common Scenarios

### 1. Scene Setup from Scratch
```bash
unity-cli gameobject --action create_primitive --primitive_type Plane --name "Ground" --scale "10,1,10"
unity-cli gameobject --action create_primitive --primitive_type Cube --name "Player" --position "0,1,0"
unity-cli gameobject --action add_component --name "Player" --component_type Rigidbody
unity-cli scene --action save
```

### 2. Code Change → Compile → Test
```bash
unity-cli editor refresh --compile       # Wait for compilation
unity-cli console --type error           # Check errors
unity-cli test --mode EditMode           # Run tests
```

### 3. Asset Edit → Reserialize → Verify
```bash
unity-cli reserialize Assets/Prefabs/Player.prefab
unity-cli exec "return AssetDatabase.LoadAssetAtPath<GameObject>(\"Assets/Prefabs/Player.prefab\") != null;"
```

### 4. Performance Profiling
```bash
unity-cli profiler enable
unity-cli editor play --wait
# ... gameplay ...
unity-cli profiler --action hierarchy --depth 3 --min 1.0 --sort self
unity-cli editor stop
```

### 5. Batch Scene Construction
```bash
unity-cli batch --params '{"commands":[
  {"command":"gameobject","params":{"action":"create","name":"Enemies"}},
  {"command":"gameobject","params":{"action":"create_primitive","primitive_type":"Sphere","name":"E1","position":"3,0,0"}},
  {"command":"gameobject","params":{"action":"create_primitive","primitive_type":"Sphere","name":"E2","position":"-3,0,0"}},
  {"command":"gameobject","params":{"action":"set_parent","name":"E1","parent":"Enemies"}},
  {"command":"gameobject","params":{"action":"set_parent","name":"E2","parent":"Enemies"}}
]}'
```

## Error Handling

All commands return JSON:
```json
{"success": true, "message": "...", "data": {...}}
```

| Error | Cause | Solution |
|-------|-------|----------|
| `no Unity instances found` | Unity not running or Connector not installed | Open Unity with Connector package |
| `connection closed before response` | AssetDatabase change triggered domain reload | Command executed successfully; verify result |
| `Compile error` | C# code in `exec` has syntax error | Check error message for line/column |
| `Unknown command` | Tool name typo | Run `unity-cli list` to see available tools |
| Timeout | Long-running operation | Add `--timeout 300000` |

## Global Options

| Flag | Description | Default |
|------|-------------|---------|
| `--port <N>` | Override Unity instance port | auto-discover |
| `--project <path>` | Select by project path substring | CWD or latest |
| `--timeout <ms>` | HTTP request timeout | 120000 |

## Custom Tools

Add `[UnityCliTool]` to any static class in an Editor assembly — auto-discovered on every request:

```csharp
[UnityCliTool(Name = "spawn", Description = "Spawn an object")]
public static class SpawnTool
{
    public class Parameters
    {
        [ToolParameter("Position as x,y,z", Required = true)]
        public string Position { get; set; }
    }

    public static object HandleCommand(JObject @params)
    {
        var p = new ToolParams(@params);
        // ... create object ...
        return new SuccessResponse("Spawned", new { name = go.name });
    }
}
```

## Architecture

```
Terminal                          Unity Editor
$ unity-cli gameobject ...
    ├─ scan ~/.unity-cli/instances/
    ├─ POST http://127.0.0.1:8090/command
    │   {"command":"gameobject","params":{...}}
    │                                   │
    │                           HttpServer → CommandRouter
    │                           → [UnityCliTool] reflection
    │                           → HandleCommand() on main thread
    │                                   │
    └── receives JSON response ←────────┘
```

- Stateless per request — no session to manage
- Survives domain reloads (`[InitializeOnLoad]`)
- Heartbeat file updated every 0.5s (CLI waits if compiling/reloading)
- Multiple Unity instances supported (different ports: 8090, 8091, ...)
