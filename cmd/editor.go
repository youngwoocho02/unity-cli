package cmd

import (
	"fmt"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func editorCmd(args []string, send sendFn, projectPath string) (*client.CommandResponse, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: unity-cli editor <play|stop|pause|refresh>")
	}

	action := args[0]
	flags := parseSubFlags(args[1:])

	switch action {
	case "play":
		_, wait := flags["wait"]
		return send("manage_editor", map[string]interface{}{
			"action":              "play",
			"wait_for_completion": wait,
		})

	case "stop":
		return send("manage_editor", map[string]interface{}{"action": "stop"})

	case "pause":
		return send("manage_editor", map[string]interface{}{"action": "pause"})

	case "refresh":
		_, compile := flags["compile"]
		if compile {
			resp, err := send("refresh_unity", map[string]interface{}{
				"compile": "request",
			})
			if err != nil {
				return nil, err
			}
			if err := waitForReady(projectPath, flagTimeout); err != nil {
				return nil, err
			}
			resp.Message = "Refresh and compilation completed."
			return resp, nil
		}
		return send("refresh_unity", map[string]interface{}{})

	default:
		return nil, fmt.Errorf("unknown editor action: %s\nAvailable: play, stop, pause, refresh", action)
	}
}
