package cmd

import (
	"testing"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

func TestTestCmd_ForwardsDirtySceneOptions(t *testing.T) {
	var captured map[string]interface{}
	send := func(cmd string, params interface{}) (*client.CommandResponse, error) {
		if cmd != "run_tests" {
			t.Fatalf("send called with command %q, want run_tests", cmd)
		}
		var ok bool
		captured, ok = params.(map[string]interface{})
		if !ok {
			t.Fatalf("params type = %T, want map[string]interface{}", params)
		}
		return &client.CommandResponse{Success: true}, nil
	}

	resp, err := testCmd([]string{"--allow-dirty-scenes", "--auto-save-scenes"}, send, 0)
	if err != nil {
		t.Fatalf("testCmd returned error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("testCmd response = %#v, want success", resp)
	}
	if captured["allowDirtyScenes"] != true {
		t.Errorf("allowDirtyScenes = %v, want true", captured["allowDirtyScenes"])
	}
	if captured["autoSaveScenes"] != true {
		t.Errorf("autoSaveScenes = %v, want true", captured["autoSaveScenes"])
	}
}
