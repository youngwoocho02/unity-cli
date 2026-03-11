package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/fedtop/unity-cli/internal/client"
)

var (
	flagPort    int
	flagProject string
	flagTimeout int
	flagJSON    bool
)

func Execute() error {
	flag.IntVar(&flagPort, "port", 0, "Override Unity instance port")
	flag.StringVar(&flagProject, "project", "", "Select Unity instance by project path")
	flag.IntVar(&flagTimeout, "timeout", 120000, "Request timeout in milliseconds")
	flag.BoolVar(&flagJSON, "json", false, "Output raw JSON")

	flag.Usage = func() { printHelp() }

	// Find first non-flag arg position
	args := os.Args[1:]
	cmdArgs := extractCommandArgs(args)

	if len(cmdArgs) == 0 {
		printHelp()
		return nil
	}

	category := cmdArgs[0]
	subArgs := cmdArgs[1:]

	// Handle help/version before instance discovery
	switch category {
	case "help", "--help", "-h":
		printHelp()
		return nil
	case "version", "--version", "-v":
		fmt.Println("unity-cli v0.1.0")
		return nil
	}

	// Parse remaining flags
	flag.CommandLine.Parse(extractFlags(args))

	inst, err := client.DiscoverInstance(flagProject, flagPort)
	if err != nil {
		return err
	}

	send := func(command string, params interface{}) (*client.CommandResponse, error) {
		return client.Send(inst, command, params, flagTimeout)
	}

	var resp *client.CommandResponse

	switch category {
	case "editor":
		resp, err = editorCmd(subArgs, send)
	case "console":
		resp, err = consoleCmd(send)
	case "exec":
		resp, err = execCmd(subArgs, send)
	case "query":
		resp, err = queryCmd(subArgs, send)
	case "game":
		resp, err = gameCmd(subArgs, send)
	case "tool":
		resp, err = toolCmd(subArgs, send)
	case "diag":
		resp, err = diagCmd(subArgs, send)
	case "profiler":
		resp, err = profilerCmd(subArgs, send)
	default:
		// Try as direct custom tool call
		resp, err = send(category, map[string]interface{}{})
	}

	if err != nil {
		return err
	}

	printResponse(resp)

	if !resp.Success {
		os.Exit(1)
	}

	return nil
}

type sendFn func(command string, params interface{}) (*client.CommandResponse, error)

func printResponse(resp *client.CommandResponse) {
	if flagJSON {
		b, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(b))
		return
	}

	if resp.Data != nil && len(resp.Data) > 0 && string(resp.Data) != "null" {
		var pretty interface{}
		if json.Unmarshal(resp.Data, &pretty) == nil {
			b, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Println(string(resp.Data))
		}
	} else {
		fmt.Println(resp.Message)
	}
}

func extractCommandArgs(args []string) []string {
	var result []string
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if a == "--port" || a == "--project" || a == "--timeout" {
			skip = true
			continue
		}
		if a == "--json" {
			continue
		}
		if len(a) > 2 && a[:2] == "--" {
			continue
		}
		result = append(result, a)
	}
	return result
}

func extractFlags(args []string) []string {
	var result []string
	for i, a := range args {
		if a == "--port" || a == "--project" || a == "--timeout" {
			result = append(result, a)
			if i+1 < len(args) {
				result = append(result, args[i+1])
			}
		}
		if a == "--json" {
			result = append(result, a)
		}
	}
	return result
}

func printHelp() {
	fmt.Print(`unity-cli v0.1.0 — Control Unity Editor from the command line

Usage: unity-cli <command> [options]

Commands:
  editor play [--wait]                        Enter play mode
  editor stop                                 Exit play mode
  editor pause                                Toggle pause
  editor refresh [--compile]                  Refresh assets / request compile

  console [--lines N] [--filter <type>]       Read console logs

  exec "<code>" [--usings <list>]             Execute C# code in Unity

  query entities --world <w> --component <c>  Query ECS entities
  query inspect --world <w> --index <N>       Inspect entity details
  query singleton --world <w> --component <c> Query singleton component
  query component-values --world <w> --component <c> --fields <list>
  query systems --world <w> [--filter <name>]

  game connect                                Connect to local server
  game load --index <N>                       Load mini-game
  game phase <phase>                          Set game phase
  game overview                               Game state overview
  game spawn-bots [count]                     Spawn bots
  game despawn-bots                           Despawn all bots
  game thin-clients-connect [count]           Connect thin clients
  game thin-clients-disconnect                Disconnect thin clients

  tool list                                   List available custom tools
  tool call <name> [--params '{"key":"val"}'] Call a custom tool
  tool help <name>                            Show tool help

  diag hierarchy --world <w> --index <N>      Entity hierarchy
  diag diff --ghost-id <N>                    Server/client component diff
  diag players [--world <w>]                  Player status summary
  diag vehicle --world <w> --index <N>        Vehicle inspection

  profiler hierarchy [--depth N]              Profiler hierarchy data

Global Options:
  --port <N>          Override Unity instance port
  --project <path>    Select Unity instance by project path
  --json              Output raw JSON
  --timeout <ms>      Request timeout (default: 120000)
`)
}
