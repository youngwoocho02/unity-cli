package cmd

import (
	"fmt"
	"strconv"

	"github.com/fedtop/unity-cli/internal/client"
)

func queryCmd(args []string, send sendFn) (*client.CommandResponse, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: unity-cli query <entities|inspect|singleton|component-values|systems>")
	}

	action := args[0]
	flags := parseSubFlags(args[1:])
	world := flagOrDefault(flags, "world", "server")

	switch action {
	case "entities":
		params := map[string]interface{}{"world": world, "component": flags["component"]}
		if v, ok := flags["max"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				params["maxResults"] = n
			}
		}
		return send("mcp_query_entities", params)

	case "inspect":
		params := map[string]interface{}{"world": world}
		if v, ok := flags["index"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				params["entityIndex"] = n
			}
		}
		if v, ok := flags["version"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				params["entityVersion"] = n
			}
		}
		if v, ok := flags["components"]; ok {
			params["components"] = v
		}
		return send("mcp_inspect_entity", params)

	case "singleton":
		return send("mcp_query_singleton", map[string]interface{}{
			"world": world, "component": flags["component"],
		})

	case "component-values":
		params := map[string]interface{}{"world": world, "component": flags["component"]}
		if v, ok := flags["fields"]; ok {
			params["fields"] = v
		}
		if v, ok := flags["max"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				params["maxResults"] = n
			}
		}
		return send("mcp_query_component_values", params)

	case "systems":
		params := map[string]interface{}{"world": world}
		if v, ok := flags["filter"]; ok {
			params["filter"] = v
		}
		if _, ok := flags["show-disabled"]; ok {
			params["showDisabled"] = true
		}
		return send("mcp_list_systems", params)

	default:
		return nil, fmt.Errorf("unknown query action: %s\nAvailable: entities, inspect, singleton, component-values, systems", action)
	}
}

func flagOrDefault(flags map[string]string, key, def string) string {
	if v, ok := flags[key]; ok {
		return v
	}
	return def
}
