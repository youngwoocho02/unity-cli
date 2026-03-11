package cmd

import (
	"fmt"
	"strconv"

	"github.com/fedtop/unity-cli/internal/client"
)

func gameCmd(args []string, send sendFn) (*client.CommandResponse, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: unity-cli game <connect|load|phase|overview|spawn-bots|despawn-bots|thin-clients-connect|thin-clients-disconnect>")
	}

	action := args[0]
	flags := parseSubFlags(args[1:])

	switch action {
	case "connect":
		return send("mcp_connect_local", map[string]interface{}{})

	case "load":
		idx, _ := strconv.Atoi(flags["index"])
		return send("mcp_load_mini_game", map[string]interface{}{"index": idx})

	case "phase":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: unity-cli game phase <Starting|Playing|Ending>")
		}
		return send("mcp_set_game_phase", map[string]interface{}{"phase": args[1]})

	case "overview":
		return send("mcp_game_overview", map[string]interface{}{})

	case "spawn-bots":
		count := 1
		if len(args) > 1 {
			if n, err := strconv.Atoi(args[1]); err == nil {
				count = n
			}
		}
		return send("mcp_spawn_bots", map[string]interface{}{"count": count})

	case "despawn-bots":
		return send("mcp_despawn_bots", map[string]interface{}{})

	case "thin-clients-connect":
		count := 1
		if len(args) > 1 {
			if n, err := strconv.Atoi(args[1]); err == nil {
				count = n
			}
		}
		return send("mcp_connect_thin_clients", map[string]interface{}{"count": count})

	case "thin-clients-disconnect":
		return send("mcp_disconnect_thin_clients", map[string]interface{}{})

	default:
		return nil, fmt.Errorf("unknown game action: %s", action)
	}
}
