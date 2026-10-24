// Command hello-native is a minimal BYOP native plugin. It reads JSON-RPC
// requests from stdin, one per line, and writes one response per line.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func main() {
	started := time.Now()
	invocations := 0
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			fmt.Fprintln(os.Stderr, "invalid request:", err)
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			response["result"] = map[string]any{"name": "hello-native", "pid": os.Getpid()}
		case "activate", "deactivate":
			response["result"] = map[string]any{}
		case "invokeCommand":
			invocations++
			response["result"] = map[string]any{
				"message": fmt.Sprintf("Hello from native plugin. pid %d, call %d, up %s",
					os.Getpid(), invocations, time.Since(started).Round(time.Millisecond)),
			}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "method not found: " + req.Method}
		}
		if err := encoder.Encode(response); err != nil {
			os.Exit(1)
		}
	}
}
