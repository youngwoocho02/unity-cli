package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/fedtop/unity-cli/internal/client"
)

func toolCmd(args []string, send sendFn) (*client.CommandResponse, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: unity-cli tool <list|call|help>")
	}

	action := args[0]

	switch action {
	case "list":
		return send("list_tools", map[string]interface{}{})

	case "call":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: unity-cli tool call <name> [--params '{...}']")
		}
		name := args[1]
		flags := parseSubFlags(args[2:])

		params := map[string]interface{}{}
		if raw, ok := flags["params"]; ok {
			json.Unmarshal([]byte(raw), &params)
		}

		return send(name, params)

	case "help":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: unity-cli tool help <name>")
		}
		return send("tool_help", map[string]interface{}{"name": args[1]})

	default:
		return nil, fmt.Errorf("unknown tool action: %s\nAvailable: list, call, help", action)
	}
}
