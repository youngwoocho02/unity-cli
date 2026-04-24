package cmd

import (
	"fmt"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

// editorCmd controls Unity play mode and asset database.
// resolve is needed for waitForReady so compile polling can follow the current project instance.
func editorCmd(args []string, send sendFn, resolve instanceResolver) (*client.CommandResponse, error) {
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
		_, force := flags["force"]
		params := map[string]interface{}{}
		if force {
			params["force"] = true
			params["mode"] = "force"
		}
		if compile {
			params["compile"] = "request"
			resp, err := send("refresh_unity", params)
			if err != nil {
				return nil, err
			}
			if !resp.Success {
				return resp, nil
			}
			hasErrors := waitForReady(resolve)
			if hasErrors {
				return nil, fmt.Errorf("compilation finished with errors (check unity-cli console)")
			}
			resp.Message = "Refresh and compilation completed."
			return resp, nil
		}
		return send("refresh_unity", params)

	default:
		return nil, fmt.Errorf("unknown editor action: %s\nAvailable: play, stop, pause, refresh", action)
	}
}
