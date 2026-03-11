package cmd

import (
	"strconv"

	"github.com/fedtop/unity-cli/internal/client"
)

func consoleCmd(send sendFn) (*client.CommandResponse, error) {
	params := map[string]interface{}{}

	// Parse from os.Args directly for console-specific flags
	args := extractCommandArgs(nil)
	flags := parseSubFlags(args)

	if v, ok := flags["lines"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			params["maxLines"] = n
		}
	}
	if v, ok := flags["filter"]; ok {
		params["filter"] = v
	}

	return send("read_console", params)
}
