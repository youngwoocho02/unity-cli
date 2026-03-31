# unity-cli — Unity Editor CLI Tool

> Control Unity Editor from the terminal. No server, no config, no MCP — just shell commands.

## Quick Reference

```bash
# Connection check
unity-cli status

# Editor control
unity-cli editor play|stop|pause|refresh
unity-cli editor refresh --compile    # compile + wait

# Execute C# code inside Unity (most powerful)
unity-cli exec "return Application.dataPath;"

# Console logs
unity-cli console --type error,warning
unity-cli console --clear
```

## Built-in Commands

### GameObject Management
```bash
# Find
unity-cli gameobject --action find --name "Player"
unity-cli gameobject --action find --tag "Enemy" --page_size 20
unity-cli gameobject --action find --component_type Rigidbody

# Create
unity-cli gameobject --action create --name "Spawn" --position "1,2,3"
unity-cli gameobject --action create_primitive --primitive_type Cube --name "Wall"

# Modify
unity-cli gameobject --action set_transform --name "Wall" --position "5,0,0" --scale "2,4,1"
unity-cli gameobject --action set_active --name "Wall" --active false
unity-cli gameobject --action rename --name "Wall" --new_name "BigWall"
unity-cli gameobject --action set_parent --name "BigWall" --parent "Environment"

# Hierarchy & Components
unity-cli gameobject --action get_hierarchy --max_depth 2
unity-cli gameobject --action get_components --name "Main Camera"
unity-cli gameobject --action add_component --name "Player" --component_type Rigidbody
unity-cli gameobject --action remove_component --name "Player" --component_type Rigidbody

# Duplicate & Destroy
unity-cli gameobject --action duplicate --name "Enemy"
unity-cli gameobject --action destroy --name "Temp"
```

### Scene Management
```bash
unity-cli scene --action list
unity-cli scene --action get_active
unity-cli scene --action open --path "Assets/Scenes/Level1.unity"
unity-cli scene --action open --path "Assets/Scenes/UI.unity" --additive true
unity-cli scene --action save
unity-cli scene --action save --path "Assets/Scenes/Level1.unity"
unity-cli scene --action new --name "TestScene" --template empty
unity-cli scene --action close --path "Assets/Scenes/UI.unity"
unity-cli scene --action set_active --path "Assets/Scenes/Level1.unity"
```

### Component Properties
```bash
# Read properties
unity-cli component --action get_properties --name "Main Camera" --component_type Camera

# Set a property (uses SerializedProperty paths)
unity-cli component --action set_property --name "Main Camera" --component_type Camera --property m_FieldOfView --value 90
unity-cli component --action set_property --name "Player" --component_type Transform --property m_LocalPosition --value "0,1,0"
```

### Asset Management
```bash
# Search
unity-cli asset --action search --filter "t:Material"
unity-cli asset --action search --filter "t:Prefab" --folder "Assets/Prefabs"

# Info
unity-cli asset --action get_info --path "Assets/Scenes/SampleScene.unity"

# CRUD
unity-cli asset --action create_folder --path "Assets/Generated"
unity-cli asset --action duplicate --path "Assets/Materials/Default.mat"
unity-cli asset --action move --path "Assets/Old.mat" --destination "Assets/Materials/Renamed.mat"
unity-cli asset --action delete --path "Assets/Temp"
unity-cli asset --action import --path "Assets/Textures/New.png"
```

### Batch Execution
```bash
# Multiple commands in one request
unity-cli batch --commands '[
  {"command":"gameobject","params":{"action":"create_primitive","primitive_type":"Cube","name":"A","position":"0,0,0"}},
  {"command":"gameobject","params":{"action":"create_primitive","primitive_type":"Sphere","name":"B","position":"3,0,0"}},
  {"command":"gameobject","params":{"action":"add_component","name":"A","component_type":"Rigidbody"}}
]'

# Stop on first error
unity-cli batch --commands '[...]' --stop_on_error true
```

### Other Commands
```bash
unity-cli test --mode EditMode                    # Run tests
unity-cli test --mode PlayMode --filter MyTest
unity-cli menu "File/Save Project"                # Execute menu item
unity-cli screenshot --view scene                 # Capture screenshot
unity-cli profiler --action hierarchy --depth 3   # Profiler data
unity-cli reserialize Assets/Prefabs/Player.prefab # Fix YAML
unity-cli list                                    # Show all available tools
```

## Common Patterns

### Scene Setup (create objects + configure)
```bash
unity-cli gameobject --action create_primitive --primitive_type Plane --name "Ground" --position "0,0,0" --scale "10,1,10"
unity-cli gameobject --action create_primitive --primitive_type Cube --name "Player" --position "0,1,0"
unity-cli gameobject --action add_component --name "Player" --component_type Rigidbody
unity-cli gameobject --action add_component --name "Player" --component_type BoxCollider
```

### Query with exec (when built-in commands aren't enough)
```bash
# Count all objects with a specific component
unity-cli exec "return FindObjectsByType<Rigidbody>(FindObjectsSortMode.None).Length;"

# Get all material names in the project
unity-cli exec "return string.Join(\"\n\", AssetDatabase.FindAssets(\"t:Material\").Select(g => AssetDatabase.GUIDToAssetPath(g)));"

# Modify PlayerSettings
unity-cli exec "PlayerSettings.companyName = \"MyCompany\";"
```

### Error Handling
- All commands return JSON: `{"success": true/false, "message": "...", "data": {...}}`
- Check `success` field for pass/fail
- `message` contains human-readable description
- `data` contains structured result

## Target Selection

GameObjects can be targeted by:
- `--instance_id 12345` — fastest, unambiguous
- `--path "Parent/Child/Target"` — hierarchy path
- `--name "Target"` — searches by name (first match)

## Notes
- Commands are stateless HTTP — safe for concurrent access from multiple AI agents
- Unity must be open with the Connector package installed
- Use `unity-cli status` to verify connection before sending commands
- All mutations register with Unity's Undo system (Ctrl+Z works)
