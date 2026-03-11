package cmd

import (
	"fmt"
	"strconv"

	"github.com/fedtop/unity-cli/internal/client"
)

func diagCmd(args []string, send sendFn) (*client.CommandResponse, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: unity-cli diag <hierarchy|diff|players|vehicle>")
	}

	action := args[0]
	flags := parseSubFlags(args[1:])
	world := flagOrDefault(flags, "world", "server")

	switch action {
	case "hierarchy":
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
		if v, ok := flags["depth"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				params["maxDepth"] = n
			}
		}
		return send("mcp_entity_hierarchy", params)

	case "diff":
		params := map[string]interface{}{}
		if v, ok := flags["ghost-id"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				params["ghostId"] = n
			}
		}
		if v, ok := flags["components"]; ok {
			params["components"] = v
		}
		return send("mcp_compare_server_client", params)

	case "players":
		return send("mcp_player_status", map[string]interface{}{"world": world})

	case "vehicle":
		params := map[string]interface{}{"world": world}
		if v, ok := flags["index"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				params["entityIndex"] = n
			}
		}
		return send("mcp_inspect_vehicle", params)

	default:
		return nil, fmt.Errorf("unknown diag action: %s\nAvailable: hierarchy, diff, players, vehicle", action)
	}
}
