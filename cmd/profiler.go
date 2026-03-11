package cmd

import (
	"fmt"
	"strconv"

	"github.com/fedtop/unity-cli/internal/client"
)

func profilerCmd(args []string, send sendFn) (*client.CommandResponse, error) {
	if len(args) == 0 {
		args = []string{"hierarchy"}
	}

	action := args[0]
	flags := parseSubFlags(args[1:])

	switch action {
	case "hierarchy":
		params := map[string]interface{}{}
		if v, ok := flags["depth"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				params["depth"] = n
			}
		}
		return send("mcp_profiler_hierarchy", params)

	default:
		return nil, fmt.Errorf("unknown profiler action: %s\nAvailable: hierarchy", action)
	}
}
