package cmd

import (
	"fmt"
	"os"
	"strings"

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
		resp, err := send("manage_editor", map[string]interface{}{
			"action":              "play",
			"wait_for_completion": false,
		})
		if err != nil || !wait {
			return resp, err
		}
		if resp != nil && !resp.Success {
			return resp, nil
		}
		if err := waitForPlayMode(resolve); err != nil {
			return nil, err
		}
		if resp == nil {
			resp = &client.CommandResponse{Success: true}
		}
		resp.Message = "Entered play mode (confirmed)."
		return resp, nil

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

func waitForPlayMode(resolve instanceResolver) error {
	fmt.Fprintln(os.Stderr, "Waiting for play mode...")
	deadline := commandDeadline(flagTimeout)
	var lastErr error
	for {
		inst, err := resolve()
		if err != nil {
			lastErr = err
		} else if isPlayModeState(inst.State) {
			fmt.Fprintln(os.Stderr, "Entered play mode.")
			return nil
		} else {
			lastErr = fmt.Errorf("unity state is %s", inst.State)
		}
		if !sleepUntilNextPoll(deadline) {
			if lastErr != nil {
				return fmt.Errorf("timed out waiting for play mode: %v", lastErr)
			}
			return fmt.Errorf("timed out waiting for play mode")
		}
	}
}

func isPlayModeState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "playing", "paused":
		return true
	default:
		return false
	}
}
