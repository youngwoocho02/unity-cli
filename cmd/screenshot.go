package cmd

import (
	"strconv"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func screenshotCmd(args []string, send sendFn) (*client.CommandResponse, error) {
	flags := parseSubFlags(args)

	params := map[string]interface{}{
		"view": "scene",
	}

	if v, ok := flags["view"]; ok {
		params["view"] = v
	}
	if v, ok := flags["width"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			params["width"] = n
		}
	}
	if v, ok := flags["height"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			params["height"] = n
		}
	}
	if v, ok := flags["output"]; ok {
		params["outputPath"] = v
	}

	return send("editor_screenshot", params)
}
