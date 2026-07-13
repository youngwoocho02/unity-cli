package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/youngwoocho02/unity-cli/internal/client"
)

type verifySummary struct {
	Project     string   `json:"project,omitempty"`
	Compile     bool     `json:"compile"`
	CompileOK   bool     `json:"compile_ok"`
	Play        bool     `json:"play"`
	EnteredPlay bool     `json:"entered_play"`
	Stopped     bool     `json:"stopped"`
	RuntimeOK   bool     `json:"runtime_ok"`
	ErrorCount  int      `json:"error_count"`
	Errors      []string `json:"errors,omitempty"`
	Messages    []string `json:"messages,omitempty"`
}

func verifyCmd(args []string, send sendFn, resolve instanceResolver) (*client.CommandResponse, error) {
	flags := parseSubFlags(args)

	summary := verifySummary{
		Compile:   !hasFlag(flags, "skip-compile"),
		CompileOK: true,
		Play:      !hasFlag(flags, "skip-play"),
		RuntimeOK: true,
	}

	lines := parsePositiveIntFlag(flags, "console-lines", 50)
	consoleType := flagValue(flags, "console-type", "error")
	stacktrace := flagValue(flags, "stacktrace", "none")
	keepPlaying := hasFlag(flags, "keep-playing")

	initial, err := resolve()
	if err != nil {
		return nil, err
	}
	summary.Project = initial.ProjectPath
	initialPlaying := isPlayState(initial.State)
	shouldStop := false

	if summary.Compile {
		resp, err := send("refresh_unity", map[string]interface{}{"compile": "request"})
		if err != nil {
			return nil, err
		}
		if !resp.Success {
			summary.CompileOK = false
			summary.Messages = append(summary.Messages, resp.Message)
			return verifyResponse(summary), nil
		}

		if waitForReady(resolve) {
			summary.CompileOK = false
			summary.Messages = append(summary.Messages, "Compilation finished with errors.")
			return verifyResponse(summary), nil
		}
	}

	if summary.Play {
		resp, err := send("manage_editor", map[string]interface{}{
			"action":              "play",
			"wait_for_completion": true,
		})
		if err != nil {
			return nil, err
		}
		if !resp.Success {
			summary.Messages = append(summary.Messages, resp.Message)
			return verifyResponse(summary), nil
		}
		summary.EnteredPlay = true
		summary.Messages = append(summary.Messages, resp.Message)
		shouldStop = !initialPlaying && !keepPlaying
	}

	consoleResp, err := send("console", map[string]interface{}{
		"type":       consoleType,
		"lines":      lines,
		"stacktrace": stacktrace,
	})
	if err != nil {
		return nil, err
	}
	if !consoleResp.Success {
		summary.RuntimeOK = false
		summary.Messages = append(summary.Messages, consoleResp.Message)
		stopVerifyPlayMode(send, shouldStop, &summary)
		return verifyResponse(summary), nil
	}

	entries, err := consoleEntries(consoleResp)
	if err != nil {
		return nil, err
	}
	summary.Errors = entries
	summary.ErrorCount = len(entries)
	summary.RuntimeOK = summary.ErrorCount == 0

	if summary.RuntimeOK && summary.CompileOK {
		summary.Messages = append(summary.Messages, "Verification passed.")
	}

	stopVerifyPlayMode(send, shouldStop, &summary)
	return verifyResponse(summary), nil
}

func stopVerifyPlayMode(send sendFn, shouldStop bool, summary *verifySummary) {
	if !shouldStop {
		return
	}
	stopResp, stopErr := send("manage_editor", map[string]interface{}{
		"action":              "stop",
		"wait_for_completion": true,
	})
	if stopErr != nil {
		summary.Messages = append(summary.Messages, "Failed to stop play mode: "+stopErr.Error())
		return
	}
	if stopResp == nil {
		summary.Messages = append(summary.Messages, "Failed to stop play mode: empty response")
		return
	}
	if !stopResp.Success {
		summary.Messages = append(summary.Messages, "Failed to stop play mode: "+stopResp.Message)
		return
	}
	summary.Stopped = true
}

func verifyResponse(summary verifySummary) *client.CommandResponse {
	success := summary.CompileOK && summary.RuntimeOK
	message := "Verify passed."
	if !success {
		message = "Verify failed."
	}
	data, _ := json.Marshal(summary)
	return &client.CommandResponse{
		Success: success,
		Message: message,
		Data:    data,
	}
}

func consoleEntries(resp *client.CommandResponse) ([]string, error) {
	if resp == nil || len(resp.Data) == 0 || string(resp.Data) == "null" {
		return nil, nil
	}
	var entries []string
	if err := json.Unmarshal(resp.Data, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse console entries: %w", err)
	}
	return entries, nil
}

func hasFlag(flags map[string]string, name string) bool {
	_, ok := flags[name]
	return ok
}

func flagValue(flags map[string]string, name, fallback string) string {
	value := strings.TrimSpace(flags[name])
	if value == "" {
		return fallback
	}
	return value
}

func parsePositiveIntFlag(flags map[string]string, name string, fallback int) int {
	value := strings.TrimSpace(flags[name])
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func isPlayState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "playing", "paused", "entering_playmode":
		return true
	default:
		return false
	}
}
