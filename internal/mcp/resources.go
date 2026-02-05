package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/protocol"
)

// resourceDef describes a single MCP resource for the resources/list response.
type resourceDef struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

// resourceDefs returns the list of all gopm MCP resources.
func resourceDefs() []resourceDef {
	return []resourceDef{
		{
			URI:         "gopm://processes",
			Name:        "Process List",
			Description: "Current list of all managed processes as JSON",
			MimeType:    "application/json",
		},
		{
			URI:         "gopm://process/{name}",
			Name:        "Process Details",
			Description: "Full describe output for a specific process",
			MimeType:    "application/json",
		},
		{
			URI:         "gopm://logs/{name}/stdout",
			Name:        "Process Stdout Logs",
			Description: "Last 100 lines of stdout for a process",
			MimeType:    "text/plain",
		},
		{
			URI:         "gopm://logs/{name}/stderr",
			Name:        "Process Stderr Logs",
			Description: "Last 100 lines of stderr for a process",
			MimeType:    "text/plain",
		},
		{
			URI:         "gopm://status",
			Name:        "Daemon Status",
			Description: "Daemon PID, uptime, and version",
			MimeType:    "application/json",
		},
	}
}

// handleResourcesList returns the list of available resources.
func handleResourcesList(req *jsonRPCRequest) *jsonRPCResponse {
	result := map[string]interface{}{
		"resources": resourceDefs(),
	}
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleResourcesRead dispatches a resources/read request to the appropriate handler.
func handleResourcesRead(c *client.Client, req *jsonRPCRequest) *jsonRPCResponse {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    codeInvalidParams,
				Message: fmt.Sprintf("invalid resources/read params: %v", err),
			},
		}
	}

	content, mimeType, err := readResource(c, params.URI)
	if err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    codeInvalidParams,
				Message: err.Error(),
			},
		}
	}

	result := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"uri":      params.URI,
				"mimeType": mimeType,
				"text":     content,
			},
		},
	}
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// readResource resolves a resource URI and returns its content and MIME type.
func readResource(c *client.Client, uri string) (string, string, error) {
	// Strip the scheme.
	path := strings.TrimPrefix(uri, "gopm://")

	switch {
	case path == "processes":
		return resourceProcesses(c)
	case path == "status":
		return resourceStatus(c)
	case strings.HasPrefix(path, "process/"):
		name := strings.TrimPrefix(path, "process/")
		if name == "" {
			return "", "", fmt.Errorf("process name is required in URI")
		}
		return resourceProcess(c, name)
	case strings.HasPrefix(path, "logs/"):
		return resourceLogs(c, strings.TrimPrefix(path, "logs/"))
	default:
		return "", "", fmt.Errorf("unknown resource URI: %s", uri)
	}
}

// resourceProcesses returns the full process list as JSON.
func resourceProcesses(c *client.Client) (string, string, error) {
	resp, err := c.Send(protocol.MethodList, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to list processes: %v", err)
	}
	if !resp.Success {
		return "", "", fmt.Errorf("%s", resp.Error)
	}

	var procs []protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &procs); err != nil {
		return "", "", fmt.Errorf("failed to parse process list: %v", err)
	}
	pretty, _ := json.MarshalIndent(procs, "", "  ")
	return string(pretty), "application/json", nil
}

// resourceProcess returns detailed info for a single process.
func resourceProcess(c *client.Client, name string) (string, string, error) {
	resp, err := c.Send(protocol.MethodDescribe, protocol.TargetParams{Target: name})
	if err != nil {
		return "", "", fmt.Errorf("failed to describe process: %v", err)
	}
	if !resp.Success {
		return "", "", fmt.Errorf("%s", resp.Error)
	}

	var info protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return "", "", fmt.Errorf("failed to parse process info: %v", err)
	}
	pretty, _ := json.MarshalIndent(info, "", "  ")
	return string(pretty), "application/json", nil
}

// resourceLogs returns the last 100 lines of stdout or stderr for a process.
// The remaining path after "logs/" must be "{name}/stdout" or "{name}/stderr".
func resourceLogs(c *client.Client, remaining string) (string, string, error) {
	// remaining is e.g. "myapp/stdout" or "myapp/stderr"
	parts := strings.SplitN(remaining, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid logs URI: expected logs/{name}/stdout or logs/{name}/stderr")
	}
	name := parts[0]
	stream := parts[1]

	if name == "" {
		return "", "", fmt.Errorf("process name is required in logs URI")
	}

	var errOnly bool
	switch stream {
	case "stdout":
		errOnly = false
	case "stderr":
		errOnly = true
	default:
		return "", "", fmt.Errorf("invalid log stream %q: expected stdout or stderr", stream)
	}

	params := protocol.LogsParams{
		Target:  name,
		Lines:   100,
		ErrOnly: errOnly,
	}

	resp, err := c.Send(protocol.MethodLogs, params)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch logs: %v", err)
	}
	if !resp.Success {
		return "", "", fmt.Errorf("%s", resp.Error)
	}

	var result struct {
		Content string `json:"content"`
		LogPath string `json:"log_path"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return string(resp.Data), "text/plain", nil
	}
	return result.Content, "text/plain", nil
}

// resourceStatus returns the daemon status (PID, uptime, version) as JSON.
func resourceStatus(c *client.Client) (string, string, error) {
	resp, err := c.Send(protocol.MethodPing, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to ping daemon: %v", err)
	}
	if !resp.Success {
		return "", "", fmt.Errorf("%s", resp.Error)
	}

	var result protocol.PingResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", "", fmt.Errorf("failed to parse ping result: %v", err)
	}
	pretty, _ := json.MarshalIndent(result, "", "  ")
	return string(pretty), "application/json", nil
}
