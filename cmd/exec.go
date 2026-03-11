package cmd

import (
	"fmt"
	"strings"

	"github.com/fedtop/unity-cli/internal/client"
)

func execCmd(args []string, send sendFn) (*client.CommandResponse, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: unity-cli exec \"<C# code>\"")
	}

	code := args[0]
	flags := parseSubFlags(args[1:])

	params := map[string]interface{}{"code": code}

	if usings, ok := flags["usings"]; ok {
		params["usings"] = strings.Split(usings, ",")
	}

	return send("execute_csharp", params)
}
