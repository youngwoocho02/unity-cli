package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

// editorCmd는 play/stop/pause/refresh를 커넥터 도구로 보낸다.
// play --wait는 명령을 보낸 뒤 heartbeat가 playing/paused가 될 때까지 폴링한다.
// refresh --compile은 refresh_unity 후 waitForReady로 컴파일이 끝날 때까지 기다린다.
func editorCmd(args []string, send sendFn, resolve instanceResolver) (*client.CommandResponse, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: unity-cli editor <play|stop|pause|refresh>")
	}

	action := args[0]
	flags := parseSubFlags(args[1:])

	switch action {
	case "play":
		_, wait := flags["wait"]
		// play 전환은 커넥터가 바로 리턴한다. --wait면 이쪽에서 heartbeat를 본다.
		resp, err := send("manage_editor", map[string]interface{}{"action": "play"})
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
			// 리프레시만 요청하고, 컴파일 완료는 heartbeat state=ready를 폴링한다.
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

// waitForPlayMode는 인스턴스 파일의 state가 playing 또는 paused가 될 때까지 기다린다.
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

// isPlayModeState는 play mode에 들어왔는지 본다. paused도 play mode다.
func isPlayModeState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "playing", "paused":
		return true
	default:
		return false
	}
}
